import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

import { toCallToolResult, type AgentToolContext } from "./shared";
import { agentToolDefinitions } from "@nextshell/shared";

export const registerExecTools = (server: McpServer, ctx: AgentToolContext): void => {
  server.registerTool("exec", agentToolDefinitions.exec, async (args) =>
    toCallToolResult(await ctx.gateway.execCommand(ctx.client, args))
  );
};
