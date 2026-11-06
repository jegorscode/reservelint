"""Reserve checks: coverage, liquidity floor, counterparty concentration.

Each check is a pure function ``(rule, data) -> Finding``. Thresholds come only from the
``Rule`` parameters. Comparisons use ``Decimal`` so a value exactly at a limit is exact:
- coverage and liquidity pass when observed >= threshold (a minimum);
- concentration passes when observed <= threshold ("no more than").

Callers must pass de-duplicated rows (see ``reservewatch.ingest``).
"""

from __future__ import annotations

from collections import defaultdict
from collections.abc import Callable, Sequence
from decimal import Decimal
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict

from reservewatch.rules import Rule, RuleSet
from reservewatch.schemas import ReserveRow, SupplyRow, norm_text


class RuleConfigError(Exception):
    """A rule is missing a parameter a check needs."""


class Finding(BaseModel):
    model_config = ConfigDict(frozen=True)

    rule_id: str
    check: str
    description: str
    observed: Decimal
    threshold: Decimal
    comparator: Literal[">=", "<="]  # the condition that must hold for a pass
    passed: bool
    citation: str
    rule_verified: bool
    details: dict[str, Any] = {}

    @property
    def status(self) -> str:
        return "pass" if self.passed else "fail"


def _param(rule: Rule, name: str) -> Decimal:
    value = rule.parameters.get(name)
    if value is None or isinstance(value, bool):
        raise RuleConfigError(f"rule {rule.id}: parameter {name!r} is missing or null")
    return Decimal(str(value))


def _total(reserves: Sequence[ReserveRow]) -> Decimal:
    if not reserves:
        raise ValueError("no reserve rows")
    return sum((r.amount for r in reserves), Decimal(0))


def _finding(
    rule: Rule,
    observed: Decimal,
    threshold: Decimal,
    comparator: Literal[">=", "<="],
    details: dict[str, Any],
) -> Finding:
    passed = observed >= threshold if comparator == ">=" else observed <= threshold
    return Finding(
        rule_id=rule.id,
        check=rule.check or "",
        description=rule.description,
        observed=observed,
        threshold=threshold,
        comparator=comparator,
        passed=passed,
        citation=rule.source,
        rule_verified=rule.verified,
        details=details,
    )


def check_coverage(rule: Rule, reserves: Sequence[ReserveRow], supply: SupplyRow) -> Finding:
    total = _total(reserves)
    ratio = total / supply.circulating_supply
    shortfall = max(supply.circulating_supply - total, Decimal(0))
    details = {
        "total_reserves": str(total),
        "circulating_supply": str(supply.circulating_supply),
        "supply_as_of": supply.as_of.isoformat(),
        "shortfall": str(shortfall),
    }
    return _finding(rule, ratio, _param(rule, "min_coverage_ratio"), ">=", details)


def check_liquidity_floor(rule: Rule, reserves: Sequence[ReserveRow]) -> Finding:
    total = _total(reserves)
    available = sum((r.amount for r in reserves if r.is_immediately_available), Decimal(0))
    details = {"total_reserves": str(total), "immediately_available": str(available)}
    threshold = _param(rule, "min_immediately_available_share")
    return _finding(rule, available / total, threshold, ">=", details)


def check_counterparty_concentration(rule: Rule, reserves: Sequence[ReserveRow]) -> Finding:
    total = _total(reserves)
    amounts: dict[str, Decimal] = defaultdict(Decimal)
    display: dict[str, str] = {}
    for r in reserves:
        key = norm_text(r.counterparty)
        display.setdefault(key, r.counterparty)
        amounts[key] += r.amount
    ranked = sorted(amounts.items(), key=lambda kv: (-kv[1], kv[0]))
    top_key, top_amount = ranked[0]
    details = {
        "total_reserves": str(total),
        "top_counterparty": display[top_key],
        "top_counterparty_amount": str(top_amount),
        "shares": {display[k]: str(v / total) for k, v in ranked},
    }
    threshold = _param(rule, "max_counterparty_share")
    return _finding(rule, top_amount / total, threshold, "<=", details)


CheckFn = Callable[..., Finding]
RESERVE_CHECKS: dict[str, CheckFn] = {
    "coverage": lambda rule, reserves, supply: check_coverage(rule, reserves, supply),
    "liquidity_floor": lambda rule, reserves, supply: check_liquidity_floor(rule, reserves),
    "counterparty_concentration": (
        lambda rule, reserves, supply: check_counterparty_concentration(rule, reserves)
    ),
}


def run_reserve_checks(
    rules: RuleSet, reserves: Sequence[ReserveRow], supply: SupplyRow
) -> list[Finding]:
    """Run every rule whose ``check`` is a known reserve check, in rules-file order."""
    return [
        RESERVE_CHECKS[rule.check](rule, reserves, supply)
        for rule in rules.rules
        if rule.check in RESERVE_CHECKS
    ]
