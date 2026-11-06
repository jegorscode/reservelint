// Package report defines the report data model and writes it as Markdown, CSV and JSON,
// plus an audit-log record that fingerprints every input and output of a run.
//
// The package is deliberately free of check logic: the runner converts engine results into
// these plain types, so report formats stay stable and testable with golden files.
package report

import (
	"time"

	"github.com/shopspring/decimal"
)

// Status is the overall or per-finding verdict.
type Status string

// Overall statuses.
const (
	StatusPass    Status = "PASS"
	StatusWarning Status = "WARNING"
	StatusFail    Status = "FAIL"
)

// Exit codes of the check command.
const (
	ExitOK        = 0 // PASS or WARNING
	ExitViolation = 1 // at least one FAIL finding
	ExitInput     = 2 // CLI, input or parsing problem
)

// Report is the complete result of one run.
type Report struct {
	Tool        string        `json:"tool"`
	Version     string        `json:"tool_version"`
	Disclaimer  string        `json:"disclaimer"`
	GeneratedAt time.Time     `json:"generated_at"`
	AsOf        time.Time     `json:"as_of"`
	Status      Status        `json:"status"`
	ExitCode    int           `json:"exit_code"`
	Partial     bool          `json:"partial_run"`
	Counts      Counts        `json:"counts"`
	Options     Options       `json:"options"`
	Inputs      []Input       `json:"inputs"`
	Rules       FileInfo      `json:"rules_file"`
	Calendar    *CalendarInfo `json:"holiday_calendar,omitempty"`
	Findings    []Finding     `json:"findings"`
	Redemption  *Redemption   `json:"redemption_timing,omitempty"`
	NotChecked  []NotChecked  `json:"rules_not_checked"`
	Caveats     []string      `json:"caveats"`
}

// Counts tallies findings by status.
type Counts struct {
	Pass    int `json:"pass"`
	Warning int `json:"warning"`
	Fail    int `json:"fail"`
}

// Options records the command-line choices that affect results.
type Options struct {
	AllowInvalidRows bool `json:"allow_invalid_rows"`
	// AllowDuplicateRows acknowledges identical reserve rows without a holding_id as distinct
	// positions: they are still kept and listed, but no DUPLICATE_ROW_WARNING is raised.
	AllowDuplicateRows bool   `json:"allow_duplicate_rows"`
	AsOfOverride       string `json:"as_of_override,omitempty"`
}

// FileInfo identifies a file by path and content hash.
type FileInfo struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// CalendarInfo describes the holiday calendar used for business-day math.
type CalendarInfo struct {
	FileInfo
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	Verified     bool   `json:"verified"`
	Timezone     string `json:"timezone"`
	CoverageFrom string `json:"coverage_from"`
	CoverageTo   string `json:"coverage_to"`
}

// Input describes one loaded data file.
type Input struct {
	Role     string `json:"role"` // reserves, supply, redemptions
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	RowsRead int    `json:"rows_read"`
	RowsUsed int    `json:"rows_used"`
	// Duplicates are exact repeats (same holding_id or an unkeyed non-reserve row) that were
	// dropped and not counted.
	Duplicates []Duplicate `json:"duplicates"`
	// IdenticalUnkeyed are identical reserve rows without a holding_id that were KEPT as
	// separate positions and therefore counted.
	IdenticalUnkeyed []Duplicate  `json:"identical_unkeyed_rows_kept"`
	InvalidRows      []InvalidRow `json:"invalid_rows"`
}

// IdenticalUnkeyedCount is the number of repeated unkeyed rows that were kept.
func (i Input) IdenticalUnkeyedCount() int {
	n := 0
	for _, d := range i.IdenticalUnkeyed {
		n += len(d.RepeatPositions)
	}
	return n
}

// DuplicateCount is the number of repeated rows dropped.
func (i Input) DuplicateCount() int {
	n := 0
	for _, d := range i.Duplicates {
		n += len(d.RepeatPositions)
	}
	return n
}

// Duplicate is a row that appeared more than once; positions are CSV line numbers or JSON
// item indexes.
type Duplicate struct {
	RowKey          string `json:"row_key"`
	FirstPosition   int    `json:"first_position"`
	RepeatPositions []int  `json:"repeat_positions"`
}

