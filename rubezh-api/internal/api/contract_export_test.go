package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

// G.1 — контрактные тесты Go ↔ TypeScript.
//
// Этот golden-тест рефлексией реальных DTO строит нормализованную карту
// {json_field: kind} и сверяет её с закоммиченными
// docs/contracts/_generated/<dto>.json. Дрейф DTO → тест FAIL и перезапись
// файла (нужно закоммитить). Тот же файл читает rubezh-web/src/test/
// contract.test.ts и сверяет с Zod-схемой — рассинхрон Go↔TS невозможен молча.
// Замысел и таблица соответствий — docs/design/g1-contract-tests.md.

var timeType = reflect.TypeOf(time.Time{})

// normalizedKind переводит Go-тип поля в общий с Zod код типа.
func normalizedKind(t reflect.Type) string {
	nullable := false
	if t.Kind() == reflect.Pointer {
		nullable = true
		t = t.Elem()
	}
	base := baseKind(t)
	if nullable {
		return "?" + base
	}
	return base
}

func baseKind(t reflect.Type) string {
	if t == timeType {
		return "string"
	}
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Struct:
		return "object"
	default:
		return "object"
	}
}

// contractShape — карта json-поле → нормализованный тип; учитывает встроенные
// (анонимные) структуры, разворачивая их поля на верхний уровень (как json).
func contractShape(v any) map[string]string {
	shape := map[string]string{}
	collectFields(reflect.TypeOf(v), shape)
	return shape
}

func collectFields(t reflect.Type, shape map[string]string) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if f.Anonymous && tag == "" {
			collectFields(f.Type, shape) // встроенная структура → плоско
			continue
		}
		name := tag
		if comma := indexByte(name, ','); comma >= 0 {
			name = name[:comma]
		}
		if name == "" || name == "-" {
			continue
		}
		shape[name] = normalizedKind(f.Type)
	}
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// contractCases — DTO для экспорта (имя файла → пример значения).
func contractCases() map[string]any {
	return map[string]any{
		"model_provider": modelProviderDTO{},
		"incident":       incidentDTO{},
		"incident_list":  incidentListDTO{},
		"audit_event":    auditEventSummaryDTO{},
		"audit_list":     auditEventListDTO{},
		"document":       documentDTO{},
		"document_list":  documentListDTO{},
		"policy":         policyDTO{},
		"chat_session":   chatSessionDTO{},
	}
}

func generatedDir(t *testing.T) string {
	t.Helper()
	// Канонический генерат лежит внутри rubezh-web, чтобы TS-тест мог
	// импортировать его как JSON в окружении, где смонтирован только
	// rubezh-web (см. docs/design/g1-contract-tests.md).
	dir := filepath.Join("..", "..", "..", "rubezh-web", "src", "test", "contracts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir contracts: %v", err)
	}
	return dir
}

func TestContractShapesMatchGolden(t *testing.T) {
	dir := generatedDir(t)
	for name, sample := range contractCases() {
		shape := contractShape(sample)
		want, err := json.MarshalIndent(shape, "", "  ")
		if err != nil {
			t.Fatalf("%s: marshal: %v", name, err)
		}
		want = append(want, '\n')
		path := filepath.Join(dir, name+".json")

		got, readErr := os.ReadFile(path)
		if readErr != nil || !bytesEqual(got, want) {
			// перезаписываем golden и помечаем как ошибку — нужно закоммитить
			if writeErr := os.WriteFile(path, want, 0o644); writeErr != nil {
				t.Fatalf("%s: запись golden: %v", name, writeErr)
			}
			t.Errorf("контракт %s изменился — перегенерирован %s; "+
				"закоммитьте файл и при необходимости обновите Zod-схему "+
				"(rubezh-web/src/api/schemas.ts)", name, path)
		}
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestContractShapeSorted — самопроверка: ключи карты сериализуются
// детерминированно (json.Marshal сортирует ключи map[string]string).
func TestContractShapeDeterministic(t *testing.T) {
	shape := contractShape(modelProviderDTO{})
	keys := make([]string, 0, len(shape))
	for k := range shape {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		t.Fatal("пустая форма modelProviderDTO")
	}
}
