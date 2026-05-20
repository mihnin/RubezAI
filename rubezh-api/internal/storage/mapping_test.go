package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
)

// withTestStorage открывает Storage по TEST_DATABASE_URL или skip.
func withTestStorage(t *testing.T) *Storage {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL не задан — integration-тест пропущен")
	}
	s, err := New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	if err := s.Ping(context.Background()); err != nil {
		s.Close()
		t.Skipf("БД недоступна: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

// seedSessionAndResult создаёт сессию, user-сообщение и
// sanitization_results строку — возвращает sanitization_result_id.
func seedSessionAndResult(t *testing.T, s *Storage) (sessionID, srID string) {
	t.Helper()
	ctx := context.Background()
	userID, err := s.UserIDForRole(ctx, "user")
	if err != nil {
		t.Fatalf("UserIDForRole: %v", err)
	}
	session, err := s.CreateChatSession(ctx, userID, nil)
	if err != nil {
		t.Fatalf("CreateChatSession: %v", err)
	}
	rec := ChatRequestRecord{
		SessionID:   session.ID,
		UserContent: "Иванов И.И.",
		Sanitization: SanitizationData{
			RiskLevel: "medium", RiskScore: 0.5,
			RiskClasses: []string{"pii"},
			Entities:    json.RawMessage(`[{"type":"PERSON","pseudonym":"ФИО_001"}]`),
		},
		Audit: AuditEvent{
			UserID: userID, EventType: "chat_request",
			Detail: map[string]any{"request_id": "test-r1"},
		},
	}
	ids, err := s.RecordChatRequest(ctx, rec)
	if err != nil {
		t.Fatalf("RecordChatRequest: %v", err)
	}
	return session.ID, ids.SanitizationResultID
}

func TestInsertPseudonymMappingsRoundTrip(t *testing.T) {
	s := withTestStorage(t)
	ctx := context.Background()
	_, srID := seedSessionAndResult(t, s)

	mappings := []PseudonymMappingInput{
		{Pseudonym: "ФИО_001", EntityType: "PERSON",
			RawHash: "h1", RawValueEncrypted: []byte{1, 2, 3, 4}},
		{Pseudonym: "ТЕЛ_002", EntityType: "PHONE",
			RawHash: "h2", RawValueEncrypted: []byte{5, 6, 7, 8, 9}},
	}

	// Используем явную транзакцию (как в Tx1 оркестратора).
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	ids, err := InsertPseudonymMappings(ctx, tx, srID, mappings)
	if err != nil {
		t.Fatalf("InsertPseudonymMappings: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("вставлено %d, ожидалось 2", len(ids))
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	rows, err := s.ListPseudonymMappings(ctx, srID)
	if err != nil {
		t.Fatalf("ListPseudonymMappings: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("прочитано %d, ожидалось 2", len(rows))
	}
	// Проверяем, что данные пишутся и читаются без искажений.
	gotByPseudonym := map[string]PseudonymMappingRow{}
	for _, r := range rows {
		gotByPseudonym[r.Pseudonym] = r
	}
	for _, m := range mappings {
		got, ok := gotByPseudonym[m.Pseudonym]
		if !ok {
			t.Errorf("не найден mapping %q", m.Pseudonym)
			continue
		}
		if got.EntityType != m.EntityType ||
			got.RawHash != m.RawHash ||
			!bytes.Equal(got.RawValueEncrypted, m.RawValueEncrypted) {
			t.Errorf("искажение %q: got=%+v, want=%+v",
				m.Pseudonym, got, m)
		}
	}
}

func TestInsertPseudonymMappingsEmptyIsNoop(t *testing.T) {
	s := withTestStorage(t)
	ctx := context.Background()
	_, srID := seedSessionAndResult(t, s)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	ids, err := InsertPseudonymMappings(ctx, tx, srID, nil)
	if err != nil {
		t.Errorf("nil-mappings: ошибка %v", err)
	}
	if ids != nil {
		t.Errorf("ожидался nil, получено %v", ids)
	}
	ids2, err := InsertPseudonymMappings(ctx, tx, srID, []PseudonymMappingInput{})
	if err != nil {
		t.Errorf("пустой слайс: ошибка %v", err)
	}
	if ids2 != nil {
		t.Errorf("ожидался nil, получено %v", ids2)
	}
}

func TestInsertPseudonymMappingsRejectsEmptyCiphertext(t *testing.T) {
	s := withTestStorage(t)
	ctx := context.Background()
	_, srID := seedSessionAndResult(t, s)

	tx, _ := s.pool.Begin(ctx)
	defer func() { _ = tx.Rollback(ctx) }()

	_, err := InsertPseudonymMappings(ctx, tx, srID, []PseudonymMappingInput{
		{Pseudonym: "X", EntityType: "PERSON", RawHash: "h",
			RawValueEncrypted: nil},
	})
	if err == nil {
		t.Error("ожидалась ошибка на пустой ciphertext (NOT NULL bytea)")
	}
}

func TestInsertPseudonymMappingsRejectsEmptyRequiredFields(t *testing.T) {
	s := withTestStorage(t)
	ctx := context.Background()
	_, srID := seedSessionAndResult(t, s)

	tx, _ := s.pool.Begin(ctx)
	defer func() { _ = tx.Rollback(ctx) }()

	cases := []struct {
		name string
		m    PseudonymMappingInput
	}{
		{"пустой Pseudonym", PseudonymMappingInput{
			EntityType: "P", RawHash: "h", RawValueEncrypted: []byte{1}}},
		{"пустой EntityType", PseudonymMappingInput{
			Pseudonym: "Π", RawHash: "h", RawValueEncrypted: []byte{1}}},
		{"пустой RawHash", PseudonymMappingInput{
			Pseudonym: "Π", EntityType: "P", RawValueEncrypted: []byte{1}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := InsertPseudonymMappings(ctx, tx, srID,
				[]PseudonymMappingInput{tc.m})
			if err == nil {
				t.Errorf("ожидалась ошибка")
			}
		})
	}
}

func TestInsertPseudonymMappingsRollbacksWithTx(t *testing.T) {
	s := withTestStorage(t)
	ctx := context.Background()
	_, srID := seedSessionAndResult(t, s)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	_, err = InsertPseudonymMappings(ctx, tx, srID,
		[]PseudonymMappingInput{{Pseudonym: "X", EntityType: "P",
			RawHash: "h", RawValueEncrypted: []byte{1, 2, 3}}})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	// Откатываем — записи не должны остаться.
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	rows, err := s.ListPseudonymMappings(ctx, srID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("после rollback должно быть 0 записей, получено %d", len(rows))
	}
}

// TestPseudonymMappingInputLogValueRedacts — инвариант «никакого raw в логах»
// проверяется на уровне slog.LogValuer: log не содержит pseudonym/raw_hash
// и ciphertext, только агрегированную мета-инфу.
func TestPseudonymMappingInputLogValueRedacts(t *testing.T) {
	m := PseudonymMappingInput{
		Pseudonym:         "ФИО_001",
		EntityType:        "PERSON",
		RawHash:           "secret-hash-12345",
		RawValueEncrypted: []byte{0xDE, 0xAD, 0xBE, 0xEF},
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	logger.Info("test", "mapping", m)

	out := buf.String()
	for _, secret := range []string{"ФИО_001", "secret-hash-12345"} {
		if strings.Contains(out, secret) {
			t.Errorf("в логе обнаружено секретное значение %q: %s", secret, out)
		}
	}
	if !strings.Contains(out, `"entity_type":"PERSON"`) {
		t.Errorf("должен быть entity_type=PERSON: %s", out)
	}
	if !strings.Contains(out, `"ciphertext_bytes":4`) {
		t.Errorf("должна быть длина ciphertext: %s", out)
	}
}

func TestPseudonymMappingRowLogValueRedacts(t *testing.T) {
	r := PseudonymMappingRow{
		ID: "row-id", Pseudonym: "Π_42",
		EntityType: "EMAIL", RawHash: "h", RawValueEncrypted: []byte{1},
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	logger.Info("test", "row", r)
	out := buf.String()
	if strings.Contains(out, "Π_42") {
		t.Errorf("в логе виден pseudonym: %s", out)
	}
	if !strings.Contains(out, `"id":"row-id"`) {
		t.Errorf("должен быть id: %s", out)
	}
}
