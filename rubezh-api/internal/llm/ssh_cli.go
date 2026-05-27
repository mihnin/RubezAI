package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// SSHRunner — абстракция выполнения remote-команды через SSH-мост.
// Принимает providerArg (codex|claude|gemini|grok) и stdin-payload (JSON),
// возвращает stdout (JSON). Реализация в production — SSHExecRunner;
// в тестах — fakeSSHRunner. Не логирует prompt/stdout/stderr во вызывающем
// коде (это инвариант ИБ); если внутренний runner логирует — обязан
// маскировать содержимое.
type SSHRunner interface {
	Run(ctx context.Context, providerArg string, stdin []byte) ([]byte, error)
}

// validSSHProviderArg — белый список значений, передаваемых первым
// аргументом ai-bridge. Источник — ModelProvider.Endpoint. Защита от
// потенциальной command-injection: даже если БД получит чужое значение,
// провайдер не зарегистрируется (или Run вернёт ошибку).
//
// `grok` оставлен для обратной совместимости (старый alias); основной
// рабочий аргумент для Grok-провайдера — `grok-build`.
var validSSHProviderArg = map[string]bool{
	"codex":      true,
	"claude":     true,
	"gemini":     true,
	"grok":       true,
	"grok-build": true,
}

// IsValidSSHProviderArg — фасад над validSSHProviderArg для main/api.
func IsValidSSHProviderArg(arg string) bool { return validSSHProviderArg[arg] }

// sshBridgeRequest — payload, который пишется в stdin удалённой команды.
// Поля совпадают с контрактом deploy/ssh-bridge/README.md.
type sshBridgeRequest struct {
	Prompt    string `json:"prompt"`
	Model     string `json:"model"`
	SessionID string `json:"session_id,omitempty"`
}

// defaultSSHModelFor возвращает встроенный fallback-model для конкретного
// удалённого CLI. Это ПОСЛЕДНИЙ рубеж устойчивости (миграция 000019
// перенесла основное управление дефолтами в model_providers.default_model;
// adapter использует этот fallback только если БД-default пустой И клиент
// не прислал явный model). Список держим узким — только модели, реально
// принимаемые серверным CLI (подтверждено live smoke через bridge).
func defaultSSHModelFor(providerArg string) string {
	switch providerArg {
	case "codex":
		return "gpt-5.3-codex"
	case "claude":
		return "claude-opus-4-7"
	case "gemini":
		return "Gemini 3.5 Flash (High)"
	case "grok", "grok-build":
		return "grok-build"
	default:
		return ""
	}
}

func normalizeSSHModel(providerArg, model string) string {
	if model == "" {
		return defaultSSHModelFor(providerArg)
	}
	switch providerArg {
	case "codex":
		if model == "codex-cli" || model == "gpt-5-codex" {
			return defaultSSHModelFor(providerArg)
		}
	case "claude":
		if model == "claude-code-cli" {
			return defaultSSHModelFor(providerArg)
		}
	case "gemini":
		if model == "gemini-cli" || model == "gemini-2.5-pro" ||
			model == "gemini-3.5-flash" {
			return defaultSSHModelFor(providerArg)
		}
	case "grok", "grok-build":
		if model == "grok" || model == "grok-cli" {
			return defaultSSHModelFor(providerArg)
		}
	}
	return model
}

// sshBridgeResponse — ожидаемый JSON-ответ из stdout. Если bridge выдал
// невалидный JSON или ok=false — провайдер возвращает ошибку без утечки
// prompt/raw в текст ошибки и логи.
//
// Files — артефакты, созданные CLI на сервере в WORKSPACE (Codex/Claude/
// Gemini умеют генерировать xlsx/pdf/png и т. п.). Bridge возвращает их
// base64-encoded; adapter формирует Markdown-блок с data:-ссылками,
// который UI рендерит как download-chips. Размер ограничен сервером
// (см. deploy/ssh-bridge/README.md §Окружение `AI_BRIDGE_FILES_MAX_*`).
type sshBridgeResponse struct {
	OK       bool            `json:"ok"`
	Provider string          `json:"provider"`
	Model    string          `json:"model"`
	Content  string          `json:"content"`
	Error    string          `json:"error,omitempty"`
	Files    []sshBridgeFile `json:"files,omitempty"`
}

