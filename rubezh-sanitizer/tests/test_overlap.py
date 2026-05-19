"""Тесты снятия пересечений детекторов."""

from __future__ import annotations

from app.detectors.registry import scan
from app.domain.entities import Category, EntityType, Match
from app.masking.overlap import resolve_overlaps


def _match(
    entity_type: EntityType, category: Category, start: int, end: int, conf: float = 1.0
) -> Match:
    return Match(
        type=entity_type,
        category=category,
        start=start,
        end=end,
        value="x" * (end - start),
        detector="regex",
        confidence=conf,
    )


def test_empty_input() -> None:
    assert resolve_overlaps([]) == []


def test_non_overlapping_matches_all_kept() -> None:
    matches = [
        _match(EntityType.EMAIL, Category.PII, 0, 5),
        _match(EntityType.INN, Category.PII, 10, 20),
    ]
    assert len(resolve_overlaps(matches)) == 2


def test_overlapping_same_span_keeps_one() -> None:
    matches = [
        _match(EntityType.KPP, Category.PII, 0, 9),
        _match(EntityType.BIK, Category.PII, 0, 9),
    ]
    assert len(resolve_overlaps(matches)) == 1


def test_secret_wins_over_lower_priority_category() -> None:
    matches = [
        _match(EntityType.COMMERCIAL_AMOUNT, Category.COMMERCIAL, 0, 10),
        _match(EntityType.SECRET_API_KEY, Category.SECRET, 2, 8),
    ]
    resolved = resolve_overlaps(matches)
    assert len(resolved) == 1
    assert resolved[0].category is Category.SECRET


def test_result_is_sorted_and_non_overlapping() -> None:
    resolved = resolve_overlaps(scan("Server=db;Database=app;Password=Secret123;"))
    for current, following in zip(resolved, resolved[1:], strict=False):
        assert current.end <= following.start


def test_partial_overlap_keeps_higher_weight() -> None:
    matches = [
        _match(EntityType.COMMERCIAL_AMOUNT, Category.COMMERCIAL, 0, 12),
        _match(EntityType.COMMERCIAL_CONTRACT, Category.COMMERCIAL, 5, 20),
    ]
    resolved = resolve_overlaps(matches)
    assert len(resolved) == 1
    assert resolved[0].end - resolved[0].start == 15


def test_nested_span_keeps_outer() -> None:
    matches = [
        _match(EntityType.INN, Category.PII, 0, 20),
        _match(EntityType.EMAIL, Category.PII, 5, 10),
    ]
    resolved = resolve_overlaps(matches)
    assert len(resolved) == 1
    assert (resolved[0].start, resolved[0].end) == (0, 20)


def test_chain_keeps_independent_neighbours() -> None:
    # A и C не пересекаются между собой, но обе пересекают B —
    # сохраняются обе (A, C), а не один общий сосед B
    a = _match(EntityType.INN, Category.PII, 0, 10, 0.9)
    b = _match(EntityType.EMAIL, Category.PII, 8, 18, 1.0)
    c = _match(EntityType.PHONE, Category.PII, 16, 26, 0.9)
    resolved = resolve_overlaps([a, b, c])
    assert [(m.start, m.end) for m in resolved] == [(0, 10), (16, 26)]


def test_deterministic_regardless_of_input_order() -> None:
    a = _match(EntityType.INN, Category.PII, 0, 10)
    b = _match(EntityType.EMAIL, Category.PII, 5, 15)
    assert resolve_overlaps([a, b]) == resolve_overlaps([b, a])
