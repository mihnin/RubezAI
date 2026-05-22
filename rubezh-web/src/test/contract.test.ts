import { describe, it, expect } from "vitest";
import { z } from "zod";
import {
  ModelSchema,
  IncidentSchema,
  IncidentListSchema,
  AuditEventSchema,
  AuditListSchema,
  DocumentSchema,
  DocumentListSchema,
  PolicySchema,
  ChatSessionSchema,
} from "../api/schemas";

// Сгенерированные Go-рефлексией формы DTO (golden из
// rubezh-api/internal/api/contract_export_test.go). См.
// docs/design/g1-contract-tests.md.
import modelProvider from "./contracts/model_provider.json";
import incident from "./contracts/incident.json";
import incidentList from "./contracts/incident_list.json";
import auditEvent from "./contracts/audit_event.json";
import auditList from "./contracts/audit_list.json";
import documentC from "./contracts/document.json";
import documentList from "./contracts/document_list.json";
import policy from "./contracts/policy.json";
import chatSession from "./contracts/chat_session.json";

/** zodKind — нормализованный код типа Zod-поля (общий язык с Go-генератором). */
function zodKind(schema: z.ZodTypeAny): string {
  // развернуть optional/default; запомнить nullability
  let cur = schema as unknown as { _def: Record<string, unknown> };
  let nullable = false;
  for (;;) {
    const tn = cur._def.typeName as string;
    if (tn === "ZodOptional" || tn === "ZodDefault") {
      cur = cur._def.innerType as typeof cur;
      continue;
    }
    if (tn === "ZodNullable") {
      nullable = true;
      cur = cur._def.innerType as typeof cur;
      continue;
    }
    break;
  }
  const tn = cur._def.typeName as string;
  let base: string;
  switch (tn) {
    case "ZodString":
    case "ZodEnum":
    case "ZodNativeEnum":
      base = "string";
      break;
    case "ZodNumber":
      base = "number";
      break;
    case "ZodBoolean":
      base = "boolean";
      break;
    case "ZodArray":
      base = "array";
      break;
    case "ZodObject":
      base = "object";
      break;
    case "ZodLiteral":
      base = typeof cur._def.value === "number" ? "number" : "string";
      break;
    default:
      base = "unknown:" + tn;
  }
  return nullable ? "?" + base : base;
}

/** zodShape — карта поле→код для ZodObject (как в Go-генераторе). */
function zodShape(schema: z.ZodTypeAny): Record<string, string> {
  const def = (schema as unknown as { _def: { shape: () => Record<string, z.ZodTypeAny> } })._def;
  const shape = def.shape();
  const out: Record<string, string> = {};
  for (const key of Object.keys(shape)) {
    out[key] = zodKind(shape[key]);
  }
  return out;
}

const CASES: { name: string; golden: Record<string, string>; schema: z.ZodTypeAny }[] = [
  { name: "model_provider", golden: modelProvider, schema: ModelSchema },
  { name: "incident", golden: incident, schema: IncidentSchema },
  { name: "incident_list", golden: incidentList, schema: IncidentListSchema },
  { name: "audit_event", golden: auditEvent, schema: AuditEventSchema },
  { name: "audit_list", golden: auditList, schema: AuditListSchema },
  { name: "document", golden: documentC, schema: DocumentSchema },
  { name: "document_list", golden: documentList, schema: DocumentListSchema },
  { name: "policy", golden: policy, schema: PolicySchema },
  { name: "chat_session", golden: chatSession, schema: ChatSessionSchema },
];

describe("Контракт Go DTO ↔ Zod (G.1)", () => {
  for (const c of CASES) {
    it(`${c.name}: поля и типы Zod совпадают с Go-DTO`, () => {
      // toEqual ловит любое расхождение: лишнее/пропущенное поле, смену
      // типа или nullability между Go-структурой и Zod-схемой.
      expect(zodShape(c.schema)).toEqual(c.golden);
    });
  }
});
