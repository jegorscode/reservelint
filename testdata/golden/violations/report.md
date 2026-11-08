# reservelint report

> Monitoring and reporting aid. Not legal advice. Not an official OCC form.

**Overall: FAIL** (4 fail, 1 warning, 2 pass)

- Generated: 2027-02-01T21:30:00Z by reservelint 0.1.0-dev
- Snapshot as of: 2027-02-01T21:00:00Z
- Exit code: 1

## Findings

| Rule | Check | Status | Observed | Requirement | Rule verified |
|---|---|---|---|---|---|
| RW-COV-001 | coverage | FAIL | 96.045% | at least 100% | yes |
| RW-LIQ-001 | liquidity_floor | FAIL | 3.0663% | at least 5% | yes |
| RW-CON-001 | counterparty_concentration | FAIL | 55.4896% | at most 40% | yes |
| RW-RED-001 | redemption_timing | FAIL | 8 business days | at most 2 business days | yes |
| RW-RED-002 | redemption_surge | PASS | 1.9298% | at most 10% | yes |
| RW-COMP-001 | reserve_composition | PASS | 0% | at most 0% | yes |
| DUPLICATE_ROW_WARNING | duplicate_rows | WARNING | 1 rows | at most 0 rows | yes |

### RW-COV-001: coverage - FAIL

Reserve asset value must at all times equal or exceed the outstanding issuance value.

- Observed: 96.045%; required at least 100%
- Citation: 12 CFR 15.11(a)(1)(iii); reserve asset value as defined in Table 1 to 15.11(e), rows 15 to 17.
- Rule verified: yes
- Rule notes: Reserve asset value = total reserve assets minus outstanding repurchase-agreement liabilities. Rows flagged is_repo_liability are treated as liabilities, so supply them or coverage is overstated when the issuer has repos outstanding. Only a snapshot is checked, but the rule applies at all times. A shortfall also triggers 15.11(g): notify the OCC on any day reserves fall below the minimum, no new issuance until restored, and liquidation after 15 consecutive business days (the tool cannot see history; see RW-NOT-001).
- circulating_supply: 100000000
- outstanding_repo_liabilities: 0
- reserve_asset_value: 96045000
- shortfall: 3955000
- supply_as_of: 2027-02-01T21:00:00Z
- total_reserves: 96045000

### RW-LIQ-001: liquidity_floor - FAIL

At least a minimum share of reserve assets must be held as demand deposits (or other deposits withdrawable on request at any time), share accounts payable on demand, or money in an account with a Federal Reserve Bank.

- Observed: 3.0663%; required at least 5%
- Citation: 12 CFR 15.11(c)(2)(i).
- Rule verified: yes
- Rule notes: The input flag is_immediately_available must be set only for assets in the categories above; the tool trusts it. The denominator is total reserve assets (repo liabilities excluded).
- immediately_available: 2945000
- total_reserves: 96045000

### RW-CON-001: counterparty_concentration - FAIL

Exposure to any one counterparty may not exceed a maximum share of reserve assets, except for exposures to the United States Government, to an appropriate custodian from assets held in its custody, and from centrally cleared repurchase and reverse repurchase agreements.

- Observed: 55.4896%; required at most 40%
- Citation: 12 CFR 15.11(c)(2)(ii)(A) and (B); appropriate custodians per 15.11(d).
- Rule verified: yes
- Rule notes: Exempt exposures are marked with the reserves column concentration_exemption (us_government, custodian, centrally_cleared_repo); they stay in the denominator but not in any counterparty's numerator. The tool cannot verify the conditions of 15.11(d) (the issuer identifies itself to the custodian, the custodian commits to GENIUS Act section 10, and ongoing due diligence), so an exemption is taken as asserted by the issuer. Exposure is measured as the sum of reserve rows per counterparty over total reserve assets.
- exempt_exposure:
- exempt_exposure_total: 0
- shares:
  - Synthetic Bank Alpha: 0.5548961424332344
  - Synthetic Bank Beta: 0.0989119683481701
  - Synthetic Bank Epsilon: 0.1483679525222552
  - Synthetic Custody Gamma: 0.0989119683481701
  - Synthetic Trust Delta: 0.0989119683481701
