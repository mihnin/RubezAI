"""Детерминированная оценка риска по найденным сущностям."""

from __future__ import annotations

from dataclasses import dataclass
from enum import StrEnum

from app.domain.entities import Category, Match


class RiskLevel(StrEnum):
    """Уровень риска. Согласован с docs/contracts/sanitize.schema.json."""

    LOW = "low"
    MEDIUM = "medium"
    HIGH = "high"
    CRITICAL = "critical"


@dataclass(frozen=True, slots=True)
class Risk:
    """Агрегированная оценка риска.

    Инвариант контракта: ``classes`` = объединение категорий всех сущностей.
    """

    score: float
    level: RiskLevel
    classes: tuple[Category, ...]


# Базовый вклад категории: секрет опаснее ПДн, ПДн опаснее коммерческих данных.
_CATEGORY_BASE: dict[Category, float] = {
    Category.SECRET: 0.9,
    Category.PII: 0.55,
    Category.COMMERCIAL: 0.4,
}


def _level_for(score: float) -> RiskLevel:
    if score >= 0.85:
        return RiskLevel.CRITICAL
    if score >= 0.6:
        return RiskLevel.HIGH
    if score >= 0.3:
        return RiskLevel.MEDIUM
    return RiskLevel.LOW


def score_risk(matches: list[Match]) -> Risk:
    """Оценивает риск по сущностям: базовый вклад категории + плотность.

    Оценка детерминирована и объяснима (rules-first): берётся максимальный
    вклад присутствующих категорий плюс небольшая надбавка за число сущностей.
    """
    if not matches:
        return Risk(score=0.0, level=RiskLevel.LOW, classes=())
    classes = tuple(sorted({m.category for m in matches}))
    base = max(_CATEGORY_BASE[category] for category in classes)
    density = min(0.1, 0.02 * len(matches))
    score = round(min(1.0, base + density), 4)
    return Risk(score=score, level=_level_for(score), classes=classes)
