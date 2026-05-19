"""Тесты обратимой псевдонимизации и обратной подстановки."""

from __future__ import annotations

import base64

from app.detectors.registry import scan
from app.masking.crypto import MappingCipher
from app.masking.overlap import resolve_overlaps
from app.masking.pseudonymizer import pseudonymize, restore


def _cipher() -> MappingCipher:
    return MappingCipher.from_base64_key(base64.b64encode(b"k" * 32).decode())


def test_pseudonym_replaces_raw_value() -> None:
    text = "почта ivan@example.ru"
    sanitized, entities, _ = pseudonymize(text, resolve_overlaps(scan(text)), _cipher())
    assert "ivan@example.ru" not in sanitized
    assert any(e.pseudonym.startswith("EMAIL_") for e in entities)


def test_same_value_gets_same_pseudonym() -> None:
    text = "ivan@example.ru и снова ivan@example.ru"
    sanitized, entities, mappings = pseudonymize(
        text, resolve_overlaps(scan(text)), _cipher()
    )
    assert len({e.pseudonym for e in entities}) == 1
    assert len(mappings) == 1
    assert sanitized.count("EMAIL_001") == 2


def test_round_trip_restore_reconstructs_original() -> None:
    cipher = _cipher()
    text = "Договор подписал Иванов Иван Иванович, почта ivan@example.ru"
    sanitized, _, mappings = pseudonymize(text, resolve_overlaps(scan(text)), cipher)
    assert restore(sanitized, mappings, cipher) == text


def test_sanitized_text_has_no_raw_values() -> None:
    text = "ИНН 7707083893, ключ AKIAIOSFODNN7EXAMPLE"
    sanitized, _, _ = pseudonymize(text, resolve_overlaps(scan(text)), _cipher())
    assert "7707083893" not in sanitized
    assert "AKIAIOSFODNN7EXAMPLE" not in sanitized


def test_mapping_stores_encrypted_not_raw() -> None:
    cipher = _cipher()
    text = "ключ AKIAIOSFODNN7EXAMPLE"
    _, _, mappings = pseudonymize(text, resolve_overlaps(scan(text)), cipher)
    assert mappings
    for mapping in mappings:
        assert b"AKIAIOSFODNN7EXAMPLE" not in mapping.raw_value_encrypted
        assert cipher.decrypt(mapping.raw_value_encrypted) == "AKIAIOSFODNN7EXAMPLE"


def test_public_entity_has_no_raw_value() -> None:
    text = "почта ivan@example.ru"
    _, entities, _ = pseudonymize(text, resolve_overlaps(scan(text)), _cipher())
    assert entities
    for entity in entities:
        assert not hasattr(entity, "value")
        assert entity.raw_hash
