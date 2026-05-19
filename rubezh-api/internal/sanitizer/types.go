// Package sanitizer — HTTP-клиент сервиса rubezh-sanitizer.
// Типы согласованы с контрактом docs/contracts/sanitize.schema.json.
package sanitizer

// PreviewRequest — тело запроса POST /sanitize/preview
// (контракт sanitize.schema.json, $defs/SanitizeRequest).
type PreviewRequest struct {
	Text       string  `json:"text"`
	DocumentID *string `json:"document_id"`
	Context    string  `json:"context"`
}

// Entity — найденная sanitizer сущность ($defs/Entity).
// Сырое значение в контракте не передаётся — только псевдоним и SHA-256-хеш.
// Инвариант спана: 0 <= Start < End <= длины исходного текста (код-поинты).
type Entity struct {
	Type       string  `json:"type"`
	Category   string  `json:"category"`
	Start      int     `json:"start"`
	End        int     `json:"end"`
	Pseudonym  string  `json:"pseudonym"`
	RawHash    string  `json:"raw_hash"`
	Confidence float64 `json:"confidence"`
	Detector   string  `json:"detector"`
}

// Risk — агрегированная оценка риска ($defs/Risk).
type Risk struct {
	Score   float64  `json:"score"`
	Level   string   `json:"level"`
	Classes []string `json:"classes"`
}

// PreviewResponse — тело ответа /sanitize/preview ($defs/SanitizeResponse).
// MappingID для stateless-эндпойнта /sanitize/preview всегда nil.
type PreviewResponse struct {
	SanitizedText string   `json:"sanitized_text"`
	Entities      []Entity `json:"entities"`
	Risk          Risk     `json:"risk"`
	MappingID     *string  `json:"mapping_id"`
}
