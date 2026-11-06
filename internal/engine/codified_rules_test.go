package engine_test

import (
	"strings"
	"testing"
	"time"

	"github.com/jegorscode/reservelint/internal/engine"
	"github.com/jegorscode/reservelint/internal/model"
)

func liability(amount string) model.ReserveRow {
	return model.ReserveRow{AssetType: "repo_liability", Amount: dec(amount), Counterparty: "Dealer", IsRepoLiability: true}
}

func exempt(amount, cp, category string) model.ReserveRow {
	r := res(amount, cp, false)
	r.ConcentrationExemption = category
	return r
}

// ---- reserve asset value (Table 1 to 15.11(e)) -----------------------------------------

func TestCoverageSubtractsRepoLiabilities(t *testing.T) {
	m := mustFinding(t)
	f := m(engine.CheckCoverage(cov, []model.ReserveRow{res("100", "A", false), liability("20")}, supply("100")))
	if f.Passed || !f.Observed.Equal(dec("0.8")) || f.Details["reserve_asset_value"] != "80" ||
		f.Details["outstanding_repo_liabilities"] != "20" || f.Details["total_reserves"] != "100" || f.Details["shortfall"] != "20" {
		t.Fatalf("%+v", f)
	}
	// Exactly at the limit after netting still passes.
	f = m(engine.CheckCoverage(cov, []model.ReserveRow{res("120", "A", false), liability("20")}, supply("100")))
	if !f.Passed || !f.Observed.Equal(dec("1")) {
		t.Fatalf("exactly 100%% after netting: %+v", f)
	}
	// One cent less fails.
	f = m(engine.CheckCoverage(cov, []model.ReserveRow{res("119.99", "A", false), liability("20")}, supply("100")))
	if f.Passed {
		t.Fatal("99.99 of 100 passed")
	}
}

func TestLiabilityRowsAreNotReserveAssets(t *testing.T) {
	m := mustFinding(t)
	// 5 of 100 assets are available; a 50 liability must not dilute the denominator.
	reserves := []model.ReserveRow{res("5", "A", true), res("95", "B", false), liability("50")}
	if f := m(engine.CheckLiquidityFloor(liq, reserves)); !f.Passed || !f.Observed.Equal(dec("0.05")) {
		t.Errorf("liquidity: %+v", f)
	}
	if f := m(engine.CheckCounterpartyConcentration(con, reserves)); f.Details["total_reserves"] != "100" {
		t.Errorf("concentration denominator: %+v", f.Details)
	}
	if _, err := engine.CheckCoverage(cov, []model.ReserveRow{liability("5")}, supply("100")); err == nil {
		t.Error("a file with only liabilities has no reserve assets")
	}
}

// ---- concentration exemptions (15.11(c)(2)(ii)(B)) -------------------------------------

func TestConcentrationExemptionsLeaveTheNumeratorButStayInTheDenominator(t *testing.T) {
	m := mustFinding(t)
	reserves := []model.ReserveRow{
		exempt("60", "US Treasury", model.ExemptUSGovernment),
		res("30", "Bank B", false),
		res("10", "Bank C", false),
	}
	f := m(engine.CheckCounterpartyConcentration(con, reserves))
	if !f.Passed || !f.Observed.Equal(dec("0.3")) || f.Details["top_counterparty"] != "Bank B" ||
		f.Details["exempt_exposure_total"] != "60" || f.Details["total_reserves"] != "100" {
		t.Fatalf("%+v", f)
	}
	if got := f.Details["exempt_exposure"].(map[string]string); got[model.ExemptUSGovernment] != "60" || len(got) != 1 {
		t.Errorf("exempt breakdown: %v", got)
	}
	// The same data with no exemption breaches the limit.
	plain := []model.ReserveRow{res("60", "US Treasury", false), res("30", "Bank B", false), res("10", "Bank C", false)}
	if f := m(engine.CheckCounterpartyConcentration(con, plain)); f.Passed {
		t.Error("60% unexempted exposure passed")
	}
}

