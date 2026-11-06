package calendar_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jegorscode/reservelint/internal/calendar"
	"github.com/jegorscode/reservelint/internal/testutil"
)

const ny = "America/New_York"

func shipped(t *testing.T) *calendar.Calendar {
	t.Helper()
	c, err := calendar.Load(filepath.Join(testutil.RepoRoot(), "config", "holidays.yaml"), ny)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func d(s string) calendar.Date { return calendar.MustDate(s) }

// ---- independent check of the shipped holiday file -------------------------------------

func nthWeekday(year int, month time.Month, wd time.Weekday, n int) time.Time {
	t := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	for t.Weekday() != wd {
		t = t.AddDate(0, 0, 1)
	}
	return t.AddDate(0, 0, 7*(n-1))
}

func lastWeekday(year int, month time.Month, wd time.Weekday) time.Time {
	t := time.Date(year, month+1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
	for t.Weekday() != wd {
		t = t.AddDate(0, 0, -1)
	}
	return t
}

// observed applies the Federal rule: Saturday -> preceding Friday, Sunday -> following Monday.
func observed(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, -1)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	}
	return t
}

// federalObserved derives the observed Federal holidays from 5 U.S.C. 6103(a) without reading
// the data file, so an arithmetic slip in the file shows up as a mismatch here.
func federalObserved(year int) []time.Time {
	fixed := func(m time.Month, day int) time.Time { return time.Date(year, m, day, 0, 0, 0, 0, time.UTC) }
	return []time.Time{
		observed(fixed(time.January, 1)),
		nthWeekday(year, time.January, time.Monday, 3),
		nthWeekday(year, time.February, time.Monday, 3),
		lastWeekday(year, time.May, time.Monday),
		observed(fixed(time.June, 19)),
		observed(fixed(time.July, 4)),
		nthWeekday(year, time.September, time.Monday, 1),
		nthWeekday(year, time.October, time.Monday, 2),
		observed(fixed(time.November, 11)),
		nthWeekday(year, time.November, time.Thursday, 4),
		observed(fixed(time.December, 25)),
	}
}

func TestShippedFileMatchesIndependentDerivation(t *testing.T) {
	c := shipped(t)
	want := map[string]bool{}
	for _, year := range []int{2026, 2027, 2028} {
		for _, h := range federalObserved(year) {
			if h.Year() <= 2027 { // New Year's Day 2028 is observed 2027-12-31
				want[h.Format("2006-01-02")] = true
			}
		}
	}
	got := map[string]bool{}
	from, to := c.Coverage()
	for day := from; !to.Before(day); day = day.AddDays(1) {
		if _, ok := c.HolidayName(day); ok {
			got[day.String()] = true
		}
	}
	for k := range want {
		if !got[k] {
			t.Errorf("derived holiday %s missing from file", k)
		}
	}
	for k := range got {
		if !want[k] {
			t.Errorf("file lists %s, which was not derived as a Federal holiday", k)
		}
	}
	if len(want) != 23 {
		t.Errorf("expected 23 observed holidays in 2026-2027, derived %d", len(want))
	}
}

func TestShippedFileIsMarkedUnverified(t *testing.T) {
	c := shipped(t)
	if c.Verified || c.Kind != "federal" {
		t.Fatalf("verified=%v kind=%s", c.Verified, c.Kind)
	}
	if from, to := c.Coverage(); from.String() != "2026-01-01" || to.String() != "2027-12-31" {
		t.Fatalf("coverage %s..%s", from, to)
	}
}

func TestObservedShifts(t *testing.T) {
	c := shipped(t)
	cases := map[string]bool{
		"2026-07-03": false, // Independence Day (Saturday) observed Friday
		"2026-07-06": true,
		"2027-06-18": false, // Juneteenth (Saturday) observed Friday
		"2027-07-05": false, // Independence Day (Sunday) observed Monday
		"2027-07-04": false, // the Sunday itself
		"2027-12-24": false, // Christmas (Saturday) observed Friday
		"2027-12-31": false, // New Year's Day 2028 (Saturday) observed Friday
		"2027-11-25": false, // Thanksgiving
		"2027-11-26": true,  // day after Thanksgiving is a business day
	}
	for date, want := range cases {
		got, err := c.IsBusinessDay(d(date))
		if err != nil || got != want {
			t.Errorf("%s: business=%v err=%v, want %v", date, got, err, want)
		}
	}
}

