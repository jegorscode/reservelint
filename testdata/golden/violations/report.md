# reservelint report

> Monitoring and reporting aid. Not legal advice. Not an official OCC form.

**Overall: FAIL** (4 fail, 0 warning, 0 pass)

- Generated: 2027-02-01T21:30:00Z by reservelint 0.1.0-dev
- Snapshot as of: 2027-02-01T21:00:00Z
- Exit code: 1

## Findings

| Rule | Check | Status | Observed | Requirement | Rule verified |
|---|---|---|---|---|---|
| RW-COV-001 | coverage | FAIL | 95% | at least 100% | NO |
| RW-LIQ-001 | liquidity_floor | FAIL | 2% | at least 5% | yes |
| RW-CON-001 | counterparty_concentration | FAIL | 55% | at most 40% | yes |
| RW-RED-001 | redemption_timing | FAIL | 8 business days | at most 2 business days | yes |

### RW-COV-001: coverage (FAIL)

Total reserves must be at least the circulating supply.

- Observed: 95%; required at least 100%
- Citation: UNVERIFIED. Citation pending: 12 CFR Part 15 reserve assets section (text to be pasted).
- Rule verified: NO
- Rule notes: Confirm whether the ratio is on face value, market value, or another measure.
- circulating_supply: 100000000
- shortfall: 5000000
- supply_as_of: 2027-02-01T21:00:00Z
- total_reserves: 95000000

### RW-LIQ-001: liquidity_floor (FAIL)

At least a minimum share of reserve assets must be held as demand deposits (or other deposits withdrawable on request at any time), share accounts payable on demand, or money in an account with a Federal Reserve Bank.

- Observed: 2%; required at least 5%
- Citation: OCC final rule, 12 CFR 15.11(c)(2)(i) (as described in the final rule preamble).
- Rule verified: yes
- Rule notes: Figure taken from preamble text, not the codified regulation text; re-check against the codified text. The input flag is_immediately_available must be set only for assets in the categories above. The weekly-liquidity (5 business days) requirement was dropped in the final rule, so none is modelled.
- immediately_available: 1900000
- total_reserves: 95000000

### RW-CON-001: counterparty_concentration (FAIL)

Exposure to any one counterparty may not exceed a maximum share of reserve assets.

- Observed: 55%; required at most 40%
- Citation: OCC final rule, 12 CFR 15.11(c)(2)(ii) (as described in the final rule preamble).
- Rule verified: yes
- Rule notes: The rule is "subject to certain exceptions" that are NOT modelled; the check applies the limit to every counterparty, so it can over-report. Exposure is measured as the sum of reserve rows per counterparty over total reserve assets. Per-institution holdings and weighted average maturity limits were dropped from the final rule.
- shares:
  - Synthetic Bank Alpha: 0.55
  - Synthetic Bank Beta: 0.1
  - Synthetic Bank Epsilon: 0.15
  - Synthetic Custody Gamma: 0.1
  - Synthetic Trust Delta: 0.1
- top_counterparty: Synthetic Bank Alpha
- top_counterparty_amount: 52250000
- total_reserves: 95000000

### RW-RED-001: redemption_timing (FAIL)

A timely redemption may not exceed a maximum number of business days following the redemption request.

- Observed: 8 business days; required at most 2 business days
- Citation: OCC final rule, 12 CFR 15.12(a)(1), 15.12(a)(2), 15.12(c)(1); business day as defined in 12 CFR 15.2 (as described in the final rule preamble).
- Rule verified: yes
- Rule notes: Figures and definitions taken from preamble text, not the codified regulation text. "Business day" means any day other than a Saturday, Sunday, or Federal holiday. The counting convention, near-breach margin and handling of open, rejected and cancelled requests are owner decisions recorded above, not text from the rule.
- as_of: 2027-02-01T21:00:00Z
- breach_amount: 2919323.2
- breaches: 10
- calendar: US Federal holidays
- calendar_kind: federal
- calendar_verified: false
- cancelled: 0
- excluded_amount: 0
- max_business_days_used: 8
- near_breach_margin: 1
- ok: 50
- open_requests: 3
- rejected: 0
- requests_evaluated: 60
- requests_total: 60
- rows_with_anomalies: 1
- timezone: America/New_York
- total_amount: 15505629.82
- warning_amount: 0
- warnings: 0

## Redemption timing

| Requests | Evaluated | OK | Warning | Breach | Rejected | Cancelled | Still open | Max business days used |
|---|---|---|---|---|---|---|---|---|
| 60 | 60 | 50 | 0 | 10 | 0 | 0 | 3 | 8 |

