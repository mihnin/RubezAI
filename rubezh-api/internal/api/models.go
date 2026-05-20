package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/rubezh-ai/rubezh-api/internal/auth"
	"github.com/rubezh-ai/rubezh-api/internal/crypto"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

// validAdapter — допустимые адаптеры (CHECK model_providers.adapter).
var validAdapter = map[string]bool{"mock": true, "openai_compatible": true}

// modelProviderDTO — публичная форма ModelProvider.
// has_api_key: bool (Итерация 9.5) — есть ли зашифрованный ключ;
// сам ключ никогда не возвращается в API (даже masked).
type modelProviderDTO struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	TrustLevel      string    `json:"trust_level"`
	Adapter         string    `json:"adapter"`
	Endpoint        string    `json:"endpoint"`
	MaxTokens       *int      `json:"max_tokens"`
	RateLimitPerMin *int      `json:"rate_limit_per_min"`
	IsEnabled       bool      `json:"is_enabled"`
	HasAPIKey       bool      `json:"has_api_key"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// createModelRequest — POST /api/models. APIKey (plaintext) опционально:
// если задан — шифруется AES-GCM с AAD=id (UUID, иммутабельный,
// MINOR-1 ревью Итерации 9.5) перед записью в БД.
type createModelRequest struct {
	Name            string  `json:"name"`
	TrustLevel      string  `json:"trust_level"`
	Adapter         string  `json:"adapter"`
	Endpoint        string  `json:"endpoint"`
	MaxTokens       *int    `json:"max_tokens"`
	RateLimitPerMin *int    `json:"rate_limit_per_min"`
	APIKey          *string `json:"api_key"`
}

// patchAPIKeyRequest — POST /api/models/:id/api-key. Plaintext шифруется
// с AAD = id того же провайдера (UUID, иммутабельный). Пустая строка
// сбрасывает ключ (NULL) — provider будет использовать env LLM_API_KEY
// fallback (deprecated, для backward compat).
type patchAPIKeyRequest struct {
	APIKey string `json:"api_key"`
}

// validate проверяет запрос на создание провайдера против контракта.
func (r createModelRequest) validate() error {
	if r.Name == "" {
		return errors.New("поле name обязательно")
	}
	if !validModelTrust[r.TrustLevel] {
		return fmt.Errorf("недопустимый trust_level: %q", r.TrustLevel)
	}
	if !validAdapter[r.Adapter] {
		return fmt.Errorf("недопустимый adapter: %q", r.Adapter)
	}
	if r.Adapter == "openai_compatible" {
		if r.Endpoint == "" {
			return errors.New("для adapter openai_compatible требуется endpoint")
		}
		parsed, err := url.ParseRequestURI(r.Endpoint)
		if err != nil || parsed.Host == "" ||
			(parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf(
				"endpoint должен быть корректным http(s)-URL: %q", r.Endpoint)
		}
	}
	if r.MaxTokens != nil && *r.MaxTokens <= 0 {
		return errors.New("max_tokens должен быть положительным")
	}
	if r.RateLimitPerMin != nil && *r.RateLimitPerMin <= 0 {
		return errors.New("rate_limit_per_min должен быть положительным")
	}
	return nil
}

func modelProviderToDTO(p storage.ModelProvider) modelProviderDTO {
	return modelProviderDTO{
		ID:              p.ID,
		Name:            p.Name,
		TrustLevel:      p.TrustLevel,
		Adapter:         p.Adapter,
		Endpoint:        p.Endpoint,
		MaxTokens:       p.MaxTokens,
		RateLimitPerMin: p.RateLimitPerMin,
		IsEnabled:       p.IsEnabled,
		HasAPIKey:       p.HasAPIKey(),
		CreatedAt:       p.CreatedAt,
		UpdatedAt:       p.UpdatedAt,
	}
}

// modelProviderAPIKeyAAD — AAD для шифрования API-ключа провайдера.
// Привязан к иммутабельному `id` (UUID, не меняется при rename) —
// закрывает MINOR-1 ревью Итерации 9.5. Тот же helper используется
// в main.go buildRouter (через storage.ModelProvider.ID).
func modelProviderAPIKeyAAD(providerID string) []byte {
	return []byte("model_provider_api_key:" + providerID)
}

// modelWriteRoles — кто может создавать/менять провайдеров и ключи
// (MAJOR-1 ревью Итерации 9.5: RBAC для security-критичных операций).
var modelWriteRoles = map[auth.Role]bool{
	auth.RoleAdmin:     true,
	auth.RoleDeveloper: true, // для dev/test-инструментов; can be tightened post-MVP
}

// listModelsHandler возвращает список провайдеров моделей.
func listModelsHandler(store *storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providers, err := store.ListModelProviders(r.Context())
		if err != nil {
			http.Error(w, "ошибка чтения провайдеров", http.StatusInternalServerError)
			return
		}
		out := make([]modelProviderDTO, len(providers))
		for i, p := range providers {
			out[i] = modelProviderToDTO(p)
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// createModelHandler регистрирует нового провайдера модели.
// cipher — для шифрования опционального api_key из тела запроса (Итерация 9.5).
// AAD = id (известен только после INSERT) — поэтому шифруем в 2 этапа:
// (1) CreateModelProvider без api_key → RETURNING id;
// (2) Encrypt(plaintext, AAD=id) → UpdateModelProviderAPIKey.
// Если шаг 2 упал — провайдер существует без ключа; admin может
// повторить через POST /api/models/:id/api-key.
func createModelHandler(
	store *storage.Storage, cipher *crypto.Cipher,
	reloadRouter func(context.Context) error, logger *slog.Logger,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role, _ := auth.RoleFromContext(r.Context())
		if !modelWriteRoles[role] {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var req createModelRequest
		if err := decodeJSON(w, r, &req); err != nil {
			http.Error(w, "некорректный JSON", http.StatusBadRequest)
			return
		}
		if err := req.validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		hasKey := req.APIKey != nil && *req.APIKey != ""
		if hasKey && cipher == nil {
			http.Error(w, "MAPPING_ENCRYPTION_KEY не настроен — api_key недоступен",
				http.StatusServiceUnavailable)
			return
		}
		created, err := store.CreateModelProvider(r.Context(), storage.ModelProvider{
			Name:            req.Name,
			TrustLevel:      req.TrustLevel,
			Adapter:         req.Adapter,
			Endpoint:        req.Endpoint,
			MaxTokens:       req.MaxTokens,
			RateLimitPerMin: req.RateLimitPerMin,
		})
		if err != nil {
			if errors.Is(err, storage.ErrModelProviderExists) {
				http.Error(w, "провайдер с таким именем уже существует",
					http.StatusConflict)
				return
			}
			http.Error(w, "не удалось создать провайдера",
				http.StatusInternalServerError)
			return
		}
		// Фаза 2: шифрование с AAD=id (закрывает MINOR-1 ревью 9.5).
		if hasKey {
			ct, encErr := cipher.Encrypt([]byte(*req.APIKey),
				modelProviderAPIKeyAAD(created.ID))
			if encErr != nil {
				http.Error(w, "ошибка шифрования api_key",
					http.StatusInternalServerError)
				return
			}
			if updErr := store.UpdateModelProviderAPIKey(r.Context(),
				created.ID, ct); updErr != nil {
				http.Error(w, "ошибка сохранения api_key",
					http.StatusInternalServerError)
				return
			}
			created.APIKeyEncrypted = ct
		}
		tryReloadRouter(r.Context(), reloadRouter, logger, "createModel")
		writeJSON(w, http.StatusCreated, modelProviderToDTO(created))
	}
}

// tryReloadRouter — best-effort hot-reload LLM Router после изменения
// провайдера (F2). Ошибка не блокирует ответ (CRUD-операция уже
// зафиксирована в БД), но логируется. Если reloadRouter == nil
// (некоторые тесты не настраивают замыкание) — no-op.
func tryReloadRouter(
	ctx context.Context, reload func(context.Context) error,
	logger *slog.Logger, op string,
) {
	if reload == nil {
		return
	}
	if err := reload(ctx); err != nil && logger != nil {
		logger.Error("hot-reload LLM Router не удался",
			"op", op, "error", err)
	}
}

// updateModelAPIKeyHandler — POST /api/models/:id/api-key.
// Заменяет зашифрованный ключ или удаляет его (api_key="").
// AAD = id (иммутабельный, MINOR-1 ревью 9.5). RBAC — admin/developer.
func updateModelAPIKeyHandler(
	store *storage.Storage, cipher *crypto.Cipher,
	reloadRouter func(context.Context) error, logger *slog.Logger,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role, _ := auth.RoleFromContext(r.Context())
		if !modelWriteRoles[role] {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		id := chi.URLParam(r, "id")
		if !isUUID(id) {
			http.NotFound(w, r)
			return
		}
		var req patchAPIKeyRequest
		if err := decodeJSON(w, r, &req); err != nil {
			http.Error(w, "некорректный JSON", http.StatusBadRequest)
			return
		}
		// Проверка существования провайдера (404 vs 5xx разведение).
		if _, err := store.GetModelProvider(r.Context(), id); err != nil {
			if errors.Is(err, storage.ErrModelProviderNotFound) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, "ошибка чтения", http.StatusInternalServerError)
			return
		}
		var encrypted []byte
		if req.APIKey != "" {
			if cipher == nil {
				http.Error(w, "MAPPING_ENCRYPTION_KEY не настроен",
					http.StatusServiceUnavailable)
				return
			}
			ct, encErr := cipher.Encrypt([]byte(req.APIKey),
				modelProviderAPIKeyAAD(id))
			if encErr != nil {
				http.Error(w, "ошибка шифрования",
					http.StatusInternalServerError)
				return
			}
			encrypted = ct
		}
		if err := store.UpdateModelProviderAPIKey(r.Context(),
			id, encrypted); err != nil {
			if errors.Is(err, storage.ErrModelProviderNotFound) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, "ошибка обновления",
				http.StatusInternalServerError)
			return
		}
		tryReloadRouter(r.Context(), reloadRouter, logger, "updateAPIKey")
		w.WriteHeader(http.StatusNoContent)
	}
}
