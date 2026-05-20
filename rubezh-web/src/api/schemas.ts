import { z } from "zod";

/**
 * Zod-схемы для рантайм-валидации ответов API.
 * Источник — docs/contracts/*.schema.json.
 *
 * Любой ответ от rubezh-api проходит через .parse(),
 * чтобы UI не доверял типу без проверки и падал детерминированно
 * при расхождении контракта.
 */

// ─── chat ────────────────────────────────────────────────────────────────

export const ChatEntitySchema = z.object({
  type: z.string(),
  pseudonym: z.string(),
});

export const ChatDeltaEventSchema = z.object({
  type: z.literal("delta"),
  text: z.string(),
});

export const ChatDecisionEventSchema = z.object({
  type: z.literal("decision"),
  decision: z.enum([
    "allow_raw",
    "allow_masked",
    "allow_summary_only",
    "deny",
    "escalate",
  ]),
  entities: z.array(ChatEntitySchema).default([]),
});

export const ChatErrorEventSchema = z.object({
  type: z.literal("error"),
  message: z.string(),
  request_id: z.string().optional(),
});

export const ChatDoneEventSchema = z.object({
  type: z.literal("done"),
});

export const ChatEventSchema = z.discriminatedUnion("type", [
  ChatDeltaEventSchema,
  ChatDecisionEventSchema,
  ChatErrorEventSchema,
  ChatDoneEventSchema,
]);

export type ChatEvent = z.infer<typeof ChatEventSchema>;
export type ChatEntity = z.infer<typeof ChatEntitySchema>;

// ─── documents ───────────────────────────────────────────────────────────

export const DocumentSchema = z.object({
  id: z.string().uuid(),
  filename: z.string(),
  status: z.enum(["pending", "processing", "done", "failed", "deleted"]),
  size_bytes: z.number().int().nonnegative(),
  created_at: z.string(),
});

export const DocumentListSchema = z.object({
  items: z.array(DocumentSchema),
  next_cursor: z.string().nullable(),
});

export type Document = z.infer<typeof DocumentSchema>;

// ─── policies ────────────────────────────────────────────────────────────

export const PolicySchema = z.object({
  id: z.string(),
  name: z.string(),
  description: z.string(),
  enabled: z.boolean(),
  thresholds: z.record(z.string(), z.unknown()).optional(),
});

export const PolicyListSchema = z.object({
  items: z.array(PolicySchema),
});

export type Policy = z.infer<typeof PolicySchema>;

// ─── models ──────────────────────────────────────────────────────────────

export const ModelSchema = z.object({
  id: z.string().uuid(),
  name: z.string(),
  provider_type: z.string(),
  base_url: z.string(),
  enabled: z.boolean(),
  trusted_local: z.boolean(),
  has_api_key: z.boolean(),
});

export const ModelListSchema = z.object({
  items: z.array(ModelSchema),
});

export type Model = z.infer<typeof ModelSchema>;

// ─── audit ───────────────────────────────────────────────────────────────

export const AuditEventSchema = z.object({
  id: z.string().uuid(),
  event_type: z.string(),
  user_id: z.string().nullable(),
  session_id: z.string().nullable(),
  detail: z.record(z.string(), z.unknown()),
  created_at: z.string(),
});

export const AuditListSchema = z.object({
  items: z.array(AuditEventSchema),
  next_cursor: z.string().nullable(),
});

export type AuditEvent = z.infer<typeof AuditEventSchema>;

// ─── incidents ───────────────────────────────────────────────────────────

export const IncidentSchema = z.object({
  id: z.string().uuid(),
  severity: z.enum(["low", "medium", "high", "critical"]),
  status: z.enum(["open", "in_progress", "closed"]),
  title: z.string(),
  description: z.string(),
  event_type: z.string(),
  audit_event_id: z.string().nullable(),
  reporter_id: z.string().nullable(),
  assignee_id: z.string().nullable(),
  created_at: z.string(),
  closed_at: z.string().nullable(),
  etag: z.string(),
});

export const IncidentListSchema = z.object({
  items: z.array(IncidentSchema),
});

export type Incident = z.infer<typeof IncidentSchema>;

// ─── auth ────────────────────────────────────────────────────────────────

export const DevLoginResponseSchema = z.object({
  token: z.string(),
  role: z.string(),
  user_id: z.string(),
  expires_at: z.string(),
});

export type DevLoginResponse = z.infer<typeof DevLoginResponseSchema>;
