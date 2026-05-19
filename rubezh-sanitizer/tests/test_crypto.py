"""Тесты шифрования mapping'ов псевдонимов (AES-256-GCM)."""

from __future__ import annotations

import base64

import pytest
from cryptography.exceptions import InvalidTag

from app.masking.crypto import MappingCipher


def _cipher() -> MappingCipher:
    return MappingCipher.from_base64_key(base64.b64encode(b"k" * 32).decode())


def test_encrypt_decrypt_round_trip() -> None:
    cipher = _cipher()
    plaintext = "Иванов Иван Иванович"
    blob = cipher.encrypt(plaintext)
    assert isinstance(blob, bytes)
    assert cipher.decrypt(blob) == plaintext


def test_ciphertext_differs_each_call() -> None:
    cipher = _cipher()
    first = cipher.encrypt("секрет")
    second = cipher.encrypt("секрет")
    assert first != second  # уникальный nonce на каждое шифрование
    assert cipher.decrypt(first) == cipher.decrypt(second) == "секрет"


def test_plaintext_not_present_in_ciphertext() -> None:
    secret = "AKIAIOSFODNN7EXAMPLE"
    assert secret.encode() not in _cipher().encrypt(secret)


def test_wrong_key_cannot_decrypt() -> None:
    blob = _cipher().encrypt("данные")
    other = MappingCipher.from_base64_key(base64.b64encode(b"x" * 32).decode())
    with pytest.raises(Exception):  # noqa: B017 — любая криптоошибка приемлема
        other.decrypt(blob)


def test_generate_produces_working_cipher() -> None:
    cipher = MappingCipher.generate()
    assert cipher.decrypt(cipher.encrypt("текст")) == "текст"


def test_invalid_key_length_rejected() -> None:
    with pytest.raises(ValueError):
        MappingCipher.from_base64_key(base64.b64encode(b"short").decode())


def test_decrypt_corrupted_blob_raises() -> None:
    cipher = _cipher()
    blob = bytearray(cipher.encrypt("данные"))
    blob[-1] ^= 0xFF  # порча GCM-тега
    with pytest.raises(InvalidTag):
        cipher.decrypt(bytes(blob))


def test_decrypt_truncated_blob_raises() -> None:
    with pytest.raises(Exception):  # noqa: B017 — blob короче nonce
        _cipher().decrypt(b"short")


def test_encrypt_decrypt_empty_string() -> None:
    cipher = _cipher()
    assert cipher.decrypt(cipher.encrypt("")) == ""


def test_encrypt_decrypt_unicode_and_long_text() -> None:
    cipher = _cipher()
    payload = "日本語 ёЁ договор №7 " * 5000
    assert cipher.decrypt(cipher.encrypt(payload)) == payload
