package report

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

const (
	maxListed       = 20 // rows listed per duplicate/invalid section in Markdown (JSON has all)
	maxRedemptionMD = 50
	redemptionCheck = "redemption_timing"
)

// cell makes text safe inside a Markdown table cell.
func cell(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	return strings.ReplaceAll(s, "\n", " ")
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func optTS(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return ts(*t)
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "NO"
}

// formatValue renders a finding value for people: percentages for ratios, days for timing.
func formatValue(check string, d decimal.Decimal) string {
	if check == redemptionCheck {
		return d.String() + " business days"
	}
	return d.Mul(decimal.NewFromInt(100)).Round(4).String() + "%"
}

func statusLabel(f Finding) string {
	if f.UncappedStatus != "" {
		return strings.ToUpper(f.Status) + " (capped from " + strings.ToUpper(f.UncappedStatus) + ")"
	}
	return strings.ToUpper(f.Status)
}

func comparatorWords(c string) string {
	if c == ">=" {
		return "at least"
	}
	return "at most"
}

func positions(p []int) string {
	parts := make([]string, len(p))
	for i, v := range p {
		parts[i] = fmt.Sprint(v)
	}
	return strings.Join(parts, ", ")
}

// Markdown renders the human-readable report.
func Markdown(r *Report) []byte {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	w("# %s report\n\n> %s\n\n", r.Tool, r.Disclaimer)
	w("**Overall: %s** (%d fail, %d warning, %d pass)\n\n", r.Status, r.Counts.Fail, r.Counts.Warning, r.Counts.Pass)
	if r.Partial {
		w("> **PARTIAL RUN.** Invalid rows were excluded (`--allow-invalid-rows`). Results cover valid rows only and must not be treated as complete.\n\n")
	}
	w("- Generated: %s by %s %s\n", ts(r.GeneratedAt), r.Tool, r.Version)
	w("- Snapshot as of: %s\n", ts(r.AsOf))
	w("- Exit code: %d\n\n", r.ExitCode)

	w("## Findings\n\n| Rule | Check | Status | Observed | Requirement | Rule verified |\n|---|---|---|---|---|---|\n")
	for _, f := range r.Findings {
		w("| %s | %s | %s | %s | %s %s | %s |\n", cell(f.RuleID), cell(f.Check), statusLabel(f),
			cell(formatValue(f.Check, f.Observed)), comparatorWords(f.Comparator), cell(formatValue(f.Check, f.Threshold)), yesNo(f.RuleVerified))
	}
	b.WriteString("\n")
	for _, f := range r.Findings {
		w("### %s: %s - %s\n\n%s\n\n", f.RuleID, f.Check, statusLabel(f), f.Description)
		if f.UncappedStatus != "" {
			w("> The check result was **%s**, but this rule is unverified, so it is capped at WARNING and does not fail the run.\n\n", strings.ToUpper(f.UncappedStatus))
		}
		w("- Observed: %s; required %s %s\n", formatValue(f.Check, f.Observed), comparatorWords(f.Comparator), formatValue(f.Check, f.Threshold))
		w("- Citation: %s\n", f.Citation)
		w("- Rule verified: %s\n", yesNo(f.RuleVerified))
		if f.RuleNotes != "" {
			w("- Rule notes: %s\n", strings.Join(strings.Fields(f.RuleNotes), " "))
		}
		keys := make([]string, 0, len(f.Details))
		for k := range f.Details {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if m, ok := f.Details[k].(map[string]string); ok {
				w("- %s:\n", k)
				names := make([]string, 0, len(m))
				for n := range m {
					names = append(names, n)
				}
				sort.Strings(names)
				for _, n := range names {
					w("  - %s: %s\n", n, m[n])
				}
				continue
			}
			w("- %s: %v\n", k, f.Details[k])
		}
		b.WriteString("\n")
	}

	if r.Redemption != nil {
		writeRedemption(&b, r.Redemption)
	}

	w("## Input integrity\n\n| File | Role | SHA-256 | Rows read | Rows used | Duplicates ignored | Invalid rows |\n|---|---|---|---|---|---|---|\n")
	for _, in := range r.Inputs {
		w("| %s | %s | `%s` | %d | %d | %d | %d |\n", cell(in.Path), in.Role, in.SHA256, in.RowsRead, in.RowsUsed, in.DuplicateCount(), len(in.InvalidRows))
	}
	w("| %s | rules | `%s` | | | | |\n", cell(r.Rules.Path), r.Rules.SHA256)
	if r.Calendar != nil {
		w("| %s | holiday calendar | `%s` | | | | |\n", cell(r.Calendar.Path), r.Calendar.SHA256)
	}
	b.WriteString("\n")
	for _, in := range r.Inputs {
		if len(in.Duplicates) > 0 {
			w("Duplicate rows in `%s` (counted once; positions are line numbers or item indexes):\n\n", in.Path)
			for i, d := range in.Duplicates {
				if i == maxListed {
					w("- ... and %d more (see report.json)\n", len(in.Duplicates)-maxListed)
					break
				}
				w("- first at %d, repeated at %s\n", d.FirstPosition, positions(d.RepeatPositions))
			}
			b.WriteString("\n")
		}
		if len(in.InvalidRows) > 0 {
			w("Invalid rows in `%s` (excluded):\n\n", in.Path)
			for i, e := range in.InvalidRows {
				if i == maxListed {
					w("- ... and %d more (see report.json)\n", len(in.InvalidRows)-maxListed)
					break
				}
				w("- %d: %s\n", e.Position, e.Message)
			}
			b.WriteString("\n")
		}
	}

	if len(r.NotChecked) > 0 {
		w("## Rules tracked but not checked\n\n")
		for _, n := range r.NotChecked {
			w("- %s: %s (%s)\n", n.RuleID, n.Description, n.Reason)
		}
		b.WriteString("\n")
	}
	if len(r.Caveats) > 0 {
		w("## Caveats\n\n")
		for _, c := range r.Caveats {
			w("- %s\n", c)
		}
		b.WriteString("\n")
	}
	w("---\n%s\n", r.Disclaimer)
	return []byte(b.String())
}

func writeRedemption(b *strings.Builder, red *Redemption) {
	s := red.Summary
	fmt.Fprintf(b, "## Redemption timing\n\n")
	fmt.Fprintf(b, "| Requests | Evaluated | OK | Warning | Breach | Rejected | Cancelled | Still open | Max business days used |\n|---|---|---|---|---|---|---|---|---|\n")
	fmt.Fprintf(b, "| %d | %d | %d | %d | %d | %d | %d | %d | %d |\n\n", s.Total, s.Evaluated, s.OK, s.Warning, s.Breach, s.Rejected, s.Cancelled, s.Open, s.MaxBusinessDay)
	fmt.Fprintf(b, "Amounts: total %s; breach %s; warning %s; excluded (rejected or cancelled) %s.\n\n",
		s.TotalAmount, s.BreachAmount, s.WarningAmount, s.ExcludedAmount)
	if len(red.Items) == 0 {
		fmt.Fprintf(b, "No warnings, breaches, open requests or anomalies.\n\n")
		return
	}
	fmt.Fprintf(b, "Requests needing attention (clock start is the later of request and onboarding; dates are New York calendar dates):\n\n")
	fmt.Fprintf(b, "| Outcome | Requested | Clock start | Clock end | Days used | Due | Amount | Status | Notes |\n|---|---|---|---|---|---|---|---|---|\n")
	for i, it := range red.Items {
		if i == maxRedemptionMD {
			fmt.Fprintf(b, "\n... and %d more (see report.json).\n", len(red.Items)-maxRedemptionMD)
			break
		}
		end := ts(it.ClockEnd)
		notes := strings.Join(it.Anomalies, "; ")
		if it.Open {
			end += " (open, to as-of)"
		}
		due := it.DueDate
		if due == "" {
			due = "-"
		}
		fmt.Fprintf(b, "| %s | %s | %s | %s | %d | %s | %s | %s | %s |\n", strings.ToUpper(it.Outcome), ts(it.RequestTS), ts(it.ClockStart),
			end, it.BusinessDaysElapsed, due, it.Amount, it.Status, cell(notes))
	}
	b.WriteString("\n")
}
