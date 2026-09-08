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
      title: "在会话里执行命令",
      description:
        "Run a shell command in a live session. Two things are the USER's settings in NextShell's Agent panel, never your choice: (1) how it runs — foreground types the command into the tab (NextShell comes to the front, the user watches it run in their real shell, the call waits up to 120s for the prompt to return) or background (a fresh SSH channel of the same connection, invisible in the tab, returns stdout/stderr; local shell tabs always run in the foreground) — read `mode` in the response; (2) whether each command needs the user's click first — if it does, the first call returns `{ status: \"pending\", requestId }` while the approval dialog is up: call exec again with ONLY that requestId to keep waiting (the request lives 5 minutes), and a `denied` error means the user refused — stop and ask them, never retry on your own. Foreground `waitTimedOut: true` means no completion mark arrived and the command may still be running in the tab: poll session_read, never re-run it. To answer a prompt shown on screen (y/n), exec the answer text. Commands pass a dangerous-command blacklist (a hit is a hard error) and refuse .env files with consent_required until the user agrees; a foreground call fails with human_intervention when the user has unsubmitted text on their command line — stop and report instead of retrying.",
      inputSchema: {
        target: z.string().min(1).optional().describe(sessionIdInputDescription),
        command: z
          .string()
          .min(1)
          .max(256 * 1024)
          .optional(),
        timeoutSec: z
          .number()
          .int()
          .min(1)
          .max(3600)
          .optional()
          .describe("Background: how long to wait for exit. Foreground: capped at 120s"),
        allowSensitive: z
          .boolean()
          .optional()
          .describe(
            "Set true only after the user explicitly agreed to touch a .env file named in a consent_required error"
          ),
        requestId: z
          .string()
          .optional()
          .describe("Continue waiting on a pending approval; pass nothing else with it")
      },
      outputSchema: outputShape(
        z.union([
          z.object({
            status: z.literal("pending"),
            requestId: z.string()
          }),
          z.object({
            sessionId: z.string(),
            mode: z.enum(["foreground", "background"]),
            command: z.string(),
            exitCode: z.number().nullable(),
            output: z.string(),
            stderr: z.string().nullable(),
            waitTimedOut: z.boolean(),
            actualCwd: z.string().nullable()
          })
        ])
      ),
      annotations: DESTRUCTIVE_ANNOTATIONS
    },
    async (args) => toCallToolResult(await ctx.gateway.execCommand(ctx.client, args))
  );
};
