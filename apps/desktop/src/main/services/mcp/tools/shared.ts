import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";

import type { AgentGateway, AgentToolResult, AgentClientIdentity } from "../agent-gateway";

export interface AgentToolContext {
  gateway: AgentGateway;
  client: AgentClientIdentity;
}

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
