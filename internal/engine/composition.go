package engine

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jegorscode/reservelint/internal/calendar"
	"github.com/jegorscode/reservelint/internal/model"
	"github.com/jegorscode/reservelint/internal/rules"
	"github.com/shopspring/decimal"
)

// NameComposition is the check name of the reserve composition rule.
const NameComposition = "reserve_composition"

// assetSpec is one eligible asset type from the rule's eligible_asset_types parameter.
type assetSpec struct {
	Citation                 string `yaml:"citation"`
	MaxRemainingDays         *int   `yaml:"max_remaining_days"`          // calendar days from the snapshot date
	MaxRemainingBusinessDays *int   `yaml:"max_remaining_business_days"` // business days from the snapshot date
}

// normType makes asset type names comparable: case, spacing and "-" versus "_" do not matter.
func normType(s string) string {
	s = model.NormText(s)
	return strings.NewReplacer(" ", "_", "-", "_").Replace(s)
}

// CheckReserveComposition tests that reserve assets comprise exclusively eligible asset types
// (12 CFR 15.11(b)): each row's asset_type must be an eligible type (or an alias of one) listed in
// the rule, and types with a remaining-maturity limit must carry a maturity_date within it.
//
// Only type and remaining maturity can be tested from the data. Collateral, overcollateralization,
// counterparty creditworthiness, fund holdings, OCC approvals and FDIC/NCUA deposit limits cannot,
// and are reported as unverifiable. Rows flagged is_repo_liability are not reserve assets and are
// skipped. cal may be nil unless a type has a business-day maturity limit.
func CheckReserveComposition(rule rules.Rule, reserves []model.ReserveRow, asOf time.Time, cal *calendar.Calendar) (Finding, error) {
	assets, total, _, err := splitReserves(reserves)
	if err != nil {
		return Finding{}, err
	}
	var eligible map[string]assetSpec
	if err := rule.Decode("eligible_asset_types", &eligible); err != nil {
		return Finding{}, err
	}
	if len(eligible) == 0 {
		return Finding{}, fmt.Errorf("rule %s: eligible_asset_types is empty", rule.ID)
	}
	specs := make(map[string]assetSpec, len(eligible))
	for id, spec := range eligible {
		for _, limit := range []*int{spec.MaxRemainingDays, spec.MaxRemainingBusinessDays} {
			if limit != nil && *limit < 0 {
				return Finding{}, fmt.Errorf("rule %s: %s has a negative maturity limit", rule.ID, id)
			}
		}
		specs[normType(id)] = spec
	}
	aliases := map[string]string{}
	if rule.Has("asset_type_aliases") {
		var raw map[string]string
		if err := rule.Decode("asset_type_aliases", &raw); err != nil {
			return Finding{}, err
		}
		for alias, target := range raw {
			t := normType(target)
			if _, ok := specs[t]; !ok {
				return Finding{}, fmt.Errorf("rule %s: alias %q points to %q, which is not an eligible asset type", rule.ID, alias, target)
			}
			if _, clash := specs[normType(alias)]; clash {
				return Finding{}, fmt.Errorf("rule %s: alias %q is already an eligible asset type", rule.ID, alias)
			}
			aliases[normType(alias)] = t
		}
	}

	tz, err := rule.String("timezone")
	if err != nil {
		return Finding{}, err
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return Finding{}, fmt.Errorf("rule %s: time zone %q: %w", rule.ID, tz, err)
	}
	if cal != nil && cal.Location().String() != tz {
		return Finding{}, fmt.Errorf("rule %s: calendar time zone %s does not match rule time zone %s", rule.ID, cal.Location(), tz)
	}
	local := asOf.In(loc)
	snapshot := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)

	eligibleTotal := decimal.Zero
	byType := map[string]decimal.Decimal{}
	var problems []string
	ineligibleTotal := decimal.Zero
	for _, r := range assets {
		id := normType(r.AssetType)
		if target, ok := aliases[id]; ok {
			id = target
		}
		spec, ok := specs[id]
		reason := ""
		switch {
		case !ok:
			reason = fmt.Sprintf("asset type %q is not an eligible reserve asset type (add it, or an alias, to the rule if it is one)", r.AssetType)
		case spec.MaxRemainingDays != nil || spec.MaxRemainingBusinessDays != nil:
			reason, err = maturityProblem(r, id, spec, snapshot, cal)
			if err != nil {
				return Finding{}, err
			}
		}
		if reason != "" {
			ineligibleTotal = ineligibleTotal.Add(r.Amount)
			problems = append(problems, describe(r, reason))
			continue
		}
		eligibleTotal = eligibleTotal.Add(r.Amount)
		byType[id] = byType[id].Add(r.Amount)
	}

	types := make(map[string]string, len(byType))
	for id, amt := range byType {
		types[id] = amt.String()
	}
	sort.Strings(problems)
	return newFinding(rule, ineligibleTotal, total, decimal.Zero, AtMost, map[string]any{
		"total_reserves":       total.String(),
		"eligible_total":       eligibleTotal.String(),
		"ineligible_total":     ineligibleTotal.String(),
		"ineligible_rows":      len(problems),
		"eligible_by_type":     types,
		"ineligible":           problems,
		"snapshot_date":        snapshot.Format("2006-01-02"),
		"timezone":             tz,
		"unverifiable_by_tool": "Only asset type and remaining maturity are tested. Collateral, overcollateralization, counterparty creditworthiness, fund holdings, OCC approvals and FDIC/NCUA deposit limits cannot be checked from this data.",
	}), nil
}

func describe(r model.ReserveRow, reason string) string {
	who := r.Counterparty
	if r.HoldingID != "" {
		who = r.HoldingID + ", " + who
	}
	return fmt.Sprintf("%s (%s, %s): %s", r.AssetType, who, r.Amount, reason)
}

// maturityProblem returns why a row breaks its type's remaining-maturity limit, or "".
func maturityProblem(r model.ReserveRow, id string, spec assetSpec, snapshot time.Time, cal *calendar.Calendar) (string, error) {
	if r.MaturityDate == nil {
		return fmt.Sprintf("maturity_date is required for %s", id), nil
	}
	maturity := time.Date(r.MaturityDate.Year(), r.MaturityDate.Month(), r.MaturityDate.Day(), 0, 0, 0, 0, time.UTC)
	days := int(maturity.Sub(snapshot).Hours() / 24)
	if days < 0 {
		return fmt.Sprintf("matured %d day(s) before the snapshot date", -days), nil
	}
	if spec.MaxRemainingDays != nil && days > *spec.MaxRemainingDays {
		return fmt.Sprintf("remaining maturity %d days exceeds the %d day limit for %s", days, *spec.MaxRemainingDays, id), nil
	}
	if spec.MaxRemainingBusinessDays != nil {
		if cal == nil {
			return "", fmt.Errorf("asset type %s has a business-day maturity limit but no holiday calendar is loaded", id)
		}
		from, _ := calendar.ParseDate(snapshot.Format("2006-01-02"))
		to, _ := calendar.ParseDate(maturity.Format("2006-01-02"))
		n, err := cal.BusinessDaysBetween(from, to)
		if err != nil {
			return "", fmt.Errorf("remaining maturity of %s: %w", r.AssetType, err)
		}
		if n > *spec.MaxRemainingBusinessDays {
			return fmt.Sprintf("remaining maturity %d business days exceeds the %d business day limit for %s", n, *spec.MaxRemainingBusinessDays, id), nil
		}
	}
	return "", nil
}
