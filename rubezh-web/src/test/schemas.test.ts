import { describe, it, expect } from "vitest";
import {
  ChatEventSchema,
  DocumentListSchema,
  IncidentSchema,
  DevLoginResponseSchema,
} from "../api/schemas";

describe("Zod-схемы контрактов API", () => {
  it("парсит delta-событие чата", () => {
    expect(
      ChatEventSchema.parse({ type: "delta", text: "Привет" }),
    ).toEqual({ type: "delta", text: "Привет" });
  });

  it("парсит decision-событие с пустыми entities", () => {
    const ev = ChatEventSchema.parse({
      type: "decision",
      decision: "allow_masked",
    });
    expect(ev).toEqual({
      type: "decision",
      decision: "allow_masked",
      entities: [],
    });
  });

  it("отклоняет invalid decision", () => {
    expect(() =>
      ChatEventSchema.parse({ type: "decision", decision: "bogus" }),
    ).toThrow();
  });

  it("парсит decision с entities", () => {
    const ev = ChatEventSchema.parse({
      type: "decision",
      decision: "deny",
      entities: [{ type: "PHONE", pseudonym: "ТЕЛЕФОН_001" }],
    });
    if (ev.type !== "decision") throw new Error("ожидалось decision");
    expect(ev.entities).toHaveLength(1);
  });

  it("парсит список документов с null-cursor", () => {
    const list = DocumentListSchema.parse({
      items: [
        {
          id: "11111111-1111-1111-1111-111111111111",
          filename: "test.pdf",
          status: "done",
          size_bytes: 1024,
          created_at: "2026-05-20T10:00:00Z",
        },
      ],
      next_cursor: null,
    });
    expect(list.items[0].status).toBe("done");
  });

  it("отклоняет invalid status документа", () => {
    expect(() =>
      DocumentListSchema.parse({
        items: [
          {
            id: "11111111-1111-1111-1111-111111111111",
            filename: "f",
            status: "weird",
            size_bytes: 0,
            created_at: "2026-05-20T10:00:00Z",
          },
        ],
        next_cursor: null,
      }),
    ).toThrow();
  });

  it("парсит инцидент со всеми обязательными полями", () => {
    const inc = IncidentSchema.parse({
      id: "11111111-1111-1111-1111-111111111111",
      severity: "high",
      status: "open",
      title: "t",
      description: "d",
      event_type: "chat",
      audit_event_id: null,
      reporter_id: null,
      assignee_id: null,
      created_at: "2026-05-20T10:00:00Z",
      closed_at: null,
      etag: "\"abc\"",
    });
    expect(inc.severity).toBe("high");
  });

  it("парсит ответ dev-login", () => {
    const out = DevLoginResponseSchema.parse({
      token: "tok",
      role: "user",
      user_id: "uid",
      expires_at: "2026-05-21T10:00:00Z",
    });
    expect(out.role).toBe("user");
  });
});
