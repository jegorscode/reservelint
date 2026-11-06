package engine_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jegorscode/reservelint/internal/config"
	"github.com/jegorscode/reservelint/internal/engine"
	"github.com/jegorscode/reservelint/internal/ingest"
	"github.com/jegorscode/reservelint/internal/model"
	"github.com/jegorscode/reservelint/internal/rules"
	"github.com/jegorscode/reservelint/internal/testutil"
	"github.com/shopspring/decimal"
)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func rule(check string, verified bool, params string) rules.Rule {
	doc := "schema_version: 1\nrules:\n  - id: T-1\n    check: " + check +
		"\n    description: test\n    source: test source\n    verified: " + map[bool]string{true: "true", false: "false"}[verified] +
		"\n    parameters:\n" + params
	set, err := rules.Parse([]byte(doc))
	if err != nil {
		panic(err)
	}
	return set.Rules[0]
}

var (
	cov = rule("coverage", true, "      min_coverage_ratio: 1.0\n")
	liq = rule("liquidity_floor", true, "      min_immediately_available_share: 0.05\n")
	con = rule("counterparty_concentration", true, "      max_counterparty_share: 0.40\n")
)

func res(amount, cp string, available bool) model.ReserveRow {
	return model.ReserveRow{AssetType: "x", Amount: dec(amount), Counterparty: cp, IsImmediatelyAvailable: available}
}

func supply(amount string) model.SupplyRow {
	return model.SupplyRow{AsOf: time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC), CirculatingSupply: dec(amount)}
}

