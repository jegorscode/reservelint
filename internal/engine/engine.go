// Package engine runs the checks. Each check is a pure function of a rule and de-duplicated
// rows; thresholds come only from the rule's parameters.
//
// Pass/fail is decided by exact cross-multiplication (numerator vs threshold * denominator),
// not by comparing a rounded quotient, so a value one unit past the limit can never round
// onto it. The reported Observed ratio is rounded to 16 decimal places for display only.
//
//	coverage, liquidity: pass when observed >= threshold (a minimum)
//	concentration:       pass when observed <= threshold ("no more than")
package engine

import (
	"errors"
	"fmt"
	"sort"

	"github.com/jegorscode/reservelint/internal/model"
	"github.com/jegorscode/reservelint/internal/rules"
	"github.com/shopspring/decimal"
)

const displayPrecision = 16

// Comparator is the condition that must hold for a finding to pass.
type Comparator string

// Comparators.
const (
	AtLeast Comparator = ">="
	AtMost  Comparator = "<="
)

// Finding is the result of one check against one rule.
type Finding struct {
	RuleID       string
	Check        string
	Description  string
	Observed     decimal.Decimal
	Threshold    decimal.Decimal
	Comparator   Comparator
	Passed       bool
	Warning      bool // passed, but with near-breach warnings
	Citation     string
	RuleVerified bool
	Details      map[string]any
}

// Status is "fail", "warning" (passed with near-breaches) or "pass".
func (f Finding) Status() string {
	switch {
	case !f.Passed:
		return "fail"
	case f.Warning:
		return "warning"
	}
	return "pass"
}

// Check names, as used in the `check` field of rules.yaml.
const (
	NameCoverage      = "coverage"
	NameLiquidity     = "liquidity_floor"
	NameConcentration = "counterparty_concentration"
)

func totalOf(reserves []model.ReserveRow) (decimal.Decimal, error) {
	if len(reserves) == 0 {
		return decimal.Zero, errors.New("no reserve rows")
	}
	total := decimal.Zero
	for _, r := range reserves {
		total = total.Add(r.Amount)
	}
	return total, nil
}

// newFinding compares numerator/denominator to threshold exactly.
func newFinding(rule rules.Rule, num, den, threshold decimal.Decimal, cmp Comparator, details map[string]any) Finding {
	limit := threshold.Mul(den)
	passed := num.GreaterThanOrEqual(limit)
	if cmp == AtMost {
		passed = num.LessThanOrEqual(limit)
	}
	return Finding{
		RuleID:       rule.ID,
		Check:        rule.CheckName(),
		Description:  rule.Description,
		Observed:     num.DivRound(den, displayPrecision),
		Threshold:    threshold,
		Comparator:   cmp,
		Passed:       passed,
		Citation:     rule.Source,
		RuleVerified: rule.IsVerified(),
		Details:      details,
	}
}

// CheckCoverage: total reserves / circulating supply must be at least the minimum ratio.
func CheckCoverage(rule rules.Rule, reserves []model.ReserveRow, supply model.SupplyRow) (Finding, error) {
	total, err := totalOf(reserves)
	if err != nil {
		return Finding{}, err
	}
	threshold, err := rule.Decimal("min_coverage_ratio")
	if err != nil {
		return Finding{}, err
	}
	shortfall := decimal.Max(supply.CirculatingSupply.Sub(total), decimal.Zero)
	return newFinding(rule, total, supply.CirculatingSupply, threshold, AtLeast, map[string]any{
		"total_reserves":     total.String(),
		"circulating_supply": supply.CirculatingSupply.String(),
		"supply_as_of":       supply.AsOf.UTC().Format("2006-01-02T15:04:05Z07:00"),
		"shortfall":          shortfall.String(),
	}), nil
}

// CheckLiquidityFloor: the share of reserves flagged immediately available must be at least
// the minimum.
func CheckLiquidityFloor(rule rules.Rule, reserves []model.ReserveRow) (Finding, error) {
	total, err := totalOf(reserves)
	if err != nil {
		return Finding{}, err
	}
	threshold, err := rule.Decimal("min_immediately_available_share")
	if err != nil {
		return Finding{}, err
	}
	available := decimal.Zero
	for _, r := range reserves {
		if r.IsImmediatelyAvailable {
			available = available.Add(r.Amount)
		}
	}
	return newFinding(rule, available, total, threshold, AtLeast, map[string]any{
		"total_reserves":        total.String(),
		"immediately_available": available.String(),
	}), nil
}

// CheckCounterpartyConcentration: the largest counterparty's share of reserves must be at most
// the maximum. Name variants that differ only in case or whitespace are one counterparty.
func CheckCounterpartyConcentration(rule rules.Rule, reserves []model.ReserveRow) (Finding, error) {
	total, err := totalOf(reserves)
	if err != nil {
		return Finding{}, err
	}
	threshold, err := rule.Decimal("max_counterparty_share")
	if err != nil {
		return Finding{}, err
	}
	amounts := map[string]decimal.Decimal{}
	display := map[string]string{}
	for _, r := range reserves {
		key := model.NormText(r.Counterparty)
		if _, ok := display[key]; !ok {
			display[key] = r.Counterparty
		}
		amounts[key] = amounts[key].Add(r.Amount)
	}
	keys := make([]string, 0, len(amounts))
	for k := range amounts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if c := amounts[keys[i]].Cmp(amounts[keys[j]]); c != 0 {
			return c > 0
		}
		return keys[i] < keys[j]
	})
	shares := make(map[string]string, len(keys))
	for _, k := range keys {
		shares[display[k]] = amounts[k].DivRound(total, displayPrecision).String()
	}
	top := keys[0]
	return newFinding(rule, amounts[top], total, threshold, AtMost, map[string]any{
		"total_reserves":          total.String(),
		"top_counterparty":        display[top],
		"top_counterparty_amount": amounts[top].String(),
		"shares":                  shares,
	}), nil
}

// RunReserveChecks runs every rule whose check is a known reserve check, in rules-file order.
// Rules with other checks (for example redemption_timing) or none are skipped here.
func RunReserveChecks(set *rules.RuleSet, reserves []model.ReserveRow, supply model.SupplyRow) ([]Finding, error) {
	var out []Finding
	for _, rule := range set.Rules {
		var (
			f   Finding
			err error
		)
		switch rule.CheckName() {
		case NameCoverage:
			f, err = CheckCoverage(rule, reserves, supply)
		case NameLiquidity:
			f, err = CheckLiquidityFloor(rule, reserves)
		case NameConcentration:
			f, err = CheckCounterpartyConcentration(rule, reserves)
		default:
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("rule %s: %w", rule.ID, err)
		}
		out = append(out, f)
	}
	return out, nil
}
