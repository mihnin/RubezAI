package chat

import (
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// DetectLeak возвращает отсортированный список псевдонимов, чьи исходные
// значения присутствуют в сыром ответе LLM. LLM получал только псевдонимы —
// присутствие raw-значения в его ответе аномально. Сравнение
// регистронезависимое, с Unicode-нормализацией NFC.
func (m PseudonymMap) DetectLeak(rawLLMOutput string) []string {
	if len(m.toRaw) == 0 {
		return nil
	}
	haystack := normalizeForLeak(rawLLMOutput)
	var leaked []string
	for pseudonym, raw := range m.toRaw {
		if raw == "" {
			continue
		}
		if strings.Contains(haystack, normalizeForLeak(raw)) {
			leaked = append(leaked, pseudonym)
		}
	}
	sort.Strings(leaked)
	return leaked
}

// normalizeForLeak приводит строку к виду для устойчивого сравнения утечки.
func normalizeForLeak(s string) string {
	return strings.ToLower(norm.NFC.String(s))
}
