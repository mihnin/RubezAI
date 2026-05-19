"""Тесты эндпойнта /sanitize/preview и соответствия контракту."""

from __future__ import annotations

import json
from pathlib import Path

import jsonschema
from fastapi.testclient import TestClient

from app.main import app

client = TestClient(app)

_SCHEMA_PATH = (
    Path(__file__).parents[2] / "docs" / "contracts" / "sanitize.schema.json"
)


def test_preview_returns_sanitized_response() -> None:
    response = client.post(
        "/sanitize/preview",
        json={"text": "ИНН 7707083893, почта a@b.ru", "context": "chat"},
    )
    assert response.status_code == 200
    body = response.json()
    assert {"sanitized_text", "entities", "risk"} <= body.keys()


def test_preview_response_has_no_raw_values() -> None:
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


def test_preview_rejects_empty_text() -> None:
    response = client.post("/sanitize/preview", json={"text": "", "context": "chat"})
    assert response.status_code == 422


def test_preview_response_validates_against_contract() -> None:
    response = client.post(
        "/sanitize/preview",
        json={"text": "сумма 5 млн рублей по договору № 7/2025", "context": "chat"},
    )
    body = response.json()
    schema = json.loads(_SCHEMA_PATH.read_text(encoding="utf-8"))
    response_schema = {"$defs": schema["$defs"], "$ref": "#/$defs/SanitizeResponse"}
    jsonschema.validate(instance=body, schema=response_schema)


def test_preview_entity_spans_are_valid() -> None:
    text = "почта ivan@example.ru, ИНН 7707083893"
    response = client.post(
        "/sanitize/preview", json={"text": text, "context": "chat"}
    )
    for entity in response.json()["entities"]:
        assert 0 <= entity["start"] < entity["end"] <= len(text)
