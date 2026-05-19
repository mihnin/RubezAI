"""Тесты эндпойнта /sanitize/preview и соответствия контракту."""

from __future__ import annotations

import hashlib
import json
from collections.abc import Iterator
from pathlib import Path

import jsonschema
import pytest
from fastapi.testclient import TestClient

from app.main import app

_SCHEMA_PATH = (
    Path(__file__).parents[2] / "docs" / "contracts" / "sanitize.schema.json"
)


@pytest.fixture
def client() -> Iterator[TestClient]:
    # контекст-менеджер запускает lifespan — app.state.cipher инициализируется
    with TestClient(app) as test_client:
        yield test_client


def test_preview_returns_sanitized_response(client: TestClient) -> None:
    response = client.post(
        "/sanitize/preview",
        json={"text": "ИНН 7707083893, почта a@b.ru", "context": "chat"},
    )
    assert response.status_code == 200
    body = response.json()
    assert {"sanitized_text", "entities", "risk"} <= body.keys()


def test_preview_response_has_no_raw_values(client: TestClient) -> None:
    secret = "AKIAIOSFODNN7EXAMPLE"
    response = client.post(
        "/sanitize/preview", json={"text": f"ключ {secret}", "context": "chat"}
    )
    body = response.json()
    assert secret not in json.dumps(body, ensure_ascii=False)
    assert secret not in body["sanitized_text"]
    for entity in body["entities"]:
        assert "value" not in entity
        assert entity["pseudonym"] and entity["raw_hash"]


def test_preview_rejects_empty_text(client: TestClient) -> None:
    response = client.post("/sanitize/preview", json={"text": "", "context": "chat"})
    assert response.status_code == 422


def test_preview_response_validates_against_contract(client: TestClient) -> None:
    response = client.post(
        "/sanitize/preview",
        json={"text": "сумма 5 млн рублей по договору № 7/2025", "context": "chat"},
    )
    body = response.json()
    schema = json.loads(_SCHEMA_PATH.read_text(encoding="utf-8"))
    response_schema = {"$defs": schema["$defs"], "$ref": "#/$defs/SanitizeResponse"}
    jsonschema.validate(instance=body, schema=response_schema)


def test_preview_entity_spans_are_valid(client: TestClient) -> None:
    text = "почта ivan@example.ru, ИНН 7707083893"
    response = client.post(
        "/sanitize/preview", json={"text": text, "context": "chat"}
    )
    for entity in response.json()["entities"]:
        assert 0 <= entity["start"] < entity["end"] <= len(text)


def test_preview_entity_span_matches_raw_hash(client: TestClient) -> None:
    # спан в ответе указывает на тот фрагмент, чей SHA-256 == raw_hash
    text = "почта ivan@example.ru, ИНН 7707083893"
    body = client.post(
        "/sanitize/preview", json={"text": text, "context": "chat"}
    ).json()
    for entity in body["entities"]:
        fragment = text[entity["start"] : entity["end"]]
        digest = hashlib.sha256(fragment.encode("utf-8")).hexdigest()
        assert digest == entity["raw_hash"]


def test_preview_no_raw_for_all_categories(client: TestClient) -> None:
    raws = [
        "Иванов Иван Иванович",
        "7707083893",
        "AKIAIOSFODNN7EXAMPLE",
        "SuperSecret123",
        "5 000 000",
    ]
    text = (
        "Иванов Иван Иванович, ИНН 7707083893, ключ AKIAIOSFODNN7EXAMPLE, "
        "password=SuperSecret123, сумма 5 000 000 рублей"
    )
    body = client.post(
        "/sanitize/preview", json={"text": text, "context": "chat"}
    ).json()
    dumped = json.dumps(body, ensure_ascii=False)
    for raw in raws:
        assert raw not in dumped


def test_preview_rejects_invalid_document_id(client: TestClient) -> None:
    response = client.post(
        "/sanitize/preview",
        json={"text": "x", "context": "chat", "document_id": "not-a-uuid"},
    )
    assert response.status_code == 422


def test_preview_no_entities_low_risk(client: TestClient) -> None:
    text = "просто текст без чувствительных данных"
    body = client.post(
        "/sanitize/preview", json={"text": text, "context": "chat"}
    ).json()
    assert body["entities"] == []
    assert body["sanitized_text"] == text
    assert body["risk"] == {"score": 0.0, "level": "low", "classes": []}


def test_preview_risk_classes_equal_union_of_categories(client: TestClient) -> None:
    body = client.post(
        "/sanitize/preview",
        json={
            "text": "ИНН 7707083893, ключ AKIAIOSFODNN7EXAMPLE, сумма 100 руб.",
            "context": "chat",
        },
    ).json()
    assert set(body["risk"]["classes"]) == {e["category"] for e in body["entities"]}
