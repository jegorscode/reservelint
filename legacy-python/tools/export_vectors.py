"""Export shared test vectors for the Go implementation.

Writes ../testdata/vectors/ (relative to the repo root): the seed-42 synthetic datasets,
the findings the Python checks produce for them, and row keys. The Go tests must reproduce
these numbers. Run from legacy-python/: ``python tools/export_vectors.py``.
"""

from __future__ import annotations

import json
import shutil
from decimal import Decimal
from pathlib import Path

from reservewatch.checks import run_reserve_checks
from reservewatch.ingest import latest_supply, load_redemptions, load_reserves, load_supply
from reservewatch.rules import load_rules
from reservewatch.schemas import ReserveRow
from reservewatch.synthetic import generate_synthetic

HERE = Path(__file__).resolve().parents[1]
OUT = HERE.parent / "testdata" / "vectors"
SEED = 42
TEN_DP = Decimal("1e-10")

# Raw reserve rows (as CSV strings) chosen to stress the key normalisation.
EDGE_CASES = [
    {"asset_type": "cash", "amount": "100.00", "counterparty": "Bank A", "maturity_date": "",
     "is_immediately_available": "true"},
    {"asset_type": "Cash ", "amount": "100", "counterparty": "  bank   a", "maturity_date": "",
     "is_immediately_available": "TRUE"},
    {"asset_type": "t-bill", "amount": "0.5", "counterparty": 'Zürich "Ä" Bank\\1',
     "maturity_date": "2027-03-01", "is_immediately_available": "0"},
    {"asset_type": "t-bill", "amount": "1234567.8900", "counterparty": "Bank\tB",
     "maturity_date": "", "is_immediately_available": "no"},
    {"asset_type": "x", "amount": "1e3", "counterparty": "😀 Bank", "maturity_date": "",
     "is_immediately_available": "yes"},
]


def main() -> None:
    if OUT.exists():
        shutil.rmtree(OUT)
    generate_synthetic(OUT, seed=SEED)
    (OUT / "manifest.json").unlink()  # holds a tool version; not part of the contract
    rules = load_rules(HERE / "rules" / "rules.yaml")
    expected: dict[str, object] = {"seed": SEED, "scenarios": {}}
    for scenario in ("clean", "violations"):
        base = OUT / scenario
        reserves = load_reserves(base / "reserves.csv")
        supply = load_supply(base / "supply.csv")
        redemptions = load_redemptions(base / "redemptions.csv")
        findings = run_reserve_checks(rules, reserves.rows, latest_supply(supply.rows))
        expected["scenarios"][scenario] = {  # type: ignore[index]
            "reserve_rows_read": reserves.rows_read,
            "reserve_duplicates": reserves.duplicate_count,
            "reserve_keys": [r.row_key for r in reserves.rows],
            "supply_keys": [r.row_key for r in supply.rows],
            "redemption_keys": [r.row_key for r in redemptions.rows],
            "findings": [
                {
                    "rule_id": f.rule_id,
                    "check": f.check,
                    "passed": f.passed,
                    "comparator": f.comparator,
                    "observed_10dp": str(f.observed.quantize(TEN_DP)),
                    "threshold": str(f.threshold),
                    "total_reserves": f.details["total_reserves"],
                }
                for f in findings
            ],
        }
    expected["reserve_key_edge_cases"] = [
        {"input": raw, "key": ReserveRow.model_validate(raw).row_key} for raw in EDGE_CASES
    ]
    (OUT / "expected.json").write_text(
        json.dumps(expected, indent=2, ensure_ascii=True) + "\n", encoding="utf-8"
    )
    print(f"wrote {OUT}")


if __name__ == "__main__":
    main()
