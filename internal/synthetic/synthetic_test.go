package synthetic_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jegorscode/reservelint/internal/model"
	"github.com/jegorscode/reservelint/internal/synthetic"
	"github.com/jegorscode/reservelint/internal/testutil"
	"github.com/shopspring/decimal"
)

// stats returns total reserves, immediately-available share and largest counterparty share,
// de-duplicating rows the way ingestion will.
func stats(rows []model.ReserveRow) (total, availShare, maxShare decimal.Decimal) {
	seen := map[string]bool{}
	byCP := map[string]decimal.Decimal{}
	avail := decimal.Zero
	for _, r := range rows {
		if seen[r.RowKey()] {
			continue
		}
		seen[r.RowKey()] = true
		total = total.Add(r.Amount)
		byCP[r.Counterparty] = byCP[r.Counterparty].Add(r.Amount)
		if r.IsImmediatelyAvailable {
			avail = avail.Add(r.Amount)
		}
	}
	top := decimal.Zero
	for _, v := range byCP {
		if v.GreaterThan(top) {
			top = v
		}
	}
	return total, avail.Div(total), top.Div(total)
}

func TestDeterministic(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	for _, dir := range []string{a, b} {
		if _, err := synthetic.Generate(dir, 7, synthetic.CSV); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range []string{"clean/reserves.csv", "clean/redemptions.csv", "violations/redemptions.csv", "manifest.json"} {
		x, _ := os.ReadFile(filepath.Join(a, rel))
		y, _ := os.ReadFile(filepath.Join(b, rel))
		if len(x) == 0 || !bytes.Equal(x, y) {
			t.Errorf("%s differs between runs", rel)
		}
	}
}

func TestSeedChangesData(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	_, _ = synthetic.Generate(a, 1, synthetic.CSV)
	_, _ = synthetic.Generate(b, 2, synthetic.CSV)
	x, _ := os.ReadFile(filepath.Join(a, "clean/redemptions.csv"))
	y, _ := os.ReadFile(filepath.Join(b, "clean/redemptions.csv"))
	if bytes.Equal(x, y) {
		t.Fatal("different seeds gave identical data")
	}
}

func TestWrittenCSVParsesAndRoundTrips(t *testing.T) {
	dir := t.TempDir()
	if _, err := synthetic.Generate(dir, 3, synthetic.CSV); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{synthetic.Clean, synthetic.Violations} {
		want, _ := synthetic.Build(name, 3)
		recs := testutil.ReadCSV(t, filepath.Join(dir, name, "reserves.csv"))
		if len(recs) != len(want.Reserves) {
			t.Fatalf("%s: %d reserve rows, want %d", name, len(recs), len(want.Reserves))
		}
		for i, rec := range recs {
			row, err := model.ParseReserve(rec)
			if err != nil || row.RowKey() != want.Reserves[i].RowKey() {
				t.Fatalf("%s reserves[%d]: %v", name, i, err)
			}
		}
		for i, rec := range testutil.ReadCSV(t, filepath.Join(dir, name, "redemptions.csv")) {
			row, err := model.ParseRedemption(rec)
			if err != nil || row.RowKey() != want.Redemptions[i].RowKey() {
				t.Fatalf("%s redemptions[%d]: %v", name, i, err)
			}
		}
		for _, rec := range testutil.ReadCSV(t, filepath.Join(dir, name, "supply.csv")) {
			if _, err := model.ParseSupply(rec); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestJSONFormatParses(t *testing.T) {
	dir := t.TempDir()
	if _, err := synthetic.Generate(dir, 3, synthetic.JSON); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "violations", "redemptions.json"))
	if err != nil {
		t.Fatal(err)
	}
	var objs []map[string]string
	if err := json.Unmarshal(raw, &objs); err != nil || len(objs) == 0 {
		t.Fatalf("%v (%d rows)", err, len(objs))
	}
	for _, o := range objs {
		if _, err := model.ParseRedemption(o); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCleanScenarioLooksHealthy(t *testing.T) {
	for seed := int64(0); seed < 20; seed++ {
		d, _ := synthetic.Build(synthetic.Clean, seed)
		total, avail, maxShare := stats(d.Reserves)
		if total.LessThan(d.Supply[0].CirculatingSupply) {
			t.Errorf("seed %d: reserves below supply", seed)
		}
		if avail.LessThan(decimal.RequireFromString("0.15")) || maxShare.GreaterThanOrEqual(decimal.RequireFromString("0.30")) {
			t.Errorf("seed %d: avail %s max %s", seed, avail, maxShare)
		}
	}
}

func TestViolationsScenarioIsDeliberatelyBad(t *testing.T) {
	d, _ := synthetic.Build(synthetic.Violations, 42)
	total, avail, maxShare := stats(d.Reserves)
	if !total.LessThan(d.Supply[0].CirculatingSupply) {
		t.Error("expected a coverage shortfall")
	}
	if !avail.LessThan(decimal.RequireFromString("0.03")) || !maxShare.GreaterThan(decimal.RequireFromString("0.5")) {
		t.Errorf("avail %s max %s", avail, maxShare)
	}
	keys := map[string]bool{}
	for _, r := range d.Reserves {
		keys[r.RowKey()] = true
	}
	if len(d.Reserves)-len(keys) != 1 {
		t.Error("expected exactly one duplicate reserve row")
	}
	stuck := 0
	for _, r := range d.Redemptions {
		if r.TransferInitiatedTS == nil && r.Status == model.StatusPending && r.RequestTS.Before(synthetic.AsOf.AddDate(0, 0, -10)) {
			stuck++
		}
	}
	if stuck < 2 {
		t.Errorf("stuck redemptions = %d", stuck)
	}
}

func TestManifestAndErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := synthetic.Generate(dir, 1, synthetic.CSV); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if !strings.Contains(string(raw), `"synthetic": true`) || !strings.Contains(string(raw), "Not legal advice") {
		t.Errorf("manifest = %s", raw)
	}
	if _, err := synthetic.Build("nope", 1); err == nil {
		t.Error("unknown scenario accepted")
	}
	if _, err := synthetic.Generate(dir, 1, "xml"); err == nil {
		t.Error("unknown format accepted")
	}
}

func TestCausalityOnboardingThenRequestThenTransfer(t *testing.T) {
	for seed := int64(0); seed < 30; seed++ {
		for _, name := range []string{synthetic.Clean, synthetic.Violations} {
			d, _ := synthetic.Build(name, seed)
			for i, r := range d.Redemptions {
				if r.OnboardedTS == nil || !r.OnboardedTS.Before(r.RequestTS) {
					t.Fatalf("%s seed %d row %d: onboarding does not strictly precede the request", name, seed, i)
				}
				if r.RequestTS.After(synthetic.AsOf) {
					t.Fatalf("%s seed %d row %d: request is after the snapshot", name, seed, i)
				}
				if tr := r.TransferInitiatedTS; tr != nil {
					if !tr.After(r.RequestTS) || !tr.After(*r.OnboardedTS) || tr.After(synthetic.AsOf) {
						t.Fatalf("%s seed %d row %d: transfer %v breaks causality", name, seed, i, tr)
					}
				}
			}
		}
	}
}

func TestRedemptionOrderingIsSane(t *testing.T) {
	d, _ := synthetic.Build(synthetic.Violations, 5)
	for i, r := range d.Redemptions {
		if r.TransferInitiatedTS != nil && r.TransferInitiatedTS.Before(r.RequestTS) {
			t.Fatal("transfer before request")
		}
		if i > 0 && r.RequestTS.Before(d.Redemptions[i-1].RequestTS) {
			t.Fatal("redemptions not sorted by request time")
		}
	}
}
