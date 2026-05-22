package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Ошибки.
var (
	ErrDocumentNotFound  = errors.New("storage: документ не найден")
	ErrDocumentForbidden = errors.New("storage: нет доступа к документу")
)

// Document — запись documents (см. миграции 000004 + 000011).
type Document struct {
	ID                  string
	OwnerID             string
	Filename            string
	ContentType         *string
	SizeBytes           *int64
	StorageKey          string
	Status              string  // pending|processing|done|failed|deleted
	Phase               *string // parsing|chunking|sanitizing|embedding|NULL
	Error               *string
	ACL                 json.RawMessage // jsonb: [{"role":...}, {"user_id":...}]
	ProcessingStartedAt *time.Time
	ProcessingAttempts  int
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// DocumentInput — данные для CreateDocument.
type DocumentInput struct {
	OwnerID     string
	Filename    string
	ContentType *string
	SizeBytes   *int64
	StorageKey  string
	ACL         json.RawMessage // nil → '[]'
}

// DocumentChunkRow — публичный DTO для GET /api/documents/:id/chunks.
// Включает sanitization_summary опционально (если есть JOIN-row).
type DocumentChunkRow struct {
	ID          string
	DocumentID  string
	ChunkIndex  int
	Content     string // sanitized (план iteration-10 §Р4)
	TokenCount  *int
	CreatedAt   time.Time
	RiskLevel   *string
	RiskScore   *float64
	RiskClasses []string
}

const documentColumns = `id, owner_id, filename, content_type, size_bytes,
	storage_key, status, phase, error, acl, processing_started_at,
	processing_attempts, created_at, updated_at`

// CreateDocument создаёт запись со status='pending' для очереди worker'а.
func (s *Storage) CreateDocument(
	ctx context.Context, in DocumentInput,
) (Document, error) {
	acl := in.ACL
	if len(acl) == 0 {
		acl = json.RawMessage("[]")
	}
	var d Document
	err := s.pool.QueryRow(ctx,
		`INSERT INTO documents
		   (owner_id, filename, content_type, size_bytes, storage_key, acl)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING `+documentColumns,
		in.OwnerID, in.Filename, in.ContentType, in.SizeBytes,
		in.StorageKey, acl,
	).Scan(&d.ID, &d.OwnerID, &d.Filename, &d.ContentType, &d.SizeBytes,
		&d.StorageKey, &d.Status, &d.Phase, &d.Error, &d.ACL,
		&d.ProcessingStartedAt, &d.ProcessingAttempts,
		&d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return Document{}, fmt.Errorf("storage: создание документа: %w", err)
	}
	return d, nil
}

// GetDocument читает документ; ErrDocumentNotFound если нет
// или если status='deleted' (информационная защита).
func (s *Storage) GetDocument(
	ctx context.Context, id string,
) (Document, error) {
	var d Document
	err := s.pool.QueryRow(ctx,
		`SELECT `+documentColumns+` FROM documents
		 WHERE id = $1 AND status != 'deleted'`, id,
	).Scan(&d.ID, &d.OwnerID, &d.Filename, &d.ContentType, &d.SizeBytes,
		&d.StorageKey, &d.Status, &d.Phase, &d.Error, &d.ACL,
		&d.ProcessingStartedAt, &d.ProcessingAttempts,
		&d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, ErrDocumentNotFound
	}
	if err != nil {
		return Document{}, fmt.Errorf("storage: get document: %w", err)
	}
	return d, nil
}

// CheckDocumentAccess проверяет ACL для роли userID+role.
// Возвращает nil если доступ есть; ErrDocumentForbidden иначе.
//
// Правила (iteration-10.md §Р5):
// - owner → всегда.
// - admin/security_officer/compliance_officer/auditor → всегда.
// - иные → только если acl содержит {"role":<role>} или {"user_id":<id>}.
func CheckDocumentAccess(doc Document, userID, role string) error {
	if doc.OwnerID == userID {
		return nil
	}
	switch role {
	case "admin", "security_officer", "compliance_officer", "auditor":
		return nil
	}
	// Парсим ACL и ищем разрешение.
	var acl []map[string]any
	if err := json.Unmarshal(doc.ACL, &acl); err != nil {
		return ErrDocumentForbidden
	}
	for _, entry := range acl {
		if r, ok := entry["role"].(string); ok && r == role {
			return nil
		}
		if u, ok := entry["user_id"].(string); ok && u == userID {
			return nil
		}
	}
	return ErrDocumentForbidden
}

// ListDocuments возвращает доступные пользователю документы.
// Фильтрация ACL делается в SQL для эффективности (см. iteration-10 §Р5).
// status='deleted' исключается всегда.
func (s *Storage) ListDocuments(
	ctx context.Context, userID, role string, limit int,
) ([]Document, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	conds := []string{"status != 'deleted'"}
	args := []any{}
	switch role {
	case "admin", "security_officer", "compliance_officer", "auditor":
		// без ограничений
	default:
		// owner или ACL содержит роль/user_id.
		args = append(args, userID, role)
		conds = append(conds, fmt.Sprintf(
			`(owner_id = $%d::uuid
			   OR acl @> jsonb_build_array(jsonb_build_object('user_id', $%d::text))
			   OR acl @> jsonb_build_array(jsonb_build_object('role', $%d::text)))`,
			len(args)-1, len(args)-1, len(args)))
	}
	args = append(args, limit)
	q := `SELECT ` + documentColumns + ` FROM documents WHERE ` +
		strings.Join(conds, " AND ") +
		fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d`, len(args))

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: list documents: %w", err)
	}
	defer rows.Close()
	var out []Document
	for rows.Next() {
		var d Document
		if err := rows.Scan(&d.ID, &d.OwnerID, &d.Filename, &d.ContentType,
			&d.SizeBytes, &d.StorageKey, &d.Status, &d.Phase, &d.Error,
			&d.ACL, &d.ProcessingStartedAt, &d.ProcessingAttempts,
			&d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, fmt.Errorf("storage: scan document: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// SoftDeleteDocument: status='deleted', storage_key=”
// (план iteration-10 §Р3.1). raw уже удалён из MinIO handler'ом.
func (s *Storage) SoftDeleteDocument(ctx context.Context, id string) error {
	cmd, err := s.pool.Exec(ctx,
		`UPDATE documents SET status='deleted', storage_key='', phase=NULL
		 WHERE id = $1 AND status != 'deleted'`, id)
	if err != nil {
		return fmt.Errorf("storage: soft-delete: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return ErrDocumentNotFound
	}
	return nil
}

// RetryDocument: failed → pending, processing_attempts=0
// (MAJOR-2 ревью плана; manual re-queue).
func (s *Storage) RetryDocument(ctx context.Context, id string) error {
	cmd, err := s.pool.Exec(ctx,
		`UPDATE documents
		 SET status='pending', processing_attempts=0, error=NULL,
		     processing_started_at=NULL, phase=NULL
		 WHERE id = $1 AND status = 'failed'`, id)
	if err != nil {
		return fmt.Errorf("storage: retry: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return ErrDocumentNotFound
	}
	return nil
}

// ListDocumentChunks возвращает чанки (sanitized content) с JOIN
// sanitization_results для risk-метаданных.
func (s *Storage) ListDocumentChunks(
	ctx context.Context, documentID string,
) ([]DocumentChunkRow, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT c.id, c.document_id, c.chunk_index, c.content, c.token_count,
		        c.created_at,
		        sr.risk_level, sr.risk_score, sr.risk_classes
		 FROM document_chunks c
		 LEFT JOIN sanitization_results sr ON sr.document_id = c.document_id
		 WHERE c.document_id = $1
		 ORDER BY c.chunk_index ASC`, documentID)
	if err != nil {
		return nil, fmt.Errorf("storage: list chunks: %w", err)
	}
	defer rows.Close()
	var out []DocumentChunkRow
	for rows.Next() {
		var r DocumentChunkRow
		if err := rows.Scan(&r.ID, &r.DocumentID, &r.ChunkIndex, &r.Content,
			&r.TokenCount, &r.CreatedAt,
			&r.RiskLevel, &r.RiskScore, &r.RiskClasses); err != nil {
			return nil, fmt.Errorf("storage: scan chunk: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
