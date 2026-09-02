import { z } from "zod";
import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";

import type { AgentGateway, AgentToolResult, AgentClientIdentity } from "../agent-gateway";

export interface AgentToolContext {
  gateway: AgentGateway;
  client: AgentClientIdentity;
}

export const agentErrorSchema = z.object({
  code: z.string(),
  message: z.string()
});

/**
 * Uniform envelope so an agent can branch on `ok` without knowing which failure
 * mode (`not_found` / `forbidden` / `human_intervention` / …) a given tool can
 * produce.
 */
export const outputShape = <T extends z.ZodTypeAny>(data: T) => ({
  ok: z.boolean(),
  data: data.optional(),
  error: agentErrorSchema.optional()
});

/** Every mutating tool takes a live session id straight from `session_list`. */
export const sessionIdInputDescription = "Live session id returned by session_list";

export const READ_ONLY_ANNOTATIONS = {
  readOnlyHint: true,
  destructiveHint: false,
  idempotentHint: true,
  openWorldHint: true
} as const;

export const DESTRUCTIVE_ANNOTATIONS = {
  readOnlyHint: false,
  destructiveHint: true,
  idempotentHint: false,
  openWorldHint: true
} as const;

export const toCallToolResult = <T>(result: AgentToolResult<T>): CallToolResult => {
  if (result.ok) {
    const payload = { ok: true, data: result.data };
    return {
      content: [{ type: "text", text: JSON.stringify(payload) }],
      structuredContent: payload as Record<string, unknown>
    };
  }
  const payload = { ok: false, error: result.error };
  return {
    content: [{ type: "text", text: JSON.stringify(payload) }],
    structuredContent: payload as unknown as Record<string, unknown>,
    isError: true
  };
};

export const sessionInfoSchema = z.object({
  id: z.string(),
  connectionId: z.string().nullable(),
  connectionName: z.string().nullable(),
  host: z.string().nullable(),
  title: z.string(),
  status: z.string(),
  type: z.string(),
  createdAt: z.string(),
  cwd: z.string().nullable(),
  lastCommand: z.string().nullable()
});

export const savedCommandSchema = z.object({
  id: z.string(),
  name: z.string(),
  description: z.string().optional(),
  group: z.string(),
  command: z.string(),
  appendCr: z.boolean().optional(),
  createdAt: z.string(),
  updatedAt: z.string()
});
