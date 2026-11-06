# reservewatch

> **Monitoring and reporting aid. Not legal advice. Not an official OCC form.**

An open-source, auditable CLI that checks stablecoin issuer reserve data against
rules defined in `rules/rules.yaml` and produces reports.

**Status: Phase 1 (scaffold).** Schemas, a rules skeleton and a synthetic data
generator exist. Checks, reports, audit log and the full CLI come in later phases.

- All regulatory thresholds live in `rules/rules.yaml`. Every rule is `verified: false`
  until a human confirms it against the primary rule text.
- Synthetic data only. No real customer data belongs in this repository.

```bash
pip install -e ".[dev]"
reservewatch generate-synthetic --out data/synthetic/
pytest
```

A full README (rule-to-check map, usage, limitations) is planned for Phase 4.
