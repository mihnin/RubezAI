import { describe, it, expect } from "vitest";
import {
  ChatMetaPayloadSchema,
  ChatDeltaPayloadSchema,
  DocumentListSchema,
  IncidentSchema,
  PolicyListSchema,
  ModelListSchema,
  AuditListSchema,
  DevLoginResponseSchema,
} from "../api/schemas";

describe("Zod-схемы контрактов API (сверены с rubezh-api DTO)", () => {
  it("chat meta: парсит decision/risk/provider/reasons/request_id", () => {
    const m = ChatMetaPayloadSchema.parse({
      decision: "allow_masked",
      risk: { level: "medium", score: 0.5, classes: ["PII"] },
      provider: "mock",
      reasons: ["match:phone"],
      request_id: "req-1",
    });
    expect(m.decision).toBe("allow_masked");
    expect(m.risk.classes).toEqual(["PII"]);
  });

  it("chat delta: только content", () => {
    expect(ChatDeltaPayloadSchema.parse({ content: "Hi" })).toEqual({
      content: "Hi",
    });
  });

  it("chat delta: отклоняет невалидный payload", () => {
    expect(() =>
      ChatDeltaPayloadSchema.parse({ text: "wrong-field" }),
    ).toThrow();
  });

  it("документы: {documents: [...]} с nullable size_bytes", () => {
    const list = DocumentListSchema.parse({
      documents: [
        {
          id: "11111111-1111-1111-1111-111111111111",
          owner_id: "22222222-2222-2222-2222-222222222222",
          filename: "test.pdf",
          content_type: "application/pdf",
          size_bytes: null,
          status: "done",
          phase: null,
          error: null,
          processing_attempts: 0,
          processing_started_at: null,
          created_at: "2026-05-20T10:00:00Z",
          updated_at: "2026-05-20T10:00:00Z",
        },
      ],
    });
    expect(list.documents[0].size_bytes).toBeNull();
  });

  it("политики: голый массив, поле is_active", () => {
    const list = PolicyListSchema.parse([
      {
        id: "11111111-1111-1111-1111-111111111111",
        name: "PII default",
        description: "d",
        is_active: true,
        current_version: 1,
        created_at: "2026-05-20T10:00:00Z",
        updated_at: "2026-05-20T10:00:00Z",
      },
    ]);
    expect(list[0].is_active).toBe(true);
  });

  it("модели: голый массив, поля adapter/endpoint/trust_level", () => {
    const list = ModelListSchema.parse([
      {
        id: "11111111-1111-1111-1111-111111111111",
        name: "m",
        trust_level: "trusted_local",
        adapter: "openai",
        endpoint: "http://localhost:1234/v1",
        max_tokens: null,
        rate_limit_per_min: null,
        is_enabled: true,
        has_api_key: false,
        created_at: "2026-05-20T10:00:00Z",
        updated_at: "2026-05-20T10:00:00Z",
      },
    ]);
    expect(list[0].adapter).toBe("openai");
  });

  it("аудит: {events: [...], next_cursor}", () => {
    const list = AuditListSchema.parse({
      events: [
        {
          id: "11111111-1111-1111-1111-111111111111",
          created_at: "2026-05-20T10:00:00Z",
          user_id: "22222222-2222-2222-2222-222222222222",
          event_type: "chat_request_received",
          model_provider_id: null,
          risk_level: "low",
          risk_classes: ["PII"],
          policy_decision: "allow_masked",
          request_id: null,
          has_leak: false,
        },
      ],
      next_cursor: null,
    });
    expect(list.events[0].risk_classes).toEqual(["PII"]);
  });

  it("инциденты: title/summary/trigger, без description/etag", () => {
    const inc = IncidentSchema.parse({
      id: "11111111-1111-1111-1111-111111111111",
      audit_event_id: null,
      user_id: null,
      reporter_id: null,
      assignee_id: null,
      severity: "high",
      status: "investigating",
      trigger: "chat",
      title: "t",
      summary: null,
      resolution: null,
      closed_at: null,
      created_at: "2026-05-20T10:00:00Z",
      updated_at: "2026-05-20T10:00:00Z",
    });
    expect(inc.severity).toBe("high");
    expect(inc.summary).toBeNull();
  });

  it("парсит ответ dev-login", () => {
    const out = DevLoginResponseSchema.parse({
      token: "user.abc",
      role: "user",
      user_id: "uid",
      expires_at: "2026-05-21T10:00:00Z",
    });
    expect(out.role).toBe("user");
  });
});
