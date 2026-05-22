"""Точка входа сервиса rubezh-sanitizer (FastAPI)."""

from __future__ import annotations

from collections.abc import AsyncIterator
from contextlib import asynccontextmanager

from fastapi import FastAPI

from app.api.routes import router
from app.config import settings
from app.deps import build_cipher, build_llm_detector


@asynccontextmanager
async def lifespan(app: FastAPI) -> AsyncIterator[None]:
    """Инициализация ресурсов при старте: шифр mapping'ов псевдонимов.

    Создаётся в lifespan, а не на import-time — ошибка ключа возникает при
    контролируемом старте приложения, а не при импорте модуля.
    """
    app.state.cipher = build_cipher()
    app.state.llm_detector = build_llm_detector()
    yield


app = FastAPI(title=settings.app_name, version="0.1.0", lifespan=lifespan)
app.include_router(router)
