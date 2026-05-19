"""Доменные модели результата обезличивания."""

from __future__ import annotations

from dataclasses import dataclass, field

from app.domain.entities import Category, EntityType
from app.domain.risk import Risk


@dataclass(frozen=True, slots=True)
class PublicEntity:
    """Сущность для выдачи наружу — без raw-значения (THREAT_MODEL, T2)."""

    type: EntityType
    category: Category
    start: int
    end: int
    pseudonym: str
    raw_hash: str
    detector: str
    confidence: float


@dataclass(frozen=True, slots=True)
class PseudonymMapping:
    """Связь псевдонима с raw-значением. raw хранится только зашифрованным."""

    pseudonym: str
    entity_type: EntityType
    raw_hash: str
    raw_value_encrypted: bytes = field(repr=False)


@dataclass(frozen=True, slots=True)
class SanitizationResult:
    """Результат обезличивания текста."""

    sanitized_text: str
    entities: tuple[PublicEntity, ...]
    risk: Risk
    mappings: tuple[PseudonymMapping, ...] = field(repr=False)
