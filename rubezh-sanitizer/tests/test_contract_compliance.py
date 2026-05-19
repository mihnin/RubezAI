"""Контрактные тесты: типы кода синхронизированы с sanitize.schema.json."""

from __future__ import annotations

import json
from pathlib import Path

from app.detectors.registry import scan
from app.domain.entities import Category, EntityType
from app.domain.risk import RiskLevel

_SCHEMA_PATH = (
    Path(__file__).parents[2] / "docs" / "contracts" / "sanitize.schema.json"
)


def _schema_defs() -> dict:
    schema = json.loads(_SCHEMA_PATH.read_text(encoding="utf-8"))
    return schema["$defs"]


def test_entity_type_enum_matches_contract() -> None:
    enum = _schema_defs()["Entity"]["properties"]["type"]["enum"]
    assert {e.value for e in EntityType} == set(enum)


def test_category_enum_matches_contract() -> None:
    enum = _schema_defs()["Entity"]["properties"]["category"]["enum"]
    assert {c.value for c in Category} == set(enum)


def test_risk_level_enum_matches_contract() -> None:
    enum = _schema_defs()["Risk"]["properties"]["level"]["enum"]
    assert {level.value for level in RiskLevel} == set(enum)


def test_detector_method_values_match_contract() -> None:
    # значения Match.detector не должны выходить за enum Entity.detector
    enum = set(_schema_defs()["Entity"]["properties"]["detector"]["enum"])
    text = "ivan@example.ru ИНН 7707083893 ключ AKIAIOSFODNN7EXAMPLE 5 млн рублей"
    used = {match.detector for match in scan(text)}
    assert used
    assert used <= enum
