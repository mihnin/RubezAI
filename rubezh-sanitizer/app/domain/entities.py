"""Доменные модели обнаруженных сущностей."""

from __future__ import annotations

from dataclasses import dataclass, field
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
    BANK_CARD = "BANK_CARD"
    KPP = "KPP"
    OGRN = "OGRN"
    BIK = "BIK"
    ACCOUNT = "ACCOUNT"
    SECRET_API_KEY = "SECRET_API_KEY"
    SECRET_JWT = "SECRET_JWT"
    SECRET_OAUTH = "SECRET_OAUTH"
    SECRET_PASSWORD = "SECRET_PASSWORD"
    SECRET_DSN = "SECRET_DSN"
    SECRET_CONN_STRING = "SECRET_CONN_STRING"
    COMMERCIAL_AMOUNT = "COMMERCIAL_AMOUNT"
    COMMERCIAL_CONTRACT = "COMMERCIAL_CONTRACT"
    COMMERCIAL_SUPPLIER = "COMMERCIAL_SUPPLIER"
    COMMERCIAL_TERMS = "COMMERCIAL_TERMS"


@dataclass(frozen=True, slots=True)
class Match:
    """Найденная сущность.

    Поле ``value`` содержит raw-значение и существует только в памяти процесса —
    наружу (в API и логи) оно не отдаётся (см. docs/THREAT_MODEL.md, T2).
    Из ``repr`` оно исключено (``field(repr=False)``) — логировать ``Match``
    безопасно.
    """

    type: EntityType
    category: Category
    start: int
    end: int
    value: str = field(repr=False)
    detector: str
    confidence: float
