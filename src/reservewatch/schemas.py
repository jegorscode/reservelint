"""Pydantic v2 schemas for reservewatch inputs.

Design notes
- Rows are frozen and reject unknown fields, so a typo in a column name fails loudly.
- All timestamps must be timezone-aware (``AwareDatetime``); naive values are rejected.
- Amounts are ``Decimal`` and strictly positive: zero and negative amounts are invalid.
- Every row has a deterministic ``row_key`` (SHA-256 over a normalised representation).
  Ingestion uses it to detect duplicates, which makes loading idempotent.
"""

from __future__ import annotations

import hashlib
import json
from datetime import UTC, date
from decimal import Decimal
from enum import StrEnum
from typing import Annotated, Any

from pydantic import (
    AwareDatetime,
    BaseModel,
    ConfigDict,
    Field,
    StringConstraints,
    field_validator,
)

NonBlankStr = Annotated[str, StringConstraints(strip_whitespace=True, min_length=1)]
PositiveAmount = Annotated[Decimal, Field(gt=0, allow_inf_nan=False)]


def _blank_to_none(value: Any) -> Any:
    """CSV exports blank cells as ''; treat them as missing."""
    if isinstance(value, str) and not value.strip():
        return None
    return value


def _norm_text(value: str) -> str:
    return " ".join(value.split()).casefold()


def _norm_amount(value: Decimal) -> str:
    return format(value.normalize(), "f")


def _norm_ts(value: AwareDatetime | None) -> str | None:
    return None if value is None else value.astimezone(UTC).isoformat()


class _Row(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)

    def key_fields(self) -> dict[str, Any]:
        """Normalised field values that define row identity."""
        raise NotImplementedError

    @property
    def row_key(self) -> str:
        payload = json.dumps(self.key_fields(), sort_keys=True, separators=(",", ":"))
        return hashlib.sha256(payload.encode("utf-8")).hexdigest()


class ReserveRow(_Row):
    asset_type: NonBlankStr
    amount: PositiveAmount
    counterparty: NonBlankStr
    maturity_date: date | None = None
    is_immediately_available: bool

    _blank_maturity = field_validator("maturity_date", mode="before")(_blank_to_none)

    def key_fields(self) -> dict[str, Any]:
        return {
            "asset_type": _norm_text(self.asset_type),
            "amount": _norm_amount(self.amount),
            "counterparty": _norm_text(self.counterparty),
            "maturity_date": self.maturity_date.isoformat() if self.maturity_date else None,
            "is_immediately_available": self.is_immediately_available,
        }


class SupplyRow(_Row):
    as_of: AwareDatetime
    circulating_supply: PositiveAmount

    def key_fields(self) -> dict[str, Any]:
        return {
            "as_of": _norm_ts(self.as_of),
            "circulating_supply": _norm_amount(self.circulating_supply),
        }


class RedemptionStatus(StrEnum):
    PENDING = "pending"
    COMPLETED = "completed"
    REJECTED = "rejected"
    CANCELLED = "cancelled"


class RedemptionRow(_Row):
    request_ts: AwareDatetime
    onboarded_ts: AwareDatetime | None = None
    transfer_initiated_ts: AwareDatetime | None = None
    amount: PositiveAmount
    status: RedemptionStatus

    _blank_ts = field_validator("onboarded_ts", "transfer_initiated_ts", mode="before")(
        _blank_to_none
    )

    def key_fields(self) -> dict[str, Any]:
        return {
            "request_ts": _norm_ts(self.request_ts),
            "onboarded_ts": _norm_ts(self.onboarded_ts),
            "transfer_initiated_ts": _norm_ts(self.transfer_initiated_ts),
            "amount": _norm_amount(self.amount),
            "status": self.status.value,
        }
