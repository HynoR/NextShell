import { z } from "zod";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

import {
  DESTRUCTIVE_ANNOTATIONS,
  outputShape,
  sessionIdInputDescription,
  toCallToolResult,
  type AgentToolContext
} from "./shared";

export const registerExecTools = (server: McpServer, ctx: AgentToolContext): void => {
  server.registerTool(
    "exec",
    {
      title: "执行远端命令",
      description:
        "Execute a command on a fresh channel of the SSH connection backing an open session — the agent borrows the user's already-authenticated connection and can never dial one itself. The session's OSC-tracked cwd is inherited; an explicit cwd wins. Commands go through the dangerous-command blacklist (a hit is a hard error), and a human keystroke in the tab since the agent's last operation fails the call with human_intervention.",
      inputSchema: {
        target: z.string().min(1).describe(sessionIdInputDescription),
        command: z
          .string()
          .min(1)
          .max(256 * 1024),
        cwd: z.string().optional().describe("Absolute remote working directory"),
        timeoutSec: z.number().int().min(1).max(3600).optional()
      },
      outputSchema: outputShape(
        z.object({
          sessionId: z.string(),
          connectionId: z.string(),
          command: z.string(),
          stdout: z.string(),
          stderr: z.string(),
          exitCode: z.number(),
          actualCwd: z.string().nullable(),
          executedAt: z.string()
        })
      ),
      annotations: DESTRUCTIVE_ANNOTATIONS
    },
    async (args) => toCallToolResult(await ctx.gateway.execCommand(ctx.client, args))
  );
};
