// Package runner orchestrates one check run: load rules and inputs, run the checks and
// assemble a report. It performs no output; the caller writes the report.
package runner

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jegorscode/reservelint/internal/calendar"
	"github.com/jegorscode/reservelint/internal/config"
	"github.com/jegorscode/reservelint/internal/engine"
	"github.com/jegorscode/reservelint/internal/ingest"
	"github.com/jegorscode/reservelint/internal/meta"
	"github.com/jegorscode/reservelint/internal/model"
	"github.com/jegorscode/reservelint/internal/report"
	"github.com/jegorscode/reservelint/internal/rules"
	"github.com/shopspring/decimal"
)

// Options configures a run.
type Options struct {
	RulesPath       string
	ReservesPath    string
	SupplyPath      string
	RedemptionsPath string
	HolidaysPath    string     // overrides the rule's holiday_calendar parameter when set
	AsOf            *time.Time // overrides the latest supply snapshot time when set

	AllowInvalidRows bool
	// AllowDuplicateRows acknowledges identical reserve rows without a holding_id as distinct
	// positions. They are kept either way; this only suppresses DUPLICATE_ROW_WARNING.
	AllowDuplicateRows bool
	Now                func() time.Time // injectable clock; time.Now when nil
}

// InputError means the run could not be completed because of bad input, configuration or
// rules (exit code 2), as opposed to a successful run that found violations.
type InputError struct{ Err error }

func (e *InputError) Error() string { return e.Err.Error() }
func (e *InputError) Unwrap() error { return e.Err }

func inputErr(err error) error { return &InputError{Err: err} }

var knownChecks = map[string]bool{
	engine.NameCoverage: true, engine.NameLiquidity: true,
	engine.NameConcentration: true, engine.NameRedemption: true, engine.NameSurge: true,
}

const maxListedErrors = 5

// Run executes a check run and returns the finalized report.
func Run(opts Options) (*report.Report, error) {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	loaded, err := config.LoadRules(opts.RulesPath)
	if err != nil {
		return nil, inputErr(err)
	}
	var redemptionRule *rules.Rule
	for i, rule := range loaded.Set.Rules {
		name := rule.CheckName()
		if name != "" && !knownChecks[name] {
			return nil, inputErr(fmt.Errorf("rule %s: unknown check %q", rule.ID, name))
		}
		if name == engine.NameRedemption {
			redemptionRule = &loaded.Set.Rules[i]
		}
	}

	reserves, err := ingest.LoadReserves(opts.ReservesPath)
	if err != nil {
		return nil, inputErr(err)
	}
	supply, err := ingest.LoadSupply(opts.SupplyPath)
	if err != nil {
		return nil, inputErr(err)
	}
	redemptions, err := ingest.LoadRedemptions(opts.RedemptionsPath)
	if err != nil {
		return nil, inputErr(err)
	}
	inputs := []report.Input{inputOf("reserves", reserves), inputOf("supply", supply), inputOf("redemptions", redemptions)}
	invalid := 0
	for _, in := range inputs {
		invalid += len(in.InvalidRows)
	}
	if invalid > 0 && !opts.AllowInvalidRows {
		return nil, inputErr(invalidRowsError(inputs))
	}

	latest, err := ingest.LatestSupply(supply.Rows)
	if err != nil {
		return nil, inputErr(fmt.Errorf("%s: no valid supply rows", opts.SupplyPath))
	}
	asOf := latest.AsOf
	if opts.AsOf != nil {
		asOf = *opts.AsOf
	}

	rep := &report.Report{
		Tool: meta.Name, Version: meta.Version, Disclaimer: meta.Disclaimer,
		GeneratedAt: now().UTC(), AsOf: asOf.UTC(),
		Partial: invalid > 0,
		Options: report.Options{AllowInvalidRows: opts.AllowInvalidRows, AllowDuplicateRows: opts.AllowDuplicateRows},
		Inputs:  inputs,
		Rules:   report.FileInfo{Path: opts.RulesPath, SHA256: loaded.SHA256},
	}
	if opts.AsOf != nil {
		rep.Options.AsOfOverride = opts.AsOf.UTC().Format(time.RFC3339)
	}

	var cal *calendar.Calendar
	if redemptionRule != nil {
		if cal, err = loadCalendar(*redemptionRule, opts.HolidaysPath); err != nil {
			return nil, inputErr(err)
		}
		from, to := cal.Coverage()
		rep.Calendar = &report.CalendarInfo{
			FileInfo: report.FileInfo{Path: calendarPath(*redemptionRule, opts.HolidaysPath), SHA256: cal.SHA256},
			Name:     cal.Name, Kind: cal.Kind, Verified: cal.Verified, Timezone: cal.Location().String(),
			CoverageFrom: from.String(), CoverageTo: to.String(),
		}
	}

	for _, rule := range loaded.Set.Rules {
		var f engine.Finding
		var runErr error
		switch rule.CheckName() {
		case "":
			rep.NotChecked = append(rep.NotChecked, report.NotChecked{RuleID: rule.ID, Description: rule.Description, Reason: "no check implemented"})
			continue
		case engine.NameCoverage:
			f, runErr = engine.CheckCoverage(rule, reserves.Rows, latest)
		case engine.NameLiquidity:
			f, runErr = engine.CheckLiquidityFloor(rule, reserves.Rows)
		case engine.NameConcentration:
			f, runErr = engine.CheckCounterpartyConcentration(rule, reserves.Rows)
		case engine.NameSurge:
			f, runErr = engine.CheckRedemptionSurge(rule, redemptions.Rows, latest)
		case engine.NameRedemption:
			var res engine.RedemptionResult
			if res, runErr = engine.CheckRedemptionTiming(rule, cal, redemptions.Rows, asOf); runErr == nil {
				f = res.Finding
				rep.Redemption = redemptionSection(res)
			}
		}
		if runErr != nil {
			return nil, inputErr(fmt.Errorf("rule %s: %w", rule.ID, runErr))
		}
		rep.Findings = append(rep.Findings, findingView(f, rule))
	}
	if f, ok := duplicateWarning(reserves, opts); ok {
		rep.Findings = append(rep.Findings, f)
	}
	rep.Caveats = caveats(rep)
	rep.Finalize()
	return rep, nil
}

