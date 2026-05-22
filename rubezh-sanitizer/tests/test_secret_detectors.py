"""Unit-тесты детекторов секретов."""

from __future__ import annotations

import pytest

from app.detectors.registry import scan
from app.domain.entities import Category, EntityType, Match


def _types(matches: list[Match]) -> set[EntityType]:
    return {m.type for m in matches}


def test_detect_jwt() -> None:
    jwt = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.dozjgNryP4J3jVmNHDpoEANYwU"
    matches = scan(f"токен доступа: {jwt}")
    assert EntityType.SECRET_JWT in _types(matches)
    found = next(m for m in matches if m.type == EntityType.SECRET_JWT)
    assert found.category is Category.SECRET


def test_detect_aws_api_key() -> None:
    assert EntityType.SECRET_API_KEY in _types(scan("ключ AKIAIOSFODNN7EXAMPLE в конфиге"))


def test_detect_sk_api_key() -> None:
    assert EntityType.SECRET_API_KEY in _types(
        scan("OPENAI_KEY=sk-abcdefghijklmnopqrstuvwxyz0123")
    )


def test_detect_github_oauth_token() -> None:
    token = "ghp_" + "a" * 36
    assert EntityType.SECRET_OAUTH in _types(scan(f"git remote с токеном {token}"))


def test_detect_password_assignment_value_only() -> None:
    matches = scan("DB_PASSWORD=Sup3rSecret!")
    assert EntityType.SECRET_PASSWORD in _types(matches)
    found = next(m for m in matches if m.type == EntityType.SECRET_PASSWORD)
    # значение матча — только сам секрет, без ключевого слова
    assert found.value == "Sup3rSecret!"


@pytest.mark.parametrize(
    "text,expected",
    [
        ("код CVC: 412", "412"),
        ("CVV 123", "123"),
        ("CVC2: 4567", "4567"),
        ("cvv2 — 999", "999"),
    ],
)
def test_detect_card_cvc(text: str, expected: str) -> None:
    matches = scan(text)
    cvc = next((m for m in matches if m.type == EntityType.SECRET_CARD_CVC), None)
    assert cvc is not None
    assert cvc.value == expected
    assert cvc.category is Category.SECRET


@pytest.mark.parametrize("text", ["CVC карты не указан", "номер 412 в очереди"])
def test_cvc_not_detected_without_keyword_and_number(text: str) -> None:
    # без связки «ключевое слово + 3–4 цифры» CVC не детектируется (точность)
    assert EntityType.SECRET_CARD_CVC not in {m.type for m in scan(text)}


def test_detect_dsn_with_credentials() -> None:
    assert EntityType.SECRET_DSN in _types(
        scan("postgres://admin:p4ssw0rd@db.local:5432/app")
    )


def test_detect_connection_string() -> None:
    conn = "Server=db.local;Database=app;User Id=sa;Password=Secret123;"
    assert EntityType.SECRET_CONN_STRING in _types(scan(conn))


def test_clean_text_has_no_secrets() -> None:
    matches = scan("Обычный текст про погоду и планы на выходные")
    assert not any(m.category is Category.SECRET for m in matches)


def test_password_word_without_value_is_not_secret() -> None:
    # слово «password» без присваивания значения секретом не считается
    assert EntityType.SECRET_PASSWORD not in _types(scan("забыл свой password снова"))


def test_password_span_uses_capture_group() -> None:
    text = "DB_PASSWORD=Sup3rSecret!"
    found = next(m for m in scan(text) if m.type == EntityType.SECRET_PASSWORD)
    assert found.value == "Sup3rSecret!"
    assert text[found.start : found.end] == "Sup3rSecret!"
    assert found.start == text.index("Sup3rSecret!")


def test_password_value_stops_at_whitespace() -> None:
    # \S+ — значение пароля обрывается на пробеле (документированное поведение)
    found = next(
        m for m in scan("пароль: qwerty 123") if m.type == EntityType.SECRET_PASSWORD
    )
    assert found.value == "qwerty"


def test_detect_password_with_words_before_separator() -> None:
    # «пароль <слова>: <значение>» — ключевое слово не вплотную к двоеточию
    # (как в реальном договоре). Значение извлекается, ключевые слова — нет.
    text = "Внутренний пароль доступа для тестирования: TestPass!2026-Secret-Key (NDA)"
    found = next(m for m in scan(text) if m.type == EntityType.SECRET_PASSWORD)
    assert found.value == "TestPass!2026-Secret-Key"
    assert text[found.start : found.end] == found.value


def test_password_short_word_after_keyword_is_not_secret() -> None:
    # короткое обычное слово после «пароль …:» не принимается за секрет (<6 симв.)
    assert EntityType.SECRET_PASSWORD not in _types(scan("пароль в форме входа: ниже"))


def test_password_in_multiline_config() -> None:
    config = "user=admin\npassword=Secret123\nhost=db.local"
    found = next(m for m in scan(config) if m.type == EntityType.SECRET_PASSWORD)
    assert found.value == "Secret123"


def test_conn_string_and_password_overlap_both_reported() -> None:
    # Внутри connection string «Password=…» ловится и conn_string, и password
    types = _types(scan("Server=db;Database=app;Password=Secret123;"))
    assert EntityType.SECRET_CONN_STRING in types
    assert EntityType.SECRET_PASSWORD in types


def test_jwt_detected_after_bearer_prefix() -> None:
    jwt = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.abcDEF123_-xyz"
    text = f"Authorization: Bearer {jwt}"
    found = next(m for m in scan(text) if m.type == EntityType.SECRET_JWT)
    assert text[found.start : found.end] == found.value
    assert not found.value.startswith("Bearer")
