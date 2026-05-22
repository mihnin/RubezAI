// Команда rubezh — CLI-клиент к API «Рубежа».
//
// Использование (примеры):
//
//	rubezh login --role user                    # dev-вход (сохранить токен)
//	rubezh login --sso                          # вход через браузер (OIDC, K.2)
//	rubezh chat --provider deepseek-cloud --model deepseek-chat "Привет"
//	rubezh models list
//	rubezh models set-key NAME --key sk-...
//	rubezh docs upload ./contract.pdf
//	rubezh docs list
//	rubezh audit list --type chat_request
//	rubezh incidents list
//
// Адрес API настраивается через --api или env RUBEZH_API_URL (default
// http://localhost:8080). Токен — в ~/.rubezh/token (после login).
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	defaultAPI = "http://localhost:8080"
	tokenFile  = ".rubezh/token"
)

type cli struct {
	api   string
	token string
}

func main() {
	root := flag.NewFlagSet("rubezh", flag.ExitOnError)
	apiFlag := root.String("api", envOr("RUBEZH_API_URL", defaultAPI), "адрес API")
	root.Usage = func() {
		fmt.Fprintln(os.Stderr, "rubezh — CLI к Рубеж ИИ. Команды:")
		fmt.Fprintln(os.Stderr, "  login           вход по роли (dev-токен)")
		fmt.Fprintln(os.Stderr, "  chat MSG        отправить сообщение в чат")
		fmt.Fprintln(os.Stderr, "  models list     список LLM-провайдеров")
		fmt.Fprintln(os.Stderr, "  models set-key  установить API-ключ провайдеру")
		fmt.Fprintln(os.Stderr, "  docs upload F   загрузить документ")
		fmt.Fprintln(os.Stderr, "  docs list       список документов")
		fmt.Fprintln(os.Stderr, "  audit list      события аудита")
		fmt.Fprintln(os.Stderr, "  incidents list  инциденты")
	}
	if len(os.Args) < 2 {
		root.Usage()
		os.Exit(2)
	}
	// Глобальные флаги разбираем до подкоманды.
	args := os.Args[1:]
	for len(args) > 0 && strings.HasPrefix(args[0], "--api") {
		_ = root.Parse(args[:1])
		args = args[1:]
	}
	if len(args) == 0 {
		root.Usage()
		os.Exit(2)
	}
	c := &cli{api: *apiFlag, token: loadToken()}
	cmd, rest := args[0], args[1:]

	var err error
	switch cmd {
	case "login":
		err = c.cmdLogin(rest)
	case "chat":
		err = c.cmdChat(rest)
	case "models":
		err = c.cmdModels(rest)
	case "docs":
		err = c.cmdDocs(rest)
	case "audit":
		err = c.cmdAudit(rest)
	case "incidents":
		err = c.cmdIncidents(rest)
	case "help", "-h", "--help":
		root.Usage()
	default:
		fmt.Fprintf(os.Stderr, "неизвестная команда: %s\n", cmd)
		root.Usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "ошибка: %v\n", err)
		os.Exit(1)
	}
}

// ─── login ───────────────────────────────────────────────────────────────

func (c *cli) cmdLogin(args []string) error {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	role := fs.String("role", "user", "роль (user|security_officer|admin|...)")
	sso := fs.Bool("sso", false, "вход через браузер (OIDC, корп. учётка)")
	_ = fs.Parse(args)
	if *sso {
		return c.cmdLoginSSO()
	}
	body, _ := json.Marshal(map[string]string{"role": *role})
	resp, err := http.Post(
		c.api+"/api/auth/dev-login", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, bodyText(resp))
	}
	type loginResp struct {
		Token     string `json:"token"`
		Role      string `json:"role"`
		UserID    string `json:"user_id"`
		ExpiresAt string `json:"expires_at"`
	}
	var lr loginResp
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		return err
	}
	if err := saveToken(lr.Token); err != nil {
		return err
	}
	fmt.Printf("✓ вход как %s (user_id=%s, expires_at=%s)\n",
		lr.Role, lr.UserID[:8], lr.ExpiresAt)
	fmt.Printf("  токен сохранён в ~/%s\n", tokenFile)
	return nil
}

