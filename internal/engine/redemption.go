package engine

import (
	"errors"
	"fmt"
	"time"

	"github.com/jegorscode/reservelint/internal/calendar"
	"github.com/jegorscode/reservelint/internal/model"
	"github.com/jegorscode/reservelint/internal/rules"
	"github.com/shopspring/decimal"
)

// NameRedemption is the check name of the redemption timing rule.
const NameRedemption = "redemption_timing"

// Outcome is the timing result of one redemption.
type Outcome string

// Outcomes. A near-breach is reported as OutcomeWarning.
const (
	OutcomeOK       Outcome = "ok"
	OutcomeWarning  Outcome = "warning"
	OutcomeBreach   Outcome = "breach"
	OutcomeExcluded Outcome = "excluded" // rejected or cancelled
)

// RedemptionEval is the timing assessment of one redemption row.
//
// The clock starts at max(request_ts, onboarded_ts) and ends at transfer_initiated_ts, or at
// the snapshot as_of while no transfer has been initiated (Open).
type RedemptionEval struct {
	Row        model.RedemptionRow
	Outcome    Outcome
	ClockStart time.Time
	ClockEnd   time.Time
	StartDate  calendar.Date
	EndDate    calendar.Date
	DueDate    calendar.Date
	Elapsed    int  // business days after StartDate through EndDate
	Open       bool // no transfer initiated; measured to the snapshot as_of
	Anomalies  []string
}

// RedemptionSummary holds counts and amounts for the report, including excluded requests.
type RedemptionSummary struct {
	Total, Evaluated            int
	OK, Warning, Breach         int
	Rejected, Cancelled         int
	TotalAmount, ExcludedAmount decimal.Decimal
	BreachAmount, WarningAmount decimal.Decimal
	OpenCount, AnomalyCount     int
	MaxElapsed                  int
}

// RedemptionResult bundles the finding with the per-row detail behind it.
type RedemptionResult struct {
	Finding     Finding
	Evaluations []RedemptionEval
	Summary     RedemptionSummary
}

// CheckRedemptionTiming evaluates every redemption against the rule. asOf is the snapshot time
// used for requests with no transfer yet. The finding passes when there is no breach;
// warnings do not fail it but set Finding.Warning.
func CheckRedemptionTiming(rule rules.Rule, cal *calendar.Calendar, rows []model.RedemptionRow, asOf time.Time) (RedemptionResult, error) {
	maxDays, err := rule.Int("max_business_days")
	if err != nil {
		return RedemptionResult{}, err
	}
	margin, err := rule.Int("near_breach_margin_business_days")
	if err != nil {
		return RedemptionResult{}, err
	}
	if maxDays < 0 || margin < 0 {
		return RedemptionResult{}, fmt.Errorf("rule %s: max_business_days and near_breach_margin_business_days must be >= 0", rule.ID)
	}
	tz, err := rule.String("timezone")
	if err != nil {
		return RedemptionResult{}, err
	}
	if cal.Location().String() != tz {
		return RedemptionResult{}, fmt.Errorf("rule %s: calendar time zone %s does not match rule time zone %s", rule.ID, cal.Location(), tz)
	}
	if len(rows) == 0 {
		return RedemptionResult{}, fmt.Errorf("no redemption rows")
	}

	res := RedemptionResult{Evaluations: make([]RedemptionEval, 0, len(rows))}
	s := &res.Summary
	for _, row := range rows {
		s.Total++
		s.TotalAmount = s.TotalAmount.Add(row.Amount)
		if row.Status == model.StatusRejected || row.Status == model.StatusCancelled {
			if row.Status == model.StatusRejected {
				s.Rejected++
			} else {
				s.Cancelled++
			}
			s.ExcludedAmount = s.ExcludedAmount.Add(row.Amount)
			res.Evaluations = append(res.Evaluations, RedemptionEval{Row: row, Outcome: OutcomeExcluded})
			continue
		}
		ev, err := evaluate(cal, row, asOf, maxDays, margin)
		if err != nil {
			return RedemptionResult{}, err
		}
		res.Evaluations = append(res.Evaluations, ev)
		s.Evaluated++
		if ev.Open {
			s.OpenCount++
		}
		if len(ev.Anomalies) > 0 {
			s.AnomalyCount++
		}
		if ev.Elapsed > s.MaxElapsed {
			s.MaxElapsed = ev.Elapsed
		}
		switch ev.Outcome {
		case OutcomeBreach:
			s.Breach++
			s.BreachAmount = s.BreachAmount.Add(row.Amount)
		case OutcomeWarning:
			s.Warning++
			s.WarningAmount = s.WarningAmount.Add(row.Amount)
		default:
			s.OK++
		}
	}
	if s.Evaluated == 0 {
		return RedemptionResult{}, fmt.Errorf("no redemptions to evaluate (all %d are rejected or cancelled)", s.Total)
	}

	res.Finding = Finding{
		RuleID:       rule.ID,
		Check:        rule.CheckName(),
		Description:  rule.Description,
		Observed:     decimal.NewFromInt(int64(s.MaxElapsed)),
		Threshold:    decimal.NewFromInt(int64(maxDays)),
		Comparator:   AtMost,
		Passed:       s.Breach == 0,
		Warning:      s.Breach == 0 && s.Warning > 0,
		Citation:     rule.Source,
		RuleVerified: rule.IsVerified(),
		Details: map[string]any{
			"as_of":                  asOf.UTC().Format(time.RFC3339),
			"timezone":               tz,
			"calendar":               cal.Name,
			"calendar_kind":          cal.Kind,
			"calendar_verified":      cal.Verified,
			"near_breach_margin":     margin,
			"requests_total":         s.Total,
			"requests_evaluated":     s.Evaluated,
			"ok":                     s.OK,
			"warnings":               s.Warning,
			"breaches":               s.Breach,
			"rejected":               s.Rejected,
			"cancelled":              s.Cancelled,
			"open_requests":          s.OpenCount,
			"rows_with_anomalies":    s.AnomalyCount,
			"total_amount":           s.TotalAmount.String(),
			"excluded_amount":        s.ExcludedAmount.String(),
			"breach_amount":          s.BreachAmount.String(),
			"warning_amount":         s.WarningAmount.String(),
			"max_business_days_used": s.MaxElapsed,
		},
	}
	return res, nil
}

