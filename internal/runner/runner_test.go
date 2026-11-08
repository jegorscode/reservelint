package runner_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jegorscode/reservelint/internal/meta"
	"github.com/jegorscode/reservelint/internal/report"
	"github.com/jegorscode/reservelint/internal/runner"
	"github.com/jegorscode/reservelint/internal/synthetic"
	"github.com/jegorscode/reservelint/internal/testutil"
	"github.com/shopspring/decimal"
)

var update = flag.Bool("update", false, "rewrite golden files in testdata/golden")

var fixedNow = time.Date(2027, 2, 1, 21, 30, 0, 0, time.UTC)

// inRepoRoot makes relative paths in reports identical on every machine.
func inRepoRoot(t *testing.T) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(testutil.RepoRoot()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

func vectorOpts(scenario string) runner.Options {
	dir := "testdata/vectors/" + scenario
	return runner.Options{
		RulesPath: "rules/rules.yaml", ReservesPath: dir + "/reserves.csv", SupplyPath: dir + "/supply.csv",
		RedemptionsPath: dir + "/redemptions.csv", Now: func() time.Time { return fixedNow },
	}
}

func run(t *testing.T, opts runner.Options) *report.Report {
	t.Helper()
	rep, err := runner.Run(opts)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func writeFile(t *testing.T, name, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func sha(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ---- golden files ----------------------------------------------------------------------

func TestGoldenReports(t *testing.T) {
	inRepoRoot(t)
	for _, scenario := range []string{"clean", "violations"} {
		t.Run(scenario, func(t *testing.T) {
			out := t.TempDir()
			if _, err := report.Write(out, run(t, vectorOpts(scenario))); err != nil {
				t.Fatal(err)
			}
			golden := filepath.Join("testdata", "golden", scenario)
			if *update {
				if err := os.RemoveAll(golden); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(golden, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			got, _ := os.ReadDir(out)
			if len(got) != 4 {
				t.Fatalf("expected 4 output files, got %d", len(got))
			}
			for _, e := range got {
				actual, _ := os.ReadFile(filepath.Join(out, e.Name()))
				path := filepath.Join(golden, e.Name())
				if *update {
					if err := os.WriteFile(path, actual, 0o644); err != nil {
						t.Fatal(err)
					}
					continue
				}
				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("missing golden file (run: go test ./internal/runner -update): %v", err)
				}
				if !bytes.Equal(actual, want) {
					t.Errorf("%s differs from golden file %s; if the change is intended run: go test ./internal/runner -update", e.Name(), path)
				}
			}
			if !*update {
				want, _ := os.ReadDir(golden)
				if len(want) != len(got) {
					t.Errorf("golden has %d files, run produced %d", len(want), len(got))
				}
			}
		})
	}
}

// ---- verdicts and exit codes -----------------------------------------------------------

func TestCleanDataPassesWithCaveats(t *testing.T) {
	inRepoRoot(t)
	rep := run(t, vectorOpts("clean"))
	if rep.Status != report.StatusPass || rep.ExitCode != 0 || rep.Counts.Pass != 6 || rep.Partial {
		t.Fatalf("%s exit %d counts %+v", rep.Status, rep.ExitCode, rep.Counts)
	}
	joined := strings.Join(rep.Caveats, "\n")
	if strings.Contains(joined, "RW-") || !strings.Contains(joined, "Holiday calendar") {
		t.Errorf("only the unverified calendar should be a caveat: %v", rep.Caveats)
	}
	if len(rep.NotChecked) != 4 {
		t.Errorf("expected 4 tracked-but-unchecked rules, got %+v", rep.NotChecked)
	}
}

// The generator's "clean" scenario must be clean under the shipped rules for any seed, and its
// "violations" scenario must fail: this is what the demo shows.
func TestGeneratedScenariosBehaveAsAdvertised(t *testing.T) {
	inRepoRoot(t)
	for seed := int64(1); seed <= 15; seed++ {
		dir := t.TempDir()
		if _, err := synthetic.Generate(dir, seed, synthetic.CSV); err != nil {
			t.Fatal(err)
		}
		for scenario, want := range map[string]report.Status{"clean": report.StatusPass, "violations": report.StatusFail} {
			opts := vectorOpts("clean")
			opts.ReservesPath = filepath.Join(dir, scenario, "reserves.csv")
			opts.SupplyPath = filepath.Join(dir, scenario, "supply.csv")
			opts.RedemptionsPath = filepath.Join(dir, scenario, "redemptions.csv")
			rep := run(t, opts)
			if rep.Status != want {
				t.Errorf("seed %d %s: %s, want %s (counts %+v)", seed, scenario, rep.Status, want, rep.Counts)
			}
			if scenario == "violations" && rep.Counts.Fail != 4 {
				t.Errorf("seed %d violations: %d failing rules, want 4 (coverage, liquidity, concentration, redemption timing)", seed, rep.Counts.Fail)
			}
		}
	}
}

func TestViolationsFail(t *testing.T) {
	inRepoRoot(t)
	rep := run(t, vectorOpts("violations"))
	// Coverage, liquidity, concentration and redemption timing all fail; the 24-hour surge check
	// passes; the vector data repeats one unkeyed line, which raises DUPLICATE_ROW_WARNING.
	if rep.Status != report.StatusFail || rep.ExitCode != 1 || rep.Counts.Fail != 4 || rep.Counts.Warning != 1 || rep.Counts.Pass != 2 {
		t.Fatalf("%s exit %d counts %+v", rep.Status, rep.ExitCode, rep.Counts)
	}
	if in := rep.Inputs[0]; in.DuplicateCount() != 0 || in.IdenticalUnkeyedCount() != 1 || in.RowsRead != 16 || in.RowsUsed != 16 {
		t.Errorf("duplicate accounting wrong: %+v", in)
	}
	last := rep.Findings[len(rep.Findings)-1]
	if last.RuleID != runner.DuplicateRowWarning || last.Status != "warning" || last.Details["amount_in_repeated_rows"] != "1045000" {
		t.Errorf("duplicate warning finding: %+v", last)
	}
	if rep.Redemption == nil || rep.Redemption.Summary.Breach < 2 || len(rep.Redemption.Items) == 0 {
		t.Errorf("redemption section: %+v", rep.Redemption)
	}
}

// ---- holding_id and duplicate handling -------------------------------------------------

const keyedHeader = "holding_id,asset_type,amount,counterparty,maturity_date,is_immediately_available\n"

// coverageOnly is a rules file with just the coverage rule, so findings are easy to read.
func coverageOnly(t *testing.T) string {
	t.Helper()
	return writeFile(t, "rules.yaml", "schema_version: 1\nrules:\n  - id: T-COV\n    check: coverage\n    description: d\n    source: s\n    verified: true\n    parameters:\n      min_coverage_ratio: 1.0\n")
}

func totalReserves(t *testing.T, rep *report.Report) string {
	t.Helper()
	for _, f := range rep.Findings {
		if f.Check == "coverage" {
			return f.Details["total_reserves"].(string)
		}
	}
	t.Fatal("no coverage finding")
	return ""
}

// Two legitimate $50M deposits at one bank, identical except for holding_id, against $100M of
// supply: both must count, so coverage is exactly 100% and passes.
func TestTwoIdenticalDepositsWithHoldingIDsSumToFullCoverage(t *testing.T) {
	inRepoRoot(t)
	opts := vectorOpts("clean") // supply is 100,000,000
	opts.RulesPath = coverageOnly(t)
	opts.ReservesPath = writeFile(t, "r.csv", keyedHeader+
		"HLD-00001,cash_deposit,50000000.00,Bank A,,true\n"+
		"HLD-00002,cash_deposit,50000000.00,Bank A,,true\n")
	rep := run(t, opts)
	if got := totalReserves(t, rep); got != "100000000" {
		t.Fatalf("total reserves = %s, want both deposits counted", got)
	}
	if rep.Findings[0].Status != "pass" || rep.Status != report.StatusPass || len(rep.Findings) != 1 {
		t.Fatalf("%s findings %+v", rep.Status, rep.Findings)
	}
	if in := rep.Inputs[0]; in.RowsUsed != 2 || in.DuplicateCount() != 0 || in.IdenticalUnkeyedCount() != 0 {
		t.Errorf("input accounting: %+v", in)
	}
}

func TestUnkeyedIdenticalDepositsAreKeptWithAWarning(t *testing.T) {
	inRepoRoot(t)
	opts := vectorOpts("clean")
	opts.RulesPath = coverageOnly(t)
	opts.ReservesPath = writeFile(t, "r.csv", "asset_type,amount,counterparty,maturity_date,is_immediately_available\n"+
		"cash_deposit,50000000.00,Bank A,,true\ncash_deposit,50000000.00,Bank A,,true\n")
	rep := run(t, opts)
	if got := totalReserves(t, rep); got != "100000000" {
		t.Fatalf("total reserves = %s: unkeyed identical rows must not be dropped", got)
	}
	if rep.Status != report.StatusWarning || rep.ExitCode != 0 || rep.Counts.Warning != 1 || rep.Counts.Fail != 0 {
		t.Fatalf("%s exit %d counts %+v", rep.Status, rep.ExitCode, rep.Counts)
	}
	w := rep.Findings[len(rep.Findings)-1]
	if w.RuleID != "DUPLICATE_ROW_WARNING" || w.Details["amount_in_repeated_rows"] != "50000000" || w.Details["groups"] != "line 2 repeated at 3" {
		t.Fatalf("warning: %+v", w)
	}
	csvData, _ := report.FindingsCSV(rep)
	md := string(report.Markdown(rep))
	if !strings.Contains(string(csvData), "DUPLICATE_ROW_WARNING") || !strings.Contains(md, "DUPLICATE_ROW_WARNING") || !strings.Contains(md, "Identical rows without a holding_id") {
		t.Error("warning should appear in findings.csv and report.md")
	}
}

func TestAllowDuplicateRowsAcknowledgesWithoutWarning(t *testing.T) {
	inRepoRoot(t)
	opts := vectorOpts("clean")
	opts.RulesPath = coverageOnly(t)
	opts.ReservesPath = writeFile(t, "r.csv", "asset_type,amount,counterparty,maturity_date,is_immediately_available\n"+
		"cash_deposit,50000000.00,Bank A,,true\ncash_deposit,50000000.00,Bank A,,true\n")
	opts.AllowDuplicateRows = true
	rep := run(t, opts)
	if got := totalReserves(t, rep); got != "100000000" {
		t.Fatalf("total reserves = %s", got)
	}
	if rep.Status != report.StatusPass || len(rep.Findings) != 1 || !rep.Options.AllowDuplicateRows || rep.Inputs[0].IdenticalUnkeyedCount() != 1 {
		t.Fatalf("%s findings %d options %+v input %+v", rep.Status, len(rep.Findings), rep.Options, rep.Inputs[0])
	}
	if !strings.Contains(strings.Join(rep.Caveats, "\n"), "--allow-duplicate-rows") {
		t.Errorf("caveats should record the acknowledgement: %v", rep.Caveats)
	}
}

// ---- codified rules: exemptions, repo liabilities, surge notice ------------------------

func TestExemptionsAndRepoLiabilitiesEndToEnd(t *testing.T) {
	inRepoRoot(t)
	opts := vectorOpts("clean") // supply is 100,000,000
	opts.RulesPath = writeFile(t, "rules.yaml", "schema_version: 1\nrules:\n"+
		"  - id: T-COV\n    check: coverage\n    description: d\n    source: s\n    verified: true\n    parameters:\n      min_coverage_ratio: 1.0\n"+
		"  - id: T-CON\n    check: counterparty_concentration\n    description: d\n    source: s\n    verified: true\n    parameters:\n      max_counterparty_share: 0.40\n")
	opts.ReservesPath = writeFile(t, "r.csv", "holding_id,asset_type,amount,counterparty,is_immediately_available,concentration_exemption,is_repo_liability\n"+
		"H1,us_treasury_bill,60000000,US Treasury,false,us_government,\n"+
		"H2,cash_deposit,40000000,Bank B,true,,\n"+
		"H3,repo_liability,10000000,Dealer,false,,true\n")
	rep := run(t, opts)
	cov, con := rep.Findings[0], rep.Findings[1]
	// Assets 100M less a 10M repo liability is 90M of reserve asset value against 100M issued.
	if cov.Status != "fail" || cov.Details["reserve_asset_value"] != "90000000" || cov.Details["outstanding_repo_liabilities"] != "10000000" || cov.Details["shortfall"] != "10000000" {
		t.Fatalf("coverage: %s %+v", cov.Status, cov.Details)
	}
	// The 60M US Government exposure is exempt; Bank B at 40M of 100M is exactly at the limit.
	if con.Status != "pass" || con.Details["exempt_exposure_total"] != "60000000" || con.Details["top_counterparty"] != "Bank B" {
		t.Fatalf("concentration: %s %+v", con.Status, con.Details)
	}
	if rep.Status != report.StatusFail || rep.ExitCode != 1 {
		t.Errorf("%s exit %d", rep.Status, rep.ExitCode)
	}
	if md := string(report.Markdown(rep)); !strings.Contains(md, "exempt_exposure_total: 60000000") || !strings.Contains(md, "us_government: 60000000") {
		t.Error("markdown should show exempt exposure")
	}
}

func TestIneligibleAssetFailsCompositionEndToEnd(t *testing.T) {
	inRepoRoot(t)
	good, _ := os.ReadFile("testdata/vectors/clean/reserves.csv")
	opts := vectorOpts("clean")
	opts.ReservesPath = writeFile(t, "r.csv", string(good)+"corporate_bond,1000000,Bank X,2027-06-01,false\n"+"us_treasury_bill,5,Bank X,2028-01-01,false\n")
	rep := run(t, opts)
	var comp report.Finding
	for _, f := range rep.Findings {
		if f.RuleID == "RW-COMP-001" {
			comp = f
		}
	}
	if comp.Status != "fail" || rep.ExitCode != 1 || comp.Details["ineligible_rows"] != 2 {
		t.Fatalf("composition: %s %+v (exit %d)", comp.Status, comp.Details, rep.ExitCode)
	}
	md := string(report.Markdown(rep))
	if !strings.Contains(md, `asset type "corporate_bond" is not an eligible reserve asset type`) || !strings.Contains(md, "exceeds the 93 day limit") ||
		!strings.Contains(md, "Collateral, overcollateralization") {
		t.Errorf("markdown should list each ineligible row and the unverifiable conditions:\n%s", md)
	}
	if csvData, _ := report.FindingsCSV(rep); !strings.Contains(string(csvData), "RW-COMP-001,reserve_composition,fail") {
		t.Error("findings.csv should carry the composition failure")
	}
}

func TestCompositionRuleAloneLoadsTheCalendarItNames(t *testing.T) {
	inRepoRoot(t)
	opts := vectorOpts("clean")
	opts.RulesPath = writeFile(t, "rules.yaml", "schema_version: 1\nrules:\n"+
		"  - id: T-COMP\n    check: reserve_composition\n    description: d\n    source: s\n    verified: true\n    parameters:\n"+
		"      timezone: America/New_York\n      holiday_calendar: config/holidays.yaml\n"+
		"      eligible_asset_types:\n        demand_deposit: {}\n        treasury_security: { max_remaining_days: 93 }\n"+
		"      asset_type_aliases:\n        cash_deposit: demand_deposit\n        us_treasury_bill: treasury_security\n")
	rep := run(t, opts)
	if rep.Calendar == nil || rep.Calendar.Path != "config/holidays.yaml" || rep.Findings[0].Status != "pass" {
		t.Fatalf("calendar %+v finding %+v", rep.Calendar, rep.Findings[0])
	}
	// Without a holiday_calendar parameter the calendar is not loaded.
	opts.RulesPath = writeFile(t, "rules2.yaml", "schema_version: 1\nrules:\n"+
		"  - id: T-COMP\n    check: reserve_composition\n    description: d\n    source: s\n    verified: true\n    parameters:\n"+
		"      timezone: America/New_York\n      eligible_asset_types:\n        demand_deposit: {}\n        treasury_security: { max_remaining_days: 93 }\n"+
		"      asset_type_aliases:\n        cash_deposit: demand_deposit\n        us_treasury_bill: treasury_security\n")
	if rep = run(t, opts); rep.Calendar != nil || rep.Findings[0].Status != "pass" {
		t.Fatalf("calendar %+v", rep.Calendar)
	}
}

func TestRedemptionSurgeWarnsWithoutFailing(t *testing.T) {
	inRepoRoot(t)
	opts := vectorOpts("clean") // supply is 100,000,000; 10% is 10,000,000
	opts.RulesPath = writeFile(t, "rules.yaml", "schema_version: 1\nrules:\n"+
		"  - id: T-SURGE\n    check: redemption_surge\n    description: d\n    source: s\n    verified: true\n    parameters:\n      max_share_of_issuance: 0.10\n      window_hours: 24\n")
	opts.RedemptionsPath = writeFile(t, "red.csv", "request_ts,amount,status\n"+
		"2027-01-25T10:00:00Z,6000000,rejected\n2027-01-25T20:00:00Z,6000000,completed\n2027-01-27T10:00:00Z,1,pending\n")
	rep := run(t, opts)
	f := rep.Findings[0]
	if f.Status != "warning" || f.Details["notice_to_occ_required"] != true || f.Details["largest_window_amount"] != "12000000" {
		t.Fatalf("%s %+v", f.Status, f.Details)
	}
	if rep.Status != report.StatusWarning || rep.ExitCode != 0 || rep.Calendar != nil {
		t.Fatalf("%s exit %d calendar %v", rep.Status, rep.ExitCode, rep.Calendar)
	}
}

func TestConflictingHoldingIDIsAnInvalidRow(t *testing.T) {
	inRepoRoot(t)
	opts := vectorOpts("clean")
	opts.ReservesPath = writeFile(t, "r.csv", keyedHeader+
		"HLD-00001,cash_deposit,1.00,Bank A,,true\nHLD-00001,cash_deposit,2.00,Bank A,,true\n")
	_, err := runner.Run(opts)
	var ie *runner.InputError
	if !errors.As(err, &ie) || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("err = %v", err)
	}
}

func TestUnverifiedRuleFailureIsCappedAtWarning(t *testing.T) {
	inRepoRoot(t)
	coverageRules := func(verified string) string {
		return writeFile(t, "rules.yaml", "schema_version: 1\nrules:\n  - id: T-COV\n    check: coverage\n    description: d\n    source: s\n    verified: "+verified+"\n    parameters:\n      min_coverage_ratio: 1.0\n")
	}
	opts := vectorOpts("violations") // reserves are 95% of supply, so the check itself fails
	opts.RulesPath = coverageRules("false")
	rep := run(t, opts)
	f := rep.Findings[0]
	if f.Status != "warning" || f.UncappedStatus != "fail" || rep.Status != report.StatusWarning || rep.ExitCode != 0 {
		t.Fatalf("finding %s/%s, overall %s exit %d", f.Status, f.UncappedStatus, rep.Status, rep.ExitCode)
	}
	if !strings.Contains(strings.Join(rep.Caveats, "\n"), "reported as WARNING") {
		t.Errorf("caveats: %v", rep.Caveats)
	}
	if md := string(report.Markdown(rep)); !strings.Contains(md, "WARNING (capped from FAIL)") {
		t.Error("markdown should show the cap")
	}
	if csvData, _ := report.FindingsCSV(rep); !strings.Contains(string(csvData), ",fail\n") {
		t.Error("csv should record the uncapped status")
	}

	opts.RulesPath = coverageRules("true") // once promoted to verified, the same failure fails the run
	rep = run(t, opts)
	if rep.Findings[0].Status != "fail" || rep.Findings[0].UncappedStatus != "" || rep.Status != report.StatusFail || rep.ExitCode != 1 {
		t.Fatalf("verified rule: %s overall %s exit %d", rep.Findings[0].Status, rep.Status, rep.ExitCode)
	}
}

func TestWarningStatusFromNearBreach(t *testing.T) {
	inRepoRoot(t)
	opts := vectorOpts("clean")
	opts.RedemptionsPath = writeFile(t, "red.csv",
		"request_ts,onboarded_ts,transfer_initiated_ts,amount,status\n"+
			"2027-01-25T15:00:00Z,,2027-01-27T15:00:00Z,100,completed\n"+ // Mon -> Wed: 2 days, warning
			"2027-01-04T15:00:00Z,,,50,rejected\n"+
			"2027-01-05T15:00:00Z,,,70,cancelled\n")
	rep := run(t, opts)
	if rep.Status != report.StatusWarning || rep.ExitCode != 0 || rep.Counts.Warning != 1 || rep.Counts.Fail != 0 {
		t.Fatalf("%s exit %d counts %+v", rep.Status, rep.ExitCode, rep.Counts)
	}
	s := rep.Redemption.Summary
	if s.Rejected != 1 || s.Cancelled != 1 || s.Total != 3 || !s.ExcludedAmount.Equal(decimal.RequireFromString("120")) {
		t.Errorf("excluded totals missing from the summary: %+v", s)
	}
}

func TestInvalidRowsFailFastByDefault(t *testing.T) {
	inRepoRoot(t)
	clean, _ := os.ReadFile("testdata/vectors/clean/reserves.csv")
	opts := vectorOpts("clean")
	opts.ReservesPath = writeFile(t, "r.csv", string(clean)+"cash_deposit,-5,Bank X,,true\ncash_deposit,5,,,true\n")
	_, err := runner.Run(opts)
	var ie *runner.InputError
	if !errors.As(err, &ie) || !strings.Contains(err.Error(), "--allow-invalid-rows") || !strings.Contains(err.Error(), "must be greater than zero") {
		t.Fatalf("err = %v", err)
	}
}

func TestAllowInvalidRowsGivesPartialWarning(t *testing.T) {
	inRepoRoot(t)
	clean, _ := os.ReadFile("testdata/vectors/clean/reserves.csv")
	opts := vectorOpts("clean")
	opts.ReservesPath = writeFile(t, "r.csv", string(clean)+"cash_deposit,-5,Bank X,,true\n")
	opts.AllowInvalidRows = true
	rep := run(t, opts)
	if !rep.Partial || rep.Status != report.StatusWarning || rep.ExitCode != 0 || !rep.Options.AllowInvalidRows {
		t.Fatalf("partial=%v %s exit %d", rep.Partial, rep.Status, rep.ExitCode)
	}
	if len(rep.Inputs[0].InvalidRows) != 1 || rep.Inputs[0].InvalidRows[0].Position != 17 {
		t.Errorf("invalid rows: %+v", rep.Inputs[0].InvalidRows)
	}
	if !strings.Contains(strings.Join(rep.Caveats, "\n"), "PARTIAL RUN") {
		t.Errorf("caveats: %v", rep.Caveats)
	}
	if md := string(report.Markdown(rep)); !strings.Contains(md, "PARTIAL RUN") || !strings.Contains(md, "Invalid rows in") {
		t.Error("markdown does not flag the partial run")
	}
}

func TestPartialRunStillFailsOnViolations(t *testing.T) {
	inRepoRoot(t)
	v, _ := os.ReadFile("testdata/vectors/violations/reserves.csv")
	opts := vectorOpts("violations")
	opts.ReservesPath = writeFile(t, "r.csv", string(v)+"x,0,Y,,true\n")
	opts.AllowInvalidRows = true
	if rep := run(t, opts); rep.Status != report.StatusFail || rep.ExitCode != 1 || !rep.Partial {
		t.Fatalf("%s exit %d partial %v", rep.Status, rep.ExitCode, rep.Partial)
	}
}

func TestInputErrors(t *testing.T) {
	inRepoRoot(t)
	badRules := writeFile(t, "rules.yaml", "schema_version: 1\nrules:\n  - id: X\n    check: no_such_check\n    description: d\n    source: s\n    verified: false\n    parameters: {}\n")
	cases := map[string]func(*runner.Options){
		"missing reserves": func(o *runner.Options) { o.ReservesPath = "nope.csv" },
		"missing rules":    func(o *runner.Options) { o.RulesPath = "nope.yaml" },
		"unknown check":    func(o *runner.Options) { o.RulesPath = badRules },
		"missing holidays": func(o *runner.Options) { o.HolidaysPath = "nope.yaml" },
		"empty supply":     func(o *runner.Options) { o.SupplyPath = writeFile(t, "s.csv", "as_of,circulating_supply\n") },
		"redemptions in 2030": func(o *runner.Options) {
			o.RedemptionsPath = writeFile(t, "r.csv", "request_ts,amount,status\n2030-02-01T15:00:00Z,5,pending\n")
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			opts := vectorOpts("clean")
			mutate(&opts)
			_, err := runner.Run(opts)
			var ie *runner.InputError
			if !errors.As(err, &ie) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestAsOfOverride(t *testing.T) {
	inRepoRoot(t)
	opts := vectorOpts("violations")
	at := time.Date(2027, 1, 21, 0, 0, 0, 0, time.UTC)
	opts.AsOf = &at
	rep := run(t, opts)
	if !rep.AsOf.Equal(at) || rep.Options.AsOfOverride != "2027-01-21T00:00:00Z" {
		t.Errorf("as_of %v override %q", rep.AsOf, rep.Options.AsOfOverride)
	}
}

// ---- reproducibility and audit ---------------------------------------------------------

func TestSameInputsSameReport(t *testing.T) {
	inRepoRoot(t)
	a, _ := report.JSON(run(t, vectorOpts("violations")))
	b, _ := report.JSON(run(t, vectorOpts("violations")))
	if !bytes.Equal(a, b) {
		t.Fatal("two runs on identical inputs produced different report.json")
	}
}

func TestAuditRecordHashesMatchFilesOnDisk(t *testing.T) {
	inRepoRoot(t)
	out := t.TempDir()
	opts := vectorOpts("violations")
	paths, err := report.Write(out, run(t, opts))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(paths.Audit) != "audit-20270201T213000Z.json" {
		t.Errorf("audit name %s", filepath.Base(paths.Audit))
	}
	var audit report.Audit
	raw, _ := os.ReadFile(paths.Audit)
	if err := json.Unmarshal(raw, &audit); err != nil {
		t.Fatal(err)
	}
	wantInputs := map[string]string{
		"reserves": opts.ReservesPath, "supply": opts.SupplyPath, "redemptions": opts.RedemptionsPath,
		"rules": opts.RulesPath, "holiday_calendar": "config/holidays.yaml",
	}
	if len(audit.Inputs) != len(wantInputs) {
		t.Fatalf("inputs: %+v", audit.Inputs)
	}
	for _, in := range audit.Inputs {
		path, ok := wantInputs[in.Role]
		if !ok || in.Path != path || in.SHA256 != sha(t, path) {
			t.Errorf("input %s: recorded %s %s, disk %s", in.Role, in.Path, in.SHA256, sha(t, path))
		}
	}
	if len(audit.Outputs) != 3 {
		t.Fatalf("outputs: %+v", audit.Outputs)
	}
	for _, o := range audit.Outputs {
		if o.SHA256 != sha(t, filepath.Join(out, o.Path)) {
			t.Errorf("output %s hash does not match the file", o.Path)
		}
	}
	if audit.ToolVersion == "" || audit.Result.Status != report.StatusFail || audit.Result.ExitCode != 1 ||
		len(audit.Result.Findings) != 7 || audit.Result.Redemption == nil || len(audit.Result.Unverified) != 0 {
		t.Errorf("audit result: %+v", audit.Result)
	}
	if !audit.Timestamp.Equal(fixedNow) || !strings.Contains(audit.Disclaimer, "Not legal advice") {
		t.Errorf("timestamp/disclaimer: %v %q", audit.Timestamp, audit.Disclaimer)
	}
}

func TestEveryOutputCarriesTheDisclaimer(t *testing.T) {
	inRepoRoot(t)
	out := t.TempDir()
	paths, err := report.Write(out, run(t, vectorOpts("clean")))
	if err != nil {
		t.Fatal(err)
	}
	md, _ := os.ReadFile(paths.Markdown)
	if strings.Count(string(md), meta.Disclaimer) < 2 || !strings.HasPrefix(string(md), "# reservelint report\n\n> "+meta.Disclaimer) {
		t.Error("markdown should open and close with the disclaimer")
	}
	for _, p := range []string{paths.JSON, paths.Audit} {
		if data, _ := os.ReadFile(p); !strings.Contains(string(data), meta.Disclaimer) {
			t.Errorf("%s lacks the disclaimer", p)
		}
	}
}