// cmdLoginSSO — браузерный OIDC-вход (K.2, RFC 8252 loopback). Поднимает
// локальный сервер на 127.0.0.1:<rand>, открывает браузер на login-эндпойнте
// «Рубежа» с cli_redirect на этот loopback, ждёт токен и сохраняет его.
func (c *cli) cmdLoginSSO() error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("loopback listen: %w", err)
	}
	defer ln.Close()
	redirect := fmt.Sprintf("http://%s/callback", ln.Addr().String())

	type result struct {
		token, role, errMsg string
	}
	done := make(chan result, 1)
	srv := &http.Server{Handler: http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/callback" {
				http.NotFound(w, r)
				return
			}
			q := r.URL.Query()
			res := result{token: q.Get("token"), role: q.Get("role"), errMsg: q.Get("error")}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if res.token != "" {
				_, _ = w.Write([]byte(
					"<h3>Вход выполнен. Можно закрыть вкладку и вернуться в терминал.</h3>"))
			} else {
				_, _ = w.Write([]byte("<h3>Ошибка входа: " + res.errMsg + "</h3>"))
			}
			done <- res
		})}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	loginURL := c.api + "/api/auth/oidc/login?cli_redirect=" + url.QueryEscape(redirect)
	fmt.Println("Открываю браузер для входа…")
	fmt.Println("Если не открылось — перейдите вручную:\n  " + loginURL)
	_ = openBrowser(loginURL)

	select {
	case res := <-done:
		if res.token == "" {
			return fmt.Errorf("вход не выполнен: %s", res.errMsg)
		}
		if err := saveToken(res.token); err != nil {
			return err
		}
		fmt.Printf("✓ вход выполнен (роль %s); токен сохранён в ~/%s\n",
			res.role, tokenFile)
		return nil
	case <-time.After(3 * time.Minute):
		return errors.New("таймаут ожидания входа (3 мин)")
	}
}

// openBrowser открывает URL в браузере ОС (кроссплатформенно).
func openBrowser(rawURL string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL).Start()
	case "darwin":
		return exec.Command("open", rawURL).Start()
	default:
		return exec.Command("xdg-open", rawURL).Start()
	}
}

// ─── chat (SSE) ──────────────────────────────────────────────────────────

func (c *cli) cmdChat(args []string) error {
	fs := flag.NewFlagSet("chat", flag.ExitOnError)
	provider := fs.String("provider", "deepseek-cloud", "имя провайдера")
	model := fs.String("model", "deepseek-chat", "имя модели у провайдера")
	sessionID := fs.String("session", "", "session_id (UUID; auto-генерируется при пустом)")
	_ = fs.Parse(args)
	if c.token == "" {
		return errors.New("требуется login")
	}
	if fs.NArg() == 0 {
		return errors.New("укажите сообщение: rubezh chat \"...\"")
	}
	if *sessionID == "" {
		// Auto-создаём сессию через POST /api/chat/sessions; backend требует
		// существующую session_id, иначе 404. CLI берёт ID из ответа.
		id, err := c.createSession("rubezh-cli")
		if err != nil {
			return fmt.Errorf("создание session_id: %w", err)
		}
		*sessionID = id
	}
	message := strings.Join(fs.Args(), " ")
	body, _ := json.Marshal(map[string]any{
		"session_id": *sessionID,
		"message":    message,
		"provider":   *provider,
		"model":      *model,
	})
	req, _ := http.NewRequest("POST", c.api+"/api/chat", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, bodyText(resp))
	}
	return streamSSE(resp.Body)
}

// streamSSE парсит RFC 6202 (event:/data:) и печатает в stdout.
func streamSSE(r io.Reader) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var name, data string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			dispatchSSE(name, data)
			name, data = "", ""
			continue
		}
		if strings.HasPrefix(line, "event: ") {
			name = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			data = strings.TrimPrefix(line, "data: ")
		}
	}
	return scanner.Err()
}

