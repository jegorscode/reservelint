// Package testutil holds helpers shared by tests only.
package testutil

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// RepoRoot returns the repository root, found relative to this source file.
func RepoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// ReadCSV reads a CSV file into one map per data row.
func ReadCSV(t testing.TB, path string) []map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	recs, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]string
	for _, rec := range recs[1:] {
		row := map[string]string{}
		for i, h := range recs[0] {
			row[h] = rec[i]
		}
		rows = append(rows, row)
	}
	return rows
}
