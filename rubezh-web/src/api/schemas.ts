import { z } from "zod";

/**
 * Zod-схемы для рантайм-валидации ответов API.
 * Источник — реальные DTO в rubezh-api/internal/api/*.go
 * (сверено через e2e Итерации E.1/E.2).
 */

// ─── chat ────────────────────────────────────────────────────────────────
// SSE-формат backend: именованные события (event: name\n data: json\n\n),
// см. chat.schema.json и rubezh-api/internal/api/chat.go sseEventPayload.

export const ChatEntitySchema = z.object({
  type: z.string(),
  pseudonym: z.string(),
});

export const ChatMetaPayloadSchema = z.object({
  decision: z.string(),
  risk: z.object({
    level: z.string(),
    score: z.number(),
    classes: z.array(z.string()),
  }),
  provider: z.string(),
  reasons: z.array(z.string()),
  request_id: z.string(),
  entities: z.array(ChatEntitySchema).optional(),
});

export const ChatDeltaPayloadSchema = z.object({
  content: z.string(),
});

export const ChatDonePayloadSchema = z.object({
  request_id: z.string(),
  // id сообщения ассистента для reveal (J.2); старые потоки могут не слать
  assistant_message_id: z.string().optional().default(""),
});

// Ответ POST /api/chat/messages/{id}/reveal (J.2).
export const RevealSchema = z.object({
  revealed_text: z.string(),
});

// Ответ POST /api/chat/preview (J.1): обезличенный текст + сущности + риск.
export const ChatPreviewSchema = z.object({
  preview_token: z.string(),
  sanitized_text: z.string(),
  entities: z.array(
    z.object({
      type: z.string(),
      category: z.string(),
      pseudonym: z.string(),
      confidence: z.number(),
      detector: z.string(),
    }),
  ),
  risk: z.object({
    level: z.string(),
    score: z.number(),
    classes: z.array(z.string()),
  }),
});
export type ChatPreview = z.infer<typeof ChatPreviewSchema>;

// Персональный ключ провайдера (L) — без самого ключа.
export const UserCredentialSchema = z.object({
  id: z.string(),
  provider_id: z.string(),
  provider_name: z.string(),
  label: z.string().nullable(),
  is_enabled: z.boolean(),
  has_key: z.boolean(),
  created_at: z.string(),
  updated_at: z.string(),
  last_used_at: z.string().nullable(),
});
export const UserCredentialListSchema = z.array(UserCredentialSchema);
export type UserCredential = z.infer<typeof UserCredentialSchema>;

export const ChatErrorPayloadSchema = z.object({
  message: z.string(),
  request_id: z.string(),
});

// Нормализованный фронтовый ChatEvent — discriminated по type,
// формируется из backend SSE-events в sse.ts.
export type ChatEvent =
  | { type: "meta"; payload: z.infer<typeof ChatMetaPayloadSchema> }
  | { type: "delta"; payload: z.infer<typeof ChatDeltaPayloadSchema> }
  | { type: "done"; payload: z.infer<typeof ChatDonePayloadSchema> }
  | { type: "error"; payload: z.infer<typeof ChatErrorPayloadSchema> };

export type ChatEntity = z.infer<typeof ChatEntitySchema>;

// ─── documents ───────────────────────────────────────────────────────────

export const DocumentSchema = z.object({
  id: z.string().uuid(),
  owner_id: z.string().uuid(),
  filename: z.string(),
  content_type: z.string().nullable(),
  size_bytes: z.number().int().nonnegative().nullable(),
  status: z.string(),
  phase: z.string().nullable(),
  error: z.string().nullable(),
  processing_attempts: z.number().int().nonnegative(),
  processing_started_at: z.string().nullable(),
  created_at: z.string(),
  updated_at: z.string(),
});

export const DocumentListSchema = z.object({
  documents: z.array(DocumentSchema),
});

export type Document = z.infer<typeof DocumentSchema>;

// ─── policies ────────────────────────────────────────────────────────────

export const PolicySchema = z.object({
  id: z.string().uuid(),
  name: z.string(),
  description: z.string(),
  is_active: z.boolean(),
  current_version: z.number().int(),
  created_at: z.string(),
  updated_at: z.string(),
});

export const PolicyListSchema = z.array(PolicySchema);

export type Policy = z.infer<typeof PolicySchema>;

// ─── models ──────────────────────────────────────────────────────────────

export const ModelSchema = z.object({
  id: z.string().uuid(),
  name: z.string(),
  trust_level: z.string(),
  adapter: z.string(),
  endpoint: z.string(),
  max_tokens: z.number().int().nullable(),
  rate_limit_per_min: z.number().int().nullable(),
  is_enabled: z.boolean(),
  has_api_key: z.boolean(),
  created_at: z.string(),
  updated_at: z.string(),
});

export const ModelListSchema = z.array(ModelSchema);

export type Model = z.infer<typeof ModelSchema>;

// ─── audit ───────────────────────────────────────────────────────────────

export const AuditEventSchema = z.object({
  id: z.string().uuid(),
  created_at: z.string(),
  user_id: z.string(),
  event_type: z.string(),
  model_provider_id: z.string().nullable(),
  risk_level: z.string().nullable(),
  risk_classes: z.array(z.string()),
  policy_decision: z.string().nullable(),
  request_id: z.string().nullable(),
  has_leak: z.boolean(),
});

export const AuditListSchema = z.object({
  events: z.array(AuditEventSchema),
  next_cursor: z.string().nullable(),
});

export type AuditEvent = z.infer<typeof AuditEventSchema>;

// ─── incidents ───────────────────────────────────────────────────────────

export const IncidentSchema = z.object({
  id: z.string().uuid(),
  audit_event_id: z.string().nullable(),
  user_id: z.string().nullable(),
  reporter_id: z.string().nullable(),
  assignee_id: z.string().nullable(),
  severity: z.enum(["low", "medium", "high", "critical"]),
  status: z.enum(["open", "investigating", "resolved", "false_positive"]),
  trigger: z.string().nullable(),
  title: z.string(),
  summary: z.string().nullable(),
  resolution: z.string().nullable(),
  closed_at: z.string().nullable(),
  created_at: z.string(),
  updated_at: z.string(), // ETag-значение для optimistic concurrency (F1)
});

export const IncidentListSchema = z.object({
  incidents: z.array(IncidentSchema),
  next_cursor: z.string().nullable(),
});

export type Incident = z.infer<typeof IncidentSchema>;

// ─── chat sessions ───────────────────────────────────────────────────────

export const ChatSessionSchema = z.object({
  id: z.string().uuid(),
  user_id: z.string().uuid(),
  title: z.string().nullable(),
  created_at: z.string(),
  updated_at: z.string(),
});

export type ChatSession = z.infer<typeof ChatSessionSchema>;

// ─── auth ────────────────────────────────────────────────────────────────

export const DevLoginResponseSchema = z.object({
  token: z.string(),
  role: z.string(),
  user_id: z.string(),
  expires_at: z.string(),
});

export type DevLoginResponse = z.infer<typeof DevLoginResponseSchema>;
