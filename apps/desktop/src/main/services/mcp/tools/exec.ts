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
        "BACKGROUND execution, invisible in the tab: runs the command on a fresh channel of the SSH connection behind an open session and returns stdout/stderr/exit code. Only use this when the user explicitly asks to run something in the background or without disturbing their terminal (e.g. '后台执行', '不打扰我'); otherwise use session_send_keys so the user can watch the work happen. Not available for local shell tabs. The session's OSC-tracked cwd is inherited; an explicit cwd wins. Commands go through the dangerous-command blacklist (a hit is a hard error), and a human keystroke in the tab since the agent's last operation fails the call with human_intervention.",
      inputSchema: {
        target: z.string().min(1).describe(sessionIdInputDescription),
        command: z
          .string()
          .min(1)
          .max(256 * 1024),
        cwd: z.string().optional().describe("Absolute remote working directory"),
        timeoutSec: z.number().int().min(1).max(3600).optional(),
        allowSensitive: z
          .boolean()
          .optional()
          .describe(
            "Set true only after the user explicitly agreed to touch a .env file named in a consent_required error"
          )
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
