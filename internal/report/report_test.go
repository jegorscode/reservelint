package report_test

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jegorscode/reservelint/internal/report"
	"github.com/shopspring/decimal"
)

func finding(id, check, status string) report.Finding {
	return report.Finding{
		RuleID: id, Check: check, Description: "desc, with comma and | pipe", Status: status,
		Observed: decimal.RequireFromString("0.02"), Comparator: ">=", Threshold: decimal.RequireFromString("0.05"),
		Citation: "cite \"quoted\"", RuleVerified: true, Details: map[string]any{"k": "v"},
	}
}

func sample(findings ...report.Finding) *report.Report {
	r := &report.Report{
		Tool: "reservelint", Version: "test", Disclaimer: "DISCLAIMER TEXT",
		GeneratedAt: time.Date(2027, 2, 1, 21, 30, 0, 0, time.UTC), AsOf: time.Date(2027, 2, 1, 21, 0, 0, 0, time.UTC),
		Inputs: []report.Input{}, Findings: findings, NotChecked: []report.NotChecked{}, Caveats: []string{},
		Rules: report.FileInfo{Path: "rules.yaml", SHA256: "abc"},
	}
	r.Finalize()
	return r
}

func TestFinalizeStatusAndExitCode(t *testing.T) {
	cases := []struct {
		name     string
		findings []report.Finding
		partial  bool
		status   report.Status
		exit     int
	}{
		{"all pass", []report.Finding{finding("A", "c", "pass")}, false, report.StatusPass, 0},
		{"warning only", []report.Finding{finding("A", "c", "pass"), finding("B", "c", "warning")}, false, report.StatusWarning, 0},
		{"fail beats warning", []report.Finding{finding("A", "c", "warning"), finding("B", "c", "fail")}, false, report.StatusFail, 1},
		{"partial pass is only a warning", []report.Finding{finding("A", "c", "pass")}, true, report.StatusWarning, 0},
		{"partial fail is still a fail", []report.Finding{finding("A", "c", "fail")}, true, report.StatusFail, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := sample(tc.findings...)
			r.Partial = tc.partial
			r.Finalize()
			if r.Status != tc.status || r.ExitCode != tc.exit {
				t.Fatalf("%s exit %d", r.Status, r.ExitCode)
			}
		})
	}
}

func TestCSVEscapesAndRoundTrips(t *testing.T) {
	data, err := report.FindingsCSV(sample(finding("A", "coverage", "fail")))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	if err != nil || len(rows) != 2 {
		t.Fatalf("%v rows=%d", err, len(rows))
	}
	got := rows[1]
	if got[0] != "A" || got[2] != "fail" || got[3] != "0.02" || got[5] != "0.05" || got[7] != `cite "quoted"` || got[8] != "desc, with comma and | pipe" {
		t.Errorf("row = %q", got)
	}
}

func TestJSONRoundTripsAndKeepsDecimalsExact(t *testing.T) {
	r := sample(finding("A", "coverage", "fail"))
	r.Findings[0].Observed = decimal.RequireFromString("0.1234567890123456")
	data, err := report.JSON(r)
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		Status   string `json:"status"`
		ExitCode int    `json:"exit_code"`
		Findings []struct {
			Observed string `json:"observed"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.Status != "FAIL" || back.ExitCode != 1 || back.Findings[0].Observed != "0.1234567890123456" {
		t.Errorf("%+v", back)
	}
}

func TestMarkdownEscapesTableCells(t *testing.T) {
	md := string(report.Markdown(sample(finding("A|B", "coverage", "fail"))))
	if !strings.Contains(md, `| A\|B |`) {
		t.Errorf("pipe in rule id not escaped:\n%s", md)
	}
	if !strings.HasPrefix(md, "# reservelint report\n\n> DISCLAIMER TEXT\n") || !strings.HasSuffix(md, "---\nDISCLAIMER TEXT\n") {
		t.Error("disclaimer should open and close the report")
	}
}

func TestValueFormatting(t *testing.T) {
	md := string(report.Markdown(sample(finding("A", "coverage", "fail"))))
	if !strings.Contains(md, "| 2% | at least 5% |") {
		t.Errorf("ratio should render as a percentage:\n%s", md)
	}
}

func TestAuditLogIsNeverOverwritten(t *testing.T) {
	dir := t.TempDir()
	r := sample(finding("A", "coverage", "fail"))
	first, err := report.Write(dir, r)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(first.Audit)
	second, err := report.Write(dir, r) // same timestamp
	if err != nil {
		t.Fatal(err)
	}
	if first.Audit == second.Audit || filepath.Base(second.Audit) != "audit-20270201T213000Z-2.json" {
		t.Fatalf("audit paths: %s, %s", first.Audit, second.Audit)
	}
	if after, _ := os.ReadFile(first.Audit); string(after) != string(before) {
		t.Error("first audit record was modified")
	}
}

func TestWriteCreatesDirectoryAndOverwritesReportFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "out")
	r := sample(finding("A", "coverage", "fail"))
	p, err := report.Write(dir, r)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Markdown, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := report.Write(dir, r); err != nil {
		t.Fatal(err)
	}
	if md, _ := os.ReadFile(p.Markdown); string(md) == "stale" {
		t.Error("report.md was not refreshed")
	}
	if audits, _ := filepath.Glob(filepath.Join(dir, "audit-*.json")); len(audits) != 2 {
		t.Errorf("expected 2 audit files, got %d", len(audits))
	}
}

func TestWriteFailsOnUnwritableDirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := report.Write(filepath.Join(file, "sub"), sample(finding("A", "c", "pass"))); err == nil {
		t.Error("writing under a regular file should fail")
	}
}
