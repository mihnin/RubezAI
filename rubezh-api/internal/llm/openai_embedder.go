package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAICompatibleEmbedder — embedder через OpenAI-совместимый endpoint
// `/v1/embeddings`. Покрывает LM Studio (`http://172.27.48.1:1234`),
// vLLM, Ollama, любой провайдер с эквивалентным API.
//
// План iteration-11-rag.md §Р2: размерность фиксирована EmbeddingDim
// (1024); провайдер ОБЯЗАН вернуть вектор именно этой длины — иначе
// fail-closed (ошибка с упоминанием "dim"). Никаких normalization /
// truncation: маскирование dim mismatch ломает cosine-сравнимость
// query-вектора и doc-векторов.
type OpenAICompatibleEmbedder struct {
	endpoint string // base URL без /v1/embeddings, БЕЗ trailing slash
	model    string // имя модели для тела запроса и колонки embeddings.model
	apiKey   string // пустой → Authorization не отправляется (local LM Studio)
	client   *http.Client
}

// NewOpenAICompatibleEmbedder — конструктор. endpoint — base URL
// (например, `http://172.27.48.1:1234`); trailing slash допустим,
// нормализуется. timeout — HTTP-deadline на один Embed-вызов;
// контекст из Embed дополнительно срезает по своему deadline.
func NewOpenAICompatibleEmbedder(
	endpoint, model, apiKey string, timeout time.Duration,
) *OpenAICompatibleEmbedder {
	return &OpenAICompatibleEmbedder{
		endpoint: strings.TrimRight(endpoint, "/"),
		model:    model,
		apiKey:   apiKey,
		client:   &http.Client{Timeout: timeout},
	}
}

// Name возвращает имя модели — пишется в колонку `embeddings.model`,
// используется `SearchChunks` для embedder-name guard (§Р9 плана).
func (e *OpenAICompatibleEmbedder) Name() string { return e.model }

// Dim возвращает фиксированную EmbeddingDim — гарантия схемы.
func (e *OpenAICompatibleEmbedder) Dim() int { return EmbeddingDim }

// embeddingsRequest — тело POST /v1/embeddings.
type embeddingsRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

// embeddingsResponse — частичный парсер ответа (нам нужны только
// data[0].embedding, остальные поля игнорируются).
type embeddingsResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// Embed выполняет POST /v1/embeddings и возвращает вектор для текста.
// Fail-closed по любой причине: HTTP-ошибка, пустой data,
// dim ≠ EmbeddingDim — всегда возвращает (nil, err). Никаких partial
// результатов и никакого fallback (иначе порча БД).
func (e *OpenAICompatibleEmbedder) Embed(
	ctx context.Context, text string,
) ([]float32, error) {
	body, err := json.Marshal(embeddingsRequest{Model: e.model, Input: text})
	if err != nil {
		return nil, fmt.Errorf("openai_embedder: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		e.endpoint+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai_embedder: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai_embedder: do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		// Читаем небольшой кусок body для диагностики, но не логируем
		// (может содержать echo текста запроса с PII).
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("openai_embedder: HTTP %d: %s",
			resp.StatusCode, strings.TrimSpace(string(preview)))
	}

	var parsed embeddingsResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("openai_embedder: decode: %w", err)
	}
	if len(parsed.Data) == 0 {
		return nil, fmt.Errorf("openai_embedder: пустой data в ответе")
	}
	vec := parsed.Data[0].Embedding
	if len(vec) != EmbeddingDim {
		return nil, fmt.Errorf(
			"openai_embedder: dim mismatch (got %d, expected %d) — "+
				"провайдер %q возвращает неподходящую модель",
			len(vec), EmbeddingDim, e.model)
	}
	return vec, nil
}
