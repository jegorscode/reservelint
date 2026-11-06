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
	NameSurge         = "redemption_surge"
)

// splitReserves separates reserve assets from outstanding repo liabilities (rows flagged
// is_repo_liability). It returns the assets, their total, and the total liabilities.
func splitReserves(reserves []model.ReserveRow) (assets []model.ReserveRow, total, liabilities decimal.Decimal, err error) {
	for _, r := range reserves {
		if r.IsRepoLiability {
			liabilities = liabilities.Add(r.Amount)
			continue
		}
		assets = append(assets, r)
		total = total.Add(r.Amount)
	}
	if len(assets) == 0 {
		return nil, decimal.Zero, decimal.Zero, errors.New("no reserve asset rows")
	}
	return assets, total, liabilities, nil
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

// CheckCoverage: reserve asset value / outstanding issuance value must be at least the minimum
// ratio (12 CFR 15.11(a)(1)(iii)). Reserve asset value is total reserve assets minus outstanding
// repurchase-agreement liabilities (Table 1 to 15.11(e), rows 15 to 17).
func CheckCoverage(rule rules.Rule, reserves []model.ReserveRow, supply model.SupplyRow) (Finding, error) {
	_, total, liabilities, err := splitReserves(reserves)
	if err != nil {
		return Finding{}, err
	}
	threshold, err := rule.Decimal("min_coverage_ratio")
	if err != nil {
		return Finding{}, err
	}
	value := total.Sub(liabilities)
	shortfall := decimal.Max(supply.CirculatingSupply.Sub(value), decimal.Zero)
	return newFinding(rule, value, supply.CirculatingSupply, threshold, AtLeast, map[string]any{
		"total_reserves":               total.String(),
		"outstanding_repo_liabilities": liabilities.String(),
		"reserve_asset_value":          value.String(),
		"circulating_supply":           supply.CirculatingSupply.String(),
		"supply_as_of":                 supply.AsOf.UTC().Format("2006-01-02T15:04:05Z07:00"),
		"shortfall":                    shortfall.String(),
	}), nil
}

// CheckLiquidityFloor: the share of reserve assets flagged immediately available must be at
// least the minimum (12 CFR 15.11(c)(2)(i)).
func CheckLiquidityFloor(rule rules.Rule, reserves []model.ReserveRow) (Finding, error) {
	assets, total, _, err := splitReserves(reserves)
	if err != nil {
		return Finding{}, err
	}
	threshold, err := rule.Decimal("min_immediately_available_share")
	if err != nil {
		return Finding{}, err
	}
	available := decimal.Zero
	for _, r := range assets {
		if r.IsImmediatelyAvailable {
			available = available.Add(r.Amount)
		}
	}
	return newFinding(rule, available, total, threshold, AtLeast, map[string]any{
		"total_reserves":        total.String(),
		"immediately_available": available.String(),
	}), nil
}

// CheckCounterpartyConcentration: the largest non-exempt counterparty exposure's share of
// reserve assets must be at most the maximum (12 CFR 15.11(c)(2)(ii)(A)). Exposures flagged with
// a concentration_exemption (15.11(c)(2)(ii)(B)) are left out of the numerator but stay in the
// denominator. Name variants that differ only in case or whitespace are one counterparty.
func CheckCounterpartyConcentration(rule rules.Rule, reserves []model.ReserveRow) (Finding, error) {
	assets, total, _, err := splitReserves(reserves)
	if err != nil {
		return Finding{}, err
	}
	threshold, err := rule.Decimal("max_counterparty_share")
	if err != nil {
		return Finding{}, err
	}
	amounts := map[string]decimal.Decimal{}
	display := map[string]string{}
	exempt := map[string]decimal.Decimal{}
	exemptTotal := decimal.Zero
	for _, r := range assets {
		if r.ConcentrationExemption != "" {
			exempt[r.ConcentrationExemption] = exempt[r.ConcentrationExemption].Add(r.Amount)
			exemptTotal = exemptTotal.Add(r.Amount)
			continue
		}
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
	exempted := make(map[string]string, len(exempt))
	for k, v := range exempt {
		exempted[k] = v.String()
	}
	top, topName, topAmount := "", "", decimal.Zero
	if len(keys) > 0 {
		top = keys[0]
		topName, topAmount = display[top], amounts[top]
	}
	return newFinding(rule, topAmount, total, threshold, AtMost, map[string]any{
		"total_reserves":          total.String(),
		"top_counterparty":        topName,
		"top_counterparty_amount": topAmount.String(),
		"shares":                  shares,
		"exempt_exposure_total":   exemptTotal.String(),
		"exempt_exposure":         exempted,
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