// ---- business-day math -----------------------------------------------------------------

func TestBusinessDaysBetween(t *testing.T) {
	c := shipped(t)
	cases := []struct {
		start, end string
		want       int
	}{
		{"2027-02-01", "2027-02-01", 0},  // same day
		{"2027-02-01", "2027-02-02", 1},  // Mon -> Tue
		{"2027-02-01", "2027-02-03", 2},  // Mon -> Wed (at the limit)
		{"2027-02-01", "2027-02-04", 3},  // Mon -> Thu (breach)
		{"2027-02-05", "2027-02-08", 1},  // Fri -> Mon
		{"2027-02-05", "2027-02-09", 2},  // Fri -> Tue
		{"2027-02-06", "2027-02-08", 1},  // Sat -> Mon
		{"2027-02-06", "2027-02-09", 2},  // Sat -> Tue
		{"2027-02-07", "2027-02-08", 1},  // Sun -> Mon
		{"2027-02-04", "2027-02-01", 0},  // end before start
		{"2027-02-12", "2027-02-17", 2},  // Fri -> Wed across Presidents' Day (Mon 15th)
		{"2027-11-24", "2027-11-29", 2},  // Wed -> Mon across Thanksgiving and the weekend
		{"2026-07-02", "2026-07-06", 1},  // Thu -> Mon across observed Fri 3rd
		{"2027-01-04", "2027-01-19", 10}, // two weeks plus MLK Monday (18th)
	}
	for _, tc := range cases {
		got, err := c.BusinessDaysBetween(d(tc.start), d(tc.end))
		if err != nil || got != tc.want {
			t.Errorf("%s -> %s: %d (err %v), want %d", tc.start, tc.end, got, err, tc.want)
		}
	}
}

func TestAddBusinessDays(t *testing.T) {
	c := shipped(t)
	cases := []struct {
		start string
		n     int
		want  string
	}{
		{"2027-02-01", 2, "2027-02-03"}, // Mon + 2 = Wed
		{"2027-02-04", 2, "2027-02-08"}, // Thu + 2 = Mon
		{"2027-02-05", 2, "2027-02-09"}, // Fri + 2 = Tue
		{"2027-02-06", 2, "2027-02-09"}, // Sat + 2 = Tue
		{"2027-02-12", 2, "2027-02-17"}, // Fri + 2 over Presidents' Day = Wed
		{"2027-11-24", 2, "2027-11-29"}, // Wed + 2 over Thanksgiving = Mon
		{"2027-02-01", 0, "2027-02-01"},
	}
	for _, tc := range cases {
		got, err := c.AddBusinessDays(d(tc.start), tc.n)
		if err != nil || got.String() != tc.want {
			t.Errorf("%s + %d: %s (err %v), want %s", tc.start, tc.n, got, err, tc.want)
		}
	}
	if _, err := c.AddBusinessDays(d("2027-02-01"), -1); err == nil {
		t.Error("negative n accepted")
	}
}

func TestAddAndBetweenAreConsistent(t *testing.T) {
	c := shipped(t)
	start := d("2026-12-20")
	for n := 0; n < 40; n++ {
		end, err := c.AddBusinessDays(start, n)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := c.BusinessDaysBetween(start, end); got != n {
			t.Fatalf("n=%d: between(start, start+n) = %d", n, got)
		}
	}
}

func TestOutOfCoverageIsAnError(t *testing.T) {
	c := shipped(t)
	for _, date := range []string{"2025-12-31", "2028-01-03"} {
		if _, err := c.IsBusinessDay(d(date)); !errors.Is(err, calendar.ErrOutOfCoverage) {
			t.Errorf("%s: err = %v", date, err)
		}
	}
	if _, err := c.BusinessDaysBetween(d("2027-12-30"), d("2028-01-04")); !errors.Is(err, calendar.ErrOutOfCoverage) {
		t.Errorf("range running past coverage: %v", err)
	}
	if _, err := c.AddBusinessDays(d("2027-12-30"), 2); !errors.Is(err, calendar.ErrOutOfCoverage) {
		t.Errorf("due date past coverage: %v", err)
	}
}

// ---- time zones ------------------------------------------------------------------------

