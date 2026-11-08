# reservelint report

> Monitoring and reporting aid. Not legal advice. Not an official OCC form.

**Overall: PASS** (0 fail, 0 warning, 6 pass)

- Generated: 2027-02-01T21:30:00Z by reservelint 0.1.0-dev
- Snapshot as of: 2027-02-01T21:00:00Z
- Exit code: 0

## Findings

| Rule | Check | Status | Observed | Requirement | Rule verified |
|---|---|---|---|---|---|
| RW-COV-001 | coverage | PASS | 102% | at least 100% | yes |
| RW-LIQ-001 | liquidity_floor | PASS | 20% | at least 5% | yes |
| RW-CON-001 | counterparty_concentration | PASS | 21.7011% | at most 40% | yes |
| RW-RED-001 | redemption_timing | PASS | 1 business days | at most 2 business days | yes |
| RW-RED-002 | redemption_surge | PASS | 1.6292% | at most 10% | yes |
| RW-COMP-001 | reserve_composition | PASS | 0% | at most 0% | yes |

### RW-COV-001: coverage - PASS

Reserve asset value must at all times equal or exceed the outstanding issuance value.

- Observed: 102%; required at least 100%
- Citation: 12 CFR 15.11(a)(1)(iii); reserve asset value as defined in Table 1 to 15.11(e), rows 15 to 17.
- Rule verified: yes
- Rule notes: Reserve asset value = total reserve assets minus outstanding repurchase-agreement liabilities. Rows flagged is_repo_liability are treated as liabilities, so supply them or coverage is overstated when the issuer has repos outstanding. Only a snapshot is checked, but the rule applies at all times. A shortfall also triggers 15.11(g): notify the OCC on any day reserves fall below the minimum, no new issuance until restored, and liquidation after 15 consecutive business days (the tool cannot see history; see RW-NOT-001).
- circulating_supply: 100000000
- outstanding_repo_liabilities: 0
- reserve_asset_value: 102000000
- shortfall: 0
- supply_as_of: 2027-02-01T21:00:00Z
- total_reserves: 102000000

### RW-LIQ-001: liquidity_floor - PASS

At least a minimum share of reserve assets must be held as demand deposits (or other deposits withdrawable on request at any time), share accounts payable on demand, or money in an account with a Federal Reserve Bank.

- Observed: 20%; required at least 5%
- Citation: 12 CFR 15.11(c)(2)(i).
- Rule verified: yes
- Rule notes: The input flag is_immediately_available must be set only for assets in the categories above; the tool trusts it. The denominator is total reserve assets (repo liabilities excluded).
- immediately_available: 20399999.98
- total_reserves: 102000000

### RW-CON-001: counterparty_concentration - PASS

Exposure to any one counterparty may not exceed a maximum share of reserve assets, except for exposures to the United States Government, to an appropriate custodian from assets held in its custody, and from centrally cleared repurchase and reverse repurchase agreements.

- Observed: 21.7011%; required at most 40%
- Citation: 12 CFR 15.11(c)(2)(ii)(A) and (B); appropriate custodians per 15.11(d).
- Rule verified: yes
- Rule notes: Exempt exposures are marked with the reserves column concentration_exemption (us_government, custodian, centrally_cleared_repo); they stay in the denominator but not in any counterparty's numerator. The tool cannot verify the conditions of 15.11(d) (the issuer identifies itself to the custodian, the custodian commits to GENIUS Act section 10, and ongoing due diligence), so an exemption is taken as asserted by the issuer. Exposure is measured as the sum of reserve rows per counterparty over total reserve assets.
- exempt_exposure:
- exempt_exposure_total: 0
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
- Citation: 12 CFR 15.12(a)(2) and 15.12(c)(1) (two business days); business day per 12 CFR 15.2.
- Rule verified: yes
- Rule notes: The two-business-day limit and the definition of when redemption occurs (15.12(a)(2)) are from the codified text supplied. The clock-start rule (valid order plus satisfied conditions such as onboarding, 15.12(a)(1)) and the business-day definition (15.2) were described in the final rule preamble and are not in the text supplied. The counting convention, near-breach margin and handling of open, rejected and cancelled requests are owner decisions, not text from the rule. Discretionary limits on redemption can only be imposed by the regulator (15.12(c)(2)); the tool does not model them.
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

