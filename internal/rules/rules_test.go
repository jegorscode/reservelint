package rules_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jegorscode/reservelint/internal/rules"
	"github.com/jegorscode/reservelint/internal/testutil"
)

func shipped(t *testing.T) *rules.RuleSet {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(testutil.RepoRoot(), "rules", "rules.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	set, err := rules.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func TestShippedRulesVerifiedFlags(t *testing.T) {
	set := shipped(t)
	got := map[string]bool{}
	for _, r := range set.Rules {
		got[r.ID] = r.IsVerified()
		if r.IsVerified() == strings.HasPrefix(r.Source, "UNVERIFIED") {
			t.Errorf("%s: verified flag and citation disagree", r.ID)
		}
	}
	for _, id := range []string{"RW-LIQ-001", "RW-CON-001", "RW-RED-001"} {
		if !got[id] {
			t.Errorf("%s should be verified", id)
		}
	}
	if got["RW-COV-001"] {
		t.Error("RW-COV-001 should stay unverified")
	}
}

func TestShippedParametersAreExact(t *testing.T) {
	set := shipped(t)
	liq, _ := set.Get("RW-LIQ-001")
	d, err := liq.Decimal("min_immediately_available_share")
	if err != nil || d.String() != "0.05" {
		t.Fatalf("liquidity = %v, %v", d, err)
	}
	con, _ := set.Get("RW-CON-001")
	if d, _ := con.Decimal("max_counterparty_share"); d.String() != "0.4" {
		t.Fatalf("concentration = %v", d)
	}
	red, _ := set.Get("RW-RED-001")
	if n, err := red.Int("max_business_days"); err != nil || n != 2 {
		t.Fatalf("redemption days = %d, %v", n, err)
	}
	for param, want := range map[string]string{
		"clock_start":      "max(request_ts, onboarded_ts)",
		"clock_end":        "transfer_initiated_ts",
		"timezone":         "America/New_York",
		"holiday_calendar": "config/holidays.yaml",
	} {
		if got, err := red.String(param); err != nil || got != want {
			t.Errorf("%s = %q, %v", param, got, err)
		}
	}
	if n, err := red.Int("near_breach_margin_business_days"); err != nil || n != 1 {
		t.Errorf("near-breach margin = %d, %v", n, err)
	}
}

const header = "schema_version: 1\nrules:\n"

func rule(extra string) string {
	return "  - id: R1\n    description: d\n    source: s\n    verified: false\n" + extra
}

func TestFloatLookingParameterIsNotRoundedThroughFloat(t *testing.T) {
	doc := header + rule("    parameters:\n      x: 0.1\n      big: 12345678901234567890.123456789\n")
	set, err := rules.Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	r := set.Rules[0]
	if d, _ := r.Decimal("x"); d.String() != "0.1" {
		t.Errorf("x = %s", d)
	}
	if d, _ := r.Decimal("big"); d.String() != "12345678901234567890.123456789" {
		t.Errorf("big = %s", d)
	}
}

func TestParameterErrors(t *testing.T) {
	set, err := rules.Parse([]byte(header + rule("    parameters:\n      n: null\n      s: hello\n      f: 1.5\n")))
	if err != nil {
		t.Fatal(err)
	}
	r := set.Rules[0]
	if _, err := r.Decimal("n"); err == nil {
		t.Error("null accepted")
	}
	if _, err := r.Decimal("missing"); err == nil {
		t.Error("missing accepted")
	}
	if _, err := r.Decimal("s"); err == nil {
		t.Error("string accepted as number")
	}
	if _, err := r.Int("f"); err == nil {
		t.Error("1.5 accepted as int")
	}
	if r.Has("n") || r.Has("missing") || !r.Has("s") {
		t.Error("Has wrong")
	}
}

func TestParseRejectsBadDocuments(t *testing.T) {
	cases := map[string]string{
		"empty":            "",
		"wrong version":    "schema_version: 2\nrules:\n" + rule(""),
		"no rules":         "schema_version: 1\nrules: []\n",
		"duplicate ids":    header + rule("") + rule(""),
		"verified missing": header + "  - id: R1\n    description: d\n    source: s\n",
		"source missing":   header + "  - id: R1\n    description: d\n    verified: true\n",
		"unknown field":    header + rule("    surprise: 1\n"),
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := rules.Parse([]byte(doc)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestGetAndUnverified(t *testing.T) {
	set := shipped(t)
	if _, ok := set.Get("nope"); ok {
		t.Error("found nonexistent rule")
	}
	if len(set.Unverified()) == 0 || len(set.Unverified()) >= len(set.Rules) {
		t.Errorf("unverified count %d of %d", len(set.Unverified()), len(set.Rules))
	}
}
