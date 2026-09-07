import { z } from "zod";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

import {
  outputShape,
  READ_ONLY_ANNOTATIONS,
  toCallToolResult,
  type AgentToolContext
} from "./shared";

/**
 * Discovery beyond the open tabs, on the user's terms: without a query the
 * agent only sees the ten hosts the user connected to most recently, and
 * opening one always goes through an authorization dialog in NextShell.
 */
export const registerHostTools = (server: McpServer, ctx: AgentToolContext): void => {
  server.registerTool(
    "host_list",
    {
      title: "查找服务器",
      description:
        "Find saved servers. Without query: the 10 hosts the user connected to most recently. With query: substring match on name, host, tags, group and notes (max 20). openSessions > 0 means a tab is already open — use session_list to get its id instead of session_open. Returns metadata only, never credentials.",
      inputSchema: {
        query: z.string().max(200).optional().describe("Keyword; omit for the recent list")
      },
      outputSchema: outputShape(
        z.object({
          mode: z.enum(["recent", "search"]),
          hosts: z.array(
            z.object({
              id: z.string(),
              name: z.string(),
              host: z.string(),
              port: z.number(),
              username: z.string(),
              groupPath: z.string(),
              tags: z.array(z.string()),
              lastConnectedAt: z.string().nullable(),
              openSessions: z.number()
            })
          ),
          truncated: z.boolean()
        })
      ),
      annotations: READ_ONLY_ANNOTATIONS
    },
    async (args) => toCallToolResult(await ctx.gateway.hostList(ctx.client, args))
  );

  server.registerTool(
    "session_open",
    {
      title: "请求打开服务器",
      description:
        "Ask the user to open a connection: NextShell raises its window and shows an authorization dialog; the tab opens only after the user clicks 授权. Returns status 'opened' with the session id (a host that already has a tab is reused without a dialog), or status 'pending' with a requestId when the user has not answered within this call — call again with the same requestId to keep waiting (the dialog stays up for 5 minutes). A 'denied' error means the user did not authorize it: stop and ask the user to confirm in NextShell before retrying.",
      inputSchema: {
        target: z.string().min(1).optional().describe("Connection id from host_list"),
        reason: z.string().max(300).optional().describe("Shown to the user in the dialog"),
        requestId: z.string().optional().describe("Continue waiting on an earlier pending request")
      },
      outputSchema: outputShape(
        z.object({
          status: z.enum(["opened", "pending"]),
          sessionId: z.string().optional(),
          requestId: z.string().optional(),
          reused: z.boolean().optional()
        })
      ),
      annotations: {
        readOnlyHint: false,
        destructiveHint: false,
        idempotentHint: false,
        openWorldHint: true
      }
    },
    async (args) => toCallToolResult(await ctx.gateway.openSession(ctx.client, args))
  );
};
