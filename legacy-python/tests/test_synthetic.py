import csv
import json
from collections import defaultdict
from decimal import Decimal

import pytest
from typer.testing import CliRunner

from reservewatch.cli import app
from reservewatch.schemas import RedemptionRow, ReserveRow, SupplyRow
from reservewatch.synthetic import Fmt, build_scenario, generate_synthetic

MODELS = {"reserves": ReserveRow, "supply": SupplyRow, "redemptions": RedemptionRow}


def _read_csv(path):
    with path.open(newline="", encoding="utf-8") as fh:
        return list(csv.DictReader(fh))


def _stats(reserves):
    reserves = list({r.row_key: r for r in reserves}.values())  # dedupe like ingest will
    total = sum(r.amount for r in reserves)
    avail = sum(r.amount for r in reserves if r.is_immediately_available)
    by_cp = defaultdict(Decimal)
    for r in reserves:
        by_cp[r.counterparty] += r.amount
    return total, avail / total, max(by_cp.values()) / total


def test_same_seed_gives_identical_files(tmp_path):
    a, b = tmp_path / "a", tmp_path / "b"
    generate_synthetic(a, seed=7)
    generate_synthetic(b, seed=7)
    for pa in sorted(a.rglob("*.csv")):
        assert pa.read_bytes() == (b / pa.relative_to(a)).read_bytes()


def test_different_seed_changes_data(tmp_path):
    generate_synthetic(tmp_path / "a", seed=1)
    generate_synthetic(tmp_path / "b", seed=2)
    assert (tmp_path / "a/clean/redemptions.csv").read_bytes() != (
        tmp_path / "b/clean/redemptions.csv"
    ).read_bytes()


@pytest.mark.parametrize("fmt", list(Fmt))
def test_written_files_validate_and_round_trip(tmp_path, fmt):
    generate_synthetic(tmp_path, seed=3, fmt=fmt)
    for scenario in ("clean", "violations"):
        expected = build_scenario(scenario, 3)
        for kind, model in MODELS.items():
            path = tmp_path / scenario / f"{kind}.{fmt.value}"
            raw = _read_csv(path) if fmt is Fmt.CSV else json.loads(path.read_text("utf-8"))
            parsed = [model(**row) for row in raw]
            assert [p.row_key for p in parsed] == [e.row_key for e in expected[kind]]


def test_manifest_flags_synthetic_and_disclaimer(tmp_path):
    generate_synthetic(tmp_path)
    manifest = json.loads((tmp_path / "manifest.json").read_text("utf-8"))
    assert manifest["synthetic"] is True
    assert "Not legal advice" in manifest["disclaimer"]


@pytest.mark.parametrize("seed", range(10))
def test_clean_scenario_looks_healthy(seed):
    data = build_scenario("clean", seed)
    total, avail_share, max_share = _stats(data["reserves"])
    assert total >= data["supply"][0].circulating_supply
    assert avail_share > Decimal("0.15")
    assert max_share < Decimal("0.30")
    keys = [r.row_key for r in data["reserves"]]
    assert len(keys) == len(set(keys))


def test_violations_scenario_is_deliberately_bad():
    data = build_scenario("violations", 42)
    total, avail_share, max_share = _stats(data["reserves"])
    assert total < data["supply"][0].circulating_supply
    assert avail_share < Decimal("0.03")
    assert max_share > Decimal("0.5")
    keys = [r.row_key for r in data["reserves"]]
    assert len(keys) - len(set(keys)) == 1
    assert sum(r.transfer_initiated_ts is None for r in data["redemptions"]) >= 2


def test_all_timestamps_are_aware(tmp_path):
    for rows in (
        build_scenario("violations", 5)["redemptions"],
        build_scenario("clean", 5)["supply"],
    ):
        for row in rows:
            for value in row.model_dump().values():
                if hasattr(value, "tzinfo"):
                    assert value.tzinfo is not None


def test_unknown_scenario():
    with pytest.raises(ValueError):
        build_scenario("nope", 1)


def test_cli_generate_synthetic(tmp_path):
    result = CliRunner().invoke(app, ["generate-synthetic", "--out", str(tmp_path), "--seed", "1"])
    assert result.exit_code == 0, result.output
    assert (tmp_path / "violations" / "reserves.csv").exists()
    assert "Not legal advice" in result.output


def test_cli_check_is_a_stub():
    assert CliRunner().invoke(app, ["check"]).exit_code == 2