// sshBridgeFile — артефакт, возвращённый bridge. Поля совпадают с
// контрактом deploy/ssh-bridge/README.md.
type sshBridgeFile struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Mime   string `json:"mime"`
	Size   int    `json:"size"`
	Base64 string `json:"base64"`
}

// SSHCLIProvider — adapter ssh_cli: внешние LLM через CLI-bridge на
// удалённом Ubuntu-сервере (codex/claude/gemini/grok CLI уже залогинены
// серверной учёткой aiagent). API-ключи провайдеров не используются —
// аутентификация делается на сервере, репозиторий не хранит ни пароля,
// ни OAuth-кодов, ни приватных ключей.
//
// Pipeline вызова:
//  1. Собрать prompt из ChatMessage[] (sanitize/policy уже отработали выше).
//  2. JSON.Marshal({prompt, model, session_id?}) → stdin.
//  3. runner.Run(ctx, providerArg, stdin) → stdout.
//  4. JSON.Unmarshal(stdout) → ChatResponse.
//
// Все ошибки логируются без prompt/content — только provider/remote/error-kind.
type SSHCLIProvider struct {
	name        string
	providerArg string // codex|claude|gemini|grok (model_providers.endpoint)
	runner      SSHRunner
	logger      *slog.Logger
}

// NewSSHCLIProvider создаёт adapter ssh_cli. providerArg валидируется
// против белого списка (см. validSSHProviderArg); невалидный → паника
// на старте быстрее, чем тихий fallback. runner и logger обязательны.
func NewSSHCLIProvider(
	name, providerArg string, runner SSHRunner, logger *slog.Logger,
) *SSHCLIProvider {
	if runner == nil {
		panic("llm.NewSSHCLIProvider: runner is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if !validSSHProviderArg[providerArg] {
		panic(fmt.Sprintf(
			"llm.NewSSHCLIProvider: invalid providerArg %q "+
				"(expected codex|claude|gemini|grok)", providerArg))
	}
	return &SSHCLIProvider{
		name: name, providerArg: providerArg,
		runner: runner, logger: logger,
	}
}

// Name возвращает имя провайдера (как в БД).
func (p *SSHCLIProvider) Name() string { return p.name }

// Complete отправляет запрос в SSH-bridge и возвращает ChatResponse.
// Контракт ошибок: prompt/stdin/stdout НЕ попадают ни в error-text,
// ни в логи — только provider/remote/error-kind.
func (p *SSHCLIProvider) Complete(
	ctx context.Context, req ChatRequest,
) (ChatResponse, error) {
	if err := ctx.Err(); err != nil {
		return ChatResponse{}, err
	}
	prompt := assembleSSHPrompt(req.Messages)
	if prompt == "" {
		return ChatResponse{}, errors.New(
			"ssh_cli: пустой prompt (нет user-сообщений)")
	}
	model := normalizeSSHModel(p.providerArg, req.Model)
	payload, err := json.Marshal(sshBridgeRequest{
		Prompt: prompt, Model: model,
	})
	if err != nil {
		return ChatResponse{}, fmt.Errorf("ssh_cli: marshal: %w", err)
	}
	out, err := p.runner.Run(ctx, p.providerArg, payload)
	if err != nil {
		p.logger.Error("ssh_cli: bridge call failed",
			"provider", p.name, "remote", p.providerArg, "error", err)
		return ChatResponse{}, fmt.Errorf(
			"ssh_cli: %s: bridge call failed", p.name)
	}
	var parsed sshBridgeResponse
	if err := json.Unmarshal(out, &parsed); err != nil {
		p.logger.Error("ssh_cli: invalid bridge JSON",
			"provider", p.name, "remote", p.providerArg)
		return ChatResponse{}, fmt.Errorf(
			"ssh_cli: %s: invalid bridge response", p.name)
	}
	if !parsed.OK {
		// parsed.Error — это код ошибки bridge (напр. "remote_cli_failed"),
		// не raw-output. См. deploy/ssh-bridge/README.md §«Error codes».
		p.logger.Error("ssh_cli: bridge reported failure",
			"provider", p.name, "remote", p.providerArg,
			"bridge_error", parsed.Error)
		return ChatResponse{}, fmt.Errorf(
			"ssh_cli: %s: bridge returned ok=false", p.name)
	}
	if parsed.Content == "" && len(parsed.Files) == 0 {
		return ChatResponse{}, fmt.Errorf(
			"ssh_cli: %s: bridge вернул пустой content без файлов", p.name)
	}
	respModel := parsed.Model
	if respModel == "" {
		respModel = model
	}
	content := appendFilesToContent(parsed.Content, parsed.Files)
	return ChatResponse{Content: content, Model: respModel}, nil
}

// appendFilesToContent добавляет к текстовому ответу LLM Markdown-блок
// с data:-ссылками на файлы. Формат специально дискретен —
// `[📎 имя.xlsx](data:mime;base64,...)` — чтобы UI мог в MessageBubble
// извлечь имя+mime+base64 регуляркой и отрендерить download-chip, а не
// гонять огромный base64 через React-markdown как обычную ссылку.
func appendFilesToContent(content string, files []sshBridgeFile) string {
	if len(files) == 0 {
		return content
	}
	var b strings.Builder
	b.WriteString(content)
	if content != "" && !strings.HasSuffix(content, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("\n📎 Файлы:\n")
	for _, f := range files {
		if f.Base64 == "" || f.Name == "" {
			continue
		}
		name := safeAttachmentName(f.Name)
		if name == "" {
			continue
		}
		mime := f.Mime
		if mime == "" {
			mime = "application/octet-stream"
		}
		// Markdown-link с display-name. UI парсит data:-URL → download-chip.
		b.WriteString("- [📎 ")
		b.WriteString(name)
		b.WriteString("](data:")
		b.WriteString(mime)
		b.WriteString(";base64,")
		b.WriteString(f.Base64)
		b.WriteString(")\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func safeAttachmentName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"\r", " ",
		"\n", " ",
		"[", "_",
		"]", "_",
	)
	name = replacer.Replace(name)
	name = strings.Join(strings.Fields(name), " ")
	return strings.TrimSpace(name)
}

// assembleSSHPrompt склеивает Messages в один текстовый prompt, понятный
// CLI-инструментам (codex/claude/gemini/grok), которые принимают одну
// строку. Формат:
//
//	[system]\n<system text>\n\n[user]\n<user text>\n\n[assistant]\n... .
//
// system-блоки могут быть несколько (orchestrator добавляет summary-system
// и rag-system) — оба склеиваются. Возвращает "" если нет ни одного user.
//
// MINOR-m1 защита: внутри content экранируются разделители
// `[system]`/`[user]`/`[assistant]` в начале строки — иначе модель
// может интерпретировать пользовательский текст как новый role-блок
// (prompt-injection через формат). Аналогично escapeRAGContent.
func assembleSSHPrompt(messages []ChatMessage) string {
	hasUser := false
	var b strings.Builder
	for _, m := range messages {
		if m.Role == "user" {
			hasUser = true
		}
		role := m.Role
		if role == "" {
			role = "user"
		}
		b.WriteString("[")
		b.WriteString(role)
		b.WriteString("]\n")
		b.WriteString(escapeSSHRoleMarkers(m.Content))
		b.WriteString("\n\n")
	}
	if !hasUser {
		return ""
	}
	return strings.TrimRight(b.String(), "\n")
}

// escapeSSHRoleMarkers экранирует `[system]`/`[user]`/`[assistant]`
// в начале строки (защита от prompt-injection через формат-разделитель).
// Подмена `[` → `\\[` достаточно: модель не интерпретирует backslash-
// префиксы как role-маркеры. Применяется построчно.
func escapeSSHRoleMarkers(content string) string {
	if !strings.Contains(content, "[") {
		return content
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "[system]") ||
			strings.HasPrefix(line, "[user]") ||
			strings.HasPrefix(line, "[assistant]") {
			lines[i] = "\\" + line
		}
	}
	return strings.Join(lines, "\n")
}
