"""Тесты безопасности логирования: raw-значения сущностей не утекают."""

from __future__ import annotations

from app.detectors.registry import scan
from app.domain.entities import Category, EntityType, Match
from app.domain.risk import score_risk


def test_match_repr_does_not_expose_raw_value() -> None:
    secret = "AKIAIOSFODNN7EXAMPLE"
    match = Match(
        type=EntityType.SECRET_API_KEY,
        category=Category.SECRET,
        start=0,
        end=len(secret),
        value=secret,
        detector="regex",
        confidence=1.0,
    )
    assert secret not in repr(match)
    assert secret not in str(match)


def test_scanned_matches_repr_has_no_raw_secret() -> None:
    secret = "AKIAIOSFODNN7EXAMPLE"
    matches = scan(f"ключ {secret} в продакшене")
    # типичная ошибка разработчика — залогировать список Match целиком
    assert secret not in repr(matches)
    for match in matches:
        assert secret not in repr(match)


def test_repr_safe_for_all_secret_categories() -> None:
    samples = {
        EntityType.SECRET_JWT: "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.sigPARTvalue",
        EntityType.SECRET_API_KEY: "AKIAIOSFODNN7EXAMPLE",
        EntityType.SECRET_OAUTH: "ghp_" + "b" * 36,
        EntityType.SECRET_PASSWORD: "Sup3rSecret!",
        EntityType.SECRET_DSN: "postgres://u:p4ssw0rd@h:5432/db",
        EntityType.SECRET_CONN_STRING: "Server=db;Password=Secret123;",
    }
    for entity_type, raw in samples.items():
        match = Match(
            type=entity_type,
            category=Category.SECRET,
            start=0,
            end=len(raw),
            value=raw,
            detector="regex",
            confidence=1.0,
        )
        assert raw not in repr(match)


def test_risk_repr_does_not_expose_raw_secret() -> None:
    secret = "AKIAIOSFODNN7EXAMPLE"
    risk = score_risk(scan(f"ключ {secret}"))
    assert secret not in repr(risk)
