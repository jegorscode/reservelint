import json

import pytest

from reservewatch.ingest import (
    IngestError,
    latest_supply,
    load_redemptions,
    load_reserves,
    load_supply,
)

HEADER = "asset_type,amount,counterparty,maturity_date,is_immediately_available\n"
ROW_A = "cash_deposit,100.00,Bank A,,true\n"
ROW_B = "us_treasury_bill,50,Bank B,2027-03-01,false\n"


def write(tmp_path, text, name="reserves.csv"):
    path = tmp_path / name
    path.write_text(text, encoding="utf-8")
    return path


def test_loads_valid_csv(tmp_path):
    result = load_reserves(write(tmp_path, HEADER + ROW_A + ROW_B))
    assert [r.counterparty for r in result.rows] == ["Bank A", "Bank B"]
    assert result.errors == [] and result.duplicates == []
    assert result.rows_read == 2


def test_loads_valid_json(tmp_path):
    data = [{"asset_type": "x", "amount": "5", "counterparty": "A", "is_immediately_available": 1}]
    result = load_reserves(write(tmp_path, json.dumps(data), "r.json"))
    assert len(result.rows) == 1


def test_duplicates_not_double_counted_and_reported(tmp_path):
    result = load_reserves(write(tmp_path, HEADER + ROW_A + ROW_B + ROW_A + ROW_A))
    assert len(result.rows) == 2
    assert result.duplicate_count == 2
    assert result.duplicates[0].first_position == 2
    assert result.duplicates[0].repeat_positions == (4, 5)


def test_ingestion_is_idempotent_on_repeated_content(tmp_path):
    once = load_reserves(write(tmp_path, HEADER + ROW_A + ROW_B, "a.csv"))
    twice = load_reserves(write(tmp_path, HEADER + ROW_A + ROW_B + ROW_A + ROW_B, "b.csv"))
    assert [r.row_key for r in once.rows] == [r.row_key for r in twice.rows]
    assert twice.duplicate_count == 2


def test_duplicate_detection_ignores_formatting(tmp_path):
    result = load_reserves(write(tmp_path, HEADER + ROW_A + "cash_deposit,100,  bank a ,,TRUE\n"))
    assert len(result.rows) == 1 and result.duplicate_count == 1


@pytest.mark.parametrize(
    "bad",
    [
        "cash,0,Bank A,,true\n",
        "cash,-5,Bank A,,true\n",
        "cash,abc,Bank A,,true\n",
        "cash,5,,,true\n",
        "cash,5,   ,,true\n",
        "cash,5,Bank A,not-a-date,true\n",
        "cash,5,Bank A,,maybe\n",
    ],
)
def test_invalid_rows_reported_with_line_numbers(tmp_path, bad):
    result = load_reserves(write(tmp_path, HEADER + ROW_A + bad))
    assert len(result.rows) == 1
    assert [e.position for e in result.errors] == [3]
    with pytest.raises(IngestError):
        result.raise_on_errors()


def test_row_with_too_many_cells(tmp_path):
    result = load_reserves(write(tmp_path, HEADER + "cash,5,Bank A,,true,extra\n"))
    assert len(result.errors) == 1 and "more cells" in result.errors[0].message


def test_missing_cell_is_error(tmp_path):
    result = load_reserves(write(tmp_path, HEADER + "cash,5\n"))
    assert len(result.errors) == 1


@pytest.mark.parametrize("text", ["", HEADER, "\n"])
def test_empty_csv_files_rejected(tmp_path, text):
    with pytest.raises(IngestError):
        load_reserves(write(tmp_path, text))


def test_empty_json_rejected(tmp_path):
    with pytest.raises(IngestError):
        load_reserves(write(tmp_path, "[]", "r.json"))


def test_missing_column_rejected(tmp_path):
    with pytest.raises(IngestError, match="counterparty"):
        load_reserves(write(tmp_path, "asset_type,amount,is_immediately_available\nx,1,true\n"))


def test_bad_json_and_wrong_shape(tmp_path):
    with pytest.raises(IngestError):
        load_reserves(write(tmp_path, "{oops", "r.json"))
    with pytest.raises(IngestError):
        load_reserves(write(tmp_path, '{"a": 1}', "r.json"))


def test_unsupported_and_missing_file(tmp_path):
    with pytest.raises(IngestError):
        load_reserves(write(tmp_path, "x", "r.txt"))
    with pytest.raises(IngestError):
        load_reserves(tmp_path / "nope.csv")


def test_supply_and_redemption_loaders(tmp_path):
    supply = load_supply(write(tmp_path, "as_of,circulating_supply\n2027-02-01T00:00:00Z,10\n"))
    assert supply.rows[0].circulating_supply == 10
    red = load_redemptions(
        write(
            tmp_path,
            "request_ts,onboarded_ts,transfer_initiated_ts,amount,status\n"
            "2027-02-01T10:00:00-05:00,,,5,pending\n"
            "2027-02-01T10:00:00,,,5,pending\n",
            "red.csv",
        )
    )
    assert len(red.rows) == 1
    assert len(red.errors) == 1  # naive timestamp rejected


def test_latest_supply(tmp_path):
    result = load_supply(
        write(
            tmp_path,
            "as_of,circulating_supply\n2027-02-01T00:00:00Z,10\n2027-02-03T00:00:00Z,12\n",
        )
    )
    assert latest_supply(result.rows).circulating_supply == 12
    with pytest.raises(ValueError):
        latest_supply([])
