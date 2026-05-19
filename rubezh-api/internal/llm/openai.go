package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const _openAITimeout = 60 * time.Second

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIRequest struct {
	Model    string          `json:"model"`
	Messages []openAIMessage `json:"messages"`
	Stream   bool            `json:"stream"`
}

type openAIResponse struct {
	Choices []struct {
		Message openAIMessage `json:"message"`
	} `json:"choices"`
}

// OpenAIProvider — адаптер OpenAI-совместимого endpoint (vLLM, DeepSeek и т. п.).
type OpenAIProvider struct {
	name     string
	endpoint string
	apiKey   string
	client   *http.Client
}

// NewOpenAIProvider создаёт провайдера для OpenAI-совместимого endpoint.
func NewOpenAIProvider(name, endpoint, apiKey string) *OpenAIProvider {
	return &OpenAIProvider{
		name:     name,
		endpoint: endpoint,
		apiKey:   apiKey,
		client:   &http.Client{Timeout: _openAITimeout},
	}
}

// Name возвращает имя провайдера.
func (p *OpenAIProvider) Name() string { return p.name }

// Complete выполняет запрос к endpoint /chat/completions OpenAI-совместимого API.
func (p *OpenAIProvider) Complete(
	ctx context.Context, req ChatRequest,
) (ChatResponse, error) {
	messages := make([]openAIMessage, len(req.Messages))
	for i, m := range req.Messages {
		messages[i] = openAIMessage{Role: m.Role, Content: m.Content}
	}
	body, err := json.Marshal(openAIRequest{
		Model: req.Model, Messages: messages, Stream: false,
	})
	if err != nil {
		return ChatResponse{}, fmt.Errorf("llm: сериализация запроса: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost, p.endpoint+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, fmt.Errorf("llm: формирование запроса: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("llm: вызов провайдера %q: %w", p.name, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return ChatResponse{}, fmt.Errorf(
			"llm: провайдер %q вернул HTTP %d", p.name, resp.StatusCode)
	}
	var parsed openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return ChatResponse{}, fmt.Errorf("llm: разбор ответа: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return ChatResponse{}, fmt.Errorf(
			"llm: провайдер %q вернул пустой ответ", p.name)
	}
	return ChatResponse{
		Content: parsed.Choices[0].Message.Content,
		Model:   req.Model,
	}, nil
}
