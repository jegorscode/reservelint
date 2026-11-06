package ingest_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jegorscode/reservelint/internal/ingest"
)

const (
	header = "asset_type,amount,counterparty,maturity_date,is_immediately_available\n"
	rowA   = "cash_deposit,100.00,Bank A,,true\n"
	rowB   = "us_treasury_bill,50,Bank B,2027-03-01,false\n"
)

func write(t *testing.T, name, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadsValidCSV(t *testing.T) {
	res, err := ingest.LoadReserves(write(t, "r.csv", header+rowA+rowB))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 2 || res.Rows[0].Counterparty != "Bank A" || res.RowsRead != 2 {
		t.Fatalf("%+v", res)
	}
	if len(res.Errors) != 0 || len(res.Duplicates) != 0 || res.ErrorOrNil() != nil {
		t.Fatal("unexpected errors or duplicates")
	}
}

func TestCSVQuirks(t *testing.T) {
	text := "\xef\xbb\xbf" + "asset_type,amount,counterparty,is_immediately_available\r\n" +
		"cash,\"1,000.5\",\"Bank, \"\"A\"\"\",true\r\n"
	res, err := ingest.LoadReserves(write(t, "r.csv", text))
	if err != nil {
		t.Fatal(err)
	}
	// "1,000.5" is not a plain number: it must be rejected, not silently misread as 1.
	if len(res.Rows) != 0 || len(res.Errors) != 1 {
		t.Fatalf("rows %d errors %v", len(res.Rows), res.Errors)
	}
	res, err = ingest.LoadReserves(write(t, "r2.csv", "\xef\xbb\xbfasset_type,amount,counterparty,is_immediately_available\r\ncash,1000.5,\"Bank, \"\"A\"\"\",true\r\n"))
	if err != nil || len(res.Rows) != 1 || res.Rows[0].Counterparty != `Bank, "A"` {
		t.Fatalf("%v %+v", err, res)
	}
}

func TestLoadsJSONKeepingNumbersExact(t *testing.T) {
	doc := `[{"asset_type":"x","amount":0.1,"counterparty":"A","is_immediately_available":true},
	         {"asset_type":"y","amount":12345678901234567890.123456789,"counterparty":"B","is_immediately_available":false,"maturity_date":null}]`
	res, err := ingest.LoadReserves(write(t, "r.json", doc))
	if err != nil || len(res.Rows) != 2 || len(res.Errors) != 0 {
		t.Fatalf("%v %+v", err, res)
	}
	if res.Rows[0].Amount.String() != "0.1" || res.Rows[1].Amount.String() != "12345678901234567890.123456789" {
		t.Fatalf("amounts %s %s", res.Rows[0].Amount, res.Rows[1].Amount)
	}
}

func TestJSONBadItems(t *testing.T) {
	res, err := ingest.LoadReserves(write(t, "r.json", `[42, null, {"asset_type":["x"]}, {"asset_type":"x","amount":1,"counterparty":"A","is_immediately_available":true}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 1 || len(res.Errors) != 3 || res.Errors[0].Position != 1 {
		t.Fatalf("%+v", res)
	}
}

func TestDuplicatesAreNotDoubleCountedAndAreReported(t *testing.T) {
	res, _ := ingest.LoadReserves(write(t, "r.csv", header+rowA+rowB+rowA+rowA))
	if len(res.Rows) != 2 || res.DuplicateCount() != 2 {
		t.Fatalf("rows %d dupes %d", len(res.Rows), res.DuplicateCount())
	}
	d := res.Duplicates[0]
	if d.FirstPosition != 2 || len(d.RepeatPositions) != 2 || d.RepeatPositions[0] != 4 || d.RepeatPositions[1] != 5 {
		t.Fatalf("%+v", d)
	}
}

func TestIdempotentOnRepeatedContent(t *testing.T) {
	once, _ := ingest.LoadReserves(write(t, "a.csv", header+rowA+rowB))
	twice, _ := ingest.LoadReserves(write(t, "b.csv", header+rowA+rowB+rowA+rowB))
	if len(once.Rows) != len(twice.Rows) || twice.DuplicateCount() != 2 {
		t.Fatal("not idempotent")
	}
	for i := range once.Rows {
		if once.Rows[i].RowKey() != twice.Rows[i].RowKey() {
			t.Fatalf("row %d differs", i)
		}
	}
}

func TestDuplicateDetectionIgnoresFormatting(t *testing.T) {
	res, _ := ingest.LoadReserves(write(t, "r.csv", header+rowA+"cash_deposit,100,  bank a ,,TRUE\n"))
	if len(res.Rows) != 1 || res.DuplicateCount() != 1 {
		t.Fatalf("rows %d dupes %d", len(res.Rows), res.DuplicateCount())
	}
}

func TestInvalidRowsReportedWithLineNumbers(t *testing.T) {
	bad := []string{
		"cash,0,Bank A,,true\n",
		"cash,-5,Bank A,,true\n",
		"cash,abc,Bank A,,true\n",
		"cash,5,,,true\n",
		"cash,5,   ,,true\n",
		"cash,5,Bank A,not-a-date,true\n",
		"cash,5,Bank A,,maybe\n",
		"cash,5,Bank A,,true,extra\n",
		"cash,5\n",
		"cash,5,\"Bank A,,true\n", // unterminated quote
	}
	for _, b := range bad {
		res, err := ingest.LoadReserves(write(t, "r.csv", header+rowA+b))
		if err != nil {
			t.Fatalf("%q: %v", b, err)
		}
		if len(res.Rows) != 1 || len(res.Errors) != 1 || res.Errors[0].Position != 3 {
			t.Errorf("%q: rows %d errors %+v", b, len(res.Rows), res.Errors)
		}
		if res.ErrorOrNil() == nil {
			t.Errorf("%q: ErrorOrNil is nil", b)
		}
	}
}

func TestEmptyAndBrokenFilesRejected(t *testing.T) {
	files := map[string]string{
		"empty.csv":       "",
		"newline.csv":     "\n",
		"header-only.csv": header,
		"empty.json":      "[]",
		"nolist.json":     `{"a":1}`,
		"broken.json":     "{oops",
		"nocolumn.csv":    "asset_type,amount,is_immediately_available\nx,1,true\n",
		"data.txt":        "x",
	}
	for name, text := range files {
		_, err := ingest.LoadReserves(write(t, name, text))
		if !errors.Is(err, ingest.ErrFile) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := ingest.LoadReserves(filepath.Join(t.TempDir(), "missing.csv")); !errors.Is(err, ingest.ErrFile) {
		t.Errorf("missing file: %v", err)
	}
}

func TestSupplyAndRedemptionLoaders(t *testing.T) {
	sup, err := ingest.LoadSupply(write(t, "s.csv", "as_of,circulating_supply\n2027-02-01T00:00:00Z,10\n2027-02-03T00:00:00Z,12\n"))
	if err != nil {
		t.Fatal(err)
	}
	latest, err := ingest.LatestSupply(sup.Rows)
	if err != nil || latest.CirculatingSupply.String() != "12" {
		t.Fatalf("%v %v", latest, err)
	}
	if _, err := ingest.LatestSupply(nil); err == nil {
		t.Error("empty supply accepted")
	}
	red, err := ingest.LoadRedemptions(write(t, "red.csv",
		"request_ts,onboarded_ts,transfer_initiated_ts,amount,status\n"+
			"2027-02-01T10:00:00-05:00,,,5,pending\n"+
			"2027-02-01T10:00:00,,,5,pending\n"+
			"2027-02-01T15:00:00Z,,,5,pending\n"))
	if err != nil {
		t.Fatal(err)
	}
	// row 2 has a naive timestamp (rejected); row 3 is the same instant as row 1 (duplicate).
	if len(red.Rows) != 1 || len(red.Errors) != 1 || red.DuplicateCount() != 1 {
		t.Fatalf("%+v", red)
	}
}

func TestRedemptionOptionalColumnsMayBeAbsent(t *testing.T) {
	red, err := ingest.LoadRedemptions(write(t, "red.csv", "request_ts,amount,status\n2027-02-01T10:00:00Z,5,pending\n"))
	if err != nil || len(red.Rows) != 1 {
		t.Fatalf("%v %+v", err, red)
	}
}
