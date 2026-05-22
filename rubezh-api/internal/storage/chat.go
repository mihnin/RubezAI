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

// ErrChatMessageNotFound — сообщение (или его контекст обезличивания) не найдено.
var ErrChatMessageNotFound = errors.New("storage: сообщение чата не найдено")

// RevealContext — данные для раскрытия псевдонимов в ответе ассистента (J.2):
// текст ответа + владелец сессии + ссылка на mapping'и парного user-сообщения.
type RevealContext struct {
	SessionID            string
	OwnerUserID          string
	AssistantContent     string
	SanitizationResultID string
}

// GetRevealContext по id сообщения АССИСТЕНТА находит цепочку
// assistant → (тот же request_id) user-сообщение → sanitization_result.
// Возвращает текст ответа, владельца сессии и sanitization_result_id для
// чтения pseudonym_mappings. ErrChatMessageNotFound — если цепочка не найдена.
func (s *Storage) GetRevealContext(
	ctx context.Context, assistantMessageID string,
) (RevealContext, error) {
	var rc RevealContext
	err := s.pool.QueryRow(ctx,
		`SELECT a.session_id, sess.user_id, a.content, sr.id
		   FROM chat_messages a
		   JOIN chat_sessions sess ON sess.id = a.session_id
		   JOIN chat_messages u ON u.session_id = a.session_id
		        AND u.request_id = a.request_id AND u.role = 'user'
		   JOIN sanitization_results sr ON sr.message_id = u.id
		  WHERE a.id = $1 AND a.role = 'assistant'`,
		assistantMessageID,
	).Scan(&rc.SessionID, &rc.OwnerUserID, &rc.AssistantContent,
		&rc.SanitizationResultID)
	if errors.Is(err, pgx.ErrNoRows) {
		return RevealContext{}, ErrChatMessageNotFound
	}
	if err != nil {
		return RevealContext{}, fmt.Errorf("storage: reveal-контекст: %w", err)
	}
	return rc, nil
}

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
// RequestID — коррелятор пары user+assistant (контракт
// chat.schema.json#ChatMessage.request_id, Итерация 9 M3).
// Mappings — зашифрованные псевдонимы (план iteration-9.md §Р2);
// вставляются в той же Tx1 после sanitization_results.
type ChatRequestRecord struct {
	SessionID    string
	UserContent  string
	RequestID    string
	Sanitization SanitizationData
	Mappings     []PseudonymMappingInput
	Audit        AuditEvent
}

// ChatRequestIDs — идентификаторы, созданные RecordChatRequest.
type ChatRequestIDs struct {
	UserMessageID        string
	SanitizationResultID string
	AuditEventID         string
}

