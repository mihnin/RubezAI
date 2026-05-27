package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rubezh-ai/rubezh-api/internal/llm"
	"github.com/rubezh-ai/rubezh-api/internal/sanitizer"
)

const _defaultModelReviewRounds = 3
const _maxModelReviewRounds = 5

type reviewVerdict struct {
	OK      bool     `json:"ok"`
	Issues  []string `json:"issues"`
	Comment string   `json:"comment,omitempty"`
}

type reviewFinding struct {
	Provider string
	Issues   []string
}

func shouldRunModelReview(req Request) bool {
	return req.Review != nil && req.Review.Enabled &&
		len(req.Review.Providers) > 0
}

// runModelReview запускает review-loop. systemPrompt — уже masked
// инструкция для primary (для revision-вызовов); reviewSystemPrompts —
// map name→masked для каждого ревизора. Оба прошли sanitize в Prepare
// (W1.1 P1-фикс).
func (o *Orchestrator) runModelReview(
	ctx context.Context, req Request, preview sanitizer.PreviewResponse,
	pmap PseudonymMap, act action, draft string,
	systemPrompt string, reviewSystemPrompts map[string]string,
	sink EventSink,
) (string, error) {
	if !shouldRunModelReview(req) {
		return draft, nil
	}
	draftText, attachments := splitModelAttachmentBlock(pmap.Remask(draft))
	sourceText := reviewSourceText(req, preview, pmap, act)
	// W2.2: ревизоры теперь видят файлы-артефакты. Для plain-text
	// форматов (csv, txt, md, json...) — содержимое (до 2KB);
	// для бинарных (xlsx, pdf, png) — только metadata.
	//
	// W2.2 MJ-1 (security): attachment.TextPreview может содержать
	// raw PII из исходного документа (если pmap не покрывает все
	// токены — например, sanitize документа произошёл отдельно при
	// worker-индексации). Прогоняем preview через pmap.Remask ещё раз
	// перед передачей ревизору — это re-маскирует raw, известные pmap'у,
	// и не даёт PII улететь во внешнюю модель.
	atts := parseAttachmentBlock(attachments)
	for i := range atts {
		if atts[i].TextPreview != "" {
			atts[i].TextPreview = pmap.Remask(atts[i].TextPreview)
		}
	}
	attachmentsForReviewers := describeAttachmentsForReviewer(atts)
	maxRounds := reviewMaxRounds(req.Review)
	if err := emitStatus(sink, req, "review_started",
		fmt.Sprintf("Серверная ревизия: проверяющих моделей %d, циклов до %d",
			len(req.Review.Providers), maxRounds)); err != nil {
		return "", err
	}
	current := strings.TrimSpace(draftText)
	for round := 1; round <= maxRounds; round++ {
		if err := emitStatus(sink, req, "review_round",
			fmt.Sprintf("Раунд ревизии %d/%d: проверяю черновик",
				round, maxRounds)); err != nil {
			return "", err
		}
		findings := make([]reviewFinding, 0, len(req.Review.Providers))
		for i, reviewer := range req.Review.Providers {
			if err := emitStatus(sink, req, "review_call",
				fmt.Sprintf("Раунд %d/%d, ревизор %d/%d: %s",
					round, maxRounds, i+1, len(req.Review.Providers),
					reviewer.Name)); err != nil {
				return "", err
			}
			// W1.1: reviewer.SystemPrompt уже не используем — берём
			// masked-версию из Prepare. Пусто/нет в map → ""
			// (buildReviewMessages добавит свой default guardrail).
			revPrompt := reviewSystemPrompts[reviewer.Name]
			// W2.2: к sourceText дописываем описание файлов-артефактов
			// от primary, чтобы ревизор оценил И текст, И артефакты.
			reviewerSource := sourceText
			if attachmentsForReviewers != "" {
				reviewerSource = sourceText + "\n\n" + attachmentsForReviewers
			}
			resp, err := o.llm.Complete(ctx, reviewer.Name, llm.ChatRequest{
				Model: reviewer.Model,
				Messages: buildReviewMessages(
					reviewerSource, current, revPrompt),
			})
			if err != nil {
				return "", fmt.Errorf("review provider %s: %w",
					reviewer.Name, err)
			}
			verdict := parseReviewVerdict(pmap.Remask(resp.Content))
			if !verdict.OK {
				findings = append(findings, reviewFinding{
					Provider: reviewer.Name,
					Issues:   verdict.Issues,
				})
			}
			if err := emitStatus(sink, req, "review_done",
				reviewDoneMessage(reviewer.Name, verdict)); err != nil {
				return "", err
			}
		}
		if len(findings) == 0 {
			if err := emitStatus(sink, req, "review_complete",
				fmt.Sprintf("Ревизия принята: все проверяющие модели ответили OK на раунде %d",
					round)); err != nil {
				return "", err
			}
			return joinModelAttachmentBlock(current, attachments), nil
		}
		if round == maxRounds {
			if err := emitStatus(sink, req, "review_fallback",
				fmt.Sprintf("Лимит ревизии исчерпан: отдаю последний вариант, осталось замечаний %d",
					countReviewIssues(findings))); err != nil {
				return "", err
			}
			return joinModelAttachmentBlock(
				fallbackReviewText(current, maxRounds, findings),
				attachments,
			), nil
		}
		if err := emitStatus(sink, req, "review_revise",
			fmt.Sprintf("Раунд %d/%d: основная модель исправляет замечаний %d",
				round, maxRounds, countReviewIssues(findings))); err != nil {
			return "", err
		}
		// systemPrompt — masked-версия (W1.1); raw req.SystemPrompt
		// сюда не передавать.
		resp, err := o.llm.Complete(ctx, req.Provider, llm.ChatRequest{
			Model: req.Model,
			Messages: buildRevisionMessages(
				sourceText, current, findings, systemPrompt),
		})
		if err != nil {
			return "", fmt.Errorf("primary revision provider %s: %w",
				req.Provider, err)
		}
		nextText, nextAttachments := splitModelAttachmentBlock(
			pmap.Remask(resp.Content))
		nextText = strings.TrimSpace(nextText)
		if nextText == "" {
			return "", fmt.Errorf("primary revision provider %s returned empty content",
				req.Provider)
		}
		current = nextText
		// W2.2: при revision attachments ВСЕГДА заменяем (primary мог
		// сознательно убрать/перегенерить файлы). Раньше пустой
		// nextAttachments оставлял старые — ревизоры в следующем раунде
		// видели бы устаревшие данные.
		attachments = nextAttachments
		nextAtts := parseAttachmentBlock(attachments)
		for i := range nextAtts {
			if nextAtts[i].TextPreview != "" {
				nextAtts[i].TextPreview = pmap.Remask(nextAtts[i].TextPreview)
			}
		}
		attachmentsForReviewers = describeAttachmentsForReviewer(nextAtts)
		if err := emitStatus(sink, req, "review_revised",
			fmt.Sprintf("Раунд %d/%d: основная модель вернула доработанную версию",
				round, maxRounds)); err != nil {
			return "", err
		}
	}
	return joinModelAttachmentBlock(current, attachments), nil
}

