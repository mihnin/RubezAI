"""Конфигурация rubezh-worker через env (pydantic-settings).

Все обязательные параметры — без default'ов, отсутствие → ошибка при
загрузке (fail-closed на старте; сервис не запустится без полного
конфига).
"""

from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    """Конфигурация сервиса.

    Источник — переменные окружения. См. .env.example в корне репо.
    """

    model_config = SettingsConfigDict(
        env_file=None,  # значения берутся напрямую из env (docker-compose)
        env_prefix="",
        case_sensitive=False,
        extra="ignore",
    )

    # HTTP-сервер (для healthcheck).
    worker_port: int = Field(default=8002, alias="WORKER_PORT")
    log_level: str = Field(default="info", alias="WORKER_LOG_LEVEL")

    # PostgreSQL — БД-очередь.
    database_url: str = Field(alias="DATABASE_URL")

    # MinIO — object storage для raw документов.
    minio_endpoint: str = Field(default="minio:9000", alias="MINIO_ENDPOINT")
    minio_access_key: str = Field(alias="MINIO_ROOT_USER")
    minio_secret_key: str = Field(alias="MINIO_ROOT_PASSWORD")
    minio_bucket: str = Field(default="rubezh-documents", alias="MINIO_BUCKET")
    minio_secure: bool = Field(default=False, alias="MINIO_SECURE")

    # Sanitizer URL — HTTP-клиент для обезличивания чанков.
    sanitizer_url: str = Field(
        default="http://rubezh-sanitizer:8001", alias="SANITIZER_URL"
    )

    # Worker-параметры (см. iteration-10.md §Р2/Р3).
    queue_poll_interval_seconds: float = Field(
        default=2.0, alias="WORKER_POLL_INTERVAL_SECONDS"
    )
    heartbeat_interval_seconds: float = Field(
        default=60.0, alias="WORKER_HEARTBEAT_INTERVAL_SECONDS"
    )
    stuck_threshold_minutes: int = Field(
        default=15, alias="WORKER_STUCK_THRESHOLD_MINUTES"
    )
    max_attempts: int = Field(default=3, alias="WORKER_MAX_ATTEMPTS")
    sanitize_concurrency: int = Field(default=4, alias="WORKER_SANITIZE_CONCURRENCY")

    # Embedder (Итерация 11 §Р2). EMBEDDER_KIND={mock|openai_compatible}.
    # Симметрия с rubezh-api: оба сервиса должны иметь идентичный
    # embedder, иначе query-вектор и doc-вектор живут в разных
    # пространствах и cosine ranking бесполезен.
    embedder_kind: str = Field(default="mock", alias="EMBEDDER_KIND")
    embedder_url: str = Field(default="", alias="EMBEDDER_URL")
    embedder_model: str = Field(default="", alias="EMBEDDER_MODEL")
    embedder_api_key: str = Field(default="", alias="EMBEDDER_API_KEY")
    embedder_timeout_seconds: float = Field(
        default=30.0, alias="EMBEDDER_TIMEOUT_SECONDS"
    )


def load_settings() -> Settings:
    """Загружает Settings из env. Падает при отсутствии обязательных полей."""
    return Settings()
