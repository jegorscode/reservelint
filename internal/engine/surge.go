package engine

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jegorscode/reservelint/internal/model"
	"github.com/jegorscode/reservelint/internal/rules"
	"github.com/shopspring/decimal"
)

// CheckRedemptionSurge finds the busiest rolling window of redemption requests and reports
// whether it exceeds the share of outstanding issuance that obliges the issuer to notify the
// OCC (12 CFR 15.12(d)(1): requests in excess of 10 percent of outstanding issuance value in a
// single 24-hour period, notice within 24 hours).
//
// Exceeding the share is not a violation; it triggers a duty to notify, which the tool cannot
// see being performed. So the finding always passes and carries a warning when notice is due.
//
// Every request received counts, whatever its later status. A window is [t, t+window) for the
// time t of a request; the strict "in excess of" comparison is made exactly.
func CheckRedemptionSurge(rule rules.Rule, rows []model.RedemptionRow, supply model.SupplyRow) (Finding, error) {
	threshold, err := rule.Decimal("max_share_of_issuance")
	if err != nil {
		return Finding{}, err
	}
	hours, err := rule.Int("window_hours")
	if err != nil {
		return Finding{}, err
	}
	if hours <= 0 {
		return Finding{}, fmt.Errorf("rule %s: window_hours must be positive", rule.ID)
	}
	if len(rows) == 0 {
		return Finding{}, errors.New("no redemption rows")
	}
	window := time.Duration(hours) * time.Hour

	sorted := append([]model.RedemptionRow(nil), rows...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].RequestTS.Before(sorted[j].RequestTS) })

	var (
		sum, best    decimal.Decimal
		bestStart    time.Time
		bestEnd      time.Time
		bestRequests int
		j            int
	)
	for i := range sorted {
		start := sorted[i].RequestTS
		for j < len(sorted) && sorted[j].RequestTS.Before(start.Add(window)) {
			sum = sum.Add(sorted[j].Amount)
			j++
		}
		if sum.GreaterThan(best) {
			best, bestStart, bestEnd, bestRequests = sum, start, sorted[j-1].RequestTS, j-i
		}
		sum = sum.Sub(sorted[i].Amount)
	}

	limit := threshold.Mul(supply.CirculatingSupply)
	exceeded := best.GreaterThan(limit)
	f := newFinding(rule, best, supply.CirculatingSupply, threshold, AtMost, map[string]any{
		"outstanding_issuance":    supply.CirculatingSupply.String(),
		"window_hours":            hours,
		"largest_window_amount":   best.String(),
		"largest_window_requests": bestRequests,
		"largest_window_first":    bestStart.UTC().Format(time.RFC3339),
		"largest_window_last":     bestEnd.UTC().Format(time.RFC3339),
		"notice_to_occ_required":  exceeded,
		"notice_deadline_hours":   24,
		"requests_considered":     len(rows),
	})
	// A duty to notify, not a failure: pass, but surface it as a warning.
	f.Passed, f.Warning = true, exceeded
	return f, nil
}
