from pathlib import Path

import pytest
from pydantic import ValidationError

from reservewatch.rules import Rule, RuleSet, load_rules

RULES_PATH = Path(__file__).resolve().parents[1] / "rules" / "rules.yaml"


def make_rule(rule_id="R1", **overrides):
    base = {
        "id": rule_id,
        "check": None,
        "description": "d",
        "parameters": {},
        "source": "s",
        "verified": False,
    }
    return {**base, **overrides}


def test_shipped_rules_load():
    assert len(load_rules(RULES_PATH).rules) >= 4


def test_verified_flag_matches_citation():
    for rule in load_rules(RULES_PATH).rules:
        assert rule.verified != rule.source.startswith("UNVERIFIED"), rule.id


def test_only_pasted_rules_are_verified():
    rules = load_rules(RULES_PATH)
    verified = {r.id for r in rules.rules if r.verified}
    assert verified == {"RW-LIQ-001", "RW-CON-001", "RW-RED-001"}
    assert "RW-COV-001" in {r.id for r in rules.unverified()}


def test_redemption_clock_defined():
    params = load_rules(RULES_PATH).get("RW-RED-001").parameters
    assert params["clock_start"] == "max(request_ts, onboarded_ts)"
    assert params["clock_end"] == "transfer_initiated_ts"
    assert params["timezone"] == "America/New_York"


def test_duplicate_ids_rejected():
    with pytest.raises(ValidationError):
        RuleSet(schema_version=1, rules=[make_rule("A"), make_rule("A")])


def test_verified_flag_is_required():
    rule = make_rule()
    del rule["verified"]
    with pytest.raises(ValidationError):
        Rule(**rule)


def test_get_unknown_rule():
    with pytest.raises(KeyError):
        load_rules(RULES_PATH).get("nope")