func TestDateOfUsesNewYorkCalendarDate(t *testing.T) {
	c := shipped(t)
	cases := map[string]string{
		"2027-02-01T03:00:00Z":      "2027-01-31", // 22:00 EST the evening before
		"2027-02-01T05:00:00Z":      "2027-02-01", // midnight EST
		"2027-01-05T04:30:00Z":      "2027-01-04", // 23:30 EST (winter)
		"2027-07-06T04:30:00Z":      "2027-07-06", // 00:30 EDT (summer): same UTC time, later date
		"2027-07-06T03:59:59Z":      "2027-07-05", // 23:59:59 EDT
		"2027-03-14T06:59:59Z":      "2027-03-14", // just before spring-forward
		"2027-03-14T07:00:00Z":      "2027-03-14", // just after spring-forward
		"2027-11-08T04:59:59Z":      "2027-11-07", // 23:59:59 EST after fall-back
		"2027-11-08T05:00:00Z":      "2027-11-08",
		"2027-02-01T22:00:00-05:00": "2027-02-01", // offsets are honoured
	}
	for in, want := range cases {
		ts, err := time.Parse(time.RFC3339, in)
		if err != nil {
			t.Fatal(err)
		}
		if got := c.DateOf(ts).String(); got != want {
			t.Errorf("%s -> %s, want %s", in, got, want)
		}
	}
}

// ---- file validation -------------------------------------------------------------------

const head = `schema_version: 1
name: test
kind: federal
verified: false
covers: {from: "2027-01-01", to: "2027-12-31"}
`

func TestParseAcceptsFedwireStyleNullObserved(t *testing.T) {
	doc := head + `holidays:
  - {name: "Sat holiday", date: "2027-06-19", observed: null}
  - {name: "Independence Day", date: "2027-07-04", observed: "2027-07-05"}
`
	c, err := calendar.Parse([]byte(doc), ny)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := c.IsBusinessDay(d("2027-06-18")); !ok {
		t.Error("Friday before a null-observed Saturday holiday should be a business day")
	}
	if ok, _ := c.IsBusinessDay(d("2027-07-05")); ok {
		t.Error("observed Monday should not be a business day")
	}
}

func TestParseRejectsBadFiles(t *testing.T) {
	cases := map[string]string{
		"bad kind":         strings.Replace(head, "federal", "other", 1) + "holidays: []\n",
		"verified missing": strings.Replace(head, "verified: false\n", "", 1) + "holidays: []\n",
		"wrong version":    strings.Replace(head, "schema_version: 1", "schema_version: 2", 1) + "holidays: []\n",
		"inverted covers":  strings.Replace(head, `from: "2027-01-01", to: "2027-12-31"`, `from: "2027-12-31", to: "2027-01-01"`, 1) + "holidays: []\n",
		"weekend observed": head + "holidays:\n  - {name: x, date: \"2027-07-04\", observed: \"2027-07-04\"}\n",
		"outside range":    head + "holidays:\n  - {name: x, date: \"2028-01-03\", observed: \"2028-01-03\"}\n",
		"duplicate":        head + "holidays:\n  - {name: x, date: \"2027-07-05\", observed: \"2027-07-05\"}\n  - {name: y, date: \"2027-07-05\", observed: \"2027-07-05\"}\n",
		"bad date":         head + "holidays:\n  - {name: x, date: \"07/05/2027\", observed: \"2027-07-05\"}\n",
		"no name":          head + "holidays:\n  - {name: \"\", date: \"2027-07-05\", observed: \"2027-07-05\"}\n",
		"unknown field":    head + "surprise: 1\nholidays: []\n",
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := calendar.Parse([]byte(doc), ny); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	if _, err := calendar.Parse([]byte(head+"holidays: []\n"), "Mars/Olympus"); err == nil {
		t.Error("bad time zone accepted")
	}
	if _, err := calendar.Load(filepath.Join(t.TempDir(), "nope.yaml"), ny); err == nil {
		t.Error("missing file accepted")
	}
}

func TestParseDate(t *testing.T) {
	if _, err := calendar.ParseDate("2027-02-30"); err == nil {
		t.Error("impossible date accepted")
	}
	if got := d("2027-02-28").AddDays(1).String(); got != "2027-03-01" {
		t.Errorf("AddDays = %s", got)
	}
	if d("2027-02-01").Weekday() != time.Monday {
		t.Error("weekday wrong")
	}
}