- top_counterparty: Synthetic Bank Alpha
- top_counterparty_amount: 53295000
- total_reserves: 96045000

### RW-RED-001: redemption_timing - FAIL

A timely redemption may not exceed a maximum number of business days following the redemption request.

- Observed: 8 business days; required at most 2 business days
- Citation: 12 CFR 15.12(a)(2) and 15.12(c)(1) (two business days); business day per 12 CFR 15.2.
- Rule verified: yes
- Rule notes: The two-business-day limit and the definition of when redemption occurs (15.12(a)(2)) are from the codified text supplied. The clock-start rule (valid order plus satisfied conditions such as onboarding, 15.12(a)(1)) and the business-day definition (15.2) were described in the final rule preamble and are not in the text supplied. The counting convention, near-breach margin and handling of open, rejected and cancelled requests are owner decisions, not text from the rule. Discretionary limits on redemption can only be imposed by the regulator (15.12(c)(2)); the tool does not model them.
- as_of: 2027-02-01T21:00:00Z
- breach_amount: 2919323.2
- breaches: 10
- calendar: US Federal holidays
- calendar_kind: federal
- calendar_verified: true
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

### RW-RED-002: redemption_surge - PASS

Redemption requests in excess of a share of outstanding issuance value in a single 24-hour period require notice to the OCC within 24 hours.

- Observed: 1.9298%; required at most 10%
- Citation: 12 CFR 15.12(d)(1).
- Rule verified: yes
- Rule notes: A duty to notify, not a violation: the finding always passes and is a WARNING when notice is due; the tool cannot see whether notice was given. Every request received counts, whatever its later status. The 24-hour period is applied as a rolling window starting at each request; the rule text does not say whether it means rolling or calendar-day windows. Outstanding issuance is the latest supply snapshot, for a single brand of stablecoin.
- largest_window_amount: 1929811.73
- largest_window_first: 2027-01-05T15:40:41Z
- largest_window_last: 2027-01-06T03:37:24Z
- largest_window_requests: 5
- notice_deadline_hours: 24
- notice_to_occ_required: false
- outstanding_issuance: 100000000
- requests_considered: 60
- window_hours: 24

### RW-COMP-001: reserve_composition - PASS

Reserve assets must comprise exclusively eligible assets: US currency, Federal Reserve account balances, demand deposits and insured shares, Treasury securities of 93 days or less remaining maturity, overnight repurchase and reverse repurchase agreements on those terms, qualifying government money market fund shares, OCC-approved similar assets, and tokenized forms of some of these.

- Observed: 0%; required at most 0%
- Citation: 12 CFR 15.11(b).
- Rule verified: yes
- Rule notes: Only the asset type and remaining maturity are tested. Collateral, overcollateralization and the tri-party, cleared or creditworthy-counterparty condition of reverse repos, money market fund holdings, OCC approval of other assets, and FDIC or NCUA deposit limits cannot be checked from the input and are listed as unverifiable. Rows flagged is_repo_liability are not reserve assets and are skipped. The list is closed ("must comprise exclusively"): an asset_type missing from it, including a typo, FAILS the check (exit 1) and the message lists every allowed type. If the asset is eligible, add its name to asset_type_aliases. "Overnight" is applied as at most one business day to maturity. The eligible type names are this tool's vocabulary, not regulatory terms.
- eligible_by_type:
  - demand_deposit: 2945000
  - treasury_security: 93100000
- eligible_total: 96045000
- ineligible_rows: 0
- ineligible_total: 0
- snapshot_date: 2027-02-01
- timezone: America/New_York
- total_reserves: 96045000
- unverifiable_by_tool: Only asset type and remaining maturity are tested. Collateral, overcollateralization, counterparty creditworthiness, fund holdings, OCC approvals and FDIC/NCUA deposit limits cannot be checked from this data.

