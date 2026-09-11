import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

import { toCallToolResult, type AgentToolContext } from "./shared";
import { agentToolDefinitions } from "@nextshell/shared";

/**
 * `session_list` and `session_history` are served from OscTap (the main-process
 * OSC scanner); `session_read` from ScreenMirror (the headless emulator). The
 * split is deliberate: OscTap owns command semantics and raw output, the mirror
 * owns "what is on screen right now".
 */
export const registerSessionTools = (server: McpServer, ctx: AgentToolContext): void => {
  server.registerTool("session_list", agentToolDefinitions.session_list, async () =>
    toCallToolResult(await ctx.gateway.listSessions(ctx.client))
  );

  server.registerTool("session_history", agentToolDefinitions.session_history, async (args) =>
    toCallToolResult(await ctx.gateway.sessionHistory(ctx.client, args))
  );

  server.registerTool("session_read", agentToolDefinitions.session_read, async (args) =>
    toCallToolResult(await ctx.gateway.readSessionScreen(ctx.client, args))
  );
};