// DuplicateRowWarning is the rule id of the data-quality finding raised for identical reserve
// rows that have no holding_id.
const DuplicateRowWarning = "DUPLICATE_ROW_WARNING"

// duplicateWarning reports identical unkeyed reserve rows. They were kept as separate positions
// (so reserves may be overstated if they are really repeated lines), hence a warning, never a
// failure. With --allow-duplicate-rows the rows are still kept and listed but no warning is raised.
func duplicateWarning(reserves *ingest.Result[model.ReserveRow], opts Options) (report.Finding, bool) {
	n := reserves.AmbiguousCount()
	if n == 0 || opts.AllowDuplicateRows {
		return report.Finding{}, false
	}
	extra := decimal.Zero
	var groups []string
	for _, g := range reserves.Ambiguous {
		for _, idx := range g.RepeatRowIndexes {
			extra = extra.Add(reserves.Rows[idx].Amount)
		}
		lines := make([]string, len(g.RepeatPositions))
		for i, p := range g.RepeatPositions {
			lines[i] = fmt.Sprint(p)
		}
		groups = append(groups, fmt.Sprintf("line %d repeated at %s", g.FirstPosition, strings.Join(lines, ", ")))
	}
	return report.Finding{
		RuleID: DuplicateRowWarning, Check: "duplicate_rows",
		Description: "Identical reserve rows without a holding_id were kept as separate positions; they may be repeated lines.",
		Status:      "warning", Observed: decimal.NewFromInt(int64(n)), Comparator: "<=", Threshold: decimal.Zero,
		Citation: "Data-quality check, not a regulatory rule.", RuleVerified: true,
		Details: map[string]any{
			"file":                    reserves.Path,
			"repeated_rows_kept":      n,
			"groups":                  strings.Join(groups, "; "),
			"amount_in_repeated_rows": extra.String(),
			"effect":                  "These rows are counted in total reserves. If they are accidental repeats, reserves are overstated by amount_in_repeated_rows.",
			"how_to_resolve":          "Give each position a unique holding_id, or pass --allow-duplicate-rows to acknowledge them as distinct positions.",
		},
	}, true
}

func calendarPath(rule rules.Rule, override string) string {
	if override != "" {
		return override
	}
	p, _ := rule.String("holiday_calendar")
	return p
}

func loadCalendar(rule rules.Rule, override string) (*calendar.Calendar, error) {
	path := calendarPath(rule, override)
	if path == "" {
		return nil, fmt.Errorf("rule %s: no holiday_calendar parameter and no --holidays given", rule.ID)
	}
	tz, err := rule.String("timezone")
	if err != nil {
		return nil, err
	}
	return calendar.Load(path, tz)
}

func inputOf[T model.Keyed](role string, r *ingest.Result[T]) report.Input {
	in := report.Input{
		Role: role, Path: r.Path, SHA256: r.SHA256, RowsRead: r.RowsRead, RowsUsed: len(r.Rows),
		Duplicates: []report.Duplicate{}, IdenticalUnkeyed: []report.Duplicate{}, InvalidRows: []report.InvalidRow{},
	}
	for _, d := range r.Duplicates {
		in.Duplicates = append(in.Duplicates, report.Duplicate{RowKey: d.Key, FirstPosition: d.FirstPosition, RepeatPositions: d.RepeatPositions})
	}
	for _, d := range r.Ambiguous {
		in.IdenticalUnkeyed = append(in.IdenticalUnkeyed, report.Duplicate{RowKey: d.Key, FirstPosition: d.FirstPosition, RepeatPositions: d.RepeatPositions})
	}
	for _, e := range r.Errors {
		in.InvalidRows = append(in.InvalidRows, report.InvalidRow{Position: e.Position, Message: e.Message})
	}
	return in
}

