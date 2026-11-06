package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jegorscode/reservelint/internal/cli"
)

func run(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := cli.Execute(args, &out, &errOut)
	return code, out.String(), errOut.String()
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

func TestCheckIsAStub(t *testing.T) {
	if code, _, errOut := run("check"); code != 2 || !strings.Contains(errOut, "not implemented") {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
}

func TestHelpCarriesDisclaimer(t *testing.T) {
	code, out, _ := run("--help")
	if code != 0 || !strings.Contains(out, "Not legal advice") {
		t.Fatalf("exit %d out %q", code, out)
	}
}

func TestBadInput(t *testing.T) {
	if code, _, _ := run("generate-synthetic", "--format", "xml", "--out", t.TempDir()); code != 1 {
		t.Errorf("bad format exit %d", code)
	}
	if code, _, _ := run("nonsense"); code != 1 {
		t.Errorf("unknown command exit %d", code)
	}
}
