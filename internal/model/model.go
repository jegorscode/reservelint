// Package model defines the validated input rows and their deterministic row keys.
//
// Design notes
//   - Rows are built only through the Parse* functions, which reject unknown fields, blank
//     required fields, non-positive amounts and timestamps without a UTC offset.
//   - Money is decimal.Decimal; float64 is never used for amounts.
//   - RowKey() is a SHA-256 over a normalised, canonical-JSON form. The normalisation and the
//     byte layout match the Python prototype, so keys can be compared across implementations.
package model

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

const dateLayout = "2006-01-02"

// ReserveRow is one line of the reserves snapshot.
type ReserveRow struct {
	AssetType              string
	Amount                 decimal.Decimal
	Counterparty           string
	MaturityDate           *time.Time // calendar date, stored at UTC midnight
	IsImmediatelyAvailable bool
}

// SupplyRow is a circulating-supply snapshot.
type SupplyRow struct {
	AsOf              time.Time
	CirculatingSupply decimal.Decimal
}

// RedemptionStatus is the lifecycle state of a redemption request.
type RedemptionStatus string

// Allowed redemption statuses.
const (
	StatusPending   RedemptionStatus = "pending"
	StatusCompleted RedemptionStatus = "completed"
	StatusRejected  RedemptionStatus = "rejected"
	StatusCancelled RedemptionStatus = "cancelled"
)

// RedemptionRow is one redemption request and its progress.
type RedemptionRow struct {
	RequestTS           time.Time
	OnboardedTS         *time.Time
	TransferInitiatedTS *time.Time
	Amount              decimal.Decimal
	Status              RedemptionStatus
}

// Keyed is implemented by every row type.
type Keyed interface{ RowKey() string }

// ---- parsing ---------------------------------------------------------------------------

type record struct {
	m    map[string]string
	errs []error
}

func newRecord(m map[string]string, allowed ...string) *record {
	r := &record{m: m}
	ok := map[string]bool{}
	for _, a := range allowed {
		ok[a] = true
	}
	var unknown []string
	for k := range m {
		if !ok[k] {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	for _, k := range unknown {
		r.fail("%s: unknown field", k)
	}
	return r
}

func (r *record) fail(format string, args ...any) {
	r.errs = append(r.errs, fmt.Errorf(format, args...))
}

func (r *record) err() error { return errors.Join(r.errs...) }

func (r *record) opt(name string) string { return strings.TrimSpace(r.m[name]) }

func (r *record) text(name string) string {
	v := r.opt(name)
	if v == "" {
		r.fail("%s: required, must not be blank", name)
	}
	return v
}

func (r *record) amount(name string) decimal.Decimal {
	v := r.opt(name)
	d, err := decimal.NewFromString(v)
	switch {
	case v == "":
		r.fail("%s: required", name)
	case err != nil:
		r.fail("%s: %q is not a number", name, v)
	case !d.IsPositive():
		r.fail("%s: must be greater than zero, got %s", name, v)
	}
	return d
}

func parseTS(v string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return time.Time{}, fmt.Errorf("%q is not an RFC 3339 timestamp with a UTC offset", v)
	}
	return t, nil
}

func (r *record) ts(name string) time.Time {
	v := r.opt(name)
	if v == "" {
		r.fail("%s: required", name)
		return time.Time{}
	}
	t, err := parseTS(v)
	if err != nil {
		r.fail("%s: %v", name, err)
	}
	return t
}

func (r *record) optTS(name string) *time.Time {
	if r.opt(name) == "" {
		return nil
	}
	t := r.ts(name)
	return &t
}

func (r *record) boolean(name string) bool {
	switch strings.ToLower(r.opt(name)) {
	case "true", "t", "yes", "y", "on", "1":
		return true
	case "false", "f", "no", "n", "off", "0":
		return false
	}
	r.fail("%s: %q is not a boolean", name, r.opt(name))
	return false
}

// ParseReserve validates one reserves record (column name -> cell text).
func ParseReserve(m map[string]string) (ReserveRow, error) {
	r := newRecord(m, "asset_type", "amount", "counterparty", "maturity_date", "is_immediately_available")
	row := ReserveRow{
		AssetType:              r.text("asset_type"),
		Amount:                 r.amount("amount"),
		Counterparty:           r.text("counterparty"),
		IsImmediatelyAvailable: r.boolean("is_immediately_available"),
	}
	if v := r.opt("maturity_date"); v != "" {
		d, err := time.Parse(dateLayout, v)
		if err != nil {
			r.fail("maturity_date: %q is not a YYYY-MM-DD date", v)
		}
		row.MaturityDate = &d
	}
	return row, r.err()
}

// ParseSupply validates one supply record.
func ParseSupply(m map[string]string) (SupplyRow, error) {
	r := newRecord(m, "as_of", "circulating_supply")
	row := SupplyRow{AsOf: r.ts("as_of"), CirculatingSupply: r.amount("circulating_supply")}
	return row, r.err()
}

// ParseRedemption validates one redemption record.
func ParseRedemption(m map[string]string) (RedemptionRow, error) {
	r := newRecord(m, "request_ts", "onboarded_ts", "transfer_initiated_ts", "amount", "status")
	row := RedemptionRow{
		RequestTS:           r.ts("request_ts"),
		OnboardedTS:         r.optTS("onboarded_ts"),
		TransferInitiatedTS: r.optTS("transfer_initiated_ts"),
		Amount:              r.amount("amount"),
	}
	switch s := RedemptionStatus(strings.ToLower(r.opt("status"))); s {
	case StatusPending, StatusCompleted, StatusRejected, StatusCancelled:
		row.Status = s
	default:
		r.fail("status: %q must be pending, completed, rejected or cancelled", r.opt("status"))
	}
	return row, r.err()
}

// ---- row keys --------------------------------------------------------------------------

// RowKey identifies a reserve row regardless of formatting noise.
func (r ReserveRow) RowKey() string {
	var maturity any
	if r.MaturityDate != nil {
		maturity = r.MaturityDate.Format(dateLayout)
	}
	return hashKey(map[string]any{
		"asset_type":               NormText(r.AssetType),
		"amount":                   r.Amount.String(),
		"counterparty":             NormText(r.Counterparty),
		"maturity_date":            maturity,
		"is_immediately_available": r.IsImmediatelyAvailable,
	})
}

// RowKey identifies a supply row.
func (r SupplyRow) RowKey() string {
	return hashKey(map[string]any{
		"as_of":              normTS(&r.AsOf),
		"circulating_supply": r.CirculatingSupply.String(),
	})
}

// RowKey identifies a redemption row.
func (r RedemptionRow) RowKey() string {
	return hashKey(map[string]any{
		"request_ts":            normTS(&r.RequestTS),
		"onboarded_ts":          normTS(r.OnboardedTS),
		"transfer_initiated_ts": normTS(r.TransferInitiatedTS),
		"amount":                r.Amount.String(),
		"status":                string(r.Status),
	})
}
