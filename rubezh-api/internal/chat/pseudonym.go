package chat

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/rubezh-ai/rubezh-api/internal/sanitizer"
)

// PseudonymMap — соответствие псевдоним ↔ исходное значение, построенное
// из исходного текста и спанов сущностей sanitizer.
//
// Инвариант «никакого raw в логах» (план iteration-9.md §Р7):
// PseudonymMap реализует slog.LogValuer; стандартное логирование через
// `slog.Info("...", "pmap", pmap)` НЕ выводит raw-значения,
// только агрегат `entries: N + redacted`.
type PseudonymMap struct {
	toRaw map[string]string
}

// Len возвращает число пар в карте.
func (m PseudonymMap) Len() int { return len(m.toRaw) }

// Raw возвращает raw-значение по псевдониму. Используется оркестратором
// для построения зашифрованных PseudonymMappingInput (план §Р2).
// Возвращает пустую строку и false, если псевдонима нет в карте.
func (m PseudonymMap) Raw(pseudonym string) (string, bool) {
	raw, ok := m.toRaw[pseudonym]
	return raw, ok
}

// LogValue реализует slog.LogValuer. Гарантирует, что raw-значения
// никогда не попадают в логи: возвращает только агрегированное
// количество записей + явный маркер redacted.
func (m PseudonymMap) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Int("entries", len(m.toRaw)),
		slog.String("redacted", "raw pseudonym values redacted by design"),
	)
}

// MappingAAD вычисляет AAD для AES-GCM шифрования mapping'а
// (план §Р1, закрытие MINOR-M3 ревью v2). AAD = SHA-256(sessionID || pseudonym),
// первые 16 байт. Гарантирует уникальность AAD per-mapping —
// защищает от swap-атак на уровне БД даже внутри одной сессии.
func MappingAAD(sessionID, pseudonym string) []byte {
	h := sha256.New()
	h.Write([]byte(sessionID))
	h.Write([]byte(pseudonym))
	sum := h.Sum(nil)
	return sum[:16]
}

// BuildPseudonymMap строит карту псевдонимов из исходного текста и сущностей.
// Спаны индексируют исходный текст по код-поинтам (инвариант контракта
// sanitize.schema.json). Fail-closed: нарушение границ спана, пересечение
// спанов либо несоответствие raw_hash срезу → ошибка, запрос дальше не идёт.
func BuildPseudonymMap(
	originalText string, entities []sanitizer.Entity,
) (PseudonymMap, error) {
	runes := []rune(originalText)
	n := len(runes)
	// сортируем копию по Start для проверки непересечения
	sorted := append([]sanitizer.Entity(nil), entities...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Start < sorted[j].Start
	})
	toRaw := make(map[string]string, len(sorted))
	prevEnd := 0
	for _, e := range sorted {
		if e.Start < 0 || e.End <= e.Start || e.End > n {
			return PseudonymMap{}, fmt.Errorf(
				"chat: спан сущности %s [%d,%d) вне границ [0,%d)",
				e.Type, e.Start, e.End, n)
		}
		if e.Start < prevEnd {
			return PseudonymMap{}, fmt.Errorf(
				"chat: спан сущности %s [%d,%d) пересекает предыдущий (конец %d)",
				e.Type, e.Start, e.End, prevEnd)
		}
		raw := string(runes[e.Start:e.End])
		sum := sha256.Sum256([]byte(raw))
		if hex.EncodeToString(sum[:]) != e.RawHash {
			return PseudonymMap{}, fmt.Errorf(
				"chat: сущность %s — спан не соответствует raw_hash "+
					"(текст или индексация рассинхронизированы)", e.Type)
		}
		toRaw[e.Pseudonym] = raw
		prevEnd = e.End
	}
	return PseudonymMap{toRaw: toRaw}, nil
}

// Restore заменяет псевдонимы на исходные значения (однопроходно).
func (m PseudonymMap) Restore(text string) string {
	return m.replace(text, false)
}

// Remask заменяет исходные значения на псевдонимы (обратно Restore).
func (m PseudonymMap) Remask(text string) string {
	return m.replace(text, true)
}

// replace выполняет однопроходную замену. mask=false: псевдоним→raw;
// mask=true: raw→псевдоним. Более длинные образцы подставляются первыми —
// корректная обработка вложенных значений.
func (m PseudonymMap) replace(text string, mask bool) string {
	if len(m.toRaw) == 0 {
		return text
	}
	type pair struct{ from, to string }
	pairs := make([]pair, 0, len(m.toRaw))
	for pseudonym, raw := range m.toRaw {
		if mask {
			pairs = append(pairs, pair{from: raw, to: pseudonym})
		} else {
			pairs = append(pairs, pair{from: pseudonym, to: raw})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if len(pairs[i].from) != len(pairs[j].from) {
			return len(pairs[i].from) > len(pairs[j].from)
		}
		return pairs[i].from < pairs[j].from
	})
	args := make([]string, 0, len(pairs)*2)
	for _, p := range pairs {
		args = append(args, p.from, p.to)
	}
	return strings.NewReplacer(args...).Replace(text)
}
