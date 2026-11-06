package report

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/jegorscode/reservelint/internal/config"
)

// Output file names inside the report directory.
const (
	FileMarkdown = "report.md"
	FileCSV      = "findings.csv"
	FileJSON     = "report.json"
)

// Paths lists the files written by Write.
type Paths struct {
	Markdown, CSV, JSON, Audit string
}

// FindingsCSV renders one row per finding for spreadsheet use.
func FindingsCSV(r *Report) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	header := []string{"rule_id", "check", "status", "observed", "comparator", "threshold", "rule_verified", "citation", "description"}
	if err := w.Write(header); err != nil {
		return nil, err
	}
	for _, f := range r.Findings {
		row := []string{f.RuleID, f.Check, f.Status, f.Observed.String(), f.Comparator, f.Threshold.String(),
			strconv.FormatBool(f.RuleVerified), f.Citation, f.Description}
		if err := w.Write(row); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

// JSON renders the machine-readable report.
func JSON(r *Report) ([]byte, error) { return marshal(r) }

func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Write renders report.md, findings.csv and report.json into dir (overwriting earlier ones)
// and then writes a new audit-<timestamp>.json that is never overwritten.
func Write(dir string, r *Report) (Paths, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Paths{}, fmt.Errorf("create report directory: %w", err)
	}
	csvBytes, err := FindingsCSV(r)
	if err != nil {
		return Paths{}, err
	}
	jsonBytes, err := JSON(r)
	if err != nil {
		return Paths{}, err
	}
	outputs := []struct {
		name string
		data []byte
	}{{FileMarkdown, Markdown(r)}, {FileCSV, csvBytes}, {FileJSON, jsonBytes}}

	var p Paths
	var written []AuditFile
	for _, o := range outputs {
		path := filepath.Join(dir, o.name)
		if err := os.WriteFile(path, o.data, 0o644); err != nil {
			return Paths{}, fmt.Errorf("write %s: %w", path, err)
		}
		written = append(written, AuditFile{Role: o.name, Path: o.name, SHA256: config.HashBytes(o.data)})
	}
	p.Markdown, p.CSV, p.JSON = filepath.Join(dir, FileMarkdown), filepath.Join(dir, FileCSV), filepath.Join(dir, FileJSON)

	auditBytes, err := marshal(BuildAudit(r, written))
	if err != nil {
		return Paths{}, err
	}
	if p.Audit, err = writeNew(dir, "audit-"+r.GeneratedAt.UTC().Format("20060102T150405Z"), auditBytes); err != nil {
		return Paths{}, err
	}
	return p, nil
}

// writeNew creates <stem>.json, or <stem>-2.json and so on, refusing to overwrite anything.
func writeNew(dir, stem string, data []byte) (string, error) {
	for n := 1; n < 1000; n++ {
		name := stem + ".json"
		if n > 1 {
			name = fmt.Sprintf("%s-%d.json", stem, n)
		}
		path := filepath.Join(dir, name)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("create audit log: %w", err)
		}
		_, werr := f.Write(data)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return "", fmt.Errorf("write audit log: %w", werr)
		}
		return path, nil
	}
	return "", errors.New("could not find a free audit log file name")
}

// AuditFile is a file fingerprint inside an audit record. Output paths are relative to the
// report directory so records do not depend on where the run happened.
type AuditFile struct {
	Role   string `json:"role"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// Audit is the record written for every run. It holds enough to reproduce the run: tool
// version, options, SHA-256 of every input file, the rules file and the holiday calendar,
// the results, and the hashes of the report files produced.
type Audit struct {
	Timestamp   time.Time   `json:"timestamp"`
	Tool        string      `json:"tool"`
	ToolVersion string      `json:"tool_version"`
	Disclaimer  string      `json:"disclaimer"`
	Options     Options     `json:"options"`
	AsOf        time.Time   `json:"as_of"`
	Inputs      []AuditFile `json:"inputs"`
	Outputs     []AuditFile `json:"outputs"`
	Result      AuditResult `json:"result"`
}

// AuditResult is the outcome stored in the audit record.
type AuditResult struct {
	Status     Status             `json:"status"`
	ExitCode   int                `json:"exit_code"`
	Partial    bool               `json:"partial_run"`
	Counts     Counts             `json:"counts"`
	Findings   []Finding          `json:"findings"`
	Redemption *RedemptionSummary `json:"redemption_summary,omitempty"`
	Unverified []string           `json:"unverified_rules"`
}

// BuildAudit assembles the audit record for a report and the output files written for it.
func BuildAudit(r *Report, outputs []AuditFile) Audit {
	a := Audit{
		Timestamp: r.GeneratedAt.UTC(), Tool: r.Tool, ToolVersion: r.Version, Disclaimer: r.Disclaimer,
		Options: r.Options, AsOf: r.AsOf.UTC(), Outputs: outputs,
		Result: AuditResult{Status: r.Status, ExitCode: r.ExitCode, Partial: r.Partial, Counts: r.Counts, Findings: r.Findings, Unverified: []string{}},
	}
	for _, in := range r.Inputs {
		a.Inputs = append(a.Inputs, AuditFile{Role: in.Role, Path: in.Path, SHA256: in.SHA256})
	}
	a.Inputs = append(a.Inputs, AuditFile{Role: "rules", Path: r.Rules.Path, SHA256: r.Rules.SHA256})
	if r.Calendar != nil {
		a.Inputs = append(a.Inputs, AuditFile{Role: "holiday_calendar", Path: r.Calendar.Path, SHA256: r.Calendar.SHA256})
	}
	if r.Redemption != nil {
		s := r.Redemption.Summary
		a.Result.Redemption = &s
	}
	for _, f := range r.Findings {
		if !f.RuleVerified {
			a.Result.Unverified = append(a.Result.Unverified, f.RuleID)
		}
	}
	return a
}
