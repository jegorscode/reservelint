package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jegorscode/reservelint/internal/config"
	"github.com/jegorscode/reservelint/internal/testutil"
)

func TestHashBytesKnownVector(t *testing.T) {
	// SHA-256("abc")
	const want = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got := config.HashBytes([]byte("abc")); got != want {
		t.Fatalf("got %s", got)
	}
}

func TestLoadRulesHashesExactBytes(t *testing.T) {
	path := filepath.Join(testutil.RepoRoot(), "rules", "rules.yaml")
	loaded, err := config.LoadRules(path)
	if err != nil {
		t.Fatal(err)
	}
	want, err := config.HashFile(path)
	if err != nil || loaded.SHA256 != want || len(loaded.Set.Rules) == 0 {
		t.Fatalf("hash %s want %s err %v", loaded.SHA256, want, err)
	}
}

func TestLoadRulesErrors(t *testing.T) {
	if _, err := config.LoadRules(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Error("missing file accepted")
	}
	bad := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(bad, []byte("schema_version: 1\nrules: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadRules(bad); err == nil {
		t.Error("invalid rules accepted")
	}
}
