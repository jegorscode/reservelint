# reservelint

> **Monitoring and reporting aid. Not legal advice. Not an official OCC form.**

An open-source, auditable CLI that checks permitted payment stablecoin issuer reserve and
redemption data against rules defined in `rules/rules.yaml` and writes a report. Written in Go:
one static binary, no runtime to install.

This tool does not determine legal compliance. A PASS means the supplied data met the
thresholds in the rules file, nothing more. Several rules are unverified placeholders and the
holiday calendar is unverified; reports say so. Synthetic data only: no real customer data
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
| `--allow-invalid-rows` | continue past invalid rows and report a PARTIAL run (debugging) |

### Exit codes

| Code | Meaning |
|---|---|
| 0 | PASS or WARNING |
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

Pushing a tag such as `v1.0.0` runs `.github/workflows/release.yml`: it vets and tests, builds static
binaries for darwin-arm64, darwin-amd64, linux-amd64 and windows-amd64, smoke-tests the Linux one
(version, exit codes 0/1/2, disclaimer), and attaches the archives plus `SHA256SUMS.txt` to a GitHub
release (tags with a suffix, like `v1.0.0-rc.1`, are marked pre-release). Each archive also holds
`rules/rules.yaml`, `config/holidays.yaml` and the docs; run the binary from the unpacked folder so
the default paths resolve. Verify a download with `sha256sum -c SHA256SUMS.txt`.

## Input files

| File | Columns (`*` required) |
|---|---|
| reserves | `asset_type*`, `amount*` (> 0), `counterparty*`, `maturity_date`, `is_immediately_available*` |
| supply | `as_of*` (RFC 3339 with offset), `circulating_supply*` (> 0) |
| redemptions | `request_ts*`, `onboarded_ts`, `transfer_initiated_ts`, `amount*`, `status*` (`pending`, `completed`, `rejected`, `cancelled`) |

Timestamps must carry a UTC offset. Identical rows (ignoring case, spacing and number
formatting) are counted once and reported as duplicates, so loading is idempotent.
Two genuinely identical reserve lines would therefore be merged.

## Principles

- No regulatory threshold is hardcoded. Each rule has a citation and a `verified` flag;
  `verified: false` means placeholder.
- Money and ratios use exact decimals (`shopspring/decimal`), never `float64`. Rule numbers are
  read from their YAML text. Pass/fail is decided by exact comparison, not a rounded ratio.
- Business days follow the Federal holiday schedule in `config/holidays.yaml` (a reviewable data
  file). Pointing `holiday_calendar` at a Fedwire-style file switches schedules. Dates outside the
  file's declared coverage are an error.

## Known limitations

- Rule text for verified rules comes from the final-rule preamble, not the codified regulation.
- Counterparty concentration ignores the rule's exceptions and can over-report.
- `is_immediately_available` is trusted as supplied.
- Holiday calendar covers 2026–2027 and is unverified.
- Stress testing and weekly/quarterly reporting rules are tracked but not checked.
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
go test ./internal/runner -update   # after an intended report format change; review the diff
```

CI (GitHub Actions) runs vet and tests on Linux, Windows and macOS, cross-builds six
platform targets, and checks that the shared test vectors still match the Python prototype.