func TestExemptionBoundaryAndCategories(t *testing.T) {
	m := mustFinding(t)
	reserves := []model.ReserveRow{
		exempt("20", "Custodian X", model.ExemptCustodian),
		exempt("20", "CCP", model.ExemptCentrallyClearedRepo),
		res("40", "Bank B", false), // exactly 40%: passes
		res("20", "Bank C", false),
	}
	f := m(engine.CheckCounterpartyConcentration(con, reserves))
	if !f.Passed || !f.Observed.Equal(dec("0.4")) {
		t.Fatalf("%+v", f)
	}
	if f.Details["exempt_exposure_total"] != "40" {
		t.Errorf("exempt total: %v", f.Details["exempt_exposure_total"])
	}
	// One cent more at Bank B breaches.
	reserves[2] = res("40.01", "Bank B", false)
	if f := m(engine.CheckCounterpartyConcentration(con, reserves)); f.Passed {
		t.Error("40.01% passed")
	}
}

func TestEverythingExemptIsZeroConcentration(t *testing.T) {
	f, err := engine.CheckCounterpartyConcentration(con, []model.ReserveRow{exempt("100", "US Treasury", model.ExemptUSGovernment)})
	if err != nil || !f.Passed || !f.Observed.IsZero() || f.Details["top_counterparty"] != "" {
		t.Fatalf("%v %+v", err, f)
	}
}

// ---- 24-hour redemption surge (15.12(d)(1)) --------------------------------------------

var surgeRule = rule("redemption_surge", true, "      max_share_of_issuance: 0.10\n      window_hours: 24\n")

func req(at, amount string, status model.RedemptionStatus) model.RedemptionRow {
	r := red(at, "", "", status)
	r.Amount = dec(amount)
	return r
}

func TestSurgeBoundaryIsStrictlyInExcess(t *testing.T) {
	m := mustFinding(t)
	// Supply 1000: 10% is 100. Exactly 100 in a day is not "in excess".
	rows := []model.RedemptionRow{
		req("2027-02-01T09:00:00Z", "60", model.StatusCompleted),
		req("2027-02-01T20:00:00Z", "40", model.StatusPending),
	}
	f := m(engine.CheckRedemptionSurge(surgeRule, rows, supply("1000")))
	if !f.Passed || f.Warning || f.Status() != "pass" || !f.Observed.Equal(dec("0.1")) || f.Details["notice_to_occ_required"] != false {
		t.Fatalf("exactly at the limit: %+v", f)
	}
	rows[1].Amount = dec("40.01")
	f = m(engine.CheckRedemptionSurge(surgeRule, rows, supply("1000")))
	if !f.Passed || !f.Warning || f.Status() != "warning" || f.Details["notice_to_occ_required"] != true {
		t.Fatalf("just over: %+v", f)
	}
	if f.Details["largest_window_requests"] != 2 || f.Details["largest_window_amount"] != "100.01" || f.Details["notice_deadline_hours"] != 24 {
		t.Errorf("details: %+v", f.Details)
	}
}

func TestSurgeWindowEdges(t *testing.T) {
	m := mustFinding(t)
	big := func(at string) model.RedemptionRow { return req(at, "60", model.StatusCompleted) }
	// 60 + 60 = 120 > 100, but only if both fall inside one 24-hour window.
	cases := map[string]bool{
		"2027-02-02T08:59:59Z": true,  // 23h59m59s later: inside
		"2027-02-02T09:00:00Z": false, // exactly 24h later: window is half-open
		"2027-02-03T09:00:00Z": false,
	}
	for second, wantNotice := range cases {
		f := m(engine.CheckRedemptionSurge(surgeRule, []model.RedemptionRow{big("2027-02-01T09:00:00Z"), big(second)}, supply("1000")))
		if f.Warning != wantNotice {
			t.Errorf("second request at %s: notice=%v, want %v", second, f.Warning, wantNotice)
		}
	}
}