### DUPLICATE_ROW_WARNING: duplicate_rows - WARNING

Identical reserve rows without a holding_id were kept as separate positions; they may be repeated lines.

- Observed: 1 rows; required at most 0 rows
- Citation: Data-quality check, not a regulatory rule.
- Rule verified: yes
- amount_in_repeated_rows: 1045000
- effect: These rows are counted in total reserves. If they are accidental repeats, reserves are overstated by amount_in_repeated_rows.
- file: testdata/vectors/violations/reserves.csv
- groups: line 2 repeated at 17
- how_to_resolve: Give each position a unique holding_id, or pass --allow-duplicate-rows to acknowledge them as distinct positions.
- repeated_rows_kept: 1

## Redemption timing

| Requests | Evaluated | OK | Warning | Breach | Rejected | Cancelled | Still open | Max business days used |
|---|---|---|---|---|---|---|---|---|
| 60 | 60 | 50 | 0 | 10 | 0 | 0 | 3 | 8 |

Amounts: total 15505629.82; breach 2919323.2; warning 0; excluded (rejected or cancelled) 0.

Requests needing attention (clock start is the later of request and onboarding; dates are New York calendar dates):

| Outcome | Requested | Clock start | Clock end | Days used | Due | Amount | Status | Notes |
|---|---|---|---|---|---|---|---|---|
| BREACH | 2027-01-04T07:23:58Z | 2027-01-04T07:23:58Z | 2027-01-08T17:27:44Z | 4 | 2027-01-06 | 390269.54 | completed |  |
| OK | 2027-01-05T02:26:54Z | 2027-01-05T05:39:00Z | 2027-01-05T05:39:00Z | 0 | 2027-01-07 | 137014.06 | completed | the completing event (transfer_initiated_ts or credited_ts) precedes the clock start; counted as 0 days |
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

| File | Role | SHA-256 | Rows read | Rows used | Exact repeats dropped | Identical unkeyed rows kept | Invalid rows |
|---|---|---|---|---|---|---|---|
| testdata/vectors/violations/reserves.csv | reserves | `7fe704a749021ce5352f0966623253b34b77b50c04cd406a959bf03346869fbf` | 16 | 16 | 0 | 1 | 0 |
| testdata/vectors/violations/supply.csv | supply | `64bd41611f30bb5e27a83f783541ebefe8b883e68f29fcc3787d369180862bb3` | 1 | 1 | 0 | 0 | 0 |
| testdata/vectors/violations/redemptions.csv | redemptions | `6a83340d04ad78b9b031654b9494d2807e6302946c7675caef90a419cf4c4fa2` | 60 | 60 | 0 | 0 | 0 |
| rules/rules.yaml | rules | `f8774d1f1de2b2a979ea283e950ff39cdb4662a8d87a6836d153a09b4d483645` | | | | | |
| config/holidays.yaml | holiday calendar | `b0c79764a7b1173d5b16ee9ea764424bb9b9ce65a00142ea6b905e775b2dd9d4` | | | | | |

Identical rows without a holding_id in `testdata/vectors/violations/reserves.csv` (kept as separate positions and counted):

- first at 2, repeated at 17

## Rules tracked but not checked

- RW-STR-001: Demonstrate operational capability to access and monetize reserve assets to meet redemptions in normal and adverse conditions, and keep a buffer of reserve assets in excess of outstanding issuance value where needed. (no check implemented)
- RW-RPT-001: Publish the monthly composition of reserves on the issuer's website by noon on the last day of each month, in a format substantially similar to Table 1 to 15.11(e). (no check implemented)
- RW-RPT-002: Have the previous month-end composition report examined by a registered public accounting firm and published with it, and submit a monthly CEO and CFO accuracy certification to the OCC. (no check implemented)
- RW-NOT-001: Notify the OCC on any day reserve assets fall below the minimum; stop new issuance until restored; begin liquidation and redemption after 15 consecutive business days below it. (no check implemented)

---
Monitoring and reporting aid. Not legal advice. Not an official OCC form.
