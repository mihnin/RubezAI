"""Обратимая псевдонимизация: замена сущностей псевдонимами и обратно."""

from __future__ import annotations

import hashlib
import re

from app.domain.entities import EntityType, Match
from app.domain.sanitization import PseudonymMapping, PublicEntity
from app.masking.crypto import MappingCipher

# Префикс псевдонима по типу сущности (ФИО_001, ДОГОВОР_014, СЧЕТ_003).
_PREFIX: dict[EntityType, str] = {
    EntityType.PERSON: "ФИО",
    EntityType.PHONE: "ТЕЛЕФОН",
    EntityType.EMAIL: "EMAIL",
    EntityType.PASSPORT: "ПАСПОРТ",
    EntityType.SNILS: "СНИЛС",
    EntityType.INN: "ИНН",
    EntityType.KPP: "КПП",
    EntityType.OGRN: "ОГРН",
    EntityType.BIK: "БИК",
    EntityType.ACCOUNT: "СЧЕТ",
    EntityType.SECRET_API_KEY: "СЕКРЕТ",
    EntityType.SECRET_JWT: "СЕКРЕТ",
    EntityType.SECRET_OAUTH: "СЕКРЕТ",
    EntityType.SECRET_PASSWORD: "СЕКРЕТ",
    EntityType.SECRET_DSN: "СЕКРЕТ",
    EntityType.SECRET_CONN_STRING: "СЕКРЕТ",
    EntityType.COMMERCIAL_AMOUNT: "СУММА",
    EntityType.COMMERCIAL_CONTRACT: "ДОГОВОР",
    EntityType.COMMERCIAL_SUPPLIER: "КОНТРАГЕНТ",
    EntityType.COMMERCIAL_TERMS: "УСЛОВИЕ",
}


def _hash(value: str) -> str:
    return hashlib.sha256(value.encode("utf-8")).hexdigest()


def pseudonymize(
    text: str, matches: list[Match], cipher: MappingCipher
) -> tuple[str, list[PublicEntity], list[PseudonymMapping]]:
    """Заменяет сущности в тексте псевдонимами.

    Одинаковое значение одного типа получает один псевдоним. Возвращает
    обезличенный текст, публичные сущности (без raw) и mapping'и
    (raw-значение зашифровано). Спаны matches не должны пересекаться.
    """
    ordered = sorted(matches, key=lambda m: (m.start, m.end))
    counters: dict[str, int] = {}
    assigned: dict[tuple[EntityType, str], str] = {}
    parts: list[str] = []
    entities: list[PublicEntity] = []
    mappings: list[PseudonymMapping] = []
    cursor = 0

    for match in ordered:
        key = (match.type, match.value)
        pseudonym = assigned.get(key)
        if pseudonym is None:
            prefix = _PREFIX[match.type]
            counters[prefix] = counters.get(prefix, 0) + 1
            pseudonym = f"{prefix}_{counters[prefix]:03d}"
            assigned[key] = pseudonym
            mappings.append(
                PseudonymMapping(
                    pseudonym=pseudonym,
                    entity_type=match.type,
                    raw_hash=_hash(match.value),
                    raw_value_encrypted=cipher.encrypt(match.value),
                )
            )
        parts.append(text[cursor : match.start])
        parts.append(pseudonym)
        cursor = match.end
        entities.append(
            PublicEntity(
                type=match.type,
                category=match.category,
                start=match.start,
                end=match.end,
                pseudonym=pseudonym,
                raw_hash=_hash(match.value),
                detector=match.detector,
                confidence=match.confidence,
            )
        )
    parts.append(text[cursor:])
    return "".join(parts), entities, mappings


def restore(
    text: str, mappings: list[PseudonymMapping], cipher: MappingCipher
) -> str:
    """Обратная подстановка: заменяет псевдонимы исходными значениями.

    Замена выполняется за один проход (re.sub): вставленное raw-значение не
    подвергается повторной подстановке, даже если текстуально совпадает с
    другим псевдонимом. Альтернативы сортируются по длине убыв. — длинный
    псевдоним матчится раньше короткого с тем же префиксом.
    """
    if not mappings:
        return text
    raw_by_pseudonym = {
        mapping.pseudonym: cipher.decrypt(mapping.raw_value_encrypted)
        for mapping in mappings
    }
    pattern = "|".join(
        re.escape(pseudonym)
        for pseudonym in sorted(raw_by_pseudonym, key=len, reverse=True)
    )
    return re.sub(pattern, lambda match: raw_by_pseudonym[match.group(0)], text)
