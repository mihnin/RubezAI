"""Шифрование raw-значений mapping'ов псевдонимов (AES-256-GCM)."""

from __future__ import annotations

import base64
import os

from cryptography.hazmat.primitives.ciphers.aead import AESGCM

_NONCE_BYTES = 12
_KEY_BYTES = 32


class MappingCipher:
    """AES-256-GCM шифрование значений.

    Формат blob: ``nonce(12 байт) || ciphertext+tag``. Nonce уникален на каждое
    шифрование, поэтому одинаковый текст даёт разный шифротекст.
    """

    def __init__(self, key: bytes) -> None:
        if len(key) != _KEY_BYTES:
            raise ValueError(
                f"Ключ AES-256 должен быть {_KEY_BYTES} байт, получено {len(key)}"
            )
        self._aesgcm = AESGCM(key)

    @classmethod
    def from_base64_key(cls, key_b64: str) -> MappingCipher:
        """Создаёт шифр из base64-кодированного 32-байтного ключа."""
        return cls(base64.b64decode(key_b64))

    @classmethod
    def generate(cls) -> MappingCipher:
        """Создаёт шифр со случайным ключом (для dev/тестов)."""
        return cls(AESGCM.generate_key(bit_length=8 * _KEY_BYTES))

    def encrypt(self, plaintext: str) -> bytes:
        nonce = os.urandom(_NONCE_BYTES)
        return nonce + self._aesgcm.encrypt(nonce, plaintext.encode("utf-8"), None)

    def decrypt(self, blob: bytes) -> str:
        nonce, ciphertext = blob[:_NONCE_BYTES], blob[_NONCE_BYTES:]
        return self._aesgcm.decrypt(nonce, ciphertext, None).decode("utf-8")
