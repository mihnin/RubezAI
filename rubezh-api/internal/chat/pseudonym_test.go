package chat

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/rubezh-ai/rubezh-api/internal/sanitizer"
)

// entity строит sanitizer.Entity с корректным raw_hash для среза текста.
func entity(text string, start, end int, etype, pseudonym string) sanitizer.Entity {
	raw := string([]rune(text)[start:end])
	sum := sha256.Sum256([]byte(raw))
	return sanitizer.Entity{
		Type: etype, Category: "pii", Start: start, End: end,
		Pseudonym: pseudonym, RawHash: hex.EncodeToString(sum[:]),
		Confidence: 0.9, Detector: "regex",
	}
}

func TestBuildPseudonymMap(t *testing.T) {
	text := "Звонил Иванову вчера"
	m, err := BuildPseudonymMap(text, []sanitizer.Entity{
		entity(text, 7, 14, "PERSON", "ФИО_001"),
	})
	if err != nil {
		t.Fatalf("BuildPseudonymMap: %v", err)
	}
	if m.Len() != 1 {
		t.Fatalf("Len = %d, ожидалось 1", m.Len())
	}
	if got := m.Restore("привет ФИО_001"); got != "привет Иванову" {
		t.Errorf("Restore = %q", got)
	}
}

func TestBuildPseudonymMapEmpty(t *testing.T) {
	m, err := BuildPseudonymMap("текст без сущностей", nil)
	if err != nil {
		t.Fatalf("BuildPseudonymMap: %v", err)
	}
	if m.Len() != 0 {
		t.Errorf("Len = %d, ожидалось 0", m.Len())
	}
}

func TestBuildPseudonymMapRejectsOutOfBoundsSpan(t *testing.T) {
	text := "короткий"
	for _, e := range []sanitizer.Entity{
		{Type: "X", Start: 0, End: 100, Pseudonym: "P", RawHash: "h"},
		{Type: "X", Start: 5, End: 3, Pseudonym: "P", RawHash: "h"},
		{Type: "X", Start: -1, End: 2, Pseudonym: "P", RawHash: "h"},
	} {
		if _, err := BuildPseudonymMap(text, []sanitizer.Entity{e}); err == nil {
			t.Errorf("спан [%d,%d): ожидалась ошибка", e.Start, e.End)
		}
	}
}

func TestBuildPseudonymMapRejectsHashMismatch(t *testing.T) {
	// спан в границах, но raw_hash не соответствует срезу — fail-closed
	text := "Звонил Иванову"
	bad := sanitizer.Entity{
		Type: "PERSON", Start: 7, End: 14,
		Pseudonym: "ФИО_001", RawHash: "0000000000000000",
	}
	if _, err := BuildPseudonymMap(text, []sanitizer.Entity{bad}); err == nil {
		t.Error("несовпадение raw_hash должно давать ошибку (fail-closed)")
	}
}

func TestPseudonymRestoreNoMap(t *testing.T) {
	m, _ := BuildPseudonymMap("чисто", nil)
	if got := m.Restore("ничего не меняем"); got != "ничего не меняем" {
		t.Errorf("Restore пустой картой изменил текст: %q", got)
	}
}

func TestPseudonymRemask(t *testing.T) {
	text := "Договор с Ивановым"
	m, err := BuildPseudonymMap(text, []sanitizer.Entity{
		entity(text, 10, 18, "PERSON", "ФИО_001"),
	})
	if err != nil {
		t.Fatalf("BuildPseudonymMap: %v", err)
	}
	if got := m.Remask("ответ про Ивановым"); got != "ответ про ФИО_001" {
		t.Errorf("Remask = %q", got)
	}
}

func TestDetectLeak(t *testing.T) {
	text := "Телефон +7 900 123-45-67 здесь"
	m, err := BuildPseudonymMap(text, []sanitizer.Entity{
		entity(text, 8, 24, "PHONE", "ТЕЛЕФОН_001"),
	})
	if err != nil {
		t.Fatalf("BuildPseudonymMap: %v", err)
	}
	// в ответе LLM только псевдоним — утечки нет
	if leaked := m.DetectLeak("Перезвоните на ТЕЛЕФОН_001"); len(leaked) != 0 {
		t.Errorf("ложная утечка: %v", leaked)
	}
	// в ответе LLM присутствует сырое значение — утечка
	leaked := m.DetectLeak("Номер +7 900 123-45-67 в ответе")
	if len(leaked) != 1 || leaked[0] != "ТЕЛЕФОН_001" {
		t.Errorf("утечка не обнаружена: %v", leaked)
	}
}

func TestDetectLeakCaseInsensitive(t *testing.T) {
	text := "Клиент Иванов"
	m, err := BuildPseudonymMap(text, []sanitizer.Entity{
		entity(text, 7, 13, "PERSON", "ФИО_001"),
	})
	if err != nil {
		t.Fatalf("BuildPseudonymMap: %v", err)
	}
	if leaked := m.DetectLeak("упомянут иванов в тексте"); len(leaked) != 1 {
		t.Errorf("регистронезависимый поиск не сработал: %v", leaked)
	}
}

func TestToPolicyInput(t *testing.T) {
	resp := sanitizer.PreviewResponse{
		SanitizedText: "ФИО_001 звонил",
		Entities: []sanitizer.Entity{
			{Type: "PERSON", Category: "pii"},
			{Type: "PHONE", Category: "pii"},
			{Type: "PERSON", Category: "pii"},
		},
		Risk: sanitizer.Risk{Score: 0.6, Level: "high", Classes: []string{"pii"}},
	}
	in := ToPolicyInput(resp, "external", "user")
	if in.ModelTrust != "external" || in.UserRole != "user" || in.Context != "chat" {
		t.Errorf("базовые поля некорректны: %+v", in)
	}
	if in.Risk.Level != "high" || in.Risk.Score != 0.6 {
		t.Errorf("risk некорректен: %+v", in.Risk)
	}
	if len(in.Risk.Classes) != 1 || in.Risk.Classes[0] != "pii" {
		t.Errorf("risk.classes некорректны: %v", in.Risk.Classes)
	}
	// типы сущностей дедуплицируются
	if len(in.EntityTypes) != 2 {
		t.Errorf("EntityTypes = %v, ожидалось 2 уникальных", in.EntityTypes)
	}
}
