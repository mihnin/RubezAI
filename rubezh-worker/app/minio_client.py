"""MinIO-клиент worker'а: только download для прочтения raw документа."""

from __future__ import annotations

import asyncio
from io import BytesIO

from minio import Minio


class WorkerMinio:
    """Минимальный sync-клиент с asyncio-wrapper."""

    def __init__(
        self, endpoint: str, access_key: str, secret_key: str,
        bucket: str, *, secure: bool = False,
    ) -> None:
        self._client = Minio(
            endpoint, access_key=access_key,
            secret_key=secret_key, secure=secure,
        )
        self._bucket = bucket

    async def download(self, storage_key: str) -> bytes:
        """Скачивает объект в bytes (через executor — minio-py sync)."""
        return await asyncio.to_thread(self._download_sync, storage_key)

    def _download_sync(self, storage_key: str) -> bytes:
        resp = self._client.get_object(self._bucket, storage_key)
        try:
            buf = BytesIO()
            for chunk in resp.stream():
                buf.write(chunk)
            return buf.getvalue()
        finally:
            resp.close()
            resp.release_conn()
