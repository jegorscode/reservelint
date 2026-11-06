# reservelint

> **Monitoring and reporting aid. Not legal advice. Not an official OCC form.**

An open-source, auditable CLI that checks permitted payment stablecoin issuer reserve and
redemption data against rules defined in `rules/rules.yaml` and writes a report. Written in Go:
one static binary, no runtime to install.

This tool does not determine legal compliance. A PASS means the supplied data met the
thresholds in the rules file, nothing more. Rules with `verified: false` only ever
WARN, and the holiday calendar is unverified; reports say so. Synthetic data only: no real customer data
belongs in this repository.

## Install and run

```bash
go build -o reservelint ./cmd/reservelint          # or: go install ./cmd/reservelint
reservelint generate-synthetic --out data/synthetic
reservelint check \
  --rules rules/rules.yaml \
  --reserves data/synthetic/violations/reserves.csv \
  --supply data/synthetic/violations/supply.csv \
  --redemptions data/synthetic/violations/redemptions.csv \
  --out report/
```

Static builds for any platform: `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-X github.com/jegorscode/reservelint/internal/meta.Version=1.0.0" ./cmd/reservelint`.

### `check` flags

| Flag | Meaning |
|---|---|
| `--rules` | rules file (default `rules/rules.yaml`) |
| `--reserves`, `--supply`, `--redemptions` | input files, `.csv` or `.json` (required) |
| `--holidays` | holiday calendar; defaults to the redemption rule's `holiday_calendar` |
| `--out` | report directory (default `report`) |
| `--as-of` | snapshot time, RFC 3339; defaults to the latest supply `as_of` |
| `--allow-duplicate-rows` | acknowledge identical reserve rows without a `holding_id` as distinct positions (no `DUPLICATE_ROW_WARNING`) |
| `--allow-invalid-rows` | continue past invalid rows and report a PARTIAL run (debugging) |

### Exit codes

| Code | Meaning |
|---|---|
| 0 | PASS or WARNING (including capped unverified rules and `DUPLICATE_ROW_WARNING`) |
| 1 | at least one FAIL finding from a verified rule (the report is still written) |
| 2 | usage, input, parsing or configuration error (nothing is written) |

Rules with `verified: false` never fail a run: if their check fails, the finding is reported as
WARNING (marked "capped from FAIL", with the uncapped status in `report.json` and
`findings.csv`) and the exit code stays 0. Set `verified: true` in `rules/rules.yaml` to promote one.

By default any invalid input row stops the run with exit 2. With `--allow-invalid-rows` the
run continues on the valid rows, the report is marked PARTIAL, and the overall status can
never be a clean PASS.

## Output

`report/` receives `report.md` (for people), `findings.csv` (spreadsheets), `report.json`
(programs) and a new `audit-<timestamp>.json` on every run. The audit record is never
overwritten. It stores the tool version, options, SHA-256 of every input file, the rules file
and the holiday calendar, the hashes of the three report files, and the results, so a run can
be reproduced and checked. Every finding carries its rule id, observed value, threshold,
pass/fail/warning, and the rule citation. See the [rule-to-check map](docs/rule-to-check-map.md).

## Releases

CI compiles **six** targets on every push (darwin, linux and windows, each for amd64 and arm64), but
the release **packages four**: darwin-arm64, darwin-amd64, linux-amd64 and windows-amd64. Build
linux-arm64 or windows-arm64 yourself with the command above.

Pushing a tag such as `v1.0.0` runs `.github/workflows/release.yml`: it vets and tests, builds static
binaries for those four targets, smoke-tests the Linux one
(version, exit codes 0/1/2, disclaimer), and attaches the archives plus `SHA256SUMS.txt` to a GitHub
release (tags with a suffix, like `v1.0.0-rc.1`, are marked pre-release). Each archive also holds
`rules/rules.yaml`, `config/holidays.yaml` and the docs; run the binary from the unpacked folder so
the default paths resolve. Verify a download with `sha256sum -c SHA256SUMS.txt`.

## Input files

| File | Columns (`*` required) |
|---|---|
| reserves | `holding_id`, `asset_type*`, `amount*` (> 0), `counterparty*`, `maturity_date`, `is_immediately_available*`, `concentration_exemption` (`us_government`, `custodian`, `centrally_cleared_repo`), `is_repo_liability` |
| supply | `as_of*` (RFC 3339 with offset), `circulating_supply*` (> 0) |
| redemptions | `request_ts*`, `onboarded_ts`, `transfer_initiated_ts`, `credited_ts`, `amount*`, `status*` (`pending`, `completed`, `rejected`, `cancelled`) |

