"""Loader and schema for ``rules/rules.yaml``.

No regulatory number appears in Python code. Checks read their parameters from a
``Rule``; every rule carries a citation and a ``verified`` flag.
"""

from __future__ import annotations

from pathlib import Path
from typing import Any

import yaml
from pydantic import BaseModel, ConfigDict, model_validator

from reservewatch.schemas import NonBlankStr


class Rule(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)

    id: NonBlankStr
    check: str | None  # name of the implementing check, or null if not implemented
    description: NonBlankStr
    parameters: dict[str, Any]
    source: NonBlankStr  # citation to the rule text
    verified: bool  # required on purpose: no default
    notes: str | None = None


class RuleSet(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)

    schema_version: int
    rules: list[Rule]

    @model_validator(mode="after")
    def _unique_ids(self) -> RuleSet:
        ids = [r.id for r in self.rules]
        dupes = sorted({i for i in ids if ids.count(i) > 1})
        if dupes:
            raise ValueError(f"duplicate rule ids: {dupes}")
        return self

    def get(self, rule_id: str) -> Rule:
        for rule in self.rules:
            if rule.id == rule_id:
                return rule
        raise KeyError(rule_id)

    def unverified(self) -> list[Rule]:
        return [r for r in self.rules if not r.verified]


def load_rules(path: Path) -> RuleSet:
    with path.open(encoding="utf-8") as fh:
        data = yaml.safe_load(fh)
    return RuleSet.model_validate(data)
