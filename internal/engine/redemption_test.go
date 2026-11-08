package engine_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jegorscode/reservelint/internal/calendar"
	"github.com/jegorscode/reservelint/internal/config"
	"github.com/jegorscode/reservelint/internal/engine"
	"github.com/jegorscode/reservelint/internal/ingest"
	"github.com/jegorscode/reservelint/internal/model"
	"github.com/jegorscode/reservelint/internal/rules"
	"github.com/jegorscode/reservelint/internal/testutil"
)

func nyCalendar(t *testing.T) *calendar.Calendar {
	t.Helper()
	c, err := calendar.Load(filepath.Join(testutil.RepoRoot(), "config", "holidays.yaml"), "America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func redRule(maxDays, margin int, tz string) rules.Rule {
	return rule("redemption_timing", true, fmt.Sprintf(
		"      max_business_days: %d\n      near_breach_margin_business_days: %d\n      timezone: %s\n", maxDays, margin, tz))
}

func ts(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func tsp(s string) *time.Time {
	if s == "" {
		return nil
	}
	t := ts(s)
	return &t
}

func red(request, onboarded, transfer string, status model.RedemptionStatus) model.RedemptionRow {
	return model.RedemptionRow{RequestTS: ts(request), OnboardedTS: tsp(onboarded), TransferInitiatedTS: tsp(transfer), Amount: dec("100"), Status: status}
}

func done(request, transfer string) model.RedemptionRow {
	return red(request, "", transfer, model.StatusCompleted)
}

var asOfDefault = ts("2027-03-01T21:00:00Z")

func evalOne(t *testing.T, row model.RedemptionRow, asOf time.Time) engine.RedemptionEval {
	t.Helper()
	r, err := engine.CheckRedemptionTiming(redRule(2, 1, "America/New_York"), nyCalendar(t), []model.RedemptionRow{row}, asOf)
	if err != nil {
		t.Fatal(err)
	}
	return r.Evaluations[0]
}

func TestTPlus2Boundaries(t *testing.T) {
	// 15:00Z is 10:00 in New York (EST). Monday 2027-02-01 is the clock start.
	cases := []struct {
		name    string
		row     model.RedemptionRow
		elapsed int
		outcome engine.Outcome
		due     string
	}{
		{"same day", done("2027-02-01T15:00:00Z", "2027-02-01T20:00:00Z"), 0, engine.OutcomeOK, "2027-02-03"},
		{"Tuesday", done("2027-02-01T15:00:00Z", "2027-02-02T15:00:00Z"), 1, engine.OutcomeOK, "2027-02-03"},
		{"Wednesday is T+2: warning", done("2027-02-01T15:00:00Z", "2027-02-03T22:00:00Z"), 2, engine.OutcomeWarning, "2027-02-03"},
		{"Thursday is a breach", done("2027-02-01T15:00:00Z", "2027-02-04T15:00:00Z"), 3, engine.OutcomeBreach, "2027-02-03"},
		{"Friday to Tuesday", done("2027-02-05T15:00:00Z", "2027-02-09T15:00:00Z"), 2, engine.OutcomeWarning, "2027-02-09"},
		{"Friday to Wednesday", done("2027-02-05T15:00:00Z", "2027-02-10T15:00:00Z"), 3, engine.OutcomeBreach, "2027-02-09"},
		{"Saturday request, Tuesday", done("2027-02-06T15:00:00Z", "2027-02-09T15:00:00Z"), 2, engine.OutcomeWarning, "2027-02-09"},
		{"Saturday request, Wednesday", done("2027-02-06T15:00:00Z", "2027-02-10T15:00:00Z"), 3, engine.OutcomeBreach, "2027-02-09"},
		{"over Presidents' Day, Wed", done("2027-02-12T15:00:00Z", "2027-02-17T15:00:00Z"), 2, engine.OutcomeWarning, "2027-02-17"},
		{"over Presidents' Day, Thu", done("2027-02-12T15:00:00Z", "2027-02-18T15:00:00Z"), 3, engine.OutcomeBreach, "2027-02-17"},
		{"over spring-forward DST", done("2027-03-12T15:00:00Z", "2027-03-16T15:00:00Z"), 2, engine.OutcomeWarning, "2027-03-16"},
		{"other UTC offset on input", done("2027-02-01T10:00:00-05:00", "2027-02-03T10:00:00-05:00"), 2, engine.OutcomeWarning, "2027-02-03"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev := evalOne(t, tc.row, asOfDefault)
			if ev.Elapsed != tc.elapsed || ev.Outcome != tc.outcome || ev.DueDate.String() != tc.due || ev.Open || len(ev.Anomalies) != 0 {
				t.Fatalf("elapsed %d outcome %s due %s open %v anomalies %v", ev.Elapsed, ev.Outcome, ev.DueDate, ev.Open, ev.Anomalies)
			}
		})
	}
}

func TestNewYorkDatesNotUTCDates(t *testing.T) {
	// 03:00Z on Feb 1 is 22:00 EST on Sunday Jan 31. Counted from Jan 31, Wednesday is 3 days
	// (breach); a UTC-date implementation would start on Feb 1 and wrongly report 2.
	ev := evalOne(t, done("2027-02-01T03:00:00Z", "2027-02-03T15:00:00Z"), asOfDefault)
	if ev.StartDate.String() != "2027-01-31" || ev.Elapsed != 3 || ev.Outcome != engine.OutcomeBreach {
		t.Fatalf("start %s elapsed %d outcome %s", ev.StartDate, ev.Elapsed, ev.Outcome)
	}
	// Late evening transfer: 23:30 EST Wednesday is still Wednesday although it is Thursday in UTC.
	ev = evalOne(t, done("2027-02-01T15:00:00Z", "2027-02-04T04:30:00Z"), asOfDefault)
	if ev.EndDate.String() != "2027-02-03" || ev.Elapsed != 2 || ev.Outcome != engine.OutcomeWarning {
		t.Fatalf("end %s elapsed %d outcome %s", ev.EndDate, ev.Elapsed, ev.Outcome)
	}
}

func TestClockStartsAtLaterOfRequestAndOnboarding(t *testing.T) {
	// Requested Monday, onboarded Wednesday 10:00 ET, transferred Wednesday: 0 days used.
	row := red("2027-02-01T15:00:00Z", "2027-02-03T15:00:00Z", "2027-02-03T20:00:00Z", model.StatusCompleted)
	ev := evalOne(t, row, asOfDefault)
	if ev.StartDate.String() != "2027-02-03" || ev.Elapsed != 0 || ev.Outcome != engine.OutcomeOK {
		t.Fatalf("%+v", ev)
	}
	// Onboarded long before the request: the request time is the start.
	row = red("2027-02-01T15:00:00Z", "2026-06-01T15:00:00Z", "2027-02-04T15:00:00Z", model.StatusCompleted)
	if ev = evalOne(t, row, asOfDefault); ev.StartDate.String() != "2027-02-01" || ev.Outcome != engine.OutcomeBreach {
		t.Fatalf("%+v", ev)
	}
}

func TestPendingRequestsMeasuredToAsOf(t *testing.T) {
	row := red("2027-02-01T15:00:00Z", "", "", model.StatusPending)
	cases := map[string]struct {
		elapsed int
		outcome engine.Outcome
	}{
		"2027-02-02T21:00:00Z": {1, engine.OutcomeOK},
		"2027-02-03T21:00:00Z": {2, engine.OutcomeWarning},
		"2027-02-04T21:00:00Z": {3, engine.OutcomeBreach},
	}
	for asOf, want := range cases {
		ev := evalOne(t, row, ts(asOf))
		if !ev.Open || ev.Elapsed != want.elapsed || ev.Outcome != want.outcome || len(ev.Anomalies) != 0 {
			t.Errorf("as_of %s: %+v", asOf, ev)
		}
	}
}

func TestExcludedStatusesCountedButNotBreaches(t *testing.T) {
	rows := []model.RedemptionRow{
		done("2027-02-01T15:00:00Z", "2027-02-02T15:00:00Z"),
		red("2027-01-04T15:00:00Z", "", "", model.StatusRejected),  // ancient, would breach
		red("2027-01-05T15:00:00Z", "", "", model.StatusCancelled), // ancient, would breach
		red("2027-01-06T15:00:00Z", "", "", model.StatusCancelled),
	}
	rows[1].Amount, rows[2].Amount, rows[3].Amount = dec("10"), dec("20"), dec("30")
	r, err := engine.CheckRedemptionTiming(redRule(2, 1, "America/New_York"), nyCalendar(t), rows, asOfDefault)
	if err != nil {
		t.Fatal(err)
	}
	s := r.Summary
	if s.Total != 4 || s.Evaluated != 1 || s.Rejected != 1 || s.Cancelled != 2 || s.Breach != 0 || !s.ExcludedAmount.Equal(dec("60")) || !s.TotalAmount.Equal(dec("160")) {
		t.Fatalf("%+v", s)
	}
	if !r.Finding.Passed || r.Finding.Status() != "pass" {
		t.Fatalf("finding %+v", r.Finding)
	}
	if r.Evaluations[1].Outcome != engine.OutcomeExcluded || r.Finding.Details["rejected"] != 1 || r.Finding.Details["cancelled"] != 2 {
		t.Fatalf("details %+v", r.Finding.Details)
	}
}

func TestFindingStatusFailWarnPass(t *testing.T) {
	check := func(rows ...model.RedemptionRow) engine.Finding {
		r, err := engine.CheckRedemptionTiming(redRule(2, 1, "America/New_York"), nyCalendar(t), rows, asOfDefault)
		if err != nil {
			t.Fatal(err)
		}
		return r.Finding
	}
	ok := done("2027-02-01T15:00:00Z", "2027-02-02T15:00:00Z")
	warn := done("2027-02-01T15:00:00Z", "2027-02-03T15:00:00Z")
	breach := done("2027-02-01T15:00:00Z", "2027-02-04T15:00:00Z")
	if f := check(ok); f.Status() != "pass" || !f.Passed {
		t.Errorf("ok: %s", f.Status())
	}
	if f := check(ok, warn); f.Status() != "warning" || !f.Passed || !f.Observed.Equal(dec("2")) {
		t.Errorf("warn: %s observed %s", f.Status(), f.Observed)
	}
	if f := check(ok, warn, breach); f.Status() != "fail" || f.Passed || !f.Observed.Equal(dec("3")) || !f.Threshold.Equal(dec("2")) || f.Comparator != engine.AtMost {
		t.Errorf("breach: %s observed %s", f.Status(), f.Observed)
	}
}

func TestMarginIsConfigurable(t *testing.T) {
	row := done("2027-02-01T15:00:00Z", "2027-02-02T15:00:00Z") // 1 day used
	for margin, want := range map[int]engine.Outcome{0: engine.OutcomeOK, 1: engine.OutcomeOK, 2: engine.OutcomeWarning} {
		r, err := engine.CheckRedemptionTiming(redRule(2, margin, "America/New_York"), nyCalendar(t), []model.RedemptionRow{row}, asOfDefault)
		if err != nil || r.Evaluations[0].Outcome != want {
			t.Errorf("margin %d: %v %v", margin, r.Evaluations[0].Outcome, err)
		}
	}
	row = done("2027-02-01T15:00:00Z", "2027-02-03T15:00:00Z") // exactly at the limit
	r, _ := engine.CheckRedemptionTiming(redRule(2, 0, "America/New_York"), nyCalendar(t), []model.RedemptionRow{row}, asOfDefault)
	if r.Evaluations[0].Outcome != engine.OutcomeOK || !r.Finding.Passed {
		t.Error("margin 0 should give no warning at the limit")
	}
}

func TestAnomaliesAreReportedNotHidden(t *testing.T) {
	// Transfer before the clock start.
	ev := evalOne(t, red("2027-02-03T15:00:00Z", "", "2027-02-01T15:00:00Z", model.StatusCompleted), asOfDefault)
	if ev.Elapsed != 0 || len(ev.Anomalies) != 1 || !strings.Contains(ev.Anomalies[0], "precedes") {
		t.Errorf("%+v", ev)
	}
	// Completed with no transfer timestamp: measured to as_of, so an old one is a breach.
	ev = evalOne(t, red("2027-02-01T15:00:00Z", "", "", model.StatusCompleted), asOfDefault)
	if !ev.Open || ev.Outcome != engine.OutcomeBreach || len(ev.Anomalies) != 1 || !strings.Contains(ev.Anomalies[0], "completed") {
		t.Errorf("%+v", ev)
	}
	// Request later than the snapshot.
	ev = evalOne(t, red("2027-03-02T15:00:00Z", "", "", model.StatusPending), asOfDefault)
	if ev.Elapsed != 0 || len(ev.Anomalies) != 1 || !strings.Contains(ev.Anomalies[0], "after the snapshot") {
		t.Errorf("%+v", ev)
	}
}

func TestDueDateBeyondCalendarIsAnomalyNotError(t *testing.T) {
	ev := evalOne(t, red("2027-12-30T15:00:00Z", "", "2027-12-30T20:00:00Z", model.StatusCompleted), ts("2027-12-30T21:00:00Z"))
	if ev.Elapsed != 0 || len(ev.Anomalies) != 1 || !strings.Contains(ev.Anomalies[0], "coverage") {
		t.Errorf("%+v", ev)
	}
}

func TestErrors(t *testing.T) {
	cal := nyCalendar(t)
	one := []model.RedemptionRow{done("2027-02-01T15:00:00Z", "2027-02-02T15:00:00Z")}
	if _, err := engine.CheckRedemptionTiming(redRule(2, 1, "America/Chicago"), cal, one, asOfDefault); err == nil {
		t.Error("time zone mismatch accepted")
	}
	if _, err := engine.CheckRedemptionTiming(redRule(2, 1, "America/New_York"), cal, nil, asOfDefault); err == nil {
		t.Error("no rows accepted")
	}
	allExcluded := []model.RedemptionRow{red("2027-02-01T15:00:00Z", "", "", model.StatusRejected)}
	if _, err := engine.CheckRedemptionTiming(redRule(2, 1, "America/New_York"), cal, allExcluded, asOfDefault); err == nil {
		t.Error("all-excluded input accepted")
	}
	outside := []model.RedemptionRow{done("2028-02-01T15:00:00Z", "2028-02-02T15:00:00Z")}
	if _, err := engine.CheckRedemptionTiming(redRule(2, 1, "America/New_York"), cal, outside, asOfDefault); err == nil {
		t.Error("date outside calendar coverage accepted")
	}
	missing := rule("redemption_timing", true, "      max_business_days: 2\n      timezone: America/New_York\n")
	if _, err := engine.CheckRedemptionTiming(missing, cal, one, asOfDefault); err == nil {
		t.Error("missing margin parameter accepted")
	}
}

func TestShippedRuleDrivesTheCheck(t *testing.T) {
	loaded, err := config.LoadRules(filepath.Join(testutil.RepoRoot(), "rules", "rules.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	rr, _ := loaded.Set.Get("RW-RED-001")
	r, err := engine.CheckRedemptionTiming(rr, nyCalendar(t), []model.RedemptionRow{done("2027-02-01T15:00:00Z", "2027-02-04T15:00:00Z")}, asOfDefault)
	if err != nil || r.Finding.Passed || !r.Finding.RuleVerified || r.Finding.RuleID != "RW-RED-001" {
		t.Fatalf("%v %+v", err, r.Finding)
	}
	if r.Finding.Details["calendar_verified"] != true {
		t.Error("finding should expose that the shipped calendar is verified")
	}
}

func TestSyntheticVectors(t *testing.T) {
	root := testutil.RepoRoot()
	loaded, _ := config.LoadRules(filepath.Join(root, "rules", "rules.yaml"))
	rr, _ := loaded.Set.Get("RW-RED-001")
	for name, wantPass := range map[string]bool{"clean": true, "violations": false} {
		dir := filepath.Join(root, "testdata", "vectors", name)
		reds, err := ingest.LoadRedemptions(filepath.Join(dir, "redemptions.csv"))
		if err != nil {
			t.Fatal(err)
		}
		sup, _ := ingest.LoadSupply(filepath.Join(dir, "supply.csv"))
		latest, _ := ingest.LatestSupply(sup.Rows)
		r, err := engine.CheckRedemptionTiming(rr, nyCalendar(t), reds.Rows, latest.AsOf)
		if err != nil {
			t.Fatal(err)
		}
		if r.Finding.Passed != wantPass {
			t.Errorf("%s: passed=%v summary %+v", name, r.Finding.Passed, r.Summary)
		}
		if name == "clean" && (r.Summary.Warning != 0 || r.Summary.AnomalyCount != 0) {
			t.Errorf("clean data should have no warnings or anomalies: %+v", r.Summary)
		}
		if name == "violations" && r.Summary.Breach < 2 {
			t.Errorf("violations should contain at least the two stuck requests: %+v", r.Summary)
		}
		if r.Summary.Total != reds.RowsRead-reds.DuplicateCount() {
			t.Errorf("%s: total %d vs rows %d", name, r.Summary.Total, len(reds.Rows))
		}
	}
}