func invalidRowsError(inputs []report.Input) error {
	var b strings.Builder
	b.WriteString("invalid input rows (re-run with --allow-invalid-rows to inspect a partial run):")
	for _, in := range inputs {
		if len(in.InvalidRows) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n  %s: %d invalid row(s)", in.Path, len(in.InvalidRows))
		for i, e := range in.InvalidRows {
			if i == maxListedErrors {
				fmt.Fprintf(&b, "\n    ... and %d more", len(in.InvalidRows)-maxListedErrors)
				break
			}
			fmt.Fprintf(&b, "\n    %d: %s", e.Position, e.Message)
		}
	}
	return errors.New(b.String())
}

func findingView(f engine.Finding, rule rules.Rule) report.Finding {
	details := f.Details
	if details == nil {
		details = map[string]any{}
	}
	status, uncapped := f.Status(), ""
	if status == "fail" && !f.RuleVerified {
		// Unverified rules inform but never break a pipeline until promoted to verified.
		status, uncapped = "warning", "fail"
	}
	return report.Finding{
		RuleID: f.RuleID, Check: f.Check, Description: f.Description, Status: status, UncappedStatus: uncapped,
		Observed: f.Observed, Comparator: string(f.Comparator), Threshold: f.Threshold,
		Citation: f.Citation, RuleVerified: f.RuleVerified, RuleNotes: strings.Join(strings.Fields(rule.Notes), " "),
		Details: details,
	}
}

func redemptionSection(res engine.RedemptionResult) *report.Redemption {
	s := res.Summary
	out := &report.Redemption{
		Summary: report.RedemptionSummary{
			Total: s.Total, Evaluated: s.Evaluated, OK: s.OK, Warning: s.Warning, Breach: s.Breach,
			Rejected: s.Rejected, Cancelled: s.Cancelled, Open: s.OpenCount, WithAnomalies: s.AnomalyCount,
			MaxBusinessDay: s.MaxElapsed, TotalAmount: s.TotalAmount, ExcludedAmount: s.ExcludedAmount,
			WarningAmount: s.WarningAmount, BreachAmount: s.BreachAmount,
		},
		Items: []report.RedemptionItem{},
	}
	for _, ev := range res.Evaluations {
		if ev.Outcome == engine.OutcomeExcluded || (ev.Outcome == engine.OutcomeOK && !ev.Open && len(ev.Anomalies) == 0) {
			continue
		}
		it := report.RedemptionItem{
			RequestTS: ev.Row.RequestTS.UTC(), OnboardedTS: utcPtr(ev.Row.OnboardedTS), TransferInitiatedTS: utcPtr(ev.Row.TransferInitiatedTS), CreditedTS: utcPtr(ev.Row.CreditedTS),
			Amount: ev.Row.Amount, Status: string(ev.Row.Status), Outcome: string(ev.Outcome),
			ClockStart: ev.ClockStart.UTC(), ClockEnd: ev.ClockEnd.UTC(),
			StartDate: ev.StartDate.String(), EndDate: ev.EndDate.String(),
			BusinessDaysElapsed: ev.Elapsed, Open: ev.Open, Anomalies: append([]string{}, ev.Anomalies...),
		}
		if ev.DueDate.Year != 0 {
			it.DueDate = ev.DueDate.String()
		}
		out.Items = append(out.Items, it)
	}
	return out
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func caveats(rep *report.Report) []string {
	out := []string{}
	if rep.Partial {
		n := 0
		for _, in := range rep.Inputs {
			n += len(in.InvalidRows)
		}
		out = append(out, fmt.Sprintf("PARTIAL RUN: %d invalid row(s) were excluded under --allow-invalid-rows; results cover valid rows only.", n))
	}
	if rep.Options.AllowDuplicateRows {
		for _, in := range rep.Inputs {
			if n := in.IdenticalUnkeyedCount(); n > 0 {
				out = append(out, fmt.Sprintf("%d identical row(s) without a holding_id in %s were kept as distinct positions (--allow-duplicate-rows).", n, in.Path))
			}
		}
	}
	for _, f := range rep.Findings {
		switch {
		case !f.RuleVerified && f.UncappedStatus == "fail":
			out = append(out, fmt.Sprintf("Rule %s (%s) is not verified against the rule text. The check FAILED, but because the rule is unverified it is reported as WARNING and does not set a failing exit code.", f.RuleID, f.Check))
		case !f.RuleVerified:
			out = append(out, fmt.Sprintf("Rule %s (%s) is not verified against the rule text; its threshold is a placeholder and its result is indicative only.", f.RuleID, f.Check))
		}
	}
	if rep.Calendar != nil && !rep.Calendar.Verified {
		out = append(out, fmt.Sprintf("Holiday calendar %q is not verified against an official schedule; business-day counts depend on it.", rep.Calendar.Name))
	}
	return out
}
