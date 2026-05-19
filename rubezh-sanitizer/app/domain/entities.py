"""Доменные модели обнаруженных сущностей."""

from __future__ import annotations

from dataclasses import dataclass
from enum import StrEnum


class Category(StrEnum):
    """Класс чувствительных данных."""

    PII = "pii"
    SECRET = "secret"
    COMMERCIAL = "commercial"


class EntityType(StrEnum):
    """Тип обнаруженной сущности. Согласован с docs/contracts/sanitize.schema.json."""

    PERSON = "PERSON"
    PHONE = "PHONE"
    EMAIL = "EMAIL"
    PASSPORT = "PASSPORT"
    SNILS = "SNILS"
    INN = "INN"
    KPP = "KPP"
    OGRN = "OGRN"
    BIK = "BIK"
    ACCOUNT = "ACCOUNT"


@dataclass(frozen=True, slots=True)
class Match:
    """Найденная сущность.

    Поле ``value`` содержит raw-значение и существует только в памяти процесса —
    наружу (в API и логи) оно не отдаётся (см. docs/THREAT_MODEL.md, T2).
    """

    type: EntityType
    category: Category
    start: int
    end: int
    value: str
    detector: str
    confidence: float
