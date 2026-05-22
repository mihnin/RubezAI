package api

import (
	"testing"

	"github.com/rubezh-ai/rubezh-api/internal/chat"
	"github.com/rubezh-ai/rubezh-api/internal/crypto"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

func testCipher(t *testing.T) *crypto.Cipher {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 7)
	}
	c, err := crypto.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	return c
}

func encMapping(
	t *testing.T, c *crypto.Cipher, sessionID, pseudonym, entityType, raw string,
) storage.PseudonymMappingRow {
	t.Helper()
	ct, err := c.Encrypt([]byte(raw), chat.MappingAAD(sessionID, pseudonym))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	return storage.PseudonymMappingRow{
		Pseudonym: pseudonym, EntityType: entityType, RawValueEncrypted: ct,
	}
}

func TestRevealPseudonymsSubstitutes(t *testing.T) {
	c := testCipher(t)
	sid := "11111111-1111-1111-1111-111111111111"
	mappings := []storage.PseudonymMappingRow{
		encMapping(t, c, sid, "ФИО_001", "PERSON", "Соколова Екатерина"),
		encMapping(t, c, sid, "КАРТА_001", "BANK_CARD", "2202 2012 3344 5566"),
	}
	text := "Договор с ФИО_001, карта КАРТА_001 принята."
	got, n := revealPseudonyms(text, sid, mappings, c, nil)
	want := "Договор с Соколова Екатерина, карта 2202 2012 3344 5566 принята."
	if got != want {
		t.Errorf("revealed = %q, ожидалось %q", got, want)
	}
	if n != 2 {
		t.Errorf("раскрыто %d, ожидалось 2", n)
	}
}

func TestRevealPseudonymsWrongSessionAADFailsClosed(t *testing.T) {
	c := testCipher(t)
	// зашифровано под одной сессией, расшифровываем под другой → AAD mismatch
	m := encMapping(t, c, "session-A", "ФИО_001", "PERSON", "секрет")
	got, n := revealPseudonyms("Привет ФИО_001", "session-B", []storage.PseudonymMappingRow{m}, c, nil)
	if n != 0 {
		t.Errorf("при несовпадении AAD не должно быть раскрытий, получено %d", n)
	}
	if got != "Привет ФИО_001" {
		t.Errorf("текст не должен меняться при провале расшифровки: %q", got)
	}
}

func TestRevealPseudonymsCorruptCiphertextSkipped(t *testing.T) {
	c := testCipher(t)
	m := storage.PseudonymMappingRow{
		Pseudonym: "ФИО_001", EntityType: "PERSON",
		RawValueEncrypted: []byte("не валидный шифротекст"),
	}
	got, n := revealPseudonyms("ФИО_001 тут", "s", []storage.PseudonymMappingRow{m}, c, nil)
	if n != 0 || got != "ФИО_001 тут" {
		t.Errorf("битый шифротекст должен пропускаться (fail-closed): got=%q n=%d", got, n)
	}
}

func TestRevealPseudonymsNoMappings(t *testing.T) {
	got, n := revealPseudonyms("чистый текст", "s", nil, testCipher(t), nil)
	if n != 0 || got != "чистый текст" {
		t.Errorf("без mapping'ов текст неизменен: got=%q n=%d", got, n)
	}
}
