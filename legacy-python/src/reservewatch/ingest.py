"""Load CSV/JSON input files into validated, de-duplicated rows.

Idempotency: rows are keyed by ``row_key``. The first occurrence is kept; later copies are
reported in ``duplicates`` and never counted. Loading a file, or the same file concatenated
with itself, yields the same rows. Invalid rows are collected in ``errors`` (with their
position) instead of being silently dropped, so callers decide whether to proceed.
"""

from __future__ import annotations

import csv
import json
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Generic, TypeVar

from pydantic import ValidationError

from reservewatch.schemas import RedemptionRow, ReserveRow, SupplyRow, _Row

R = TypeVar("R", bound=_Row)


class IngestError(Exception):
    """The file as a whole cannot be used (unreadable, empty, wrong shape or columns)."""


@dataclass(frozen=True)
class RowError:
    position: int  # CSV: line number in the file; JSON: 1-based item index
    message: str


@dataclass(frozen=True)
class Duplicate:
    row_key: str
    first_position: int
    repeat_positions: tuple[int, ...]


@dataclass
class IngestResult(Generic[R]):  # noqa: UP046 (PEP 695 syntax needs Python 3.12)
    path: Path
    rows: list[R] = field(default_factory=list)
    duplicates: list[Duplicate] = field(default_factory=list)
    errors: list[RowError] = field(default_factory=list)
    rows_read: int = 0

    @property
    def duplicate_count(self) -> int:
        return sum(len(d.repeat_positions) for d in self.duplicates)

    def raise_on_errors(self) -> None:
        if self.errors:
            first = self.errors[0]
            raise IngestError(
                f"{self.path}: {len(self.errors)} invalid row(s); first at {first.position}: "
                f"{first.message}"
            )


def _read_csv(path: Path, required: set[str]) -> list[tuple[int, dict[str, Any]]]:
    with path.open(newline="", encoding="utf-8-sig") as fh:
        reader = csv.DictReader(fh)
        header = set(reader.fieldnames or [])
        missing = required - header
        if missing:
            raise IngestError(f"{path}: missing required column(s): {sorted(missing)}")
        return [(reader.line_num, row) for row in reader]


def _read_json(path: Path) -> list[tuple[int, dict[str, Any]]]:
    try:
        data = json.loads(path.read_text(encoding="utf-8-sig"))
    except json.JSONDecodeError as exc:
        raise IngestError(f"{path}: invalid JSON: {exc}") from exc
    if not isinstance(data, list):
        raise IngestError(f"{path}: JSON must be a list of objects")
    return list(enumerate(data, start=1))


def _validate(model: type[R], row: Any) -> R | str:
    if not isinstance(row, dict):
        return "row is not an object"
    if None in row:
        return "row has more cells than header columns"
    try:
        return model.model_validate(row)
    except ValidationError as exc:
        return "; ".join(f"{'.'.join(map(str, e['loc']))}: {e['msg']}" for e in exc.errors())


def _load(path: Path, model: type[R]) -> IngestResult[R]:
    path = Path(path)
    suffix = path.suffix.lower()
    if suffix not in {".csv", ".json"}:
        raise IngestError(f"{path}: unsupported file type {suffix!r} (use .csv or .json)")
    try:
        if suffix == ".csv":
            required = {n for n, f in model.model_fields.items() if f.is_required()}
            raw = _read_csv(path, required)
        else:
            raw = _read_json(path)
    except (OSError, UnicodeDecodeError) as exc:
        raise IngestError(f"{path}: cannot read file: {exc}") from exc
    if not raw:
        raise IngestError(f"{path}: file contains no data rows")

    result: IngestResult[R] = IngestResult(path=path, rows_read=len(raw))
    first_seen: dict[str, int] = {}
    repeats: dict[str, list[int]] = {}
    for position, row in raw:
        parsed = _validate(model, row)
        if isinstance(parsed, str):
            result.errors.append(RowError(position, parsed))
            continue
        key = parsed.row_key
        if key in first_seen:
            repeats.setdefault(key, []).append(position)
            continue
        first_seen[key] = position
        result.rows.append(parsed)
    result.duplicates = [Duplicate(k, first_seen[k], tuple(p)) for k, p in repeats.items()]
    return result


def load_reserves(path: Path) -> IngestResult[ReserveRow]:
    return _load(path, ReserveRow)


def load_supply(path: Path) -> IngestResult[SupplyRow]:
    return _load(path, SupplyRow)


def load_redemptions(path: Path) -> IngestResult[RedemptionRow]:
    return _load(path, RedemptionRow)


def latest_supply(rows: list[SupplyRow]) -> SupplyRow:
    """The supply snapshot with the latest ``as_of`` (a file may hold several)."""
    if not rows:
        raise ValueError("no supply rows")
    return max(rows, key=lambda r: r.as_of)
