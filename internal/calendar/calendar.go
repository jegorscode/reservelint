// Package calendar does business-day math against a reviewable holiday data file.
//
// A business day is any day other than a Saturday, Sunday or listed holiday. Dates are
// calendar dates in the calendar's time zone (America/New_York for redemption timing), never
// UTC dates. The holiday file declares the range it covers; asking about a date outside it is
// an error so a stale file can never make a holiday look like a business day.
package calendar

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	_ "time/tzdata" // embed zone data so a static Windows binary can load America/New_York

	"gopkg.in/yaml.v3"
)

// ErrOutOfCoverage is returned for a date outside the holiday file's declared range.
var ErrOutOfCoverage = errors.New("date outside holiday calendar coverage")

// Date is a calendar date with no time or zone.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// ParseDate parses YYYY-MM-DD.
func ParseDate(s string) (Date, error) {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(s))
	if err != nil {
		return Date{}, fmt.Errorf("%q is not a YYYY-MM-DD date", s)
	}
	return Date{t.Year(), t.Month(), t.Day()}, nil
}

// MustDate is ParseDate for literals; it panics on bad input.
func MustDate(s string) Date {
	d, err := ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

func (d Date) time() time.Time { return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC) }

// String renders YYYY-MM-DD.
func (d Date) String() string { return d.time().Format("2006-01-02") }

// Weekday returns the day of the week.
func (d Date) Weekday() time.Weekday { return d.time().Weekday() }

// AddDays returns the date n calendar days later (n may be negative).
func (d Date) AddDays(n int) Date {
	t := d.time().AddDate(0, 0, n)
	return Date{t.Year(), t.Month(), t.Day()}
}

// Before reports whether d is strictly earlier than o.
func (d Date) Before(o Date) bool { return d.time().Before(o.time()) }

// Calendar answers business-day questions.
type Calendar struct {
	Name     string
	Kind     string
	Verified bool
	loc      *time.Location
	from, to Date
	holidays map[Date]string // observed date -> holiday name
}

type document struct {
	SchemaVersion int     `yaml:"schema_version"`
	Name          string  `yaml:"name"`
	Kind          string  `yaml:"kind"`
	Source        string  `yaml:"source"`
	Verified      *bool   `yaml:"verified"`
	Notes         string  `yaml:"notes"`
	Covers        covers  `yaml:"covers"`
	Holidays      []entry `yaml:"holidays"`
}

type covers struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

type entry struct {
	Name     string  `yaml:"name"`
	Date     string  `yaml:"date"`
	Observed *string `yaml:"observed"`
}

// Load reads a holiday file and binds it to an IANA time zone.
func Load(path, timezone string) (*Calendar, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read holiday calendar: %w", err)
	}
	c, err := Parse(data, timezone)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// Parse decodes and validates holiday-file bytes.
func Parse(data []byte, timezone string) (*Calendar, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("time zone %q: %w", timezone, err)
	}
	var doc document
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("parse holiday calendar: %w", err)
	}
	switch {
	case doc.SchemaVersion != 1:
		return nil, fmt.Errorf("schema_version %d unsupported (want 1)", doc.SchemaVersion)
	case doc.Kind != "federal" && doc.Kind != "fedwire":
		return nil, fmt.Errorf("kind %q must be federal or fedwire", doc.Kind)
	case doc.Verified == nil:
		return nil, errors.New("verified must be set explicitly")
	}
	c := &Calendar{Name: doc.Name, Kind: doc.Kind, Verified: *doc.Verified, loc: loc, holidays: map[Date]string{}}
	if c.from, err = ParseDate(doc.Covers.From); err != nil {
		return nil, fmt.Errorf("covers.from: %w", err)
	}
	if c.to, err = ParseDate(doc.Covers.To); err != nil {
		return nil, fmt.Errorf("covers.to: %w", err)
	}
	if c.to.Before(c.from) {
		return nil, errors.New("covers.to is before covers.from")
	}
	for i, h := range doc.Holidays {
		if strings.TrimSpace(h.Name) == "" {
			return nil, fmt.Errorf("holidays[%d]: name is required", i)
		}
		if _, err := ParseDate(h.Date); err != nil {
			return nil, fmt.Errorf("holidays[%d] (%s): date: %w", i, h.Name, err)
		}
		if h.Observed == nil {
			continue // holiday causes no weekday closure under this schedule
		}
		obs, err := ParseDate(*h.Observed)
		if err != nil {
			return nil, fmt.Errorf("holidays[%d] (%s): observed: %w", i, h.Name, err)
		}
		if wd := obs.Weekday(); wd == time.Saturday || wd == time.Sunday {
			return nil, fmt.Errorf("holidays[%d] (%s): observed date %s is a weekend; use null", i, h.Name, obs)
		}
		if obs.Before(c.from) || c.to.Before(obs) {
			return nil, fmt.Errorf("holidays[%d] (%s): observed date %s outside covers range", i, h.Name, obs)
		}
		if prev, dup := c.holidays[obs]; dup {
			return nil, fmt.Errorf("holidays[%d] (%s): observed date %s already used by %s", i, h.Name, obs, prev)
		}
		c.holidays[obs] = h.Name
	}
	return c, nil
}

// Location is the time zone dates are computed in.
func (c *Calendar) Location() *time.Location { return c.loc }

// Coverage returns the inclusive date range the calendar is authoritative for.
func (c *Calendar) Coverage() (from, to Date) { return c.from, c.to }

// DateOf returns the calendar date of an instant in the calendar's time zone.
func (c *Calendar) DateOf(t time.Time) Date {
	l := t.In(c.loc)
	return Date{l.Year(), l.Month(), l.Day()}
}

func (c *Calendar) covered(d Date) error {
	if d.Before(c.from) || c.to.Before(d) {
		return fmt.Errorf("%w: %s not in %s..%s", ErrOutOfCoverage, d, c.from, c.to)
	}
	return nil
}

// IsBusinessDay reports whether d is a weekday and not a holiday.
func (c *Calendar) IsBusinessDay(d Date) (bool, error) {
	if err := c.covered(d); err != nil {
		return false, err
	}
	if wd := d.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return false, nil
	}
	_, holiday := c.holidays[d]
	return !holiday, nil
}

// HolidayName returns the holiday observed on d, if any.
func (c *Calendar) HolidayName(d Date) (string, bool) {
	n, ok := c.holidays[d]
	return n, ok
}

// BusinessDaysBetween counts business days after start up to and including end. A start on
// Monday and an end on Wednesday is 2. It is 0 when end is not after start.
func (c *Calendar) BusinessDaysBetween(start, end Date) (int, error) {
	if err := c.covered(start); err != nil {
		return 0, err
	}
	if err := c.covered(end); err != nil {
		return 0, err
	}
	n := 0
	for d := start.AddDays(1); !end.Before(d); d = d.AddDays(1) {
		if ok, _ := c.IsBusinessDay(d); ok {
			n++
		}
	}
	return n, nil
}

// AddBusinessDays returns the date n business days after start (the "T+n" due date). n must
// be non-negative.
func (c *Calendar) AddBusinessDays(start Date, n int) (Date, error) {
	if n < 0 {
		return Date{}, errors.New("n must be non-negative")
	}
	if err := c.covered(start); err != nil {
		return Date{}, err
	}
	d := start
	for counted := 0; counted < n; {
		d = d.AddDays(1)
		ok, err := c.IsBusinessDay(d)
		if err != nil {
			return Date{}, err
		}
		if ok {
			counted++
		}
	}
	return d, nil
}