func dispatchSSE(name, data string) {
	if name == "" || data == "" {
		return
	}
	switch name {
	case "meta":
		var m struct {
			Decision  string   `json:"decision"`
			Reasons   []string `json:"reasons"`
			Provider  string   `json:"provider"`
			RequestID string   `json:"request_id"`
		}
		_ = json.Unmarshal([]byte(data), &m)
		fmt.Fprintf(os.Stderr, "[%s | decision=%s | req=%s]\n",
			m.Provider, m.Decision, m.RequestID)
		if len(m.Reasons) > 0 {
			fmt.Fprintf(os.Stderr, "  reasons: %s\n", strings.Join(m.Reasons, ", "))
		}
	case "delta":
		var d struct {
			Content string `json:"content"`
		}
		_ = json.Unmarshal([]byte(data), &d)
		fmt.Print(d.Content)
	case "done":
		fmt.Println()
	case "error":
		var e struct {
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
		}
		_ = json.Unmarshal([]byte(data), &e)
		fmt.Fprintf(os.Stderr, "[error: %s]\n", e.Message)
	}
}

// ─── models ──────────────────────────────────────────────────────────────

func (c *cli) cmdModels(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: rubezh models list | set-key NAME --key K")
	}
	switch args[0] {
	case "list":
		return c.modelsList()
	case "set-key":
		return c.modelsSetKey(args[1:])
	default:
		return fmt.Errorf("неизвестная подкоманда: %s", args[0])
	}
}

func (c *cli) modelsList() error {
	var list []struct {
		ID, Name, Adapter, Endpoint, TrustLevel string
		IsEnabled, HasAPIKey                    bool
	}
	type m struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Adapter    string `json:"adapter"`
		Endpoint   string `json:"endpoint"`
		TrustLevel string `json:"trust_level"`
		IsEnabled  bool   `json:"is_enabled"`
		HasAPIKey  bool   `json:"has_api_key"`
	}
	var ml []m
	if err := c.getJSON("/api/models", &ml); err != nil {
		return err
	}
	_ = list
	fmt.Printf("%-22s %-18s %-12s %-7s %-6s %s\n",
		"NAME", "ADAPTER", "TRUST", "ENABLED", "KEY", "ENDPOINT")
	for _, p := range ml {
		key := "—"
		if p.HasAPIKey {
			key = "✓"
		}
		on := "no"
		if p.IsEnabled {
			on = "yes"
		}
		fmt.Printf("%-22s %-18s %-12s %-7s %-6s %s\n",
			p.Name, p.Adapter, p.TrustLevel, on, key, p.Endpoint)
	}
	return nil
}

func (c *cli) modelsSetKey(args []string) error {
	fs := flag.NewFlagSet("models set-key", flag.ExitOnError)
	key := fs.String("key", "", "API-ключ (или $ENV)")
	_ = fs.Parse(args)
	if fs.NArg() == 0 {
		return errors.New("укажите имя провайдера: models set-key NAME --key K")
	}
	name := fs.Arg(0)
	if strings.HasPrefix(*key, "$") {
		*key = os.Getenv(strings.TrimPrefix(*key, "$"))
	}
	if *key == "" {
		return errors.New("--key обязателен (или задайте через ENV: --key '$DEEPSEEK_KEY')")
	}
	// Поиск ID по name
	var ml []struct {
		ID, Name string
	}
	type m struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	var raw []m
	if err := c.getJSON("/api/models", &raw); err != nil {
		return err
	}
	_ = ml
	var id string
	for _, p := range raw {
		if p.Name == name {
			id = p.ID
			break
		}
	}
	if id == "" {
		return fmt.Errorf("провайдер %q не найден", name)
	}
	body, _ := json.Marshal(map[string]string{"api_key": *key})
	return c.postNoBody("/api/models/"+id+"/api-key", body)
}

// ─── docs ────────────────────────────────────────────────────────────────

