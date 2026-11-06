package engine_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jegorscode/reservelint/internal/calendar"
	"github.com/jegorscode/reservelint/internal/config"
	"github.com/jegorscode/reservelint/internal/engine"
	"github.com/jegorscode/reservelint/internal/model"
	"github.com/jegorscode/reservelint/internal/rules"
	"github.com/jegorscode/reservelint/internal/testutil"
)

const compParams = `      timezone: America/New_York
      eligible_asset_types:
        us_currency: { citation: "b1" }
        demand_deposit: { citation: "b2" }
        treasury_security: { citation: "b3", max_remaining_days: 93 }
        repo_proceeds: { citation: "b4", max_remaining_business_days: 1 }
      asset_type_aliases:
        cash_deposit: demand_deposit
        us_treasury_bill: treasury_security
`

func compRule(params string) rules.Rule { return rule("reserve_composition", true, params) }

var (
	monday = ts("2027-02-01T21:00:00Z") // Monday, 16:00 in New York
	friday = ts("2027-02-05T21:00:00Z")
)

func asset(typ, amount, maturity string) model.ReserveRow {
	r := model.ReserveRow{AssetType: typ, Amount: dec(amount), Counterparty: "Bank X"}
	if maturity != "" {
		d, err := time.Parse("2006-01-02", maturity)
		if err != nil {
			panic(err)
		}
		r.MaturityDate = &d
	}
	return r
}

func check(t *testing.T, r rules.Rule, asOf time.Time, cal *calendar.Calendar, rows ...model.ReserveRow) engine.Finding {
	t.Helper()
	f, err := engine.CheckReserveComposition(r, rows, asOf, cal)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func problems(f engine.Finding) string { return strings.Join(f.Details["ineligible"].([]string), "\n") }

func TestAllEligibleAssetsPass(t *testing.T) {
	f := check(t, compRule(compParams), monday, nil,
		asset("us_currency", "10", ""), asset("demand_deposit", "20", ""), asset("treasury_security", "70", "2027-05-05"))
	if !f.Passed || !f.Observed.IsZero() || f.Details["eligible_total"] != "100" || f.Details["ineligible_rows"] != 0 {
		t.Fatalf("%+v", f)
	}
	by := f.Details["eligible_by_type"].(map[string]string)
	if by["us_currency"] != "10" || by["treasury_security"] != "70" {
		t.Errorf("by type: %v", by)
	}
}

func TestTreasuryRemainingMaturityBoundary(t *testing.T) {
	// 2027-02-01 plus 93 days is 2027-05-05: exactly at the limit passes, a day later fails.
	if f := check(t, compRule(compParams), monday, nil, asset("treasury_security", "1", "2027-05-05")); !f.Passed {
		t.Fatalf("93 days should pass: %s", problems(f))
	}
	f := check(t, compRule(compParams), monday, nil, asset("treasury_security", "1", "2027-05-06"), asset("us_currency", "9", ""))
	if f.Passed || !strings.Contains(problems(f), "remaining maturity 94 days exceeds the 93 day limit") || !f.Observed.Equal(dec("0.1")) {
		t.Fatalf("94 days: %+v", f)
	}
}

func TestMaturityMissingOrAlreadyPassed(t *testing.T) {
	f := check(t, compRule(compParams), monday, nil, asset("treasury_security", "1", ""))
	if f.Passed || !strings.Contains(problems(f), "maturity_date is required") {
		t.Errorf("missing maturity: %s", problems(f))
	}
	f = check(t, compRule(compParams), monday, nil, asset("treasury_security", "1", "2027-01-31"))
	if f.Passed || !strings.Contains(problems(f), "matured 1 day(s) before the snapshot date") {
		t.Errorf("matured yesterday: %s", problems(f))
	}
	if f = check(t, compRule(compParams), monday, nil, asset("treasury_security", "1", "2027-02-01")); !f.Passed {
		t.Errorf("maturing today should pass: %s", problems(f))
	}
}

func TestNamesAliasesAndSpelling(t *testing.T) {
	f := check(t, compRule(compParams), monday, nil,
		asset("cash_deposit", "1", ""), asset("US_Treasury_Bill", "1", "2027-03-01"),
		asset(" Demand-Deposit ", "1", ""), asset("US  Currency", "1", ""))
	if !f.Passed {
		t.Fatalf("%s", problems(f))
	}
	f = check(t, compRule(compParams), monday, nil, asset("bitcoin", "5", ""), asset("us_currency", "5", ""))
	msg := problems(f)
	if !strings.Contains(msg, "allowed under test source: demand_deposit, repo_proceeds, treasury_security, us_currency") {
		t.Errorf("message should cite the rule and list every allowed type, sorted: %s", msg)
	}
	if !strings.Contains(msg, "(also accepted as aliases: cash_deposit, us_treasury_bill)") {
		t.Errorf("message should list the aliases: %s", msg)
	}
	if f.Passed || !strings.Contains(problems(f), `asset type "bitcoin" is not an eligible reserve asset type`) ||
		f.Details["ineligible_total"] != "5" || !f.Observed.Equal(dec("0.5")) {
		t.Fatalf("%+v", f)
	}
}

func TestOvernightIsBusinessDaysAndNeedsACalendar(t *testing.T) {
	cal := nyCalendar(t)
	r := compRule(compParams)
	cases := []struct {
		name     string
		asOf     time.Time
		maturity string
		pass     bool
	}{
		{"Friday to Monday", friday, "2027-02-08", true},
		{"Friday to Tuesday", friday, "2027-02-09", false},
		{"Monday to Tuesday", monday, "2027-02-02", true},
		{"Monday to Wednesday", monday, "2027-02-03", false},
		{"Friday before Presidents' Day to Tuesday", ts("2027-02-12T21:00:00Z"), "2027-02-16", true},
		{"same day", monday, "2027-02-01", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := check(t, r, tc.asOf, cal, asset("repo_proceeds", "1", tc.maturity))
			if f.Passed != tc.pass {
				t.Fatalf("passed=%v: %s", f.Passed, problems(f))
			}
		})
	}
	if _, err := engine.CheckReserveComposition(r, []model.ReserveRow{asset("repo_proceeds", "1", "2027-02-02")}, monday, nil); err == nil {
		t.Error("business-day limit without a calendar accepted")
	}
}