func evaluate(cal *calendar.Calendar, row model.RedemptionRow, asOf time.Time, maxDays, margin int) (RedemptionEval, error) {
	ev := RedemptionEval{Row: row, ClockStart: row.RequestTS}
	if row.OnboardedTS != nil && row.OnboardedTS.After(ev.ClockStart) {
		ev.ClockStart = *row.OnboardedTS
	}
	// Redemption occurs when the outgoing transfer is initiated or, at the customer's request,
	// when value is credited to their account with the issuer (12 CFR 15.12(a)(2)). Use the
	// earlier of the two if both are recorded.
	end := row.TransferInitiatedTS
	if row.CreditedTS != nil && (end == nil || row.CreditedTS.Before(*end)) {
		end = row.CreditedTS
	}
	switch {
	case end != nil:
		ev.ClockEnd = *end
	default:
		ev.ClockEnd, ev.Open = asOf, true
		if row.Status == model.StatusCompleted {
			ev.Anomalies = append(ev.Anomalies, "status is completed but neither transfer_initiated_ts nor credited_ts is set; measured to as_of")
		}
	}
	if ev.ClockEnd.Before(ev.ClockStart) {
		if ev.Open {
			ev.Anomalies = append(ev.Anomalies, "clock starts after the snapshot as_of; counted as 0 days")
		} else {
			ev.Anomalies = append(ev.Anomalies, "the completing event (transfer_initiated_ts or credited_ts) precedes the clock start; counted as 0 days")
		}
		ev.ClockEnd = ev.ClockStart
	}
	ev.StartDate, ev.EndDate = cal.DateOf(ev.ClockStart), cal.DateOf(ev.ClockEnd)
	var err error
	if ev.Elapsed, err = cal.BusinessDaysBetween(ev.StartDate, ev.EndDate); err != nil {
		return ev, fmt.Errorf("redemption requested %s: %w", row.RequestTS.Format(time.RFC3339), err)
	}
	switch ev.DueDate, err = cal.AddBusinessDays(ev.StartDate, maxDays); {
	case errors.Is(err, calendar.ErrOutOfCoverage):
		// The elapsed count is still exact; only the informational due date is unavailable.
		ev.Anomalies = append(ev.Anomalies, "due date falls beyond the holiday calendar coverage")
	case err != nil:
		return ev, fmt.Errorf("redemption requested %s: %w", row.RequestTS.Format(time.RFC3339), err)
	}
	switch {
	case ev.Elapsed > maxDays:
		ev.Outcome = OutcomeBreach
	case maxDays-ev.Elapsed < margin:
		ev.Outcome = OutcomeWarning
	default:
		ev.Outcome = OutcomeOK
	}
	return ev, nil
}
