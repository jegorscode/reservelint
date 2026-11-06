# Rule-to-check map

> Monitoring and reporting aid. Not legal advice. Not an official OCC form.

How each rule in [`rules/rules.yaml`](../rules/rules.yaml) maps to code, inputs and report
output. Thresholds live only in the rules file; the code reads them from rule parameters.
"Verified" is the owner's confirmation of the rule against rule text, as recorded in the file.
The citations for verified rules refer to the final rule **preamble**, not the codified
regulation text; re-check them against the codified text before relying on them.

| Rule | Check (code) | Inputs | Parameters (from rules.yaml) | Passes when | Verified |
|---|---|---|---|---|---|
| RW-COV-001 | `coverage` ([`CheckCoverage`](../internal/engine/engine.go)) | reserves, supply | `min_coverage_ratio` | total reserves / circulating supply >= ratio | **No** (placeholder) |
| RW-LIQ-001 | `liquidity_floor` ([`CheckLiquidityFloor`](../internal/engine/engine.go)) | reserves | `min_immediately_available_share` | share flagged `is_immediately_available` >= minimum | Yes |
| RW-CON-001 | `counterparty_concentration` ([`CheckCounterpartyConcentration`](../internal/engine/engine.go)) | reserves | `max_counterparty_share` | largest counterparty share <= maximum | Yes |
| RW-RED-001 | `redemption_timing` ([`CheckRedemptionTiming`](../internal/engine/redemption.go)) | redemptions, supply `as_of`, holiday calendar | `max_business_days`, `near_breach_margin_business_days`, `timezone`, `holiday_calendar` | no evaluated redemption exceeds the maximum business days | Yes |
| RW-STR-001 | none | none | none | not checked | No |
| RW-REP-001 | none | none | none | not checked | No |
| RW-REP-002 | none | none | none | not checked | No |

Rules with no check are listed under "Rules tracked but not checked" in every report.

## Check details

**Coverage.** `total reserves / circulating supply`, using the latest supply snapshot.
Details record the totals and the shortfall in currency units.

**Liquidity floor.** Share of reserve amount whose `is_immediately_available` flag is true.
The tool trusts the flag: it must be set only for demand deposits, on-demand share accounts and
Federal Reserve Bank balances (RW-LIQ-001 notes).

**Counterparty concentration.** Rows are summed per counterparty after trimming, collapsing
whitespace and ignoring case. The rule's exceptions are **not modelled**, so the check can
over-report.

**Redemption timing.**
- Clock start: `max(request_ts, onboarded_ts)`; clock end: `transfer_initiated_ts`.
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
