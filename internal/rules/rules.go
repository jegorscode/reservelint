// Package rules defines the rule schema. No regulatory figure lives in Go code: checks read
// their parameters from a Rule loaded from rules/rules.yaml.
package rules

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/shopspring/decimal"
	"gopkg.in/yaml.v3"
)

// SupportedSchemaVersion is the only rules.yaml schema_version this build understands.
const SupportedSchemaVersion = 1

// Rule is one entry of rules.yaml.
type Rule struct {
	ID          string               `yaml:"id"`
	Check       *string              `yaml:"check"` // nil: tracked but not implemented
	Description string               `yaml:"description"`
	Parameters  map[string]yaml.Node `yaml:"parameters"`
	Source      string               `yaml:"source"` // citation to the rule text
	Verified    *bool                `yaml:"verified"`
	Notes       string               `yaml:"notes"`
}

// RuleSet is the parsed rules file.
type RuleSet struct {
	SchemaVersion int    `yaml:"schema_version"`
	Rules         []Rule `yaml:"rules"`
}

// IsVerified reports whether the owner has confirmed the rule against the rule text.
func (r Rule) IsVerified() bool { return r.Verified != nil && *r.Verified }

// CheckName returns the implementing check's name, or "" if none.
func (r Rule) CheckName() string {
	if r.Check == nil {
		return ""
	}
	return *r.Check
}

// Decimal returns a numeric parameter parsed exactly from its YAML text (never via float64).
func (r Rule) Decimal(name string) (decimal.Decimal, error) {
	n, err := r.scalar(name)
	if err != nil {
		return decimal.Decimal{}, err
	}
	if n.Tag != "!!int" && n.Tag != "!!float" {
		return decimal.Decimal{}, fmt.Errorf("rule %s: parameter %q is not a number", r.ID, name)
	}
	d, err := decimal.NewFromString(n.Value)
	if err != nil {
		return decimal.Decimal{}, fmt.Errorf("rule %s: parameter %q: %w", r.ID, name, err)
	}
	return d, nil
}

// Int returns a whole-number parameter.
func (r Rule) Int(name string) (int, error) {
	d, err := r.Decimal(name)
	if err != nil {
		return 0, err
	}
	if !d.IsInteger() {
		return 0, fmt.Errorf("rule %s: parameter %q must be a whole number", r.ID, name)
	}
	return int(d.IntPart()), nil
}

// String returns a text parameter.
func (r Rule) String(name string) (string, error) {
	n, err := r.scalar(name)
	if err != nil {
		return "", err
	}
	return n.Value, nil
}

// Has reports whether the parameter exists and is not null.
func (r Rule) Has(name string) bool {
	n, ok := r.Parameters[name]
	return ok && n.Tag != "!!null"
}

func (r Rule) scalar(name string) (yaml.Node, error) {
	n, ok := r.Parameters[name]
	if !ok || n.Tag == "!!null" {
		return yaml.Node{}, fmt.Errorf("rule %s: parameter %q is missing or null", r.ID, name)
	}
	if n.Kind != yaml.ScalarNode {
		return yaml.Node{}, fmt.Errorf("rule %s: parameter %q must be a scalar", r.ID, name)
	}
	return n, nil
}

// Get returns the rule with the given id.
func (s *RuleSet) Get(id string) (Rule, bool) {
	for _, r := range s.Rules {
		if r.ID == id {
			return r, true
		}
	}
	return Rule{}, false
}

// Unverified lists rules not yet confirmed against the rule text.
func (s *RuleSet) Unverified() []Rule {
	var out []Rule
	for _, r := range s.Rules {
		if !r.IsVerified() {
			out = append(out, r)
		}
	}
	return out
}

// Validate checks structural requirements: version, unique ids, required fields.
func (s *RuleSet) Validate() error {
	var errs []error
	if s.SchemaVersion != SupportedSchemaVersion {
		errs = append(errs, fmt.Errorf("schema_version %d unsupported (want %d)", s.SchemaVersion, SupportedSchemaVersion))
	}
	if len(s.Rules) == 0 {
		errs = append(errs, errors.New("no rules defined"))
	}
	seen := map[string]bool{}
	for i, r := range s.Rules {
		where := fmt.Sprintf("rules[%d] (%s)", i, r.ID)
		switch {
		case strings.TrimSpace(r.ID) == "":
			errs = append(errs, fmt.Errorf("rules[%d]: id is required", i))
		case seen[r.ID]:
			errs = append(errs, fmt.Errorf("%s: duplicate id", where))
		}
		seen[r.ID] = true
		if strings.TrimSpace(r.Description) == "" {
			errs = append(errs, fmt.Errorf("%s: description is required", where))
		}
		if strings.TrimSpace(r.Source) == "" {
			errs = append(errs, fmt.Errorf("%s: source (citation) is required", where))
		}
		if r.Verified == nil {
			errs = append(errs, fmt.Errorf("%s: verified must be set explicitly", where))
		}
	}
	return errors.Join(errs...)
}

// Parse decodes and validates a rules document. Unknown fields are errors.
func Parse(data []byte) (*RuleSet, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var set RuleSet
	if err := dec.Decode(&set); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("rules file is empty")
		}
		return nil, fmt.Errorf("parse rules: %w", err)
	}
	if err := set.Validate(); err != nil {
		return nil, fmt.Errorf("invalid rules: %w", err)
	}
	return &set, nil
}
