"""Unit-тесты детерминированной оценки риска."""

from __future__ import annotations

import pytest

from app.detectors.registry import scan
from app.domain.entities import Category
from app.domain.risk import RiskLevel, _level_for, score_risk


def test_empty_matches_is_low_risk() -> None:
    risk = score_risk(scan("просто текст без чувствительных данных"))
    assert risk.level is RiskLevel.LOW
    assert risk.score == 0.0
    assert risk.classes == ()


def test_secret_is_critical() -> None:
    risk = score_risk(scan("ключ AKIAIOSFODNN7EXAMPLE в репозитории"))
    assert risk.level is RiskLevel.CRITICAL
    assert Category.SECRET in risk.classes


def test_pii_is_at_least_medium() -> None:
    risk = score_risk(scan("контакт ivan@example.ru"))
    assert risk.level in (RiskLevel.MEDIUM, RiskLevel.HIGH)
    assert Category.PII in risk.classes


def test_classes_cover_union_of_categories() -> None:
    # инвариант контракта sanitize.schema.json: classes ⊇ объединение categories
    matches = scan("ivan@example.ru, ключ AKIAIOSFODNN7EXAMPLE, сумма 100 000 руб.")
    risk = score_risk(matches)
    assert set(risk.classes) == {m.category for m in matches}


def test_score_within_unit_range() -> None:
    risk = score_risk(scan("ivan@example.ru AKIAIOSFODNN7EXAMPLE бюджет 5 млн рублей"))
    assert 0.0 <= risk.score <= 1.0


def test_more_entities_raise_score() -> None:
    one = score_risk(scan("ivan@example.ru"))
    many = score_risk(scan("ivan@example.ru, petr@example.ru, anna@example.ru"))
    assert many.score >= one.score


@pytest.mark.parametrize(
    ("score", "expected"),
    [
        (0.0, RiskLevel.LOW),
        (0.29, RiskLevel.LOW),
        (0.30, RiskLevel.MEDIUM),
        (0.59, RiskLevel.MEDIUM),
        (0.60, RiskLevel.HIGH),
        (0.84, RiskLevel.HIGH),
        (0.85, RiskLevel.CRITICAL),
        (1.0, RiskLevel.CRITICAL),
    ],
)
def test_level_thresholds(score: float, expected: RiskLevel) -> None:
    assert _level_for(score) is expected


def test_pii_with_density_reaches_high() -> None:
    text = ", ".join(f"user{i}@example.ru" for i in range(6))
    assert score_risk(scan(text)).level is RiskLevel.HIGH


def test_commercial_only_never_reaches_high() -> None:
    text = ", ".join(["сумма 100 000 руб."] * 8)
    risk = score_risk(scan(text))
    assert risk.level is RiskLevel.MEDIUM
    assert risk.score <= 0.5


def test_empty_and_whitespace_text_is_low() -> None:
    for text in ("", "   \n\t  "):
        risk = score_risk(scan(text))
        assert risk.level is RiskLevel.LOW
        assert risk.score == 0.0
        assert risk.classes == ()
