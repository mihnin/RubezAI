"""Embeddings: interface + mock-реализация для MVP."""

from .interface import Embedder
from .mock import MockEmbedder

__all__ = ["Embedder", "MockEmbedder"]
