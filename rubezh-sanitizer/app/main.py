"""Точка входа сервиса rubezh-sanitizer (FastAPI)."""

from __future__ import annotations

from fastapi import FastAPI

from app.api.routes import router
from app.config import settings

app = FastAPI(title=settings.app_name, version="0.1.0")
app.include_router(router)
