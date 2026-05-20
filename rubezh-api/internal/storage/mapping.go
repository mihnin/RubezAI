package storage

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
)

// PseudonymMappingInput — входные данные для записи зашифрованного
// mapping'а псевдонима. RawValueEncrypted — готовый ciphertext в формате
// nonce(12) || ct || GCM-tag(16) из internal/crypto. Шифрование
// выполняется в оркестраторе вне транзакции (план iteration-9.md §Р2);
// storage только сохраняет байты.
//
// Инвариант «никакого raw в логах»: этот тип не содержит raw-значения.
// Для дополнительной безопасности LogValue возвращает агрегированную
// метаинформацию вместо полей.
type PseudonymMappingInput struct {
	Pseudonym         string
	EntityType        string
	RawHash           string
	RawValueEncrypted []byte
}

// LogValue — slog.LogValuer. Возвращает безопасное представление
// (без содержимого) на случай, если структура попадёт в логи.
// Pseudonym и raw_hash сами по себе публичны, но политика проекта —
// «маппинги не логируются», поэтому единое redacted-представление.
func (p PseudonymMappingInput) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("entity_type", p.EntityType),
		slog.Int("ciphertext_bytes", len(p.RawValueEncrypted)),
		slog.String("redacted", "pseudonym/raw_hash redacted"),
	)
}

// InsertPseudonymMappings вставляет N записей в pseudonym_mappings в
// рамках переданной транзакции. Используется из RecordChatRequest
// (расширение Tx1) после INSERT'а sanitization_results (нужен FK).
//
// Пустой слайс — no-op (валидно: запросы без обнаруженных сущностей
// не создают mapping'ов). Возвращает []id новых записей в том же
// порядке, что и входной слайс.
func InsertPseudonymMappings(
	ctx context.Context, tx pgx.Tx, sanitizationResultID string,
	mappings []PseudonymMappingInput,
) ([]string, error) {
	if len(mappings) == 0 {
		return nil, nil
	}
	// Пакетная вставка через unnest — один SQL для N сущностей.
	pseudonyms := make([]string, len(mappings))
	entityTypes := make([]string, len(mappings))
	hashes := make([]string, len(mappings))
	cipherbytes := make([][]byte, len(mappings))
	for i, m := range mappings {
		if m.Pseudonym == "" || m.EntityType == "" || m.RawHash == "" {
			return nil, fmt.Errorf(
				"storage: пустой обязательный атрибут mapping #%d", i)
		}
		if len(m.RawValueEncrypted) == 0 {
			return nil, fmt.Errorf(
				"storage: пустой ciphertext mapping #%d (Pseudonym=%q)",
				i, m.Pseudonym)
		}
		pseudonyms[i] = m.Pseudonym
		entityTypes[i] = m.EntityType
		hashes[i] = m.RawHash
		cipherbytes[i] = m.RawValueEncrypted
	}

	rows, err := tx.Query(ctx,
		`INSERT INTO pseudonym_mappings
		   (sanitization_result_id, pseudonym, entity_type, raw_hash,
		    raw_value_encrypted)
		 SELECT $1, unnest($2::text[]), unnest($3::text[]),
		        unnest($4::text[]), unnest($5::bytea[])
		 RETURNING id`,
		sanitizationResultID, pseudonyms, entityTypes, hashes, cipherbytes,
	)
	if err != nil {
		return nil, fmt.Errorf("storage: вставка pseudonym_mappings: %w", err)
	}
	defer rows.Close()

	ids := make([]string, 0, len(mappings))
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("storage: чтение id mapping'а: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: обход pseudonym_mappings: %w", err)
	}
	if len(ids) != len(mappings) {
		return nil, fmt.Errorf(
			"storage: вставлено %d из %d mapping'ов", len(ids), len(mappings))
	}
	return ids, nil
}

// ListPseudonymMappings возвращает mapping'и для sanitization_result_id.
// Используется для forensics-сценариев (Итерация 9+; reveal-flow —
// пост-MVP). raw_value_encrypted возвращается как есть; расшифровка —
// на стороне оркестратора с проверкой AAD.
func (s *Storage) ListPseudonymMappings(
	ctx context.Context, sanitizationResultID string,
) ([]PseudonymMappingRow, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, pseudonym, entity_type, raw_hash, raw_value_encrypted
		 FROM pseudonym_mappings
		 WHERE sanitization_result_id = $1
		 ORDER BY id`,
		sanitizationResultID)
	if err != nil {
		return nil, fmt.Errorf("storage: чтение pseudonym_mappings: %w", err)
	}
	defer rows.Close()

	var out []PseudonymMappingRow
	for rows.Next() {
		var r PseudonymMappingRow
		if err := rows.Scan(&r.ID, &r.Pseudonym, &r.EntityType,
			&r.RawHash, &r.RawValueEncrypted); err != nil {
			return nil, fmt.Errorf("storage: скан pseudonym_mapping: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PseudonymMappingRow — прочитанная запись pseudonym_mappings.
type PseudonymMappingRow struct {
	ID                string
	Pseudonym         string
	EntityType        string
	RawHash           string
	RawValueEncrypted []byte
}

// LogValue — см. PseudonymMappingInput.LogValue.
func (r PseudonymMappingRow) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("id", r.ID),
		slog.String("entity_type", r.EntityType),
		slog.Int("ciphertext_bytes", len(r.RawValueEncrypted)),
		slog.String("redacted", "pseudonym/raw_hash redacted"),
	)
}
