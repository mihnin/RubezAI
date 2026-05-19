"""Зависимости приложения (создаются один раз при старте)."""

from __future__ import annotations

import logging

from app.config import settings
from app.masking.crypto import MappingCipher

_logger = logging.getLogger("rubezh-sanitizer")


def build_cipher() -> MappingCipher:
    """Шифр mapping'ов. Без MAPPING_ENCRYPTION_KEY — эфемерный ключ (только dev).

    В production ключ обязателен: при эфемерном ключе обратная подстановка
    псевдонимов невозможна после перезапуска сервиса.
    """
    if settings.mapping_encryption_key:
        return MappingCipher.from_base64_key(settings.mapping_encryption_key)
    _logger.warning(
        "MAPPING_ENCRYPTION_KEY не задан — используется эфемерный ключ (только dev)"
    )
    return MappingCipher.generate()
