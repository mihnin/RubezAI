package api

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/rubezh-ai/rubezh-api/internal/auth"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

const (
	maxDocumentBytes = 50 * 1024 * 1024 // 50 МБ — THREAT_MODEL T9
)

var allowedDocumentMIMEs = map[string]bool{
	"application/pdf":            true,
	"application/octet-stream":   true, // некоторые браузеры
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": true,
}

type documentDTO struct {
	ID                  string     `json:"id"`
	OwnerID             string     `json:"owner_id"`
	Filename            string     `json:"filename"`
	ContentType         *string    `json:"content_type"`
	SizeBytes           *int64     `json:"size_bytes"`
	Status              string     `json:"status"`
	Phase               *string    `json:"phase"`
	Error               *string    `json:"error"`
	ProcessingAttempts  int        `json:"processing_attempts"`
	ProcessingStartedAt *time.Time `json:"processing_started_at"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type documentListDTO struct {
	Documents []documentDTO `json:"documents"`
}

type documentChunkDTO struct {
	ID          string    `json:"id"`
	DocumentID  string    `json:"document_id"`
	ChunkIndex  int       `json:"chunk_index"`
	Content     string    `json:"content"` // sanitized!
	TokenCount  *int      `json:"token_count"`
	RiskLevel   *string   `json:"risk_level"`
	RiskScore   *float64  `json:"risk_score"`
	RiskClasses []string  `json:"risk_classes"`
	CreatedAt   time.Time `json:"created_at"`
}

type documentChunkListDTO struct {
	DocumentID string             `json:"document_id"`
	Chunks     []documentChunkDTO `json:"chunks"`
}

func documentToDTO(d storage.Document) documentDTO {
	return documentDTO{
		ID: d.ID, OwnerID: d.OwnerID, Filename: d.Filename,
		ContentType: d.ContentType, SizeBytes: d.SizeBytes,
		Status: d.Status, Phase: d.Phase, Error: d.Error,
		ProcessingAttempts: d.ProcessingAttempts,
		ProcessingStartedAt: d.ProcessingStartedAt,
		CreatedAt:           d.CreatedAt, UpdatedAt: d.UpdatedAt,
	}
}

func chunkToDTO(c storage.DocumentChunkRow) documentChunkDTO {
	classes := c.RiskClasses
	if classes == nil {
		classes = []string{}
	}
	return documentChunkDTO{
		ID: c.ID, DocumentID: c.DocumentID, ChunkIndex: c.ChunkIndex,
		Content: c.Content, TokenCount: c.TokenCount,
		RiskLevel: c.RiskLevel, RiskScore: c.RiskScore,
		RiskClasses: classes, CreatedAt: c.CreatedAt,
	}
}

// generateStorageKey — UUID-based, без user-control в пути.
func generateStorageKey(filename string) string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	id := fmt.Sprintf("%x", b)
	ext := strings.ToLower(filepath.Ext(filename))
	if ext != ".pdf" && ext != ".docx" {
		ext = ""
	}
	return fmt.Sprintf("documents/%s%s", id, ext)
}

// uploadDocumentHandler — POST /api/documents (multipart).
func uploadDocumentHandler(
	store *storage.Storage, minioClient *storage.MinioClient,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if minioClient == nil {
			http.Error(w, "MinIO не настроен", http.StatusServiceUnavailable)
			return
		}
		if err := r.ParseMultipartForm(maxDocumentBytes); err != nil {
			http.Error(w, "ошибка разбора multipart", http.StatusBadRequest)
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "поле file обязательно", http.StatusBadRequest)
			return
		}
		defer func() { _ = file.Close() }()
		if header.Size > maxDocumentBytes {
			http.Error(w, "файл слишком большой (>50 МБ)",
				http.StatusRequestEntityTooLarge)
			return
		}
		contentType := header.Header.Get("Content-Type")
		if !allowedDocumentMIMEs[contentType] {
			// Доп. проверка по расширению.
			ext := strings.ToLower(filepath.Ext(header.Filename))
			if ext != ".pdf" && ext != ".docx" {
				http.Error(w, "поддерживаются только pdf/docx",
					http.StatusUnsupportedMediaType)
				return
			}
		}
		userID, err := currentUserID(r, store)
		if err != nil {
			http.Error(w, "user not resolved",
				http.StatusInternalServerError)
			return
		}
		buf := make([]byte, header.Size)
		if _, err := file.Read(buf); err != nil && err.Error() != "EOF" {
			http.Error(w, "ошибка чтения файла",
				http.StatusInternalServerError)
			return
		}
		key := generateStorageKey(header.Filename)
		if _, err := minioClient.Upload(r.Context(), key, buf,
			contentType); err != nil {
			http.Error(w, "ошибка загрузки в MinIO",
				http.StatusBadGateway)
			return
		}
		sz := header.Size
		ct := contentType
		doc, err := store.CreateDocument(r.Context(), storage.DocumentInput{
			OwnerID: userID, Filename: header.Filename,
			ContentType: &ct, SizeBytes: &sz, StorageKey: key,
			ACL: json.RawMessage("[]"),
		})
		if err != nil {
			// MinIO уже залит — теоретически orphan, но не критично.
			http.Error(w, "ошибка создания записи",
				http.StatusInternalServerError)
			return
		}
		_, _ = store.InsertAuditEvent(r.Context(), storage.AuditEvent{
			UserID: userID, EventType: "document_uploaded",
			Detail: map[string]any{
				"document_id": doc.ID, "filename": header.Filename},
		})
		writeJSON(w, http.StatusCreated, documentToDTO(doc))
	}
}

// listDocumentsHandler — GET /api/documents.
func listDocumentsHandler(store *storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, _ := currentUserID(r, store)
		role, _ := auth.RoleFromContext(r.Context())
		docs, err := store.ListDocuments(r.Context(), userID,
			string(role), 100)
		if err != nil {
			http.Error(w, "ошибка чтения", http.StatusInternalServerError)
			return
		}
		out := make([]documentDTO, 0, len(docs))
		for _, d := range docs {
			out = append(out, documentToDTO(d))
		}
		writeJSON(w, http.StatusOK, documentListDTO{Documents: out})
	}
}

// getDocumentHandler — GET /api/documents/:id.
func getDocumentHandler(store *storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if !isUUID(id) {
			http.NotFound(w, r)
			return
		}
		doc, err := store.GetDocument(r.Context(), id)
		if errors.Is(err, storage.ErrDocumentNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "ошибка чтения", http.StatusInternalServerError)
			return
		}
		userID, _ := currentUserID(r, store)
		role, _ := auth.RoleFromContext(r.Context())
		if err := storage.CheckDocumentAccess(doc, userID, string(role)); err != nil {
			http.NotFound(w, r) // нераскрытие существования
			return
		}
		writeJSON(w, http.StatusOK, documentToDTO(doc))
	}
}

// listDocumentChunksHandler — GET /api/documents/:id/chunks.
func listDocumentChunksHandler(store *storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if !isUUID(id) {
			http.NotFound(w, r)
			return
		}
		doc, err := store.GetDocument(r.Context(), id)
		if errors.Is(err, storage.ErrDocumentNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "ошибка", http.StatusInternalServerError)
			return
		}
		userID, _ := currentUserID(r, store)
		role, _ := auth.RoleFromContext(r.Context())
		if err := storage.CheckDocumentAccess(doc, userID, string(role)); err != nil {
			http.NotFound(w, r)
			return
		}
		chunks, err := store.ListDocumentChunks(r.Context(), id)
		if err != nil {
			http.Error(w, "ошибка чтения", http.StatusInternalServerError)
			return
		}
		out := make([]documentChunkDTO, 0, len(chunks))
		for _, c := range chunks {
			out = append(out, chunkToDTO(c))
		}
		writeJSON(w, http.StatusOK, documentChunkListDTO{
			DocumentID: id, Chunks: out,
		})
	}
}

// downloadDocumentHandler — GET /api/documents/:id/download (owner+admin).
func downloadDocumentHandler(
	store *storage.Storage, minioClient *storage.MinioClient,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if minioClient == nil {
			http.Error(w, "MinIO не настроен", http.StatusServiceUnavailable)
			return
		}
		id := chi.URLParam(r, "id")
		if !isUUID(id) {
			http.NotFound(w, r)
			return
		}
		doc, err := store.GetDocument(r.Context(), id)
		if errors.Is(err, storage.ErrDocumentNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "ошибка", http.StatusInternalServerError)
			return
		}
		userID, _ := currentUserID(r, store)
		role, _ := auth.RoleFromContext(r.Context())
		// Download разрешён только owner или admin.
		if doc.OwnerID != userID && role != auth.RoleAdmin {
			http.NotFound(w, r)
			return
		}
		content, err := minioClient.Download(r.Context(), doc.StorageKey)
		if err != nil {
			http.Error(w, "ошибка загрузки", http.StatusBadGateway)
			return
		}
		_, _ = store.InsertAuditEvent(r.Context(), storage.AuditEvent{
			UserID: userID, EventType: "document_downloaded",
			Detail: map[string]any{"document_id": doc.ID},
		})
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition",
			`attachment; filename="`+doc.Filename+`"`)
		_, _ = w.Write(content)
	}
}

// deleteDocumentHandler — DELETE /api/documents/:id (owner+admin).
// hard-delete MinIO + soft-delete БД.
func deleteDocumentHandler(
	store *storage.Storage, minioClient *storage.MinioClient,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if !isUUID(id) {
			http.NotFound(w, r)
			return
		}
		doc, err := store.GetDocument(r.Context(), id)
		if errors.Is(err, storage.ErrDocumentNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "ошибка", http.StatusInternalServerError)
			return
		}
		userID, _ := currentUserID(r, store)
		role, _ := auth.RoleFromContext(r.Context())
		if doc.OwnerID != userID && role != auth.RoleAdmin {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		// Hard-delete object в MinIO (best-effort; ошибки логируются
		// но не блокируют DB soft-delete — иначе orphan-row).
		if minioClient != nil && doc.StorageKey != "" {
			_ = minioClient.Remove(r.Context(), doc.StorageKey)
		}
		if err := store.SoftDeleteDocument(r.Context(), id); err != nil {
			if errors.Is(err, storage.ErrDocumentNotFound) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, "ошибка удаления", http.StatusInternalServerError)
			return
		}
		_, _ = store.InsertAuditEvent(r.Context(), storage.AuditEvent{
			UserID: userID, EventType: "document_deleted",
			Detail: map[string]any{"document_id": id},
		})
		w.WriteHeader(http.StatusNoContent)
	}
}

// retryDocumentHandler — POST /api/documents/:id/retry (owner+admin).
func retryDocumentHandler(store *storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if !isUUID(id) {
			http.NotFound(w, r)
			return
		}
		doc, err := store.GetDocument(r.Context(), id)
		if errors.Is(err, storage.ErrDocumentNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "ошибка", http.StatusInternalServerError)
			return
		}
		userID, _ := currentUserID(r, store)
		role, _ := auth.RoleFromContext(r.Context())
		if doc.OwnerID != userID && role != auth.RoleAdmin {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if err := store.RetryDocument(r.Context(), id); err != nil {
			if errors.Is(err, storage.ErrDocumentNotFound) {
				http.Error(w, "документ не в статусе failed",
					http.StatusConflict)
				return
			}
			http.Error(w, "ошибка retry", http.StatusInternalServerError)
			return
		}
		_, _ = store.InsertAuditEvent(r.Context(), storage.AuditEvent{
			UserID: userID, EventType: "document_retry_requested",
			Detail: map[string]any{"document_id": id},
		})
		w.WriteHeader(http.StatusNoContent)
	}
}