func (c *cli) cmdDocs(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: rubezh docs list | upload PATH")
	}
	switch args[0] {
	case "list":
		var d struct {
			Documents []struct {
				ID        string `json:"id"`
				Filename  string `json:"filename"`
				Status    string `json:"status"`
				SizeBytes *int64 `json:"size_bytes"`
			} `json:"documents"`
		}
		if err := c.getJSON("/api/documents", &d); err != nil {
			return err
		}
		fmt.Printf("%-38s %-12s %-10s %s\n", "ID", "STATUS", "SIZE", "FILENAME")
		for _, x := range d.Documents {
			sz := "—"
			if x.SizeBytes != nil {
				sz = fmt.Sprintf("%d", *x.SizeBytes)
			}
			fmt.Printf("%-38s %-12s %-10s %s\n", x.ID, x.Status, sz, x.Filename)
		}
		return nil
	case "upload":
		if len(args) < 2 {
			return errors.New("укажите путь: docs upload ./file.pdf")
		}
		return c.docUpload(args[1])
	default:
		return fmt.Errorf("неизвестная подкоманда: %s", args[0])
	}
}

func (c *cli) docUpload(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, f); err != nil {
		return err
	}
	_ = w.Close()
	req, _ := http.NewRequest("POST", c.api+"/api/documents", &buf)
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, bodyText(resp))
	}
	fmt.Println("✓ загружено")
	return nil
}

// ─── audit / incidents ───────────────────────────────────────────────────

func (c *cli) cmdAudit(args []string) error {
	if len(args) == 0 || args[0] != "list" {
		return errors.New("usage: rubezh audit list [--type TYPE]")
	}
	fs := flag.NewFlagSet("audit list", flag.ExitOnError)
	t := fs.String("type", "", "фильтр event_type")
	_ = fs.Parse(args[1:])
	path := "/api/audit-events"
	if *t != "" {
		path += "?event_type=" + url.QueryEscape(*t)
	}
	var a struct {
		Events []struct {
			CreatedAt      string `json:"created_at"`
			EventType      string `json:"event_type"`
			RiskLevel      string `json:"risk_level"`
			PolicyDecision string `json:"policy_decision"`
			HasLeak        bool   `json:"has_leak"`
		} `json:"events"`
	}
	if err := c.getJSON(path, &a); err != nil {
		return err
	}
	fmt.Printf("%-30s %-26s %-8s %-12s %s\n",
		"TIME", "EVENT", "RISK", "DECISION", "LEAK")
	for _, e := range a.Events {
		leak := ""
		if e.HasLeak {
			leak = "⚠"
		}
		fmt.Printf("%-30s %-26s %-8s %-12s %s\n",
			e.CreatedAt, e.EventType, e.RiskLevel, e.PolicyDecision, leak)
	}
	return nil
}

func (c *cli) cmdIncidents(args []string) error {
	if len(args) == 0 || args[0] != "list" {
		return errors.New("usage: rubezh incidents list")
	}
	var d struct {
		Incidents []struct {
			Severity string `json:"severity"`
			Status   string `json:"status"`
			Title    string `json:"title"`
			Trigger  string `json:"trigger"`
		} `json:"incidents"`
	}
	if err := c.getJSON("/api/incidents", &d); err != nil {
		return err
	}
	fmt.Printf("%-9s %-14s %-22s %s\n", "SEVERITY", "STATUS", "TRIGGER", "TITLE")
	for _, i := range d.Incidents {
		fmt.Printf("%-9s %-14s %-22s %s\n", i.Severity, i.Status, i.Trigger, i.Title)
	}
	return nil
}

// ─── HTTP helpers ────────────────────────────────────────────────────────

func (c *cli) getJSON(path string, out any) error {
	req, _ := http.NewRequest("GET", c.api+path, nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, bodyText(resp))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *cli) postNoBody(path string, body []byte) error {
	req, _ := http.NewRequest("POST", c.api+path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, bodyText(resp))
	}
	return nil
}

func bodyText(resp *http.Response) string {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return strings.TrimSpace(string(b))
}

// ─── token persistence ───────────────────────────────────────────────────

func tokenPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, tokenFile)
}

func loadToken() string {
	b, err := os.ReadFile(tokenPath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func saveToken(t string) error {
	p := tokenPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(t), 0o600)
}

// ─── utils ───────────────────────────────────────────────────────────────

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// createSession создаёт новую chat-сессию через API и возвращает её id.
// Используется CLI при первом chat-запросе без явного --session.
func (c *cli) createSession(title string) (string, error) {
	body, _ := json.Marshal(map[string]string{"title": title})
	req, _ := http.NewRequest("POST", c.api+"/api/chat/sessions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, bodyText(resp))
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.ID, nil
}