// ChatTerminationRecord — входные данные транзакции завершения запроса.
// RequestID — коррелятор; одинаков для пары user+assistant.
type ChatTerminationRecord struct {
	SessionID        string
	AssistantContent string // пусто → сообщение ассистента не создаётся
	ModelProviderID  *string
	RequestID        string
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
	// Если RequestID не задан — записываем NULL (backward compat для
	// сообщений, созданных до Итерации 9).
	var reqIDArg any
	if rec.RequestID != "" {
		reqIDArg = rec.RequestID
	}
	if err = tx.QueryRow(ctx,
		`INSERT INTO chat_messages (session_id, role, content, request_id)
		 VALUES ($1, 'user', $2, $3) RETURNING id`,
		rec.SessionID, rec.UserContent, reqIDArg,
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

	// Зашифрованные mapping'и — внутри Tx1 после sanitization_results
	// (план iteration-9.md §Р2). Шифрование сделано в оркестраторе,
	// здесь только INSERT.
	if len(rec.Mappings) > 0 {
		if _, err = InsertPseudonymMappings(ctx, tx,
			ids.SanitizationResultID, rec.Mappings); err != nil {
			return ChatRequestIDs{}, err
		}
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
	var reqIDArg any
	if rec.RequestID != "" {
		reqIDArg = rec.RequestID
	}
	if rec.AssistantContent != "" {
		if err = tx.QueryRow(ctx,
			`INSERT INTO chat_messages
			   (session_id, role, content, model_provider_id, request_id)
			 VALUES ($1, 'assistant', $2, $3, $4) RETURNING id`,
			rec.SessionID, rec.AssistantContent, rec.ModelProviderID, reqIDArg,
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

// ChatMessageWithSummary — сообщение истории сессии плюс опциональный
// snapshot обезличивания (для отрисовки chip'ов псевдонимов на фронте).
// Используется в GET /api/chat/sessions/:id/messages (план §Р5).
// Поля Sanitization.Entities[] СТРОГО whitelist — только публичные:
// type, category, pseudonym, raw_hash. Поля start/end **не возвращаются**
// (защита от утечки спанов; план §Р5 + контракт chat.schema.json).
// JSON-теги соответствуют chat.schema.json#ChatMessage.
type ChatMessageWithSummary struct {
	ID                  string               `json:"id"`
	SessionID           string               `json:"session_id"`
	Role                string               `json:"role"`
	Content             string               `json:"content"`
	ModelProviderID     *string              `json:"model_provider_id"`
	RequestID           *string              `json:"request_id"`
	CreatedAt           time.Time            `json:"created_at"`
	SanitizationSummary *SanitizationSummary `json:"sanitization_summary"`
}

// SanitizationSummary — публичный snapshot обезличивания для UI.
type SanitizationSummary struct {
	EntityCount int                  `json:"entity_count"`
	Risk        SanitizationRisk     `json:"risk"`
	Entities    []SanitizationEntity `json:"entities"`
}

// SanitizationRisk — публичные поля риска.
type SanitizationRisk struct {
	Level   string   `json:"level"`
	Score   float64  `json:"score"`
	Classes []string `json:"classes"`
}

// SanitizationEntity — публичные поля сущности (whitelist; см. план §Р5).
// start/end НЕ включены — это инвариант через тип-систему.
type SanitizationEntity struct {
	Type      string `json:"type"`
	Category  string `json:"category"`
	Pseudonym string `json:"pseudonym"`
	RawHash   string `json:"raw_hash"`
}

// ListChatMessages возвращает все сообщения сессии в хронологическом
// порядке с JOIN sanitization_results. Whitelist полей entity делается
// в Go: при разборе jsonb из БД берутся ТОЛЬКО публичные ключи (type,
// category, pseudonym, raw_hash); start/end игнорируются.
func (s *Storage) ListChatMessages(
	ctx context.Context, sessionID string,
) ([]ChatMessageWithSummary, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT m.id, m.session_id, m.role, m.content, m.model_provider_id,
		        m.request_id, m.created_at,
		        sr.risk_level, sr.risk_score, sr.risk_classes, sr.entities
		 FROM chat_messages m
		 LEFT JOIN sanitization_results sr ON sr.message_id = m.id
		 WHERE m.session_id = $1
		 ORDER BY m.created_at ASC, m.id ASC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("storage: list messages: %w", err)
	}
	defer rows.Close()
	var out []ChatMessageWithSummary
	for rows.Next() {
		msg, err := scanChatMessageWithSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	return out, rows.Err()
}

// scanChatMessageWithSummary читает строку JOIN'а и применяет whitelist
// к sanitization_summary. Вынесено для лимита функций ≤60 строк.
func scanChatMessageWithSummary(
	rows pgx.Rows,
) (ChatMessageWithSummary, error) {
	var (
		m         ChatMessageWithSummary
		riskLevel *string
		riskScore *float64
		classes   []string
		entities  []byte
	)
	if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content,
		&m.ModelProviderID, &m.RequestID, &m.CreatedAt,
		&riskLevel, &riskScore, &classes, &entities); err != nil {
		return ChatMessageWithSummary{}, fmt.Errorf(
			"storage: scan message: %w", err)
	}
	if riskLevel == nil {
		return m, nil // нет sanitization_result (assistant или старые)
	}
	whitelisted, err := whitelistEntities(entities)
	if err != nil {
		return ChatMessageWithSummary{}, err
	}
	score := 0.0
	if riskScore != nil {
		score = *riskScore
	}
	if classes == nil {
		classes = []string{}
	}
	m.SanitizationSummary = &SanitizationSummary{
		EntityCount: len(whitelisted),
		Risk: SanitizationRisk{
			Level: *riskLevel, Score: score, Classes: classes,
		},
		Entities: whitelisted,
	}
	return m, nil
}

// whitelistEntities декодирует jsonb sanitization_results.entities и
// возвращает СТРОГИЙ whitelist (без start/end). Это критический
// инвариант безопасности (план §Р5): даже если в БД попали лишние
// поля, наружу они не выйдут.
func whitelistEntities(raw []byte) ([]SanitizationEntity, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	// Дешифровываем в [{any}, ...] и затем строим whitelist.
	var src []map[string]any
	if err := json.Unmarshal(raw, &src); err != nil {
		return nil, fmt.Errorf("storage: parse entities: %w", err)
	}
	out := make([]SanitizationEntity, 0, len(src))
	for _, e := range src {
		// Whitelist: только эти ключи переходят дальше. start/end
		// игнорируются даже если присутствуют в jsonb.
		typeStr, _ := e["type"].(string)
		catStr, _ := e["category"].(string)
		ps, _ := e["pseudonym"].(string)
		hash, _ := e["raw_hash"].(string)
		out = append(out, SanitizationEntity{
			Type: typeStr, Category: catStr,
			Pseudonym: ps, RawHash: hash,
		})
	}
	return out, nil
}
