# Rule-to-check map

> Monitoring and reporting aid. Not legal advice. Not an official OCC form.

How each rule in [`rules/rules.yaml`](../rules/rules.yaml) maps to code, inputs and report
output. Thresholds live only in the rules file; the code reads them from rule parameters.
"Verified" is the owner's confirmation of the rule against rule text, as recorded in the file.
Sources are the codified text of 12 CFR 15.11 and 15.12 as supplied by the owner. Three points
still rest on the final rule preamble or owner decisions, and are marked in the rules file: the
clock-start rule for redemptions (15.12(a)(1)), the business-day definition (15.2), and the
counting convention.

| Rule | Check (code) | Inputs | Parameters (from rules.yaml) | Passes when | Verified |
|---|---|---|---|---|---|
| RW-COV-001 | `coverage` ([`CheckCoverage`](../internal/engine/engine.go)) | reserves, supply | `min_coverage_ratio` | (total reserve assets - repo liabilities) / outstanding issuance >= ratio | Yes |
| RW-LIQ-001 | `liquidity_floor` ([`CheckLiquidityFloor`](../internal/engine/engine.go)) | reserves | `min_immediately_available_share` | share flagged `is_immediately_available` >= minimum | Yes |
| RW-CON-001 | `counterparty_concentration` ([`CheckCounterpartyConcentration`](../internal/engine/engine.go)) | reserves | `max_counterparty_share` | largest non-exempt counterparty share <= maximum | Yes |
| RW-RED-001 | `redemption_timing` ([`CheckRedemptionTiming`](../internal/engine/redemption.go)) | redemptions, supply `as_of`, holiday calendar | `max_business_days`, `near_breach_margin_business_days`, `timezone`, `holiday_calendar` | no evaluated redemption exceeds the maximum business days | Yes |
| RW-RED-002 | `redemption_surge` ([`CheckRedemptionSurge`](../internal/engine/surge.go)) | redemptions, supply | `max_share_of_issuance`, `window_hours` | always (a duty to notify, not a violation); WARNING when requests in a 24-hour window exceed the share | Yes |
| RW-COMP-001 | `reserve_composition` ([`CheckReserveComposition`](../internal/engine/composition.go)) | reserves, snapshot date, holiday calendar | `eligible_asset_types`, `asset_type_aliases`, `timezone`, `holiday_calendar` | every reserve asset is an eligible type and within its remaining-maturity limit | Yes |
| RW-STR-001 | none | none | none | not checked (qualitative operational requirements) | Yes |
| RW-RPT-001 | none | none | none | not checked (monthly composition report) | Yes |
| RW-RPT-002 | none | none | none | not checked (monthly examination and certification) | Yes |
| RW-NOT-001 | none | none | none | not checked (needs daily history) | Yes |

Rules with no check are listed under "Rules tracked but not checked" in every report.

## Check details

**Coverage.** Reserve asset value / outstanding issuance, using the latest supply snapshot.
Reserve asset value is total reserve assets minus outstanding repurchase-agreement liabilities
(Table 1 to 15.11(e), rows 15 to 17). Supply liabilities as reserves rows with
`is_repo_liability` true; they are excluded from every other total. Details record the assets,
liabilities, reserve asset value and shortfall. A shortfall also triggers the notification and
issuance duties of 15.11(g), which the tool cannot track over time (RW-NOT-001).

**Liquidity floor.** Share of reserve amount whose `is_immediately_available` flag is true.
The tool trusts the flag: it must be set only for demand deposits, on-demand share accounts and
Federal Reserve Bank balances (RW-LIQ-001 notes).

**Counterparty concentration.** Rows are summed per counterparty after trimming, collapsing
whitespace and ignoring case. Rows marked `concentration_exemption` (`us_government`,
`custodian`, `centrally_cleared_repo`, the exposures excluded by 15.11(c)(2)(ii)(B)) are left out
of every counterparty's numerator but stay in the denominator, and the report lists the exempt
amounts. The tool cannot verify the custodian conditions of 15.11(d); an exemption is taken as
asserted by the issuer.

