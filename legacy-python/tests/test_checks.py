from datetime import UTC, datetime
from decimal import Decimal
from pathlib import Path

import pytest

from reservewatch.checks import (
    RuleConfigError,
    check_counterparty_concentration,
    check_coverage,
    check_liquidity_floor,
    run_reserve_checks,
)
from reservewatch.ingest import latest_supply, load_reserves, load_supply
from reservewatch.rules import Rule, load_rules
from reservewatch.schemas import ReserveRow, SupplyRow
from reservewatch.synthetic import generate_synthetic

RULES_PATH = Path(__file__).resolve().parents[1] / "rules" / "rules.yaml"


def rule(check, verified=True, **params):
    return Rule(
        id="T-1",
        check=check,
        description="test",
        parameters=params,
        source="test source",
        verified=verified,
    )


def res(amount, cp="A", available=False):
    return ReserveRow(
        asset_type="x", amount=amount, counterparty=cp, is_immediately_available=available
    )


def supply(amount):
    return SupplyRow(as_of=datetime(2027, 2, 1, tzinfo=UTC), circulating_supply=amount)


LIQ = rule("liquidity_floor", min_immediately_available_share=0.05)
CON = rule("counterparty_concentration", max_counterparty_share=0.40)
COV = rule("coverage", min_coverage_ratio=1.0)

# --- coverage -------------------------------------------------------------------------


def test_coverage_exactly_at_limit_passes():
    f = check_coverage(COV, [res(100)], supply(100))
    assert f.passed and f.observed == Decimal(1) and f.details["shortfall"] == "0"


def test_coverage_one_cent_short_fails():
    f = check_coverage(COV, [res("99.99")], supply("100.00"))
    assert not f.passed and f.details["shortfall"] == "0.01"


def test_coverage_surplus_passes_with_no_shortfall():
    f = check_coverage(COV, [res(150)], supply(100))
    assert f.passed and f.details["shortfall"] == "0"


def test_coverage_sums_rows():
    assert check_coverage(COV, [res(60), res(40, "B")], supply(100)).passed


# --- liquidity ------------------------------------------------------------------------


def test_liquidity_exactly_at_limit_passes():
    f = check_liquidity_floor(LIQ, [res(5, available=True), res(95, "B")])
    assert f.passed and f.observed == Decimal("0.05")


def test_liquidity_just_below_limit_fails():
    f = check_liquidity_floor(LIQ, [res("4.99", available=True), res("95.01", "B")])
    assert not f.passed


def test_liquidity_none_available_fails_with_zero():
    f = check_liquidity_floor(LIQ, [res(10)])
    assert not f.passed and f.observed == 0


# --- concentration --------------------------------------------------------------------


def test_concentration_exactly_at_limit_passes():
    f = check_counterparty_concentration(CON, [res(40, "A"), res(30, "B"), res(30, "C")])
    assert f.passed and f.observed == Decimal("0.4")


def test_concentration_just_over_limit_fails():
    f = check_counterparty_concentration(CON, [res("40.01", "A"), res("29.99", "B"), res(30, "C")])
    assert not f.passed and f.details["top_counterparty"] == "A"


def test_concentration_merges_name_variants_and_rows():
    loose = rule("counterparty_concentration", max_counterparty_share=0.60)
    reserves = [res(25, "Bank A"), res(25, "  bank   a "), res(50, "Bank B")]
    f = check_counterparty_concentration(loose, reserves)
    assert f.observed == Decimal("0.5") and f.details["shares"]["Bank A"] == "0.5"


def test_single_counterparty_is_100_percent():
    f = check_counterparty_concentration(CON, [res(10)])
    assert not f.passed and f.observed == 1


# --- plumbing -------------------------------------------------------------------------


def test_missing_or_null_parameter_raises():
    with pytest.raises(RuleConfigError):
        check_coverage(rule("coverage"), [res(1)], supply(1))
    with pytest.raises(RuleConfigError):
        check_coverage(rule("coverage", min_coverage_ratio=None), [res(1)], supply(1))


def test_no_reserves_raises():
    with pytest.raises(ValueError):
        check_liquidity_floor(LIQ, [])


def test_finding_carries_rule_metadata():
    f = check_coverage(rule("coverage", verified=False, min_coverage_ratio=1), [res(1)], supply(1))
    assert f.rule_id == "T-1" and f.citation == "test source" and f.rule_verified is False
    assert f.comparator == ">=" and f.status == "pass"
    assert f.model_dump(mode="json")["observed"] == "1"


def test_float_threshold_has_no_binary_error():
    # 0.1 as a binary float is not exactly 0.1; 10/100 must still count as at the limit.
    tenth = rule("liquidity_floor", min_immediately_available_share=0.1)
    f = check_liquidity_floor(tenth, [res(10, available=True), res(90, "B")])
    assert f.passed and f.threshold == Decimal("0.1")


# --- end to end on synthetic data -----------------------------------------------------


def run_scenario(tmp_path, scenario):
    generate_synthetic(tmp_path, seed=42)
    reserves = load_reserves(tmp_path / scenario / "reserves.csv")
    sup = load_supply(tmp_path / scenario / "supply.csv")
    findings = run_reserve_checks(load_rules(RULES_PATH), reserves.rows, latest_supply(sup.rows))
    return reserves, {f.check: f for f in findings}


def test_clean_scenario_passes_all(tmp_path):
    _, findings = run_scenario(tmp_path, "clean")
    assert set(findings) == {"coverage", "liquidity_floor", "counterparty_concentration"}
    assert all(f.passed for f in findings.values())


def test_violations_scenario_fails_all_and_dedupes(tmp_path):
    reserves, findings = run_scenario(tmp_path, "violations")
    assert reserves.duplicate_count == 1
    assert not any(f.passed for f in findings.values())
    expected_total = sum(r.amount for r in reserves.rows)
    assert findings["coverage"].details["total_reserves"] == str(expected_total)


def test_unverified_rule_flagged_in_findings(tmp_path):
    _, findings = run_scenario(tmp_path, "clean")
    assert findings["coverage"].rule_verified is False
    assert findings["liquidity_floor"].rule_verified is True
