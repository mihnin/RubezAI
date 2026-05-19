"""Regex-детекторы коммерчески чувствительных данных."""

from __future__ import annotations

from app.detectors.regex_detector import RegexDetector
from app.domain.entities import Category, EntityType

# Денежная сумма: число (с разрядами) + опц. масштаб + валюта.
_AMOUNT = (
    r"(?i)\b\d[\d  .,]*\s*(?:тыс\.?|млн|млрд)?\s*"
    r"(?:руб\.?|рубл\w*|₽|rub|usd|eur|долл\w*|евро)\b"
)

# Номер договора/контракта: ключевое слово + токен, содержащий хотя бы цифру
# (lookahead отсекает «договор поставки» без номера).
_CONTRACT = (
    r"(?i)\b(?:договор|контракт|госконтракт)\w*\s*(?:№|n|no|#)?\s*"
    r"(?=[\w\-/]*\d)[\w\-/]{1,20}"
)

# Контрагент: организация в кавычках после организационно-правовой формы.
_SUPPLIER = r"\b(?:ООО|АО|ЗАО|ПАО|НКО|ИП)\s+[«\"][^»\"\n]{1,60}[»\"]"

# Коммерчески чувствительные условия — ключевые слова и фразы.
_TERMS = (
    r"(?i)\b(?:маржа|маржинальн\w*|наценк\w+|себестоимост\w+|"
    r"услови\w+\s+поставки|тендерн\w+\s+услови\w+|отсрочк\w+\s+платежа)\b"
)


def commercial_detectors() -> list[RegexDetector]:
    """Все regex-детекторы коммерчески чувствительных данных (итерация 3).

    COMMERCIAL_TERMS — риск-сигнал (наличие обсуждения маржи/условий),
    а не обязательно маскируемый токен; стратегия маскирования — итерация 4.
    """
    com = Category.COMMERCIAL
    return [
        RegexDetector(name="amount", entity_type=EntityType.COMMERCIAL_AMOUNT,
                      category=com, pattern=_AMOUNT, confidence=0.8),
        RegexDetector(name="contract", entity_type=EntityType.COMMERCIAL_CONTRACT,
                      category=com, pattern=_CONTRACT, confidence=0.8),
        RegexDetector(name="supplier", entity_type=EntityType.COMMERCIAL_SUPPLIER,
                      category=com, pattern=_SUPPLIER, confidence=0.7),
        RegexDetector(name="terms", entity_type=EntityType.COMMERCIAL_TERMS,
                      category=com, pattern=_TERMS, confidence=0.6),
    ]