// InvalidRow is a row that failed validation.
type InvalidRow struct {
	Position int    `json:"position"`
	Message  string `json:"message"`
}

// Finding is one rule's result.
type Finding struct {
	RuleID      string `json:"rule_id"`
	Check       string `json:"check"`
	Description string `json:"description"`
	Status      string `json:"status"` // pass, warning, fail
	// UncappedStatus is set when the check failed but the rule is unverified, so Status was
	// capped at "warning". It holds the status the check actually produced ("fail").
	UncappedStatus string          `json:"uncapped_status,omitempty"`
	Observed       decimal.Decimal `json:"observed"`
	Comparator     string          `json:"comparator"`
	Threshold      decimal.Decimal `json:"threshold"`
	Citation       string          `json:"citation"`
	RuleVerified   bool            `json:"rule_verified"`
	RuleNotes      string          `json:"rule_notes,omitempty"`
	Details        map[string]any  `json:"details"`
}

// Redemption is the redemption-timing section: summary totals plus every request that is not
// plainly on time (warnings, breaches, open requests and anomalies).
type Redemption struct {
	Summary RedemptionSummary `json:"summary"`
	Items   []RedemptionItem  `json:"items"`
}

// RedemptionSummary mirrors the engine summary, including excluded requests.
type RedemptionSummary struct {
	Total          int             `json:"total"`
	Evaluated      int             `json:"evaluated"`
	OK             int             `json:"ok"`
	Warning        int             `json:"warning"`
	Breach         int             `json:"breach"`
	Rejected       int             `json:"rejected"`
	Cancelled      int             `json:"cancelled"`
	Open           int             `json:"open"`
	WithAnomalies  int             `json:"with_anomalies"`
	MaxBusinessDay int             `json:"max_business_days_used"`
	TotalAmount    decimal.Decimal `json:"total_amount"`
	ExcludedAmount decimal.Decimal `json:"excluded_amount"`
	WarningAmount  decimal.Decimal `json:"warning_amount"`
	BreachAmount   decimal.Decimal `json:"breach_amount"`
}

// RedemptionItem is one redemption worth a reviewer's attention.
type RedemptionItem struct {
	RequestTS           time.Time       `json:"request_ts"`
	OnboardedTS         *time.Time      `json:"onboarded_ts"`
	TransferInitiatedTS *time.Time      `json:"transfer_initiated_ts"`
	CreditedTS          *time.Time      `json:"credited_ts"`
	Amount              decimal.Decimal `json:"amount"`
	Status              string          `json:"status"`
	Outcome             string          `json:"outcome"`
	ClockStart          time.Time       `json:"clock_start"`
	ClockEnd            time.Time       `json:"clock_end"`
	StartDate           string          `json:"start_date"`
	EndDate             string          `json:"end_date"`
	DueDate             string          `json:"due_date,omitempty"`
	BusinessDaysElapsed int             `json:"business_days_elapsed"`
	Open                bool            `json:"open"`
	Anomalies           []string        `json:"anomalies"`
}

// NotChecked is a rule the tool tracks but does not evaluate.
type NotChecked struct {
	RuleID      string `json:"rule_id"`
	Description string `json:"description"`
	Reason      string `json:"reason"`
}

// Finalize computes counts, the overall status and the exit code from the findings.
//
// FAIL if any finding failed. Otherwise WARNING if any finding warned or the run was partial
// (invalid rows were excluded, so it cannot be called a clean pass). Otherwise PASS.
func (r *Report) Finalize() {
	r.Counts = Counts{}
	for _, f := range r.Findings {
		switch f.Status {
		case "fail":
			r.Counts.Fail++
		case "warning":
			r.Counts.Warning++
		default:
			r.Counts.Pass++
		}
	}
	switch {
	case r.Counts.Fail > 0:
		r.Status, r.ExitCode = StatusFail, ExitViolation
	case r.Counts.Warning > 0 || r.Partial:
		r.Status, r.ExitCode = StatusWarning, ExitOK
	default:
		r.Status, r.ExitCode = StatusPass, ExitOK
	}
}
