"""Regex-детекторы секретов: API-ключи, токены, пароли, строки подключения."""

from __future__ import annotations

from app.detectors.regex_detector import RegexDetector
from app.domain.entities import Category, EntityType

# JWT: три base64url-сегмента; заголовок и payload начинаются с eyJ ('{"').
_JWT = r"\beyJ[A-Za-z0-9_\-]+\.eyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+"

# API-ключи известных форматов: AWS (AKIA…), Google (AIza…), OpenAI-подобные.
_API_KEY = (
    r"\bAKIA[0-9A-Z]{16}\b"
    r"|\bAIza[0-9A-Za-z_\-]{35}\b"
    r"|\bsk-[A-Za-z0-9\-]{20,}\b"
)

# OAuth-токены: GitHub (ghp_/gho_/ghu_/ghs_/ghr_) и Google (ya29.).
_OAUTH = r"\bgh[pousr]_[A-Za-z0-9]{36}\b|\bya29\.[0-9A-Za-z_\-]{20,}\b"

# Пароль в присваивании; группа 1 — только значение секрета (без ключевого
# слова). Между ключевым словом и разделителем допускаются уточняющие слова
# («пароль доступа для тестирования: …»), но не более 30 символов и без
# переноса строки/двоеточия — это ограничивает ложные срабатывания. Значение —
# ≥6 непробельных символов; класс [^\s;'"] обрывает его на разделителях
# key=value-конструкций.
_PASSWORD = r"(?i)(?:password|passwd|pwd|пароль)[^\n:=]{0,30}[:=]\s*([^\s;'\"]{6,})"

# CVC/CVV банковской карты: 3–4 цифры после ключевого слова. group 1 — только
# число (без слова). Keyword-anchored — «голые» 3 цифры не ловятся (точность).
_CARD_CVC = r"(?i)\b(?:CVC2?|CVV2?|CVP2)\b\s*[:\-—]?\s*(\d{3,4})\b"

# DSN: URI со встроенными учётными данными — scheme://user:pass@host.
_DSN = r"\b[a-zA-Z][a-zA-Z0-9+.\-]*://[^\s:/@]+:[^\s:/@]+@[^\s/]+"

# Строка подключения вида key=value;… содержащая пароль.
_CONN_STRING = (
    r"(?i)(?:server|host|data source|database)\s*=\s*[^;]+;"
    r"[^\n]{0,200}?(?:password|pwd)\s*=\s*\S+"
)


def secret_detectors() -> list[RegexDetector]:
    """Все regex-детекторы секретов (итерация 3).

    Детекторы могут пересекаться: внутри connection string «Password=…»
    параллельно ловится детектором password. Снятие пересечений — итерация 4.
    """
    secret = Category.SECRET
    return [
        RegexDetector(name="jwt", entity_type=EntityType.SECRET_JWT,
                      category=secret, pattern=_JWT),
        RegexDetector(name="api_key", entity_type=EntityType.SECRET_API_KEY,
                      category=secret, pattern=_API_KEY),
        RegexDetector(name="oauth", entity_type=EntityType.SECRET_OAUTH,
                      category=secret, pattern=_OAUTH),
        RegexDetector(name="password", entity_type=EntityType.SECRET_PASSWORD,
                      category=secret, pattern=_PASSWORD, group=1),
        RegexDetector(name="card_cvc", entity_type=EntityType.SECRET_CARD_CVC,
                      category=secret, pattern=_CARD_CVC, group=1),
        RegexDetector(name="dsn", entity_type=EntityType.SECRET_DSN,
                      category=secret, pattern=_DSN),
        RegexDetector(name="conn_string", entity_type=EntityType.SECRET_CONN_STRING,
                      category=secret, pattern=_CONN_STRING),
    ]
