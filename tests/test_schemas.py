from datetime import UTC, date, datetime, timedelta
from decimal import Decimal

import pytest
from pydantic import ValidationError

from reservewatch.schemas import RedemptionRow, ReserveRow, SupplyRow


def reserve(**overrides):
    base = {
        "asset_type": "cash_deposit",
        "amount": "100.00",
        "counterparty": "Bank A",
        "is_immediately_available": True,
    }
    return ReserveRow(**{**base, **overrides})


def redemption(**overrides):
    base = {
        "request_ts": "2027-02-01T10:00:00Z",
        "amount": "5",
        "status": "pending",
    }
    return RedemptionRow(**{**base, **overrides})


@pytest.mark.parametrize("amount", ["0", "-1", "-0.01", "NaN", "Infinity"])
def test_reserve_rejects_bad_amounts(amount):
    with pytest.raises(ValidationError):
        reserve(amount=amount)


@pytest.mark.parametrize("amount", ["0", "-5"])
def test_redemption_and_supply_reject_non_positive(amount):
    with pytest.raises(ValidationError):
        redemption(amount=amount)
    with pytest.raises(ValidationError):
        SupplyRow(as_of="2027-02-01T00:00:00Z", circulating_supply=amount)


@pytest.mark.parametrize("counterparty", ["", "   "])
def test_reserve_requires_counterparty(counterparty):
    with pytest.raises(ValidationError):
        reserve(counterparty=counterparty)


def test_reserve_missing_counterparty_field():
    with pytest.raises(ValidationError):
        ReserveRow(asset_type="x", amount="1", is_immediately_available=True)


def test_blank_csv_cells_become_none():
    assert reserve(maturity_date="").maturity_date is None
    row = redemption(onboarded_ts="", transfer_initiated_ts="  ")
    assert row.onboarded_ts is None and row.transfer_initiated_ts is None


def test_maturity_date_parses():
    assert reserve(maturity_date="2027-03-01").maturity_date == date(2027, 3, 1)


@pytest.mark.parametrize("field", ["request_ts", "onboarded_ts", "transfer_initiated_ts"])
def test_naive_timestamps_rejected(field):
    with pytest.raises(ValidationError):
        redemption(**{field: "2027-02-01T10:00:00"})


def test_naive_supply_timestamp_rejected():
    with pytest.raises(ValidationError):
        SupplyRow(as_of="2027-02-01T10:00:00", circulating_supply="1")


def test_unknown_field_and_status_rejected():
    with pytest.raises(ValidationError):
        reserve(surprise="x")
    with pytest.raises(ValidationError):
        redemption(status="done")


def test_rows_are_frozen():
    with pytest.raises(ValidationError):
        reserve().amount = Decimal(1)


def test_row_key_is_deterministic_and_hex():
    key = reserve().row_key
    assert key == reserve().row_key
    assert len(key) == 64 and int(key, 16) >= 0


def test_row_key_ignores_formatting_noise():
    assert reserve(amount="100").row_key == reserve(amount="100.000").row_key
    assert reserve(counterparty="  bank   a ").row_key == reserve().row_key


def test_row_key_differs_when_data_differs():
    assert reserve(amount="101").row_key != reserve().row_key
    assert reserve(counterparty="Bank B").row_key != reserve().row_key
    assert reserve(is_immediately_available=False).row_key != reserve().row_key


def test_row_key_same_instant_different_offsets():
    utc = redemption(request_ts="2027-02-01T15:00:00Z")
    eastern = redemption(request_ts="2027-02-01T10:00:00-05:00")
    assert utc.row_key == eastern.row_key
    assert utc.request_ts.utcoffset() == timedelta(0)
    assert eastern.request_ts.utcoffset() == timedelta(hours=-5)


def test_row_key_differs_between_distinct_instants():
    later = datetime(2027, 2, 1, 15, 0, 1, tzinfo=UTC)
    assert redemption(request_ts=later).row_key != redemption().row_key
