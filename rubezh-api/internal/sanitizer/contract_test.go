package sanitizer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

// schemaProps читает набор свойств $defs/<def> из контракта sanitize.schema.json.
func schemaProps(t *testing.T, def string) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile("../../../docs/contracts/sanitize.schema.json")
	if err != nil {
		t.Fatalf("чтение контракта: %v", err)
	}
	var schema struct {
		Defs map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("разбор контракта: %v", err)
	}
	d, ok := schema.Defs[def]
	if !ok {
		t.Fatalf("в контракте отсутствует $defs/%s", def)
	}
	props := map[string]bool{}
	for name := range d.Properties {
		props[name] = true
	}
	if len(props) == 0 {
		t.Fatalf("$defs/%s не содержит свойств", def)
	}
	return props
}

// assertKeysMatch сверяет JSON-ключи Go-значения с набором свойств контракта:
// расхождение в любую сторону — ошибка (поле вне контракта либо пропущенное).
func assertKeysMatch(t *testing.T, def string, value any) {
	t.Helper()
	props := schemaProps(t, def)
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("сериализация %s: %v", def, err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &keys); err != nil {
		t.Fatalf("разбор %s: %v", def, err)
	}
	for key := range keys {
		if !props[key] {
			t.Errorf("%s: поле %q отсутствует в контракте", def, key)
		}
	}
	for prop := range props {
		if _, ok := keys[prop]; !ok {
			t.Errorf("%s: свойство контракта %q отсутствует в Go-типе", def, prop)
		}
	}
}

func TestPreviewRequestMatchesContract(t *testing.T) {
	assertKeysMatch(t, "SanitizeRequest", PreviewRequest{
		Text: "x", DocumentID: nil, Context: "chat",
	})
}

func TestEntityMatchesContract(t *testing.T) {
	assertKeysMatch(t, "Entity", Entity{
		Type: "PHONE", Category: "pii", Start: 0, End: 1,
		Pseudonym: "p", RawHash: "h", Confidence: 0.5, Detector: "regex",
	})
}

func TestRiskMatchesContract(t *testing.T) {
	assertKeysMatch(t, "Risk", Risk{Score: 0.5, Level: "low", Classes: []string{}})
}

func TestPreviewResponseMatchesContract(t *testing.T) {
	assertKeysMatch(t, "SanitizeResponse", PreviewResponse{
		SanitizedText: "x", Entities: []Entity{}, Risk: Risk{}, MappingID: nil,
	})
}

// TestPreviewAgainstLiveSanitizer — интеграционный тест против работающего
// rubezh-sanitizer (пропускается, если TEST_SANITIZER_URL не задан).
func TestPreviewAgainstLiveSanitizer(t *testing.T) {
	base := os.Getenv("TEST_SANITIZER_URL")
	if base == "" {
		t.Skip("TEST_SANITIZER_URL не задан — интеграционный тест пропущен")
	}
	resp, err := NewClient(base).Preview(context.Background(), PreviewRequest{
		Text: "Просто текст без чувствительных данных.", Context: "chat",
	})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if resp.SanitizedText == "" {
		t.Error("sanitized_text пуст")
	}
}

// TestSpanProvenanceWithLiveSanitizer проверяет инвариант происхождения спанов
// (MAJOR-NEW-2 плана): start/end индексируют byte-identical текст запроса по
// код-поинтам. Самопроверка через raw_hash: sha256([]rune(text)[start:end])
// обязан совпасть с raw_hash, который вернул sanitizer.
func TestSpanProvenanceWithLiveSanitizer(t *testing.T) {
	base := os.Getenv("TEST_SANITIZER_URL")
	if base == "" {
		t.Skip("TEST_SANITIZER_URL не задан — интеграционный тест пропущен")
	}
	text := "Договор подписал Иванов Иван Иванович, " +
		"телефон +7 900 123-45-67, ИНН 7707083893."
	resp, err := NewClient(base).Preview(context.Background(), PreviewRequest{
		Text: text, Context: "chat",
	})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if len(resp.Entities) == 0 {
		t.Fatal("sanitizer не нашёл ни одной сущности — тест бессмыслен")
	}
	runes := []rune(text)
	for _, e := range resp.Entities {
		if e.Start < 0 || e.End <= e.Start || e.End > len(runes) {
			t.Errorf("сущность %s: спан [%d,%d) вне границ [0,%d)",
				e.Type, e.Start, e.End, len(runes))
			continue
		}
		raw := string(runes[e.Start:e.End])
		sum := sha256.Sum256([]byte(raw))
		if got := hex.EncodeToString(sum[:]); got != e.RawHash {
			t.Errorf("сущность %s: sha256([]rune(text)[%d:%d]) = %s, "+
				"raw_hash контракта = %s — спаны индексируют не тот текст",
				e.Type, e.Start, e.End, got, e.RawHash)
		}
	}
}
