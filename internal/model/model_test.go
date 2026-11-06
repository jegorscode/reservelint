package model_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jegorscode/reservelint/internal/model"
	"github.com/jegorscode/reservelint/internal/testutil"
)

func reserve(overrides map[string]string) map[string]string {
	m := map[string]string{
		"asset_type": "cash_deposit", "amount": "100.00", "counterparty": "Bank A",
		"maturity_date": "", "is_immediately_available": "true",
	}
	for k, v := range overrides {
		m[k] = v
	}
	return m
}

func TestParseReserveValid(t *testing.T) {
	row, err := model.ParseReserve(reserve(map[string]string{"maturity_date": "2027-03-01"}))
	if err != nil {
		t.Fatal(err)
	}
	if row.MaturityDate == nil || row.MaturityDate.Format("2006-01-02") != "2027-03-01" {
		t.Fatalf("maturity = %v", row.MaturityDate)
	}
	if !row.IsImmediatelyAvailable || row.Amount.String() != "100" {
		t.Fatalf("row = %+v", row)
	}
}

func TestParseReserveRejects(t *testing.T) {
	cases := map[string]map[string]string{
		"zero amount":     {"amount": "0"},
		"negative amount": {"amount": "-0.01"},
		"NaN":             {"amount": "NaN"},
		"Infinity":        {"amount": "Infinity"},
		"not a number":    {"amount": "abc"},
		"empty amount":    {"amount": ""},
		"empty cpty":      {"counterparty": ""},
		"blank cpty":      {"counterparty": "   "},
		"blank asset":     {"asset_type": ""},
		"bad date":        {"maturity_date": "03/01/2027"},
		"bad bool":        {"is_immediately_available": "maybe"},
		"unknown field":   {"surprise": "x"},
	}
	for name, override := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := model.ParseReserve(reserve(override)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestParseReserveMissingColumn(t *testing.T) {
	m := reserve(nil)
	delete(m, "counterparty")
	if _, err := model.ParseReserve(m); err == nil {
		t.Fatal("expected error for missing counterparty")
	}
}

func TestParseReserveReportsAllErrors(t *testing.T) {
	_, err := model.ParseReserve(reserve(map[string]string{"amount": "-1", "counterparty": ""}))
	if err == nil || !strings.Contains(err.Error(), "amount") || !strings.Contains(err.Error(), "counterparty") {
		t.Fatalf("want both errors, got %v", err)
	}
}

func TestBooleanSpellings(t *testing.T) {
	for _, v := range []string{"true", "TRUE", "t", "yes", "1", "on"} {
		row, err := model.ParseReserve(reserve(map[string]string{"is_immediately_available": v}))
		if err != nil || !row.IsImmediatelyAvailable {
			t.Errorf("%q: %v %v", v, row.IsImmediatelyAvailable, err)
		}
	}
	for _, v := range []string{"false", "0", "no", "off"} {
		row, err := model.ParseReserve(reserve(map[string]string{"is_immediately_available": v}))
		if err != nil || row.IsImmediatelyAvailable {
			t.Errorf("%q: %v %v", v, row.IsImmediatelyAvailable, err)
		}
	}
}

func redemption(overrides map[string]string) map[string]string {
	m := map[string]string{
		"request_ts": "2027-02-01T10:00:00Z", "onboarded_ts": "", "transfer_initiated_ts": "",
		"amount": "5", "status": "pending",
	}
	for k, v := range overrides {
		m[k] = v
	}
	return m
}

func TestTimestampsMustCarryOffset(t *testing.T) {
	for _, field := range []string{"request_ts", "onboarded_ts", "transfer_initiated_ts"} {
		if _, err := model.ParseRedemption(redemption(map[string]string{field: "2027-02-01T10:00:00"})); err == nil {
			t.Errorf("%s: naive timestamp accepted", field)
		}
	}
	if _, err := model.ParseSupply(map[string]string{"as_of": "2027-02-01T10:00:00", "circulating_supply": "1"}); err == nil {
		t.Error("naive supply timestamp accepted")
	}
}

func TestRedemptionBlankOptionalsAndStatus(t *testing.T) {
	row, err := model.ParseRedemption(redemption(map[string]string{"onboarded_ts": "  "}))
	if err != nil || row.OnboardedTS != nil || row.TransferInitiatedTS != nil {
		t.Fatalf("%+v %v", row, err)
	}
	if _, err := model.ParseRedemption(redemption(map[string]string{"status": "done"})); err == nil {
		t.Error("bad status accepted")
	}
	if _, err := model.ParseRedemption(redemption(map[string]string{"amount": "0"})); err == nil {
		t.Error("zero amount accepted")
	}
}

func TestSupplyRejectsNonPositive(t *testing.T) {
	for _, v := range []string{"0", "-5"} {
		if _, err := model.ParseSupply(map[string]string{"as_of": "2027-02-01T00:00:00Z", "circulating_supply": v}); err == nil {
			t.Errorf("supply %s accepted", v)
		}
	}
}

func mustReserve(t *testing.T, overrides map[string]string) model.ReserveRow {
	t.Helper()
	row, err := model.ParseReserve(reserve(overrides))
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func TestRowKeyIgnoresFormattingNoise(t *testing.T) {
	base := mustReserve(t, nil).RowKey()
	if len(base) != 64 {
		t.Fatalf("key length %d", len(base))
	}
	same := []map[string]string{
		{"amount": "100"}, {"amount": "100.000"}, {"counterparty": "  bank   a "}, {"is_immediately_available": "TRUE"},
	}
	for _, o := range same {
		if got := mustReserve(t, o).RowKey(); got != base {
			t.Errorf("%v changed key", o)
		}
	}
	differ := []map[string]string{
		{"amount": "101"}, {"counterparty": "Bank B"}, {"is_immediately_available": "false"}, {"maturity_date": "2027-03-01"},
	}
	for _, o := range differ {
		if got := mustReserve(t, o).RowKey(); got == base {
			t.Errorf("%v did not change key", o)
		}
	}
}

func TestRowKeySameInstantDifferentOffsets(t *testing.T) {
	a, _ := model.ParseRedemption(redemption(map[string]string{"request_ts": "2027-02-01T15:00:00Z"}))
	b, _ := model.ParseRedemption(redemption(map[string]string{"request_ts": "2027-02-01T10:00:00-05:00"}))
	c, _ := model.ParseRedemption(redemption(map[string]string{"request_ts": "2027-02-01T15:00:01Z"}))
	if a.RowKey() != b.RowKey() || a.RowKey() == c.RowKey() {
		t.Fatal("offset handling wrong")
	}
}

// ---- parity with the Python prototype vectors ------------------------------------------

type vectors struct {
	Scenarios map[string]struct {
		ReserveKeys    []string `json:"reserve_keys"`
		SupplyKeys     []string `json:"supply_keys"`
		RedemptionKeys []string `json:"redemption_keys"`
	} `json:"scenarios"`
	EdgeCases []struct {
		Input map[string]string `json:"input"`
		Key   string            `json:"key"`
	} `json:"reserve_key_edge_cases"`
}

func loadVectors(t *testing.T) (vectors, string) {
	t.Helper()
	dir := filepath.Join(testutil.RepoRoot(), "testdata", "vectors")
	raw, err := os.ReadFile(filepath.Join(dir, "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	var v vectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v, dir
}

func TestRowKeyParityEdgeCases(t *testing.T) {
	v, _ := loadVectors(t)
	if len(v.EdgeCases) == 0 {
		t.Fatal("no edge cases in vectors")
	}
	for i, c := range v.EdgeCases {
		row, err := model.ParseReserve(c.Input)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if got := row.RowKey(); got != c.Key {
			t.Errorf("case %d (%q): key %s, python %s", i, c.Input["counterparty"], got, c.Key)
		}
	}
}

func TestRowKeyParityScenarios(t *testing.T) {
	v, dir := loadVectors(t)
	for name, want := range v.Scenarios {
		seen := map[string]bool{}
		var reserveKeys []string
		for _, rec := range testutil.ReadCSV(t, filepath.Join(dir, name, "reserves.csv")) {
			row, err := model.ParseReserve(rec)
			if err != nil {
				t.Fatalf("%s reserves: %v", name, err)
			}
			if k := row.RowKey(); !seen[k] { // Python vectors list first occurrences only
				seen[k] = true
				reserveKeys = append(reserveKeys, k)
			}
		}
		assertKeys(t, name+" reserves", reserveKeys, want.ReserveKeys)

		var supplyKeys, redemptionKeys []string
		for _, rec := range testutil.ReadCSV(t, filepath.Join(dir, name, "supply.csv")) {
			row, err := model.ParseSupply(rec)
			if err != nil {
				t.Fatal(err)
			}
			supplyKeys = append(supplyKeys, row.RowKey())
		}
		assertKeys(t, name+" supply", supplyKeys, want.SupplyKeys)
		for _, rec := range testutil.ReadCSV(t, filepath.Join(dir, name, "redemptions.csv")) {
			row, err := model.ParseRedemption(rec)
			if err != nil {
				t.Fatal(err)
			}
			redemptionKeys = append(redemptionKeys, row.RowKey())
		}
		assertKeys(t, name+" redemptions", redemptionKeys, want.RedemptionKeys)
	}
}

func assertKeys(t *testing.T, what string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %d keys, want %d", what, len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s[%d]: %s, want %s", what, i, got[i], want[i])
		}
	}
}