### RW-RED-002: redemption_surge - PASS

Redemption requests in excess of a share of outstanding issuance value in a single 24-hour period require notice to the OCC within 24 hours.

- Observed: 1.6292%; required at most 10%
- Citation: 12 CFR 15.12(d)(1).
- Rule verified: yes
- Rule notes: A duty to notify, not a violation: the finding always passes and is a WARNING when notice is due; the tool cannot see whether notice was given. Every request received counts, whatever its later status. The 24-hour period is applied as a rolling window starting at each request; the rule text does not say whether it means rolling or calendar-day windows. Outstanding issuance is the latest supply snapshot, for a single brand of stablecoin.
- largest_window_amount: 1629195.49
- largest_window_first: 2027-01-07T21:30:02Z
- largest_window_last: 2027-01-08T19:46:54Z
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
  - demand_deposit: 20399999.98
  - treasury_security: 81600000.02
- eligible_total: 102000000
- ineligible_rows: 0
- ineligible_total: 0
- snapshot_date: 2027-02-01
- timezone: America/New_York
- total_reserves: 102000000
- unverifiable_by_tool: Only asset type and remaining maturity are tested. Collateral, overcollateralization, counterparty creditworthiness, fund holdings, OCC approvals and FDIC/NCUA deposit limits cannot be checked from this data.

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

| File | Role | SHA-256 | Rows read | Rows used | Exact repeats dropped | Identical unkeyed rows kept | Invalid rows |
|---|---|---|---|---|---|---|---|
| testdata/vectors/clean/reserves.csv | reserves | `5932ad67332df7319566b0877b81d3391bf61db7818afcc5eb95f7630f8b02f6` | 15 | 15 | 0 | 0 | 0 |
| testdata/vectors/clean/supply.csv | supply | `64bd41611f30bb5e27a83f783541ebefe8b883e68f29fcc3787d369180862bb3` | 1 | 1 | 0 | 0 | 0 |
| testdata/vectors/clean/redemptions.csv | redemptions | `eaebffca2e4a3ac75ad929d9e9e3f6331e02da8034ab38855ecc4858c7390c15` | 60 | 60 | 0 | 0 | 0 |
| rules/rules.yaml | rules | `f8774d1f1de2b2a979ea283e950ff39cdb4662a8d87a6836d153a09b4d483645` | | | | | |
| config/holidays.yaml | holiday calendar | `1e25d093645d8ca19aa710cc0ae78fff4d4059587d829e183caf803ffe463137` | | | | | |

## Rules tracked but not checked

- RW-STR-001: Demonstrate operational capability to access and monetize reserve assets to meet redemptions in normal and adverse conditions, and keep a buffer of reserve assets in excess of outstanding issuance value where needed. (no check implemented)
- RW-RPT-001: Publish the monthly composition of reserves on the issuer's website by noon on the last day of each month, in a format substantially similar to Table 1 to 15.11(e). (no check implemented)
- RW-RPT-002: Have the previous month-end composition report examined by a registered public accounting firm and published with it, and submit a monthly CEO and CFO accuracy certification to the OCC. (no check implemented)
- RW-NOT-001: Notify the OCC on any day reserve assets fall below the minimum; stop new issuance until restored; begin liquidation and redemption after 15 consecutive business days below it. (no check implemented)

## Caveats

- Holiday calendar "US Federal holidays" is not verified against an official schedule; business-day counts depend on it.

---
Monitoring and reporting aid. Not legal advice. Not an official OCC form.