func mustFinding(t *testing.T) func(engine.Finding, error) engine.Finding {
	return func(f engine.Finding, err error) engine.Finding {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
}

// ---- coverage --------------------------------------------------------------------------

func TestCoverageBoundaries(t *testing.T) {
	m := mustFinding(t)
	f := m(engine.CheckCoverage(cov, []model.ReserveRow{res("100", "A", false)}, supply("100")))
	if !f.Passed || !f.Observed.Equal(dec("1")) || f.Details["shortfall"] != "0" {
		t.Errorf("exactly at limit: %+v", f)
	}
	f = m(engine.CheckCoverage(cov, []model.ReserveRow{res("99.99", "A", false)}, supply("100.00")))
	if f.Passed || f.Details["shortfall"] != "0.01" {
		t.Errorf("one cent short: %+v", f)
	}
	f = m(engine.CheckCoverage(cov, []model.ReserveRow{res("150", "A", false)}, supply("100")))
	if !f.Passed || f.Details["shortfall"] != "0" {
		t.Errorf("surplus: %+v", f)
	}
	f = m(engine.CheckCoverage(cov, []model.ReserveRow{res("60", "A", false), res("40", "B", false)}, supply("100")))
	if !f.Passed {
		t.Error("rows should be summed")
	}
}

// ---- liquidity -------------------------------------------------------------------------

func TestLiquidityBoundaries(t *testing.T) {
	m := mustFinding(t)
	f := m(engine.CheckLiquidityFloor(liq, []model.ReserveRow{res("5", "A", true), res("95", "B", false)}))
	if !f.Passed || !f.Observed.Equal(dec("0.05")) {
		t.Errorf("exactly 5%%: %+v", f)
	}
	f = m(engine.CheckLiquidityFloor(liq, []model.ReserveRow{res("4.99", "A", true), res("95.01", "B", false)}))
	if f.Passed {
		t.Error("4.99% passed")
	}
	f = m(engine.CheckLiquidityFloor(liq, []model.ReserveRow{res("10", "A", false)}))
	if f.Passed || !f.Observed.IsZero() {
		t.Errorf("none available: %+v", f)
	}
}

// A quotient rounded to 16 places would equal 0.05 here; the exact comparison must still fail.
func TestLiquidityJustBelowLimitDoesNotRoundOntoIt(t *testing.T) {
	m := mustFinding(t)
	f := m(engine.CheckLiquidityFloor(liq, []model.ReserveRow{
		res("4.99999999999999999999", "A", true), res("95.00000000000000000001", "B", false),
	}))
	if f.Passed {
		t.Fatalf("passed with observed %s", f.Observed)
	}
	if !f.Observed.Equal(dec("0.05")) {
		t.Fatalf("test premise broken: observed %s should display as 0.05", f.Observed)
	}
}

func TestFloatThresholdHasNoBinaryError(t *testing.T) {
	m := mustFinding(t)
	tenth := rule("liquidity_floor", true, "      min_immediately_available_share: 0.1\n")
	f := m(engine.CheckLiquidityFloor(tenth, []model.ReserveRow{res("10", "A", true), res("90", "B", false)}))
	if !f.Passed || !f.Threshold.Equal(dec("0.1")) {
		t.Fatalf("%+v", f)
	}
}

// ---- concentration ---------------------------------------------------------------------

func TestConcentrationBoundaries(t *testing.T) {
	m := mustFinding(t)
	f := m(engine.CheckCounterpartyConcentration(con, []model.ReserveRow{res("40", "A", false), res("30", "B", false), res("30", "C", false)}))
	if !f.Passed || !f.Observed.Equal(dec("0.4")) {
		t.Errorf("exactly 40%%: %+v", f)
	}
	f = m(engine.CheckCounterpartyConcentration(con, []model.ReserveRow{res("40.01", "A", false), res("29.99", "B", false), res("30", "C", false)}))
	if f.Passed || f.Details["top_counterparty"] != "A" {
		t.Errorf("40.01%%: %+v", f)
	}
	f = m(engine.CheckCounterpartyConcentration(con, []model.ReserveRow{res("10", "Only", false)}))
	if f.Passed || !f.Observed.Equal(dec("1")) {
		t.Errorf("single counterparty: %+v", f)
	}
}

func TestConcentrationMergesNameVariants(t *testing.T) {
	m := mustFinding(t)
	loose := rule("counterparty_concentration", true, "      max_counterparty_share: 0.60\n")
	f := m(engine.CheckCounterpartyConcentration(loose, []model.ReserveRow{
		res("25", "Bank A", false), res("25", "  bank   a ", false), res("50", "Bank B", false),
	}))
	shares := f.Details["shares"].(map[string]string)
	if !f.Observed.Equal(dec("0.5")) || shares["Bank A"] != "0.5" || f.Details["top_counterparty"] != "Bank A" {
		t.Errorf("%+v", f)
	}
}

func TestConcentrationTieIsDeterministic(t *testing.T) {
	m := mustFinding(t)
	for i := 0; i < 20; i++ {
		f := m(engine.CheckCounterpartyConcentration(con, []model.ReserveRow{res("50", "Zed", false), res("50", "Abe", false)}))
		if f.Details["top_counterparty"] != "Abe" {
			t.Fatalf("tie broke to %v", f.Details["top_counterparty"])
		}
	}
}

// ---- plumbing --------------------------------------------------------------------------

func TestMissingParameterAndEmptyInput(t *testing.T) {
	noParam := rule("coverage", true, "      other: 1\n")
	if _, err := engine.CheckCoverage(noParam, []model.ReserveRow{res("1", "A", false)}, supply("1")); err == nil {
		t.Error("missing parameter accepted")
	}
	nullParam := rule("coverage", true, "      min_coverage_ratio: null\n")
	if _, err := engine.CheckCoverage(nullParam, []model.ReserveRow{res("1", "A", false)}, supply("1")); err == nil {
		t.Error("null parameter accepted")
	}
	if _, err := engine.CheckLiquidityFloor(liq, nil); err == nil {
		t.Error("no reserves accepted")
	}
}

func TestFindingCarriesRuleMetadata(t *testing.T) {
	unverified := rule("coverage", false, "      min_coverage_ratio: 1\n")
	f, err := engine.CheckCoverage(unverified, []model.ReserveRow{res("1", "A", false)}, supply("1"))
	if err != nil {
		t.Fatal(err)
	}
	if f.RuleID != "T-1" || f.Citation != "test source" || f.RuleVerified || f.Comparator != engine.AtLeast || f.Status() != "pass" || f.Check != "coverage" {
		t.Fatalf("%+v", f)
	}
}

// ---- regression: legitimate identical positions ----------------------------------------

// Two $50M deposits at one bank that differ only by holding_id must both count toward reserves.
// Before holding_id existed they were merged, understating reserves and failing coverage.
func TestIdenticalDepositsWithDistinctHoldingIDsBothCountTowardCoverage(t *testing.T) {
	m := mustFinding(t)
	path := filepath.Join(t.TempDir(), "r.csv")
	body := "holding_id,asset_type,amount,counterparty,is_immediately_available\n" +
		"HLD-00001,cash_deposit,50000000.00,Bank A,true\nHLD-00002,cash_deposit,50000000.00,Bank A,true\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := ingest.LoadReserves(path)
	if err != nil {
		t.Fatal(err)
	}
	f := m(engine.CheckCoverage(cov, res.Rows, supply("100000000")))
	if !f.Passed || !f.Observed.Equal(dec("1")) || f.Details["total_reserves"] != "100000000" {
		t.Fatalf("both deposits should count: %+v", f)
	}

	// The old behaviour, for contrast: merging them halves reserves and the check fails.
	legacy, _ := ingest.LoadReservesWith(path, ingest.Options{DropIdenticalUnkeyed: true})
	if len(legacy.Rows) != 2 {
		t.Fatalf("keyed rows must never be merged, even in legacy mode: %d rows", len(legacy.Rows))
	}
	unkeyed := "asset_type,amount,counterparty,is_immediately_available\ncash_deposit,50000000.00,Bank A,true\ncash_deposit,50000000.00,Bank A,true\n"
	if err := os.WriteFile(path, []byte(unkeyed), 0o644); err != nil {
		t.Fatal(err)
	}
	legacy, _ = ingest.LoadReservesWith(path, ingest.Options{DropIdenticalUnkeyed: true})
	f = m(engine.CheckCoverage(cov, legacy.Rows, supply("100000000")))
	if f.Passed {
		t.Fatal("legacy merge of unkeyed rows should understate reserves")
	}
}

// ---- parity with the Python prototype --------------------------------------------------

type expected struct {
	Scenarios map[string]struct {
		ReserveRowsRead   int `json:"reserve_rows_read"`
		ReserveDuplicates int `json:"reserve_duplicates"`
		Findings          []struct {
			RuleID        string `json:"rule_id"`
			Check         string `json:"check"`
			Passed        bool   `json:"passed"`
			Comparator    string `json:"comparator"`
			Observed10dp  string `json:"observed_10dp"`
			Threshold     string `json:"threshold"`
			TotalReserves string `json:"total_reserves"`
		} `json:"findings"`
	} `json:"scenarios"`
}

func TestParityWithPythonVectors(t *testing.T) {
	root := testutil.RepoRoot()
	raw, err := os.ReadFile(filepath.Join(root, "testdata", "vectors", "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	var want expected
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadRules(filepath.Join(root, "rules", "rules.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for name, sc := range want.Scenarios {
		dir := filepath.Join(root, "testdata", "vectors", name)
		// The Python prototype dropped identical rows; reproduce that so numbers can be compared.
		reserves, err := ingest.LoadReservesWith(filepath.Join(dir, "reserves.csv"), ingest.Options{DropIdenticalUnkeyed: true})
		if err != nil {
			t.Fatal(err)
		}
		sup, err := ingest.LoadSupply(filepath.Join(dir, "supply.csv"))
		if err != nil {
			t.Fatal(err)
		}
		latest, _ := ingest.LatestSupply(sup.Rows)
		if reserves.RowsRead != sc.ReserveRowsRead || reserves.DuplicateCount() != sc.ReserveDuplicates {
			t.Errorf("%s: read %d dupes %d, python read %d dupes %d", name,
				reserves.RowsRead, reserves.DuplicateCount(), sc.ReserveRowsRead, sc.ReserveDuplicates)
		}
		got, err := engine.RunReserveChecks(loaded.Set, reserves.Rows, latest)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(sc.Findings) {
			t.Fatalf("%s: %d findings, python %d", name, len(got), len(sc.Findings))
		}
		for i, w := range sc.Findings {
			g := got[i]
			if g.Check != w.Check || g.Passed != w.Passed || string(g.Comparator) != w.Comparator {
				t.Errorf("%s/%s: got %s passed=%v, python passed=%v", name, w.Check, g.Check, g.Passed, w.Passed)
			}
			if g.Observed.StringFixed(10) != w.Observed10dp {
				t.Errorf("%s/%s: observed %s, python %s", name, w.Check, g.Observed.StringFixed(10), w.Observed10dp)
			}
			if !g.Threshold.Equal(dec(w.Threshold)) {
				t.Errorf("%s/%s: threshold %s, python %s", name, w.Check, g.Threshold, w.Threshold)
			}
			if tr, _ := g.Details["total_reserves"].(string); !dec(tr).Equal(dec(w.TotalReserves)) {
				t.Errorf("%s/%s: total %s, python %s", name, w.Check, tr, w.TotalReserves)
			}
		}
	}
}

func TestEndToEndOnGoSyntheticData(t *testing.T) {
	root := testutil.RepoRoot()
	loaded, err := config.LoadRules(filepath.Join(root, "rules", "rules.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	// The vectors are Python-generated; the clean one must pass and the violations one must fail.
	for name, wantPass := range map[string]bool{"clean": true, "violations": false} {
		dir := filepath.Join(root, "testdata", "vectors", name)
		r, _ := ingest.LoadReserves(filepath.Join(dir, "reserves.csv"))
		s, _ := ingest.LoadSupply(filepath.Join(dir, "supply.csv"))
		latest, _ := ingest.LatestSupply(s.Rows)
		fs, err := engine.RunReserveChecks(loaded.Set, r.Rows, latest)
		if err != nil || len(fs) != 3 {
			t.Fatalf("%s: %v (%d findings)", name, err, len(fs))
		}
		for _, f := range fs {
			if f.Passed != wantPass {
				t.Errorf("%s/%s: passed=%v", name, f.Check, f.Passed)
			}
		}
		if !fs[0].RuleVerified {
			t.Errorf("%s: coverage is verified against the codified text", name)
		}
	}
}
