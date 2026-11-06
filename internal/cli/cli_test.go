package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jegorscode/reservelint/internal/cli"
	"github.com/jegorscode/reservelint/internal/testutil"
)

func run(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := cli.Execute(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func vec(scenario string) []string {
	dir := filepath.Join(testutil.RepoRoot(), "testdata", "vectors", scenario)
	return []string{
		"--rules", filepath.Join(testutil.RepoRoot(), "rules", "rules.yaml"),
		"--holidays", filepath.Join(testutil.RepoRoot(), "config", "holidays.yaml"),
		"--reserves", filepath.Join(dir, "reserves.csv"),
		"--supply", filepath.Join(dir, "supply.csv"),
		"--redemptions", filepath.Join(dir, "redemptions.csv"),
	}
}

func check(out string, extra ...string) []string {
	args := append([]string{"check", "--out", out}, vec("clean")...)
	return append(args, extra...)
}

func TestGenerateSynthetic(t *testing.T) {
	dir := t.TempDir()
	code, out, _ := run("generate-synthetic", "--out", dir, "--seed", "1")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "violations", "reserves.csv")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Not legal advice") {
		t.Errorf("disclaimer missing: %s", out)
	}
}

func TestHelpCarriesDisclaimerAndExitCodes(t *testing.T) {
	code, out, _ := run("--help")
	if code != 0 || !strings.Contains(out, "Not legal advice") {
		t.Fatalf("exit %d out %q", code, out)
	}
	if _, out, _ := run("check", "--help"); !strings.Contains(out, "Exit codes: 0 pass or warning, 1") {
		t.Errorf("check help lacks exit codes: %s", out)
	}
}

func TestCheckCleanExitsZeroAndWritesFourFiles(t *testing.T) {
	dir := t.TempDir()
	code, out, errOut := run(check(dir)...)
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errOut)
	}
	if !strings.HasPrefix(out, "PASS: 0 fail, 0 warning, 5 pass") || !strings.Contains(out, "Not legal advice") {
		t.Errorf("stdout: %s", out)
	}
	for _, name := range []string{"report.md", "findings.csv", "report.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Error(err)
		}
	}
	if audits, _ := filepath.Glob(filepath.Join(dir, "audit-*.json")); len(audits) != 1 {
		t.Errorf("audit files: %v", audits)
	}
}

func TestCheckViolationsExitsOne(t *testing.T) {
	dir := t.TempDir()
	args := append([]string{"check", "--out", dir}, vec("violations")...)
	code, out, errOut := run(args...)
	if code != 1 || !strings.HasPrefix(out, "FAIL: 4 fail, 1 warning, 1 pass") || errOut != "" {
		t.Fatalf("exit %d stdout %q stderr %q", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "report.md")); err != nil {
		t.Error("a FAIL run must still write the report")
	}
}

func TestAllowDuplicateRowsFlag(t *testing.T) {
	args := append([]string{"check", "--out", t.TempDir()}, vec("violations")...)
	if code, out, _ := run(append(args, "--allow-duplicate-rows")...); code != 1 || !strings.HasPrefix(out, "FAIL: 4 fail, 0 warning, 1 pass") {
		t.Fatalf("with the flag: exit %d stdout %q", code, out)
	}
}

func TestInvalidRowsExitTwoUnlessAllowed(t *testing.T) {
	dir := t.TempDir()
	good, _ := os.ReadFile(filepath.Join(testutil.RepoRoot(), "testdata", "vectors", "clean", "reserves.csv"))
	bad := filepath.Join(dir, "reserves.csv")
	if err := os.WriteFile(bad, append(good, []byte("cash_deposit,-1,Bank X,,true\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	args := check(filepath.Join(dir, "out"), "--reserves", bad) // later flag wins
	code, out, errOut := run(args...)
	if code != 2 || out != "" || !strings.Contains(errOut, "--allow-invalid-rows") {
		t.Fatalf("exit %d stdout %q stderr %q", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "out")); err == nil {
		t.Error("nothing should be written when the run is refused")
	}
	code, out, _ = run(append(args, "--allow-invalid-rows")...)
	if code != 0 || !strings.HasPrefix(out, "WARNING") || !strings.Contains(out, "PARTIAL RUN") {
		t.Fatalf("exit %d stdout %q", code, out)
	}
}

func TestUsageAndInputErrorsExitTwo(t *testing.T) {
	cases := map[string][]string{
		"unknown command":    {"nonsense"},
		"missing flags":      {"check"},
		"bad generate fmt":   {"generate-synthetic", "--format", "xml", "--out", t.TempDir()},
		"bad as-of":          check(t.TempDir(), "--as-of", "yesterday"),
		"missing input file": check(t.TempDir(), "--supply", filepath.Join(t.TempDir(), "nope.csv")),
		"missing rules":      check(t.TempDir(), "--rules", filepath.Join(t.TempDir(), "nope.yaml")),
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			if code, _, errOut := run(args...); code != 2 || errOut == "" {
				t.Fatalf("exit %d stderr %q", code, errOut)
			}
		})
	}
}