func reviewSourceText(
	_ Request, preview sanitizer.PreviewResponse, pmap PseudonymMap, act action,
) string {
	// Для ревизоров всегда выбираем обезличенный вариант, если sanitizer
	// нашёл сущности. Так черновик не надо гонять обратно через клиент для
	// повторного обезличивания, и внешние проверяющие не видят raw-значения.
	if pmap.Len() > 0 {
		return preview.SanitizedText
	}
	return act.sendText
}

func buildReviewMessages(
	sourceText, draft, systemPrompt string,
) []llm.ChatMessage {
	system := strings.TrimSpace(systemPrompt)
	if system == "" {
		system = defaultReviewSystemPrompt()
	} else {
		system += "\n\nОбязательные ограничения платформы: " +
			reviewGuardrails()
	}
	return []llm.ChatMessage{
		{
			Role:    "system",
			Content: system,
		},
		{
			Role: "user",
			Content: fmt.Sprintf(
				"Обезличенный запрос пользователя:\n%s\n\nЧерновик ответа:\n%s",
				sourceText, draft),
		},
	}
}

func defaultReviewSystemPrompt() string {
	return "Ты модель-ревизор. Проверь черновик ответа другой " +
		"модели на полноту, точность, противоречия и безопасность. " +
		reviewGuardrails()
}