Timestamps must carry a UTC offset.

### Duplicates and `holding_id`

Two real positions (say, two $50M deposits at one bank) can match in every field, so identical
content alone is not proof of a repeated line. Reserve rows are therefore identified like this:

- **With a `holding_id`** (for example `HLD-00123`): the id is part of the row's identity.
  Identical content with different ids is two positions and both count. The same id and same
  content again is a repeated line: it is dropped, counted once and reported (loading stays
  idempotent). The same id with *different* content is an invalid row.
- **Without a `holding_id`**: identical lines are ambiguous. They are **kept** as separate
  positions, never silently dropped, and the run reports a `DUPLICATE_ROW_WARNING` finding
  (in `report.md`, `findings.csv`, `report.json`) with the line numbers and the amount involved.
  This is a WARNING (exit 0). If the repeats are accidental, total reserves are overstated by that
  amount, so give every position a unique `holding_id`. `--allow-duplicate-rows` acknowledges the
  rows as distinct positions: they are still kept and listed, but no warning is raised.

Ids are compared exactly after trimming (case-sensitive). Supply and redemption rows have no ids:
an exact repeat is dropped and reported.

## Principles

- No regulatory threshold is hardcoded. Each rule has a citation and a `verified` flag;
  `verified: false` means placeholder.
- Money and ratios use exact decimals (`shopspring/decimal`), never `float64`. Rule numbers are
  read from their YAML text. Pass/fail is decided by exact comparison, not a rounded ratio.
- Business days follow the Federal holiday schedule in `config/holidays.yaml` (a reviewable data
  file). Pointing `holiday_calendar` at a Fedwire-style file switches schedules. Dates outside the
  file's declared coverage are an error.

## Known limitations

- Three points rest on the final rule preamble or owner decisions, not supplied codified text: the redemption clock start (12 CFR 15.12(a)(1)), the business-day definition (15.2) and the day-counting convention.
- Concentration exemptions (`concentration_exemption`) and `is_repo_liability` are taken as supplied; the tool cannot verify the custodian conditions of 15.11(d).
- Reserve composition (15.11(b)) tests asset type and remaining maturity only; collateral, overcollateralization, counterparty creditworthiness, fund holdings, OCC approvals and deposit-insurance limits cannot be checked. Any `asset_type` the rule does not list, including a typo, is a FAIL (exit 1) whose message lists every allowed type; add a legitimate name as an alias in `rules/rules.yaml`.
- The 24-hour redemption-surge window is applied as a rolling window; the rule text does not say rolling or calendar day.
- `is_immediately_available` is trusted as supplied.
- Identical reserve rows without a `holding_id` are kept (and warned about), so accidental repeats
  can overstate reserves until a `holding_id` is supplied; that is the cautious trade-off against
  merging real positions and understating reserves.
- Holiday calendar covers 2026–2027 and is unverified.
- Operational capability, the monthly composition report and certification, and the 15.11(g) notification and 15-day liquidation duties are tracked but not checked (the last needs daily history).
- Out of scope: AML/sanctions checks, a UI, blockchain RPC calls (supply comes from a file).

## Layout

| Path | Purpose |
|---|---|
| `cmd/reservelint` | entry point |
| `internal/model` | validated rows and deterministic row keys |
| `internal/rules`, `internal/config` | rule schema; file loading and SHA-256 hashing |
| `internal/ingest` | CSV/JSON loading with dedupe and row errors |
| `internal/engine` | coverage, liquidity, concentration and redemption-timing checks |
| `internal/calendar`, `config/holidays.yaml` | business-day math in America/New_York |
| `internal/runner` | one check run, from files to a finalized report |
| `internal/report` | Markdown, CSV, JSON and audit-log writers |
| `internal/synthetic` | seeded sample data with deliberate violations |
| `internal/cli` | cobra commands |
| `testdata/vectors` | datasets and expected results exported from the Python prototype |
| `testdata/golden` | golden report files (regenerate: `go test ./internal/runner -update`) |
| `legacy-python/` | frozen Python prototype used only to cross-check the Go code |

## Development

```bash
go vet ./... && go test ./...
go test -race ./...                 # needs cgo (a C compiler); CI runs it on Linux
go test ./internal/runner -update   # after an intended report format change; review the diff
```

CI (GitHub Actions) runs vet and tests on Linux and Windows (plus the race detector on
Linux), cross-compiles all six platform targets (including macOS), and checks that the shared test vectors still match the Python prototype.

## License

[MIT](LICENSE). The license covers the software; it does not change the disclaimer above. This
tool is a monitoring and reporting aid, not legal advice and not an official OCC form.
