# G.1 — Контрактные тесты Go ↔ TypeScript

## Проблема

В Итерации E.2 нашли 4 расхождения между Zod-схемами
(`rubezh-web/src/api/schemas.ts`) и Go-DTO (`rubezh-api/internal/api/*.go`):
`items` vs `documents`, `in_progress` vs `investigating` и др. Изменение
Go-DTO не ломает компиляцию фронтенда — только рантайм через `Zod.parse()`.
`schemas.test.ts` проверяет Zod вручную написанными примерами — их легко
забыть обновить.

## Решение

Двухсторонний автоматический контракт через **нормализованный экспорт формы**:

1. **Go (golden-тест)** `internal/api/contract_export_test.go` рефлексией
   реальных DTO строит карту `{json_field: kind}` и сверяет с
   закоммиченными `docs/contracts/_generated/<dto>.json`. Дрейф DTO →
   тест FAIL с требованием перегенерировать и закоммитить файл.
2. **TS** `rubezh-web/src/test/contract.test.ts` читает те же
   `_generated/*.json`, извлекает форму соответствующей Zod-схемы через
   `_def` и сверяет **множество полей** и **нормализованный тип** каждого.
   Расхождение (лишнее/пропущенное поле, смена типа/nullability) → FAIL.

Так изменение Go-DTO ломает либо Go-golden (если не перегенерировали), либо
TS-контракт (если Zod не обновили) — рассинхрон становится невозможен молча.

## Нормализованные типы (общий язык Go ↔ Zod)

| Код | Go | Zod |
|-----|----|----|
| `string` | `string`, `time.Time` | `ZodString`, `ZodEnum`, `ZodLiteral(string)` |
| `number` | `int*`, `uint*`, `float*` | `ZodNumber` |
| `boolean` | `bool` | `ZodBoolean` |
| `array` | срез/массив | `ZodArray` |
| `object` | вложенная структура | `ZodObject` |
| `?<код>` | `*T` (указатель) | `ZodNullable(<код>)` |

`ZodOptional`/`ZodDefault` разворачиваются до внутреннего типа.

## Соответствие DTO ↔ Zod

| Go DTO | Zod-схема | Файл |
|--------|-----------|------|
| `modelProviderDTO` | `ModelSchema` | `model_provider.json` |
| `incidentDTO` | `IncidentSchema` | `incident.json` |
| `incidentListDTO` | `IncidentListSchema` | `incident_list.json` |
| `auditEventSummaryDTO` | `AuditEventSchema` | `audit_event.json` |
| `auditEventListDTO` | `AuditListSchema` | `audit_list.json` |
| `documentDTO` | `DocumentSchema` | `document.json` |
| `documentListDTO` | `DocumentListSchema` | `document_list.json` |
| `policyDTO` | `PolicySchema` | `policy.json` |
| `chatSessionDTO` | `ChatSessionSchema` | `chat_session.json` |

Списки `policies`/`models` — голые массивы (`z.array`), сверяются через
схему элемента (`PolicySchema`/`ModelSchema`).

## Процесс синхронизации (README)

При изменении любого DTO: `go test ./internal/api/` перегенерирует
`docs/contracts/_generated/*.json` (при дрейфе тест упадёт и перезапишет
файл) → закоммитить файл → обновить Zod-схему до зелёного
`npm test`. CI гоняет оба шага.