func reviewGuardrails() string {
	return "Работай только с обезличенными данными и псевдонимами. " +
		"Не раскрывай и не запрашивай реальные ПДн. Верни строго JSON без Markdown: " +
		"{\"ok\":true,\"issues\":[]} если черновик можно отдавать пользователю, " +
		"или {\"ok\":false,\"issues\":[\"короткое замечание\"]} если нужны правки. " +
		"Не возвращай финальный текст ответа, только вердикт."
}

func buildRevisionMessages(
	sourceText, draft string, findings []reviewFinding, systemPrompt string,
) []llm.ChatMessage {
	system := strings.TrimSpace(systemPrompt)
	if system == "" {
		system = "Ты основная модель. Доработай черновик ответа по замечаниям ревизоров."
	}
	system += "\n\nОбязательные ограничения платформы: работай только с " +
		"обезличенными данными и псевдонимами. Верни только финальную версию " +
		"ответа для пользователя: без комментариев о ревизии, без оценок и " +
		"без служебного JSON."
	return []llm.ChatMessage{
		{Role: "system", Content: system},
		{
			Role: "user",
			Content: fmt.Sprintf(
				"Обезличенный запрос пользователя:\n%s\n\nТекущий черновик ответа:\n%s\n\nЗамечания ревизоров:\n%s",
				sourceText, draft, formatReviewFindings(findings)),
		},
	}
}

func reviewMaxRounds(params *ReviewParams) int {
	if params == nil || params.MaxRounds <= 0 {
		return _defaultModelReviewRounds
	}
	if params.MaxRounds > _maxModelReviewRounds {
		return _maxModelReviewRounds
	}
	return params.MaxRounds
}

func parseReviewVerdict(content string) reviewVerdict {
	clean := strings.TrimSpace(stripJSONFence(content))
	if obj := extractJSONObject(clean); obj != "" {
		var v reviewVerdict
		if err := json.Unmarshal([]byte(obj), &v); err == nil {
			return normalizeReviewVerdict(v)
		}
	}
	lower := strings.ToLower(strings.Trim(clean, " \t\r\n.!?"))
	if lower == "ok" || lower == "ок" || lower == "okay" {
		return reviewVerdict{OK: true, Issues: []string{}}
	}
	if clean == "" {
		clean = "ревизор не вернул JSON-вердикт"
	}
	return reviewVerdict{OK: false, Issues: []string{trimIssue(clean)}}
}

func normalizeReviewVerdict(v reviewVerdict) reviewVerdict {
	if v.OK {
		return reviewVerdict{OK: true, Issues: []string{}}
	}
	issues := make([]string, 0, len(v.Issues)+1)
	for _, issue := range v.Issues {
		issue = strings.TrimSpace(issue)
		if issue != "" {
			issues = append(issues, trimIssue(issue))
		}
	}
	if len(issues) == 0 && strings.TrimSpace(v.Comment) != "" {
		issues = append(issues, trimIssue(v.Comment))
	}
	if len(issues) == 0 {
		issues = append(issues, "ревизор не подтвердил ответ и не перечислил замечания")
	}
	return reviewVerdict{OK: false, Issues: issues}
}

func stripJSONFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) >= 2 {
		lines = lines[1:]
	}
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
		lines = lines[:len(lines)-1]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func extractJSONObject(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return ""
	}
	return s[start : end+1]
}

func reviewDoneMessage(provider string, verdict reviewVerdict) string {
	if verdict.OK {
		return fmt.Sprintf("Ревизор %s: OK", provider)
	}
	return fmt.Sprintf("Ревизор %s: замечаний %d",
		provider, len(verdict.Issues))
}

func countReviewIssues(findings []reviewFinding) int {
	total := 0
	for _, f := range findings {
		total += len(f.Issues)
	}
	return total
}