func TestSurgeCountsEveryStatusAndHandlesUnsortedInput(t *testing.T) {
	m := mustFinding(t)
	rows := []model.RedemptionRow{
		req("2027-02-01T12:00:00Z", "40", model.StatusRejected),
		req("2027-02-01T06:00:00Z", "40", model.StatusCancelled),
		req("2027-02-01T09:00:00Z", "40", model.StatusCompleted),
	}
	f := m(engine.CheckRedemptionSurge(surgeRule, rows, supply("1000")))
	if !f.Warning || f.Details["largest_window_amount"] != "120" || f.Details["largest_window_first"] != "2027-02-01T06:00:00Z" {
		t.Fatalf("%+v", f.Details)
	}
	if rows[0].RequestTS.Format(time.RFC3339) != "2027-02-01T12:00:00Z" {
		t.Error("the caller's slice must not be reordered")
	}
}

func TestSurgeSameInstantRequestsAndErrors(t *testing.T) {
	m := mustFinding(t)
	same := []model.RedemptionRow{
		req("2027-02-01T09:00:00Z", "60", model.StatusCompleted),
		req("2027-02-01T09:00:00Z", "60", model.StatusCompleted),
	}
	if f := m(engine.CheckRedemptionSurge(surgeRule, same, supply("1000"))); !f.Warning {
		t.Error("two simultaneous requests totalling 12% should trigger notice")
	}
	if _, err := engine.CheckRedemptionSurge(surgeRule, nil, supply("1000")); err == nil {
		t.Error("no rows accepted")
	}
	noWindow := rule("redemption_surge", true, "      max_share_of_issuance: 0.10\n")
	if _, err := engine.CheckRedemptionSurge(noWindow, same, supply("1000")); err == nil {
		t.Error("missing window_hours accepted")
	}
	zero := rule("redemption_surge", true, "      max_share_of_issuance: 0.10\n      window_hours: 0\n")
	if _, err := engine.CheckRedemptionSurge(zero, same, supply("1000")); err == nil {
		t.Error("zero-hour window accepted")
	}
}

// ---- credited_ts completes a redemption (15.12(a)(2)) ----------------------------------

func TestCreditedTimestampCompletesARedemption(t *testing.T) {
	credited := done("2027-02-01T15:00:00Z", "")
	credited.TransferInitiatedTS = nil
	at := ts("2027-02-03T15:00:00Z") // Wednesday: 2 business days
	credited.CreditedTS = &at
	ev := evalOne(t, credited, asOfDefault)
	if ev.Open || ev.Elapsed != 2 || ev.Outcome != engine.OutcomeWarning || len(ev.Anomalies) != 0 {
		t.Fatalf("credit-only redemption: %+v", ev)
	}

	// With both recorded the earlier event ends the clock.
	later := ts("2027-02-05T15:00:00Z")
	both := credited
	both.TransferInitiatedTS = &later
	if ev := evalOne(t, both, asOfDefault); ev.Elapsed != 2 {
		t.Errorf("earliest completing event should win, elapsed %d", ev.Elapsed)
	}
	earlierTransfer := ts("2027-02-02T15:00:00Z")
	both.TransferInitiatedTS = &earlierTransfer
	if ev := evalOne(t, both, asOfDefault); ev.Elapsed != 1 || ev.Outcome != engine.OutcomeOK {
		t.Errorf("earlier transfer should win: %+v", ev)
	}
}

func TestCompletedWithNeitherEventNamesBothColumns(t *testing.T) {
	ev := evalOne(t, red("2027-02-01T15:00:00Z", "", "", model.StatusCompleted), asOfDefault)
	if !ev.Open || len(ev.Anomalies) != 1 || !strings.Contains(ev.Anomalies[0], "credited_ts") {
		t.Fatalf("%+v", ev)
	}
}

func TestCreditedBeforeClockStartIsAnAnomaly(t *testing.T) {
	row := red("2027-02-03T15:00:00Z", "", "", model.StatusCompleted)
	at := ts("2027-02-01T15:00:00Z")
	row.CreditedTS = &at
	ev := evalOne(t, row, asOfDefault)
	if ev.Elapsed != 0 || len(ev.Anomalies) != 1 || !strings.Contains(ev.Anomalies[0], "precedes") {
		t.Fatalf("%+v", ev)
	}
}
