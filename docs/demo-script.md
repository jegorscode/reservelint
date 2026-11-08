# Five-minute demo

> Monitoring and reporting aid. Not legal advice. Not an official OCC form. All data below is
> synthetic.

A walkthrough you can run yourself or screen-share. It shows one healthy dataset, one with
deliberate problems, and the files each run leaves behind.

## Setup (once)

Download a release archive and unpack it, or build from source (`go build -o reservelint ./cmd/reservelint`).
Run everything from the unpacked or repo folder so the default `rules/` and `config/` paths resolve.

```bash
reservelint generate-synthetic --out demo/data
```

This writes `clean/` and `violations/` folders, each with `reserves.csv`, `supply.csv` and
`redemptions.csv`, plus a `manifest.json` that lists the planted problems.

## 1. A healthy run (about 1 minute)

```bash
reservelint check --reserves demo/data/clean/reserves.csv --supply demo/data/clean/supply.csv \
  --redemptions demo/data/clean/redemptions.csv --out demo/report-clean
```

Expected first line: `PASS: 0 fail, 0 warning, 6 pass`. Exit code 0
(`echo $?` in bash, `$LASTEXITCODE` in PowerShell).

Open `demo/report-clean/report.md` and point at:
- the findings table: rule id, observed value, requirement, and whether the rule is verified;
- the absence of a **Caveats** section on a clean run: when a rule or the holiday calendar is
  unverified, or a run is partial, the report says so there;
- the disclaimer at the top and bottom.

## 2. A run with problems (about 2 minutes)

```bash
reservelint check --reserves demo/data/violations/reserves.csv --supply demo/data/violations/supply.csv \
  --redemptions demo/data/violations/redemptions.csv --out demo/report-bad
```

Expected first line: `FAIL: 4 fail, 0 warning, 2 pass`. Exit code **1**, so a pipeline can gate on it.

In `report.md` show:
- coverage about 95% against a 100% requirement, with the shortfall in dollars;
- liquidity about 2% against 5%, and one counterparty at about 55% against the 40% limit;
- redemption timing: a table of the late requests with their start, end, business days used and due date;
- every finding quotes its regulatory citation and says whether that rule is verified.

## 3. What each run leaves behind (about 1 minute)

```
demo/report-bad/
  report.md      for people
  findings.csv   open in a spreadsheet
  report.json    for programs
  audit-<timestamp>.json
```

Open the audit file: it records the tool version, the options, the SHA-256 of every input file, the
rules file and the holiday calendar, and the hashes of the report files. It is a new file every run
and is never overwritten, so a result can be reproduced and checked later.

## 4. Exit codes and bad data (about 1 minute)

| Exit code | Meaning |
|---|---|
| 0 | PASS or WARNING |
| 1 | at least one FAIL from a verified rule |
| 2 | bad input: missing file, unreadable rows, unknown settings |

Edit a copy of `reserves.csv` so one amount is `-5` and run `check` again: it stops with exit 2 and
names the row. `--allow-invalid-rows` continues but marks the report PARTIAL.

## What to say about its limits

- It checks the data you give it against rules in `rules/rules.yaml`. It does not know whether the
  data is true, and it does not decide legal compliance.
- Some points rest on the final rule's preamble or on owner decisions; the rule notes say which.
- It checks one snapshot, not history, and does not produce the monthly composition report.
- It has not been reviewed by a lawyer or regulator. That is what the feedback is for.