func formatReviewFindings(findings []reviewFinding) string {
	var b strings.Builder
	for _, f := range findings {
		for _, issue := range f.Issues {
			if strings.TrimSpace(issue) == "" {
				continue
			}
			fmt.Fprintf(&b, "- %s: %s\n", f.Provider, trimIssue(issue))
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "- замечания не распознаны"
	}
	return out
}

func fallbackReviewText(
	current string, maxRounds int, findings []reviewFinding,
) string {
	return fmt.Sprintf(
		"Вот что получилось после всех циклов ревизии (%d/%d). "+
			"Проверяющие модели всё ещё оставили замечания:\n\n%s\n\n%s",
		maxRounds, maxRounds, formatReviewFindings(findings),
		strings.TrimSpace(current),
	)
}

func trimIssue(issue string) string {
	issue = strings.TrimSpace(issue)
	const maxRunes = 900
	if len([]rune(issue)) <= maxRunes {
		return issue
	}
	r := []rune(issue)
	return strings.TrimSpace(string(r[:maxRunes])) + "..."
}

func splitModelAttachmentBlock(content string) (text, attachments string) {
	marker := "\n📎 Файлы:"
	if idx := strings.Index(content, marker); idx >= 0 {
		return strings.TrimSpace(content[:idx]), content[idx:]
	}
	prefix := "📎 Файлы:"
	if strings.HasPrefix(content, prefix) {
		return "", content
	}
	return content, ""
}

// reviewAttachment — метаданные одного файла-артефакта для review-loop'а
// (W2.2). Не содержит base64-payload (для бинарных) — у ревизора есть
// только имя/mime/размер и текстовый preview (для plain-text форматов).
type reviewAttachment struct {
	Name        string
	Mime        string
	SizeBytes   int
	TextPreview string // первые ≤2KB декодированного текста для plain-формата
}

// parseAttachmentBlock разбирает «📎 Файлы:» блок Markdown-link'ов
// формата `[📎 имя](data:mime;base64,...)`. Возвращает []reviewAttachment
// для prompt-инъекции к ревизорам.
//
// Для plain-text форматов (csv/txt/md/json/yaml/sql/xml/html и
// text/*) base64 декодируется и первые ≤2048 рун кладутся в TextPreview.
// Для бинарных (xlsx/pdf/png) preview пустой — ревизор видит только
// метаданные «у ответа есть файл X.xlsx (12 KB)».
func parseAttachmentBlock(block string) []reviewAttachment {
	if block == "" {
		return nil
	}
	// MN-3: размер блока > лимита — пропускаем (regex по гигантской
	// строке = CPU-spike, а preview всё равно обрезается до 2KB).
	if len(block) > _maxAttachmentBlockBytes {
		return nil
	}
	out := []reviewAttachment{}
	// Линии вида `- [📎 имя](data:mime;base64,...)`.
	re := attachmentLinkRegexp()
	for _, m := range re.FindAllStringSubmatch(block, -1) {
		if len(m) < 4 {
			continue
		}
		name := strings.TrimSpace(m[1])
		mime := strings.TrimSpace(m[2])
		b64 := strings.TrimSpace(m[3])
		size := approxBase64BytesLen(b64)
		preview := ""
		if isTextMimeForReview(mime, name) {
			if raw, err := base64DecodeForReview(b64); err == nil {
				preview = limitRunes(string(raw), 2048)
			}
		}
		out = append(out, reviewAttachment{
			Name: name, Mime: mime,
			SizeBytes: size, TextPreview: preview,
		})
	}
	return out
}

// describeAttachmentsForReviewer — текстовое представление файлов для
// LLM-ревизора. Plain-text файлы отдаёт preview-блоком, бинарные —
// только metadata-строкой. Возвращает пустую строку если файлов нет.
func describeAttachmentsForReviewer(atts []reviewAttachment) string {
	if len(atts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Файлы-артефакты от основной модели:\n")
	for _, a := range atts {
		fmt.Fprintf(&b, "- %s (%s, %d B)\n", a.Name, a.Mime, a.SizeBytes)
		if a.TextPreview != "" {
			b.WriteString("  --- содержимое (первые ≤2KB) ---\n")
			for _, line := range strings.Split(a.TextPreview, "\n") {
				b.WriteString("  ")
				b.WriteString(line)
				b.WriteString("\n")
			}
			b.WriteString("  --- конец содержимого ---\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func joinModelAttachmentBlock(text, attachments string) string {
	text = strings.TrimSpace(text)
	if strings.TrimSpace(attachments) == "" {
		return text
	}
	if text == "" {
		return strings.TrimSpace(attachments)
	}
	return text + "\n" + strings.TrimSpace(attachments)
}
