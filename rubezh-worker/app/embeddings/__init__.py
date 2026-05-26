"""Embeddings: interface + реализации (mock + openai-совместимая)."""

from .factory import build_embedder
from .interface import EMBEDDING_DIM, Embedder
from .mock import MockEmbedder
from .openai_compatible import OpenAICompatibleEmbedder

__all__ = [
    "EMBEDDING_DIM",
    "Embedder",
    "MockEmbedder",
    "OpenAICompatibleEmbedder",
    "build_embedder",
]
