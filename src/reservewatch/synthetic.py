"""Synthetic data generator. All data is fictional; names are not real institutions.

Two scenarios are written side by side:
- ``clean``: data intended to look healthy under any plausible rule set.
- ``violations``: data with deliberate problems (shortfall, thin liquidity, one dominant
  counterparty, slow and stuck redemptions, one duplicated reserve row).

Output is fully determined by ``seed``. Thresholds are deliberately NOT referenced here:
the violations are large enough to trip any plausible threshold, not tuned to one.
"""

from __future__ import annotations

import csv
import json
import random
from datetime import UTC, datetime, timedelta
from decimal import Decimal
from enum import StrEnum
from pathlib import Path
from typing import Any

from pydantic import BaseModel

from reservewatch import DISCLAIMER, __version__
from reservewatch.schemas import RedemptionRow, RedemptionStatus, ReserveRow, SupplyRow

AS_OF = datetime(2027, 2, 1, 21, 0, tzinfo=UTC)
SUPPLY_CENTS = 100_000_000 * 100
COUNTERPARTIES = [
    "Synthetic Bank Alpha",
    "Synthetic Bank Beta",
    "Synthetic Custody Gamma",
    "Synthetic Trust Delta",
    "Synthetic Bank Epsilon",
]
N_REDEMPTIONS = 60


class Fmt(StrEnum):
    CSV = "csv"
    JSON = "json"


def _money(cents: int) -> Decimal:
    return Decimal(cents).scaleb(-2)


def _split(total: int, weights: list[float]) -> list[int]:
    parts = [int(total * w / sum(weights)) for w in weights]
    parts[-1] += total - sum(parts)
    return parts


def _build_reserves(
    rng: random.Random, total_cents: int, cp_weights: list[float], cash_fraction: float
) -> list[ReserveRow]:
    """Per counterparty: one available cash deposit and two T-bill rows (not available)."""
    rows: list[ReserveRow] = []
    for cp, cp_cents in zip(COUNTERPARTIES, _split(total_cents, cp_weights), strict=True):
        cash = int(cp_cents * cash_fraction)
        bill_a = int((cp_cents - cash) * rng.uniform(0.3, 0.7))
        bill_b = cp_cents - cash - bill_a
        rows.append(
            ReserveRow(
                asset_type="cash_deposit",
                amount=_money(cash),
                counterparty=cp,
                is_immediately_available=True,
            )
        )
        for cents in (bill_a, bill_b):
            maturity = AS_OF.date() + timedelta(days=rng.randint(7, 90))
            rows.append(
                ReserveRow(
                    asset_type="us_treasury_bill",
                    amount=_money(cents),
                    counterparty=cp,
                    maturity_date=maturity,
                    is_immediately_available=False,
                )
            )
    return rows


def _build_redemptions(rng: random.Random, violations: bool) -> list[RedemptionRow]:
    rows: list[RedemptionRow] = []
    for i in range(N_REDEMPTIONS):
        request = AS_OF - timedelta(days=rng.uniform(0.2, 30))
        onboarded = request - timedelta(days=rng.uniform(1, 400))
        if i % 9 == 0:  # new customer, onboarded shortly after the request
            onboarded = request + timedelta(hours=rng.uniform(1, 6))
        delay = timedelta(days=rng.uniform(0.05, 1.2))
        stuck = False
        if violations and i % 7 == 3:
            delay = timedelta(days=rng.uniform(4, 9))
        if violations and i in (5, 11):  # old request, still no transfer
            request, stuck = AS_OF - timedelta(days=12), True
            onboarded = request - timedelta(days=30)
        transfer = request + delay
        pending = stuck or transfer > AS_OF
        rows.append(
            RedemptionRow(
                request_ts=request.replace(microsecond=0),
                onboarded_ts=onboarded.replace(microsecond=0),
                transfer_initiated_ts=None if pending else transfer.replace(microsecond=0),
                amount=_money(rng.randint(10_000, 50_000_000)),
                status=RedemptionStatus.PENDING if pending else RedemptionStatus.COMPLETED,
            )
        )
    return sorted(rows, key=lambda r: r.request_ts)


def build_scenario(name: str, seed: int) -> dict[str, list[BaseModel]]:
    """Return ``{"reserves": [...], "supply": [...], "redemptions": [...]}`` for a scenario."""
    rng = random.Random(f"{seed}:{name}")
    if name == "clean":
        weights = [rng.uniform(0.85, 1.15) for _ in COUNTERPARTIES]
        reserves = _build_reserves(rng, int(SUPPLY_CENTS * 1.02), weights, cash_fraction=0.20)
        supply_cents = SUPPLY_CENTS
    elif name == "violations":
        reserves = _build_reserves(
            rng, int(SUPPLY_CENTS * 0.95), [5.5, 1, 1, 1, 1.5], cash_fraction=0.02
        )
        reserves.append(reserves[0])  # exact duplicate of a small cash row
        supply_cents = SUPPLY_CENTS
    else:
        raise ValueError(f"unknown scenario: {name}")
    supply = [SupplyRow(as_of=AS_OF, circulating_supply=_money(supply_cents))]
    return {
        "reserves": reserves,
        "supply": supply,
        "redemptions": _build_redemptions(rng, violations=name == "violations"),
    }


def _csv_cell(value: Any) -> str:
    if value is None:
        return ""
    if isinstance(value, bool):
        return "true" if value else "false"
    return str(value)


def _write(path: Path, rows: list[BaseModel], fmt: Fmt) -> None:
    dumped = [r.model_dump(mode="json") for r in rows]
    if fmt is Fmt.JSON:
        path.write_text(json.dumps(dumped, indent=2) + "\n", encoding="utf-8")
        return
    with path.open("w", newline="", encoding="utf-8") as fh:
        writer = csv.DictWriter(
            fh, fieldnames=list(type(rows[0]).model_fields), lineterminator="\n"
        )
        writer.writeheader()
        for row in dumped:
            writer.writerow({k: _csv_cell(v) for k, v in row.items()})


MANIFEST_NOTES = {
    "clean": ["No deliberate violations."],
    "violations": [
        "Reserves are below circulating supply (coverage shortfall).",
        "Very little of the reserve pool is flagged immediately available.",
        "One counterparty holds more than half of reserves.",
        "Several redemptions took many days; two old requests have no transfer yet.",
        "One reserve row appears twice (duplicate detection).",
    ],
    "both": ["Some customers are onboarded after the request time (clock-start ambiguity)."],
}


def generate_synthetic(out_dir: Path, seed: int = 42, fmt: Fmt = Fmt.CSV) -> list[Path]:
    """Write both scenarios under ``out_dir`` and return the paths written."""
    written: list[Path] = []
    for name in ("clean", "violations"):
        scenario_dir = out_dir / name
        scenario_dir.mkdir(parents=True, exist_ok=True)
        for kind, rows in build_scenario(name, seed).items():
            path = scenario_dir / f"{kind}.{fmt.value}"
            _write(path, rows, fmt)
            written.append(path)
    manifest = out_dir / "manifest.json"
    manifest.write_text(
        json.dumps(
            {
                "synthetic": True,
                "disclaimer": DISCLAIMER,
                "tool_version": __version__,
                "seed": seed,
                "as_of": AS_OF.isoformat(),
                "deliberate_issues": MANIFEST_NOTES,
            },
            indent=2,
        )
        + "\n",
        encoding="utf-8",
    )
    written.append(manifest)
    return written
