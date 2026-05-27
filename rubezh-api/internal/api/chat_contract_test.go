package api

import (
	"encoding/json"
	"os"
	"testing"
)

// chatSchemaProps читает свойства $defs/<def> из контракта chat.schema.json.
func chatSchemaProps(t *testing.T, def string) map[string]bool {
	t.Helper()
	return schemaPropsFrom(t, "../../../docs/contracts/chat.schema.json", def)
}

// schemaPropsFrom читает свойства $defs/<def> из произвольного JSON-schema-файла.
// Используется для контрактных тестов на rag.schema.json (RagHitMeta и т. п.).
// Ревью архитектора Итерации 11 MINOR-3.
func schemaPropsFrom(t *testing.T, path, def string) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("чтение контракта %s: %v", path, err)
	}
	var schema struct {
		Defs map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("разбор контракта %s: %v", path, err)
	}
	d, ok := schema.Defs[def]
	if !ok {
		t.Fatalf("в контракте %s отсутствует $defs/%s", path, def)
	}
	props := make(map[string]bool, len(d.Properties))
	for name := range d.Properties {
		props[name] = true
	}
	if len(props) == 0 {
		t.Fatalf("%s $defs/%s не содержит свойств", path, def)
	}
	return props
}

// assertSchemaFrom сверяет JSON-ключи Go-значения со свойствами контракта
// из произвольного schema-файла. Аналог assertChatSchema, но универсальный.
func assertSchemaFrom(t *testing.T, path, def string, value any) {
	t.Helper()
	props := schemaPropsFrom(t, path, def)
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

// assertChatSchema сверяет JSON-ключи Go-значения со свойствами контракта.
func assertChatSchema(t *testing.T, def string, value any) {
	t.Helper()
	props := chatSchemaProps(t, def)
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

func TestChatRequestMatchesContract(t *testing.T) {
	// rag — pointer; чтобы попасть в JSON-ключи (omitempty), заполняем
	// пустой ссылкой. Свойства внутри rag проверяются отдельно.
	previewToken := ""
	assertChatSchema(t, "ChatRequest", chatRequestDTO{
		SessionID: nil, Message: "x", Provider: "p", Model: "m",
		SystemPrompt: "system",
		PreviewToken: &previewToken,
		RAG:          &chatRAGParamsDTO{Enabled: false},
		Review: &chatReviewParamsDTO{
			Enabled:   false,
			MaxRounds: 3,
			SystemPrompts: map[string]string{
				"review-a": "prompt",
			},
		},
	})
}

func TestSseRagHitsMatchesContract(t *testing.T) {
	// SseRagHits — payload события rag_hits (Итерация 11 §Р4 Ф4c).
	assertChatSchema(t, "SseRagHits", sseRagHitsPayload{
		RequestID: "r", Hits: []sseRagHitPayload{},
	})
}

// TestRagHitMetaMatchesContract — inner schema каждого элемента hits[]
// внутри SseRagHits. Контракт — rag.schema.json#RagHitMeta. Ревью
// архитектора Итерации 11 MINOR-3: outer schema было покрыто, inner — нет.
func TestRagHitMetaMatchesContract(t *testing.T) {
	assertSchemaFrom(t, "../../../docs/contracts/rag.schema.json",
		"RagHitMeta", sseRagHitPayload{
			DocumentID: "d", Filename: "f", ChunkIndex: 0, Relevance: 0.0,
		})
}

func TestChatSessionRequestMatchesContract(t *testing.T) {
	assertChatSchema(t, "ChatSessionRequest", chatSessionRequestDTO{Title: nil})
}

func TestChatSessionMatchesContract(t *testing.T) {
	assertChatSchema(t, "ChatSession", chatSessionDTO{})
}

func TestSseRiskMatchesContract(t *testing.T) {
	assertChatSchema(t, "SseRisk", sseRiskPayload{Classes: []string{}})
}

func TestSseMetaMatchesContract(t *testing.T) {
	assertChatSchema(t, "SseMeta", sseMetaPayload{Reasons: []string{}})
}

func TestSseDeltaMatchesContract(t *testing.T) {
	assertChatSchema(t, "SseDelta", sseDeltaPayload{})
}

func TestSseStatusMatchesContract(t *testing.T) {
	assertChatSchema(t, "SseStatus", sseStatusPayload{})
}

func TestSseDoneMatchesContract(t *testing.T) {
	assertChatSchema(t, "SseDone", sseDonePayload{})
}

func TestSseErrorMatchesContract(t *testing.T) {
	assertChatSchema(t, "SseError", sseErrorPayload{})
}
