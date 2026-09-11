import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

import { toCallToolResult, type AgentToolContext } from "./shared";
import { agentToolDefinitions } from "@nextshell/shared";

/**
 * Out-of-band control of the PTY: signals and focus. Running commands is
 * `exec`'s job (see ./exec.ts); typing arbitrary keystrokes is deliberately
 * not offered.
 */
export const registerControlTools = (server: McpServer, ctx: AgentToolContext): void => {
  server.registerTool(
    "session_send_signal",
    agentToolDefinitions.session_send_signal,
    async (args) => toCallToolResult(await ctx.gateway.sendSignal(ctx.client, args))
  );

  server.registerTool("session_focus", agentToolDefinitions.session_focus, async (args) =>
    toCallToolResult(await ctx.gateway.focusSession(ctx.client, args))
  );
};