func TestSnapshotDateIsANewYorkCalendarDate(t *testing.T) {
	r := compRule(compParams)
	bill := asset("treasury_security", "1", "2027-05-05")
	// 03:00Z on Feb 1 is still Jan 31 in New York, so this bill has 94 days left.
	if f := check(t, r, ts("2027-02-01T03:00:00Z"), nil, bill); f.Passed || f.Details["snapshot_date"] != "2027-01-31" {
		t.Errorf("late-evening New York snapshot: %+v", f.Details)
	}
	if f := check(t, r, ts("2027-02-01T15:00:00Z"), nil, bill); !f.Passed || f.Details["snapshot_date"] != "2027-02-01" {
		t.Errorf("midday snapshot: %+v", f.Details)
	}
}

func TestRepoLiabilityRowsAreNotChecked(t *testing.T) {
	liab := asset("anything_goes", "50", "")
	liab.IsRepoLiability = true
	f := check(t, compRule(compParams), monday, nil, asset("us_currency", "100", ""), liab)
	if !f.Passed || f.Details["total_reserves"] != "100" {
		t.Fatalf("%+v", f.Details)
	}
}

func TestCompositionRuleConfigurationErrors(t *testing.T) {
	cal := nyCalendar(t)
	rows := []model.ReserveRow{asset("us_currency", "1", "")}
	bad := map[string]string{
		"alias to nowhere": strings.Replace(compParams, "cash_deposit: demand_deposit", "cash_deposit: gold", 1),
		"alias clashes":    strings.Replace(compParams, "cash_deposit: demand_deposit", "us_currency: demand_deposit", 1),
		"negative limit":   strings.Replace(compParams, "max_remaining_days: 93", "max_remaining_days: -1", 1),
		"no timezone":      strings.Replace(compParams, "      timezone: America/New_York\n", "", 1),
		"empty types":      "      timezone: America/New_York\n      eligible_asset_types: {}\n",
		"no types":         "      timezone: America/New_York\n",
	}
	for name, params := range bad {
		t.Run(name, func(t *testing.T) {
			if _, err := engine.CheckReserveComposition(compRule(params), rows, monday, cal); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	chicago := strings.Replace(compParams, "America/New_York", "America/Chicago", 1)
	if _, err := engine.CheckReserveComposition(compRule(chicago), rows, monday, cal); err == nil {
		t.Error("calendar and rule time zones differ but were accepted")
	}
	if _, err := engine.CheckReserveComposition(compRule(compParams), nil, monday, cal); err == nil {
		t.Error("no reserves accepted")
	}
}

func TestShippedCompositionRuleAcceptsTheSampleAssetTypes(t *testing.T) {
	loaded, err := config.LoadRules(filepath.Join(testutil.RepoRoot(), "rules", "rules.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	r, ok := loaded.Set.Get("RW-COMP-001")
	if !ok || r.CheckName() != engine.NameComposition || !r.IsVerified() {
		t.Fatalf("shipped rule: %+v", r)
	}
	f := check(t, r, monday, nyCalendar(t),
		asset("cash_deposit", "1", ""), asset("us_treasury_bill", "1", "2027-04-01"), asset("reverse_repo", "1", "2027-02-02"))
	if !f.Passed {
		t.Fatalf("%s", problems(f))
	}
	f = check(t, r, monday, nyCalendar(t), asset("corporate_bond", "1", "2027-04-01"))
	if f.Passed {
		t.Fatal("a corporate bond is not an eligible reserve asset")
	}
}
