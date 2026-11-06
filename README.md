# reservelint

> **Monitoring and reporting aid. Not legal advice. Not an official OCC form.**

An open-source, auditable CLI that checks stablecoin issuer reserve data against rules
defined in `rules/rules.yaml` and produces reports. Written in Go: one static binary, no
runtime to install.

**Status: Go rewrite, Phase 1.** Models, the rules loader, a synthetic data generator and a
minimal CLI exist. Ingestion, checks, calendar, reports and the audit log come in later phases.
The Python prototype (ingestion and reserve checks) lives in [legacy-python/](legacy-python/)
and is used only to cross-check the Go code.

## Principles
- No regulatory threshold is hardcoded. Every rule lives in `rules/rules.yaml` with a
  citation and a `verified` flag; `verified: false` means placeholder.
- Money and ratios use exact decimals (`shopspring/decimal`), never `float64`. Rule numbers
  are parsed from their YAML text, so `0.05` never passes through a binary float.
- Synthetic data only. No real customer data belongs in this repository.

## Layout
| Path | Purpose |
|---|---|
| `cmd/reservelint` | entry point |
| `internal/model` | validated rows and deterministic row keys |
| `internal/rules`, `internal/config` | rule schema; file loading and SHA-256 hashing |
| `internal/synthetic` | seeded sample data with deliberate violations |
| `internal/cli` | cobra commands |
| `internal/ingest`, `engine`, `calendar`, `report` | planned (Phases 2 to 4) |
| `testdata/vectors` | shared vectors exported from the Python prototype |

## Try it
```bash
go test ./...
go run ./cmd/reservelint generate-synthetic --out data/synthetic
```

## Roadmap
1. Phase 1: models, rules, synthetic data, CLI skeleton (this).
2. Phase 2: ingestion with dedupe; coverage, liquidity, concentration checks.
3. Phase 3: business-day calendar (`config/holidays.yaml`, Federal schedule by default) and
   redemption timing.
4. Phase 4: Markdown/CSV/JSON reports, audit log, golden-file test, full README with the
   rule-to-check map, release builds.
