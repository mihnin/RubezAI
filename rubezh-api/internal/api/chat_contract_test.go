package api

import (
	"encoding/json"
	"os"
	"testing"
)

// chatSchemaProps читает свойства $defs/<def> из контракта chat.schema.json.
func chatSchemaProps(t *testing.T, def string) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile("../../../docs/contracts/chat.schema.json")
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
	props := make(map[string]bool, len(d.Properties))
	for name := range d.Properties {
		props[name] = true
	}
	if len(props) == 0 {
		t.Fatalf("$defs/%s не содержит свойств", def)
	}
	return props
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
	assertChatSchema(t, "ChatRequest", chatRequestDTO{
		SessionID: nil, Message: "x", Provider: "p", Model: "m",
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

func TestSseDoneMatchesContract(t *testing.T) {
	assertChatSchema(t, "SseDone", sseDonePayload{})
}

func TestSseErrorMatchesContract(t *testing.T) {
	assertChatSchema(t, "SseError", sseErrorPayload{})
}