**Reserve composition.** Each reserve asset's `asset_type` must be one of the eligible types in the
rule (or an alias of one), compared ignoring case, spaces and `-` versus `_`. Types with a limit
(Treasuries: 93 calendar days; overnight repos: 1 business day) need a `maturity_date` within it,
measured from the snapshot date as a New York calendar date; a matured asset is ineligible. The
check fails if any reserve asset is ineligible and lists each one with the reason. It tests type
and remaining maturity only: collateral, overcollateralization, counterparty creditworthiness,
fund holdings, OCC approvals and FDIC/NCUA deposit limits cannot be checked from this data and are
listed as unverifiable. An `asset_type` that is eligible but not in the list fails until you add
it, or an alias, to `rules/rules.yaml`. Rows flagged `is_repo_liability` are skipped.

**Redemption surge.** Finds the busiest rolling 24-hour window of requests (every status counts)
and compares its total with `max_share_of_issuance` of the latest supply, exactly and strictly
("in excess of"). Exceeding it means the issuer must notify the OCC within 24 hours; the tool
cannot see that happen, so it reports a WARNING with the window, amount and request count, never
a FAIL. The rule text does not say whether the period is rolling or a calendar day; rolling is
the more conservative reading.

**Redemption timing.**
- Clock start: `max(request_ts, onboarded_ts)`; clock end: `transfer_initiated_ts`, or
  `credited_ts` when value was credited to the customer's account with the issuer (15.12(a)(2)).
  If both are set, the earlier one ends the clock.
- Requests with no transfer yet are measured to the snapshot `as_of` (default: latest supply
  `as_of`, override with `--as-of`).
- Elapsed days are business days **after** the start date through the end date, as calendar
  dates in `America/New_York`. Start Monday: Wednesday is 2 (at the limit), Thursday is 3 (breach).
- Business day: not a Saturday, Sunday or a holiday listed in `config/holidays.yaml`.
- A request is a WARNING when fewer than `near_breach_margin_business_days` business days remain
  (margin 1, maximum 2: exactly 2 days used). Warnings do not fail the run.
- `rejected` and `cancelled` requests are excluded from breach checks but counted, with
  amounts, in the summary.
- Anomalies (transfer before clock start, completed with no transfer time, request after
  `as_of`) are listed on the row; a completed row with no transfer time is measured to `as_of`.

## Data-quality finding: DUPLICATE_ROW_WARNING

Not a regulatory rule (no entry in `rules.yaml`, never capped or failing). Raised when identical
reserve rows without a `holding_id` were kept as separate positions: observed = number of repeated
rows, threshold 0, status WARNING, exit code unaffected. Details give the line numbers and the
amount in the repeated rows, which is how much reserves would be overstated if they are accidental
repeats. Resolve by adding unique `holding_id` values, or acknowledge with `--allow-duplicate-rows`
(rows stay counted and listed; the warning is not raised). Rows sharing a `holding_id` and content
are dropped as repeats; the same id with different content is an invalid row.

## Unverified rules

A rule with `verified: false` is informational. If its check fails, the finding's status is
capped at WARNING (`uncapped_status: fail` in the JSON and CSV, "capped from FAIL" in the
Markdown), the exit code is not affected, and a caveat names the rule. Promote a rule by setting
`verified: true` after comparing it with the rule text.

## Comparison semantics

Pass/fail compares exact decimals by cross-multiplication (for example
`available >= threshold * total`), never a rounded quotient. A value exactly at a limit passes.

## Where results appear

| Output | Content |
|---|---|
| `report.md` | summary, per-rule detail with citation, redemption table, input integrity, caveats |
| `findings.csv` | one row per finding |
| `report.json` | everything, including all duplicates and invalid rows |
| `audit-<timestamp>.json` | tool version, options, SHA-256 of every input, the rules file and the calendar, output hashes, results; never overwritten |
