# reservelint report

> Monitoring and reporting aid. Not legal advice. Not an official OCC form.

**Overall: PASS** (0 fail, 0 warning, 4 pass)

- Generated: 2027-02-01T21:30:00Z by reservelint 0.1.0-dev
- Snapshot as of: 2027-02-01T21:00:00Z
- Exit code: 0

## Findings

| Rule | Check | Status | Observed | Requirement | Rule verified |
|---|---|---|---|---|---|
| RW-COV-001 | coverage | PASS | 102% | at least 100% | NO |
| RW-LIQ-001 | liquidity_floor | PASS | 20% | at least 5% | yes |
| RW-CON-001 | counterparty_concentration | PASS | 21.7011% | at most 40% | yes |
| RW-RED-001 | redemption_timing | PASS | 1 business days | at most 2 business days | yes |

### RW-COV-001: coverage - PASS

Total reserves must be at least the circulating supply.

- Observed: 102%; required at least 100%
- Citation: UNVERIFIED. Citation pending: 12 CFR Part 15 reserve assets section (text to be pasted).
- Rule verified: NO
- Rule notes: Confirm whether the ratio is on face value, market value, or another measure.
- circulating_supply: 100000000
- shortfall: 0
- supply_as_of: 2027-02-01T21:00:00Z
- total_reserves: 102000000

### RW-LIQ-001: liquidity_floor - PASS

At least a minimum share of reserve assets must be held as demand deposits (or other deposits withdrawable on request at any time), share accounts payable on demand, or money in an account with a Federal Reserve Bank.

- Observed: 20%; required at least 5%
- Citation: OCC final rule, 12 CFR 15.11(c)(2)(i) (as described in the final rule preamble).
- Rule verified: yes
- Rule notes: Figure taken from preamble text, not the codified regulation text; re-check against the codified text. The input flag is_immediately_available must be set only for assets in the categories above. The weekly-liquidity (5 business days) requirement was dropped in the final rule, so none is modelled.
- immediately_available: 20399999.98
- total_reserves: 102000000

### RW-CON-001: counterparty_concentration - PASS

Exposure to any one counterparty may not exceed a maximum share of reserve assets.

- Observed: 21.7011%; required at most 40%
- Citation: OCC final rule, 12 CFR 15.11(c)(2)(ii) (as described in the final rule preamble).
- Rule verified: yes
- Rule notes: The rule is "subject to certain exceptions" that are NOT modelled; the check applies the limit to every counterparty, so it can over-report. Exposure is measured as the sum of reserve rows per counterparty over total reserve assets. Per-institution holdings and weighted average maturity limits were dropped from the final rule.
- shares:
  - Synthetic Bank Alpha: 0.2060243296078431
  - Synthetic Bank Beta: 0.2112277698039216
  - Synthetic Bank Epsilon: 0.1924857718627451
  - Synthetic Custody Gamma: 0.1732514242156863
  - Synthetic Trust Delta: 0.2170107045098039
- top_counterparty: Synthetic Trust Delta
- top_counterparty_amount: 22135091.86
- total_reserves: 102000000

### RW-RED-001: redemption_timing - PASS

A timely redemption may not exceed a maximum number of business days following the redemption request.

- Observed: 1 business days; required at most 2 business days
- Citation: OCC final rule, 12 CFR 15.12(a)(1), 15.12(a)(2), 15.12(c)(1); business day as defined in 12 CFR 15.2 (as described in the final rule preamble).
- Rule verified: yes
- Rule notes: Figures and definitions taken from preamble text, not the codified regulation text. "Business day" means any day other than a Saturday, Sunday, or Federal holiday. The counting convention, near-breach margin and handling of open, rejected and cancelled requests are owner decisions recorded above, not text from the rule.
- as_of: 2027-02-01T21:00:00Z
- breach_amount: 0
- breaches: 0
- calendar: US Federal holidays
- calendar_kind: federal
- calendar_verified: false
- cancelled: 0
- excluded_amount: 0
- max_business_days_used: 1
- near_breach_margin: 1
- ok: 60
- open_requests: 2
- rejected: 0
- requests_evaluated: 60
- requests_total: 60
- rows_with_anomalies: 0
- timezone: America/New_York
- total_amount: 15114161.51
- warning_amount: 0
- warnings: 0

## Redemption timing

| Requests | Evaluated | OK | Warning | Breach | Rejected | Cancelled | Still open | Max business days used |
|---|---|---|---|---|---|---|---|---|
| 60 | 60 | 60 | 0 | 0 | 0 | 0 | 2 | 1 |

Amounts: total 15114161.51; breach 0; warning 0; excluded (rejected or cancelled) 0.

Requests needing attention (clock start is the later of request and onboarding; dates are New York calendar dates):

| Outcome | Requested | Clock start | Clock end | Days used | Due | Amount | Status | Notes |
|---|---|---|---|---|---|---|---|---|
| OK | 2027-02-01T10:39:02Z | 2027-02-01T10:39:02Z | 2027-02-01T21:00:00Z (open, to as-of) | 0 | 2027-02-03 | 106426.02 | pending |  |
| OK | 2027-02-01T16:11:37Z | 2027-02-01T16:11:37Z | 2027-02-01T21:00:00Z (open, to as-of) | 0 | 2027-02-03 | 259506.97 | pending |  |

## Input integrity

| File | Role | SHA-256 | Rows read | Rows used | Duplicates ignored | Invalid rows |
|---|---|---|---|---|---|---|
| testdata/vectors/clean/reserves.csv | reserves | `5932ad67332df7319566b0877b81d3391bf61db7818afcc5eb95f7630f8b02f6` | 15 | 15 | 0 | 0 |
| testdata/vectors/clean/supply.csv | supply | `64bd41611f30bb5e27a83f783541ebefe8b883e68f29fcc3787d369180862bb3` | 1 | 1 | 0 | 0 |
| testdata/vectors/clean/redemptions.csv | redemptions | `eaebffca2e4a3ac75ad929d9e9e3f6331e02da8034ab38855ecc4858c7390c15` | 60 | 60 | 0 | 0 |
| rules/rules.yaml | rules | `e9267c6318359e79ab18135b99fe107cadaa76d7c467ce47e0bfd7ebbaf2dc21` | | | | |
| config/holidays.yaml | holiday calendar | `1e25d093645d8ca19aa710cc0ae78fff4d4059587d829e183caf803ffe463137` | | | | |

## Rules tracked but not checked

- RW-STR-001: Reserve stress testing requirement. (no check implemented)
- RW-REP-001: Weekly reserve reporting. (no check implemented)
- RW-REP-002: Quarterly reporting. (no check implemented)

## Caveats

- Rule RW-COV-001 (coverage) is not verified against the rule text; its threshold is a placeholder and its result is indicative only.
- Holiday calendar "US Federal holidays" is not verified against an official schedule; business-day counts depend on it.

---
Monitoring and reporting aid. Not legal advice. Not an official OCC form.
