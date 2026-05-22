"""Зависимости приложения (создаются один раз при старте)."""

from __future__ import annotations

import logging

from app.config import settings
from app.detectors.base import Detector
from app.llm_review.detector import LLMReviewDetector
from app.llm_review.openai_client import OpenAILLMReviewClient
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


def build_llm_detector() -> Detector | None:
    """Детектор LLM-review (фильтр 2/3) из конфигурации.

    Возвращает ``None``, если SANITIZER_LLM_URL не задан — тогда пайплайн
    работает на одних детерминированных детекторах (фильтр 1). Сам детектор
    fail-open: недоступность модели не ломает обезличивание.
    """
    if not settings.llm_url or not settings.llm_model:
        _logger.info("LLM-review отключён (SANITIZER_LLM_URL/MODEL не заданы)")
        return None
    client = OpenAILLMReviewClient(
        url=settings.llm_url,
        model=settings.llm_model,
        api_key=settings.llm_key,
        timeout=settings.llm_timeout,
    )
    _logger.info("LLM-review включён: %s (%s)", settings.llm_url, settings.llm_model)
    return LLMReviewDetector(client)
