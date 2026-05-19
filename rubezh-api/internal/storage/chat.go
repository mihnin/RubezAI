package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrChatSessionNotFound — чат-сессия с указанным id не найдена.
var ErrChatSessionNotFound = errors.New("storage: чат-сессия не найдена")

// ChatSession — запись таблицы chat_sessions.
type ChatSession struct {
	ID        string
	UserID    string
	Title     *string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ChatMessage — запись таблицы chat_messages.
type ChatMessage struct {
	ID              string
	SessionID       string
	Role            string
	Content         string
	ModelProviderID *string
	CreatedAt       time.Time
}

// SanitizationData — данные результата обезличивания для записи в БД.
type SanitizationData struct {
	RiskLevel   string
	RiskScore   float64
	RiskClasses []string
	Entities    json.RawMessage
}

// ChatRequestRecord — входные данные транзакции записи запроса чата.
type ChatRequestRecord struct {
	SessionID    string
	UserContent  string
	Sanitization SanitizationData
	Audit        AuditEvent
}

// ChatRequestIDs — идентификаторы, созданные RecordChatRequest.
type ChatRequestIDs struct {
	UserMessageID        string
	SanitizationResultID string
	AuditEventID         string
}

// ChatTerminationRecord — входные данные транзакции завершения запроса.
type ChatTerminationRecord struct {
	SessionID        string
	AssistantContent string // пусто → сообщение ассистента не создаётся
	ModelProviderID  *string
	Audit            AuditEvent
}

// ChatTerminationIDs — идентификаторы, созданные RecordChatTermination.
type ChatTerminationIDs struct {
	AssistantMessageID string // пусто, если ассистентское сообщение не писалось
	AuditEventID       string
}

// CreateChatSession создаёт новую чат-сессию пользователя.
func (s *Storage) CreateChatSession(
	ctx context.Context, userID string, title *string,
) (ChatSession, error) {
	session := ChatSession{UserID: userID, Title: title}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO chat_sessions (user_id, title) VALUES ($1, $2)
		 RETURNING id, created_at, updated_at`,
		userID, title,
	).Scan(&session.ID, &session.CreatedAt, &session.UpdatedAt)
	if err != nil {
		return ChatSession{}, fmt.Errorf("storage: создание чат-сессии: %w", err)
	}
	return session, nil
}

// ListChatSessions возвращает сессии пользователя (новые — первыми).
func (s *Storage) ListChatSessions(
	ctx context.Context, userID string,
) ([]ChatSession, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, title, created_at, updated_at
		 FROM chat_sessions WHERE user_id = $1 ORDER BY created_at DESC`,
		userID)
	if err != nil {
		return nil, fmt.Errorf("storage: список чат-сессий: %w", err)
	}
	defer rows.Close()

	sessions := make([]ChatSession, 0)
	for rows.Next() {
		var cs ChatSession
		if err := rows.Scan(
			&cs.ID, &cs.UserID, &cs.Title, &cs.CreatedAt, &cs.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("storage: чтение чат-сессии: %w", err)
		}
		sessions = append(sessions, cs)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: обход чат-сессий: %w", err)
	}
	return sessions, nil
}

// GetChatSession читает сессию по id; ErrChatSessionNotFound — если нет.
func (s *Storage) GetChatSession(
	ctx context.Context, id string,
) (ChatSession, error) {
	var cs ChatSession
	err := s.pool.QueryRow(ctx,
		`SELECT id, user_id, title, created_at, updated_at
		 FROM chat_sessions WHERE id = $1`, id,
	).Scan(&cs.ID, &cs.UserID, &cs.Title, &cs.CreatedAt, &cs.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ChatSession{}, ErrChatSessionNotFound
	}
	if err != nil {
		return ChatSession{}, fmt.Errorf("storage: чтение чат-сессии: %w", err)
	}
	return cs, nil
}

// RecordChatRequest атомарно записывает сообщение пользователя, результат
// обезличивания и audit-событие chat_request (Транзакция 1 потока /api/chat).
func (s *Storage) RecordChatRequest(
	ctx context.Context, rec ChatRequestRecord,
) (ChatRequestIDs, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ChatRequestIDs{}, fmt.Errorf("storage: начало транзакции: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var ids ChatRequestIDs
	if err = tx.QueryRow(ctx,
		`INSERT INTO chat_messages (session_id, role, content)
		 VALUES ($1, 'user', $2) RETURNING id`,
		rec.SessionID, rec.UserContent,
	).Scan(&ids.UserMessageID); err != nil {
		return ChatRequestIDs{}, fmt.Errorf("storage: запись сообщения: %w", err)
	}

	entities := rec.Sanitization.Entities
	if len(entities) == 0 {
		entities = json.RawMessage("[]")
	}
	classes := rec.Sanitization.RiskClasses
	if classes == nil {
		classes = []string{}
	}
	if err = tx.QueryRow(ctx,
		`INSERT INTO sanitization_results
		   (message_id, risk_level, risk_score, risk_classes, entities)
		 VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		ids.UserMessageID, rec.Sanitization.RiskLevel,
		rec.Sanitization.RiskScore, classes, entities,
	).Scan(&ids.SanitizationResultID); err != nil {
		return ChatRequestIDs{}, fmt.Errorf("storage: запись обезличивания: %w", err)
	}

	if ids.AuditEventID, err = insertAuditEvent(ctx, tx, rec.Audit); err != nil {
		return ChatRequestIDs{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return ChatRequestIDs{}, fmt.Errorf("storage: фиксация транзакции: %w", err)
	}
	return ids, nil
}

// RecordChatTermination атомарно записывает (опционально) сообщение
// ассистента и терминальное audit-событие (Транзакция 2 потока /api/chat).
func (s *Storage) RecordChatTermination(
	ctx context.Context, rec ChatTerminationRecord,
) (ChatTerminationIDs, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ChatTerminationIDs{}, fmt.Errorf("storage: начало транзакции: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var ids ChatTerminationIDs
	if rec.AssistantContent != "" {
		if err = tx.QueryRow(ctx,
			`INSERT INTO chat_messages
			   (session_id, role, content, model_provider_id)
			 VALUES ($1, 'assistant', $2, $3) RETURNING id`,
			rec.SessionID, rec.AssistantContent, rec.ModelProviderID,
		).Scan(&ids.AssistantMessageID); err != nil {
			return ChatTerminationIDs{}, fmt.Errorf(
				"storage: запись ответа ассистента: %w", err)
		}
	}

	if ids.AuditEventID, err = insertAuditEvent(ctx, tx, rec.Audit); err != nil {
		return ChatTerminationIDs{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return ChatTerminationIDs{}, fmt.Errorf("storage: фиксация транзакции: %w", err)
	}
	return ids, nil
}