Amounts: total 15505629.82; breach 2919323.2; warning 0; excluded (rejected or cancelled) 0.

Requests needing attention (clock start is the later of request and onboarding; dates are New York calendar dates):

| Outcome | Requested | Clock start | Clock end | Days used | Due | Amount | Status | Notes |
|---|---|---|---|---|---|---|---|---|
| BREACH | 2027-01-04T07:23:58Z | 2027-01-04T07:23:58Z | 2027-01-08T17:27:44Z | 4 | 2027-01-06 | 390269.54 | completed |  |
| OK | 2027-01-05T02:26:54Z | 2027-01-05T05:39:00Z | 2027-01-05T05:39:00Z | 0 | 2027-01-07 | 137014.06 | completed | transfer_initiated_ts precedes the clock start; counted as 0 days |
| BREACH | 2027-01-05T15:40:41Z | 2027-01-05T15:40:41Z | 2027-01-11T23:37:00Z | 4 | 2027-01-07 | 452212.02 | completed |  |
| BREACH | 2027-01-09T12:31:16Z | 2027-01-09T12:31:16Z | 2027-01-14T19:36:08Z | 4 | 2027-01-12 | 68225.95 | completed |  |
| BREACH | 2027-01-15T11:06:18Z | 2027-01-15T11:06:18Z | 2027-01-23T23:50:53Z | 4 | 2027-01-20 | 393954.81 | completed |  |
| BREACH | 2027-01-15T21:52:08Z | 2027-01-15T21:52:08Z | 2027-01-22T19:21:10Z | 4 | 2027-01-20 | 146250.29 | completed |  |
| BREACH | 2027-01-18T08:39:45Z | 2027-01-18T09:42:31Z | 2027-01-24T05:28:37Z | 4 | 2027-01-20 | 116763.02 | completed |  |
| BREACH | 2027-01-20T21:00:00Z | 2027-01-20T21:00:00Z | 2027-02-01T21:00:00Z (open, to as-of) | 8 | 2027-01-22 | 496466.84 | pending |  |
| BREACH | 2027-01-20T21:00:00Z | 2027-01-20T21:00:00Z | 2027-02-01T21:00:00Z (open, to as-of) | 8 | 2027-01-22 | 357266.44 | pending |  |
| BREACH | 2027-01-22T04:42:22Z | 2027-01-22T04:42:22Z | 2027-01-29T12:03:43Z | 6 | 2027-01-25 | 353717.89 | completed |  |
| BREACH | 2027-01-26T09:30:31Z | 2027-01-26T09:30:31Z | 2027-02-01T21:00:00Z (open, to as-of) | 4 | 2027-01-28 | 144196.4 | pending |  |

## Input integrity

| File | Role | SHA-256 | Rows read | Rows used | Duplicates ignored | Invalid rows |
|---|---|---|---|---|---|---|
| testdata/vectors/violations/reserves.csv | reserves | `7fe704a749021ce5352f0966623253b34b77b50c04cd406a959bf03346869fbf` | 16 | 15 | 1 | 0 |
| testdata/vectors/violations/supply.csv | supply | `64bd41611f30bb5e27a83f783541ebefe8b883e68f29fcc3787d369180862bb3` | 1 | 1 | 0 | 0 |
| testdata/vectors/violations/redemptions.csv | redemptions | `6a83340d04ad78b9b031654b9494d2807e6302946c7675caef90a419cf4c4fa2` | 60 | 60 | 0 | 0 |
| rules/rules.yaml | rules | `e9267c6318359e79ab18135b99fe107cadaa76d7c467ce47e0bfd7ebbaf2dc21` | | | | |
| config/holidays.yaml | holiday calendar | `1e25d093645d8ca19aa710cc0ae78fff4d4059587d829e183caf803ffe463137` | | | | |

Duplicate rows in `testdata/vectors/violations/reserves.csv` (counted once; positions are line numbers or item indexes):

- first at 2, repeated at 17

## Rules tracked but not checked

- RW-STR-001: Reserve stress testing requirement. (no check implemented)
- RW-REP-001: Weekly reserve reporting. (no check implemented)
- RW-REP-002: Quarterly reporting. (no check implemented)

## Caveats

- Rule RW-COV-001 (coverage) is not verified against the rule text; its threshold is a placeholder and its result is indicative only.
- Holiday calendar "US Federal holidays" is not verified against an official schedule; business-day counts depend on it.

---
Monitoring and reporting aid. Not legal advice. Not an official OCC form.
