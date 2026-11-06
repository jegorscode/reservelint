// Package config loads files from disk and fingerprints them for the audit log.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/jegorscode/reservelint/internal/rules"
)

// LoadedRules is a parsed rules file together with the hash of its exact bytes.
type LoadedRules struct {
	Path   string
	SHA256 string
	Set    *rules.RuleSet
}

// HashBytes returns the lowercase hex SHA-256 of data.
func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// HashFile returns the lowercase hex SHA-256 of a file's bytes.
func HashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return HashBytes(data), nil
}

// LoadRules reads, hashes and validates a rules file in a single read, so the hash always
// describes the bytes that were parsed.
func LoadRules(path string) (*LoadedRules, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read rules: %w", err)
	}
	set, err := rules.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &LoadedRules{Path: path, SHA256: HashBytes(data), Set: set}, nil
}
