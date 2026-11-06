# reservewatch (legacy Python prototype)

Frozen reference implementation of the first two phases, kept to cross-check the Go
rewrite (`reservelint`, repository root). Do not add features here.

- `rules/rules.yaml` is a frozen snapshot, independent of the root `rules/rules.yaml`.
- `tools/export_vectors.py` regenerates `../testdata/vectors/`, which the Go tests must match.

> Monitoring and reporting aid. Not legal advice. Not an official OCC form.
