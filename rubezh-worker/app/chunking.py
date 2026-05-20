"""Chunking параграфов в куски целевого размера для embeddings/RAG.

План iteration-10.md §Р3 шаг 4:
- Целевой размер ~800 токенов (cl100k_base, tiktoken).
- Максимум 1024 на чанк (фиксировано размером embeddings.vector(1024)).
- Минимум 50 токенов (отсеиваем шум — короткие изолированные строки).
- Алгоритм: greedy-склейка параграфов до достижения целевого размера.
- Если параграф **сам** превышает max — разбиваем по предложениям
  (точка/восклицание/вопрос + пробел/EOL).
"""

from __future__ import annotations

from dataclasses import dataclass

import tiktoken


@dataclass(frozen=True)
class Chunk:
    """Куск текста готовый для sanitize + embed."""

    text: str
    token_count: int


_DEFAULT_TARGET = 800
_DEFAULT_MAX = 1024
_DEFAULT_MIN = 50


def chunk_paragraphs(
    paragraphs: list[str],
    *,
    target_tokens: int = _DEFAULT_TARGET,
    max_tokens: int = _DEFAULT_MAX,
    min_tokens: int = _DEFAULT_MIN,
    encoding_name: str = "cl100k_base",
) -> list[Chunk]:
    """Склейка параграфов в чанки до ~target_tokens.

    Если параграф больше max_tokens — разбивается по предложениям
    (точка/восклицание/вопрос, простая heuristic).

    Returns: list[Chunk] непустых чанков в порядке появления.
    """
    enc = tiktoken.get_encoding(encoding_name)
    units = _explode_oversize(paragraphs, max_tokens, enc)

    chunks: list[Chunk] = []
    current: list[str] = []
    current_tokens = 0
    for unit, count in units:
        if current_tokens + count <= target_tokens:
            current.append(unit)
            current_tokens += count
            continue
        if current_tokens >= min_tokens:
            chunks.append(Chunk("\n\n".join(current), current_tokens))
            current = [unit]
            current_tokens = count
        else:
            # Текущий накопленный — слишком маленький, добавляем
            # к нему текущий unit даже если выйдет за target.
            current.append(unit)
            current_tokens += count
    if current and current_tokens >= min_tokens:
        chunks.append(Chunk("\n\n".join(current), current_tokens))
    return chunks


def _explode_oversize(
    paragraphs: list[str], max_tokens: int, enc: tiktoken.Encoding
) -> list[tuple[str, int]]:
    """Разбивает параграфы, превышающие max_tokens, на предложения.

    Возвращает [(text, token_count), ...] — единицы для greedy-склейки.
    """
    out: list[tuple[str, int]] = []
    for para in paragraphs:
        tokens = len(enc.encode(para))
        if tokens <= max_tokens:
            out.append((para, tokens))
            continue
        # Разбивка по предложениям. Простая heuristic — split по
        # шаблонам ". " / "! " / "? " / "\n". Точнее не нужно для MVP.
        sentences = _split_sentences(para)
        for sent in sentences:
            n = len(enc.encode(sent))
            if n > max_tokens:
                # Превышает даже как одно предложение — режем по символам
                # с примерным token-ratio 1 токен ≈ 4 chars (английский,
                # для русского хуже, но это fallback).
                step = max_tokens * 4
                for start in range(0, len(sent), step):
                    piece = sent[start : start + step]
                    out.append((piece, len(enc.encode(piece))))
            else:
                out.append((sent, n))
    return out


def _split_sentences(text: str) -> list[str]:
    """Простая heuristic-разбивка по предложениям."""
    import re

    # Разделитель — . / ! / ? с последующим пробелом или EOL.
    parts = re.split(r"(?<=[.!?])\s+", text)
    return [p.strip() for p in parts if p.strip()]
