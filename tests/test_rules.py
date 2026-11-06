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


def test_shipped_rules_load_and_are_all_unverified():
    rules = load_rules(RULES_PATH)
    assert len(rules.rules) >= 4
    assert all(not r.verified for r in rules.rules)
    assert rules.unverified() == rules.rules


def test_shipped_rules_have_citation_placeholders():
    for rule in load_rules(RULES_PATH).rules:
        assert rule.source.startswith("UNVERIFIED")


def test_open_questions_are_null_not_guessed():
    params = load_rules(RULES_PATH).get("RW-RED-001").parameters
    assert params["clock_start"] is None and params["clock_end"] is None


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
