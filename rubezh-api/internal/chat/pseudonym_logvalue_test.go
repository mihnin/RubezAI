package chat

import (
	"bytes"
	"encoding/hex"
	"log/slog"
	"strings"
	"testing"

	"github.com/rubezh-ai/rubezh-api/internal/sanitizer"
)

// TestPseudonymMapLogValueRedacts — критический инвариант:
// при логировании PseudonymMap никакие raw-значения и pseudonym'ы
// не должны попасть в вывод (план iteration-9.md §Р7).
func TestPseudonymMapLogValueRedacts(t *testing.T) {
	msg := "Иванов Иванович работает"
	entities := []sanitizer.Entity{
		entity(msg, 0, 16, "PERSON", "ФИО_001"),
	}
	pmap, err := BuildPseudonymMap(msg, entities)
	if err != nil {
		t.Fatalf("BuildPseudonymMap: %v", err)
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	logger.Info("processing", "pmap", pmap)

	out := buf.String()
	// Ни raw-значение, ни pseudonym не должны утечь.
	for _, secret := range []string{
		"Иванов Иванович", // raw
		"ФИО_001",         // pseudonym (тоже redacted по политике)
	} {
		if strings.Contains(out, secret) {
			t.Errorf("в логе обнаружено секретное значение %q: %s",
				secret, out)
		}
	}
	// Агрегат должен быть.
	if !strings.Contains(out, `"entries":1`) {
		t.Errorf("должно быть entries=1: %s", out)
	}
	if !strings.Contains(out, `"redacted":"raw pseudonym values redacted`) {
		t.Errorf("должна быть метка redacted: %s", out)
	}
}

// TestPseudonymMapRawAccessor — Raw(pseudonym) возвращает raw,
// используется оркестратором для шифрования.
func TestPseudonymMapRawAccessor(t *testing.T) {
	msg := "контакт ivan@example.com сегодня"
	pmap, err := BuildPseudonymMap(msg, []sanitizer.Entity{
		entity(msg, 8, 24, "EMAIL", "EMAIL_001"),
	})
	if err != nil {
		t.Fatalf("BuildPseudonymMap: %v", err)
	}
	raw, ok := pmap.Raw("EMAIL_001")
	if !ok {
		t.Fatal("Raw(EMAIL_001) вернул not-found")
	}
	if raw != "ivan@example.com" {
		t.Errorf("raw = %q, ожидалось ivan@example.com", raw)
	}
	if _, ok := pmap.Raw("НЕТ_001"); ok {
		t.Error("Raw для несуществующего псевдонима вернул ok=true")
	}
}

// TestMappingAADIsDeterministicAndUnique — AAD-формула:
// одинаковые входы → одинаковый AAD; разные псевдонимы внутри сессии
// → разные AAD; одинаковый псевдоним в разных сессиях → разные AAD.
func TestMappingAADIsDeterministicAndUnique(t *testing.T) {
	aad1 := MappingAAD("session-1", "ФИО_001")
	aad2 := MappingAAD("session-1", "ФИО_001")
	if !bytes.Equal(aad1, aad2) {
		t.Errorf("AAD не детерминирован: %s vs %s",
			hex.EncodeToString(aad1), hex.EncodeToString(aad2))
	}
	aadOther := MappingAAD("session-1", "ФИО_002")
	if bytes.Equal(aad1, aadOther) {
		t.Error("AAD одинаков для разных pseudonym в одной сессии (swap-уязвимость)")
	}
	aadOtherSession := MappingAAD("session-2", "ФИО_001")
	if bytes.Equal(aad1, aadOtherSession) {
		t.Error("AAD одинаков для разных сессий (swap-уязвимость)")
	}
	if len(aad1) != 16 {
		t.Errorf("длина AAD = %d, ожидалось 16", len(aad1))
	}
}
