"""Regex-детекторы персональных данных (ПДн) и валидаторы контрольных сумм."""

from __future__ import annotations

from app.detectors.regex_detector import RegexDetector
from app.domain.entities import Category, EntityType

# --- валидаторы контрольных сумм ---


def _digits(value: str) -> list[int]:
    return [int(ch) for ch in value if ch.isdigit()]


def _is_degenerate(digits: list[int]) -> bool:
    """Все цифры одинаковы (0000000000 и т. п.) — заведомо невалидный реквизит."""
    return len(set(digits)) <= 1


def validate_inn(value: str) -> bool:
    """Проверка контрольной суммы ИНН (10 цифр — ЮЛ, 12 цифр — ФЛ)."""
    digits = _digits(value)
    if _is_degenerate(digits):
        return False

    def control(weights: list[int]) -> int:
        return sum(w * digits[i] for i, w in enumerate(weights)) % 11 % 10

    if len(digits) == 10:
        return control([2, 4, 10, 3, 5, 9, 4, 6, 8]) == digits[9]
    if len(digits) == 12:
        c11 = control([7, 2, 4, 10, 3, 5, 9, 4, 6, 8])
        c12 = control([3, 7, 2, 4, 10, 3, 5, 9, 4, 6, 8])
        return c11 == digits[10] and c12 == digits[11]
    return False


def validate_snils(value: str) -> bool:
    """Проверка контрольной суммы СНИЛС (11 цифр)."""
    digits = _digits(value)
    if len(digits) != 11 or _is_degenerate(digits):
        return False
    payload = sum(num * (9 - i) for i, num in enumerate(digits[:9])) % 101
    if payload == 100:
        payload = 0
    return payload == digits[9] * 10 + digits[10]


def validate_card_luhn(value: str) -> bool:
    """Проверка номера банковской карты по алгоритму Луна (13–19 цифр)."""
    digits = _digits(value)
    if not 13 <= len(digits) <= 19 or _is_degenerate(digits):
        return False
    checksum = 0
    for i, num in enumerate(reversed(digits)):
        if i % 2 == 1:
            num *= 2
            if num > 9:
                num -= 9
        checksum += num
    return checksum % 10 == 0


def validate_ogrn(value: str) -> bool:
    """Проверка контрольной суммы ОГРН (13 цифр) или ОГРНИП (15 цифр)."""
    digits = _digits(value)
    if _is_degenerate(digits):
        return False
    if len(digits) == 13:
        body = int("".join(str(d) for d in digits[:12]))
        return body % 11 % 10 == digits[12]
    if len(digits) == 15:
        body = int("".join(str(d) for d in digits[:14]))
        return body % 13 % 10 == digits[14]
    return False


# --- шаблоны ПДн ---

# ФИО: три слова с заглавной (Иванов Иван Иванович) либо «Фамилия И. О.».
# Это эвристика — точное распознавание ФИО даёт NER (итерация 4).
_PERSON_PATTERN = (
    r"[А-ЯЁ][а-яё]+\s+[А-ЯЁ][а-яё]+\s+[А-ЯЁ][а-яё]+"
    r"|[А-ЯЁ][а-яё]+\s+[А-ЯЁ]\.\s?[А-ЯЁ]\."
)


def pii_detectors() -> list[RegexDetector]:
    """Все regex-детекторы ПДн."""
    pii = Category.PII
    return [
        RegexDetector(
            name="person", entity_type=EntityType.PERSON, category=pii,
            pattern=_PERSON_PATTERN, confidence=0.6,
        ),
        RegexDetector(
            name="email", entity_type=EntityType.EMAIL, category=pii,
            pattern=r"[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}",
        ),
        RegexDetector(
            name="phone", entity_type=EntityType.PHONE, category=pii,
            pattern=r"(?<!\d)(?:\+7|8)[\s\-]?\(?\d{3}\)?[\s\-]?\d{3}[\s\-]?\d{2}[\s\-]?\d{2}(?!\d)",
        ),
        RegexDetector(
            name="passport", entity_type=EntityType.PASSPORT, category=pii,
            # серия (4) + номер (6); между ними допустим знак «№»/«N» и пробелы
            # («4501 № 234567», «4509 123456»).
            pattern=r"\b\d{4}\s+(?:[№N]\s*)?\d{6}\b",
        ),
        # Банковская карта: слитная 16-значная последовательность с валидной
        # контрольной суммой Луна (высокая точность; 16 цифр — доминирующий
        # формат, не пересекается с ИНН-12 и ОГРН-13/15).
        RegexDetector(
            name="bank_card_luhn", entity_type=EntityType.BANK_CARD, category=pii,
            pattern=r"\b\d{16}\b", validator=validate_card_luhn,
        ),
        # Карта в «карточном» форматировании 4-4-4-4 с разделителями ловится и
        # без валидной Luhn-суммы: само форматирование — сильный признак карты.
        # Разделитель обязателен, поэтому 20-значный счёт сюда не попадает.
        RegexDetector(
            name="bank_card_grouped", entity_type=EntityType.BANK_CARD, category=pii,
            pattern=r"\b\d{4}[\s\-]\d{4}[\s\-]\d{4}[\s\-]\d{4}\b", confidence=0.85,
        ),
        RegexDetector(
            name="snils", entity_type=EntityType.SNILS, category=pii,
            pattern=r"\b\d{3}-\d{3}-\d{3}[\s\-]\d{2}\b", validator=validate_snils,
        ),
        RegexDetector(
            name="inn", entity_type=EntityType.INN, category=pii,
            pattern=r"\b\d{12}\b|\b\d{10}\b", validator=validate_inn,
        ),
        RegexDetector(
            name="ogrn", entity_type=EntityType.OGRN, category=pii,
            pattern=r"\b\d{15}\b|\b\d{13}\b", validator=validate_ogrn,
        ),
        RegexDetector(
            name="kpp", entity_type=EntityType.KPP, category=pii,
            pattern=r"\b\d{4}[\dA-Z]{2}\d{3}\b",
        ),
        RegexDetector(
            name="bik", entity_type=EntityType.BIK, category=pii,
            pattern=r"\b04\d{7}\b",
        ),
        RegexDetector(
            name="account", entity_type=EntityType.ACCOUNT, category=pii,
            pattern=r"\b\d{20}\b",
        ),
    ]
