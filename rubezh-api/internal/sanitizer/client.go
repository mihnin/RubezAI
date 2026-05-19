package sanitizer

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

// _previewTimeout — таймаут вызова /sanitize/preview.
const _previewTimeout = 30 * time.Second

// Client — HTTP-клиент сервиса rubezh-sanitizer.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient создаёт клиент к sanitizer по базовому URL.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: _previewTimeout},
	}
}

// Preview вызывает POST /sanitize/preview — обезличивание текста.
func (c *Client) Preview(
	ctx context.Context, req PreviewRequest,
) (PreviewResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return PreviewResponse{}, fmt.Errorf("sanitizer: сериализация запроса: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost, c.baseURL+"/sanitize/preview", bytes.NewReader(body))
	if err != nil {
		return PreviewResponse{}, fmt.Errorf("sanitizer: формирование запроса: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return PreviewResponse{}, fmt.Errorf("sanitizer: вызов /sanitize/preview: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return PreviewResponse{}, fmt.Errorf(
			"sanitizer: HTTP %d: %s", resp.StatusCode, snippet)
	}

	var parsed PreviewResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return PreviewResponse{}, fmt.Errorf("sanitizer: разбор ответа: %w", err)
	}
	return parsed, nil
}
