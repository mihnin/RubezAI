"""Async HTTP-клиент к rubezh-sanitizer.

План iteration-10.md §Р3 шаг 6: параллельные вызовы для документа
из N чанков через asyncio.Semaphore(concurrency=4).
"""

from __future__ import annotations

import asyncio
from typing import Any

import httpx


class SanitizerClient:
    """HTTP-клиент `rubezh-sanitizer`.

    POST /sanitize/preview с {"text": ..., "context": "document"}.
    Ответ — sanitize.schema.json#SanitizeResponse (sanitized_text,
    entities[], risk, mapping_id?).
    """

    def __init__(
        self, base_url: str, *, timeout_seconds: float = 30.0,
        concurrency: int = 4,
    ) -> None:
        self._base = base_url.rstrip("/")
        self._client = httpx.AsyncClient(timeout=timeout_seconds)
        self._sem = asyncio.Semaphore(concurrency)

    async def preview(self, text: str) -> dict[str, Any]:
        """Sanitize одного чанка. Возвращает JSON-ответ."""
        async with self._sem:
            resp = await self._client.post(
                f"{self._base}/sanitize/preview",
                json={"text": text, "context": "document"},
            )
            resp.raise_for_status()
            return resp.json()

    async def preview_batch(self, texts: list[str]) -> list[dict[str, Any]]:
        """Параллельный sanitize N чанков (limited concurrency)."""
        return await asyncio.gather(*[self.preview(t) for t in texts])

    async def aclose(self) -> None:
        """Закрывает HTTP-клиент."""
        await self._client.aclose()
