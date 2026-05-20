package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/rubezh-ai/rubezh-api/internal/auth"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

// incidentNoteDTO — публичная форма IncidentNote (контракт
// incidents.schema.json#IncidentNote).
type incidentNoteDTO struct {
	ID         string    `json:"id"`
	IncidentID string    `json:"incident_id"`
	AuthorID   string    `json:"author_id"`
	Content    string    `json:"content"`
	CreatedAt  time.Time `json:"created_at"`
}

type incidentNoteListDTO struct {
	Notes []incidentNoteDTO `json:"notes"`
}

type incidentNoteCreateDTO struct {
	Content string `json:"content"`
}

func noteToDTO(n storage.IncidentNote) incidentNoteDTO {
	return incidentNoteDTO{
		ID: n.ID, IncidentID: n.IncidentID, AuthorID: n.AuthorID,
		Content: n.Content, CreatedAt: n.CreatedAt,
	}
}

// addIncidentNoteHandler — POST /api/incidents/:id/notes.
// Append-only (триггер БД блокирует UPDATE/DELETE). Доступ: sec/admin
// и assignee (даже если роль ниже — но в MVP assignee всегда sec/admin).
func addIncidentNoteHandler(store *storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role, _ := auth.RoleFromContext(r.Context())
		canWrite := incidentWriteRoles[role]
		id := chi.URLParam(r, "id")
		if !isUUID(id) {
			http.NotFound(w, r)
			return
		}
		var dto incidentNoteCreateDTO
		if err := decodeJSON(w, r, &dto); err != nil {
			http.Error(w, "некорректный JSON", http.StatusBadRequest)
			return
		}
		if len(dto.Content) < 1 || len(dto.Content) > 2000 {
			http.Error(w, "content: 1..2000 символов",
				http.StatusBadRequest)
			return
		}
		actorID, err := currentUserID(r, store)
		if err != nil {
			http.Error(w, "user not resolved",
				http.StatusInternalServerError)
			return
		}
		if !canWrite {
			// assignee может писать заметки.
			inc, e := store.GetIncident(r.Context(), id)
			if errors.Is(e, storage.ErrIncidentNotFound) {
				http.NotFound(w, r)
				return
			}
			if e != nil || inc.AssigneeID == nil || *inc.AssigneeID != actorID {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}
		note, err := store.AddIncidentNote(r.Context(),
			storage.IncidentNoteInput{
				IncidentID: id, AuthorID: actorID, Content: dto.Content,
			})
		if errors.Is(err, storage.ErrIncidentNoteTooLong) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(w, "ошибка записи", http.StatusInternalServerError)
			return
		}
		_, _ = store.InsertAuditEvent(r.Context(), storage.AuditEvent{
			UserID: actorID, EventType: "incident_note_added",
			Detail: map[string]any{"incident_id": id, "note_id": note.ID},
		})
		writeJSON(w, http.StatusCreated, noteToDTO(note))
	}
}

func listIncidentNotesHandler(store *storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role, _ := auth.RoleFromContext(r.Context())
		if !incidentReadRoles[role] {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		id := chi.URLParam(r, "id")
		if !isUUID(id) {
			http.NotFound(w, r)
			return
		}
		notes, err := store.ListIncidentNotes(r.Context(), id)
		if err != nil {
			http.Error(w, "ошибка чтения", http.StatusInternalServerError)
			return
		}
		dtos := make([]incidentNoteDTO, 0, len(notes))
		for _, n := range notes {
			dtos = append(dtos, noteToDTO(n))
		}
		writeJSON(w, http.StatusOK, incidentNoteListDTO{Notes: dtos})
	}
}
