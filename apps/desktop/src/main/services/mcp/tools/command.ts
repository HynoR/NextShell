import { z } from "zod";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

import {
  outputShape,
  READ_ONLY_ANNOTATIONS,
  savedCommandSchema,
  toCallToolResult,
  type AgentToolContext
} from "./shared";

export const registerCommandTools = (server: McpServer, ctx: AgentToolContext): void => {
  server.registerTool(
    "command_search",
    {
      title: "检索命令库",
      description:
        "Search the user's saved quick-command library (local and workspace entries), so the agent can reuse commands the user already curated instead of inventing them.",
      inputSchema: {
        query: z.string().optional().describe("Substring filter over the command text and name"),
        limit: z.number().int().min(1).max(200).optional()
      },
      outputSchema: outputShape(
        z.object({
          matches: z.array(
            z.object({
              id: z.string(),
              command: z.string(),
              name: z.string().nullable(),
              group: z.string().nullable(),
              appendCr: z.boolean(),
              scope: z.enum(["local", "workspace"]),
              workspaceId: z.string().optional(),
              workspaceName: z.string().optional(),
              lastUsedAt: z.string().nullable()
            })
          ),
          total: z.number(),
          truncated: z.boolean()
        })
      ),
      annotations: READ_ONLY_ANNOTATIONS
    },
    async (args) =>
      toCallToolResult(
        await ctx.gateway.searchCommands(ctx.client, { query: args.query, limit: args.limit })
      )
  );

  server.registerTool(
    "command_save",
    {
      title: "保存快速命令",
      description:
        "Save or update a quick command in the user's library, exactly like the in-app edit dialog. Pass an id (from command_search) to update an existing entry, or a workspaceId to write into that workspace's shared list.",
      inputSchema: {
        id: z.string().optional().describe("Existing entry id to update; omit to create"),
        workspaceId: z
          .string()
          .optional()
          .describe("Write into this workspace's command list instead of the local library"),
        name: z.string().min(1).max(200),
        group: z.string().max(200).optional(),
        command: z
          .string()
          .min(1)
          .max(64 * 1024),
        appendCr: z
          .boolean()
          .optional()
          .describe("Run the command immediately when inserted, instead of just pasting it")
      },
      outputSchema: outputShape(
        z.object({
          command: savedCommandSchema,
          workspaceId: z.string().optional()
        })
      ),
      annotations: {
        readOnlyHint: false,
        destructiveHint: false,
        idempotentHint: true,
        openWorldHint: true
      }
    },
    async (args) => toCallToolResult(await ctx.gateway.saveCommand(ctx.client, args))
  );
};
