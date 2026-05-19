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

# Пароль в присваивании; группа 1 — только значение секрета (без ключевого слова).
_PASSWORD = r"(?i)(?:password|passwd|pwd|пароль)\s*[:=]\s*(\S{4,})"

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
        RegexDetector(name="dsn", entity_type=EntityType.SECRET_DSN,
                      category=secret, pattern=_DSN),
        RegexDetector(name="conn_string", entity_type=EntityType.SECRET_CONN_STRING,
                      category=secret, pattern=_CONN_STRING),
    ]
