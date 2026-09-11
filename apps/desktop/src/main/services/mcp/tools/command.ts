import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

import { toCallToolResult, type AgentToolContext } from "./shared";
import { agentToolDefinitions } from "@nextshell/shared";

export const registerCommandTools = (server: McpServer, ctx: AgentToolContext): void => {
  server.registerTool("command_search", agentToolDefinitions.command_search, async (args) =>
    toCallToolResult(
      await ctx.gateway.searchCommands(ctx.client, { query: args.query, limit: args.limit })
    )
  );

  server.registerTool("command_save", agentToolDefinitions.command_save, async (args) =>
    toCallToolResult(await ctx.gateway.saveCommand(ctx.client, args))
  );
};
