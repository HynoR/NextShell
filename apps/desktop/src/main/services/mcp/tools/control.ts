import { z } from "zod";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

import {
  DESTRUCTIVE_ANNOTATIONS,
  outputShape,
  sessionIdInputDescription,
  toCallToolResult,
  type AgentToolContext
} from "./shared";

/**
 * Out-of-band control of the PTY: signals and focus. Running commands is
 * `exec`'s job (see ./exec.ts); typing arbitrary keystrokes is deliberately
 * not offered.
 */
export const registerControlTools = (server: McpServer, ctx: AgentToolContext): void => {
  server.registerTool(
    "session_send_signal",
    {
      title: "向会话发送控制信号",
      description:
        "Send a control character to a live session: interrupt (Ctrl-C), eof (Ctrl-D), suspend (Ctrl-Z) or quit (Ctrl-\\). Use interrupt to stop something you started that is taking too long.",
      inputSchema: {
        target: z.string().min(1).describe(sessionIdInputDescription),
        signal: z.enum(["interrupt", "eof", "suspend", "quit"])
      },
      outputSchema: outputShape(
        z.object({
          sessionId: z.string(),
          signal: z.enum(["interrupt", "eof", "suspend", "quit"])
        })
      ),
      annotations: DESTRUCTIVE_ANNOTATIONS
    },
    async (args) => toCallToolResult(await ctx.gateway.sendSignal(ctx.client, args))
  );

  server.registerTool(
    "session_focus",
    {
      title: "聚焦终端标签",
      description:
        "Bring NextShell's window forward and switch to this session's tab. Use it to hand something back to the user — a prompt that needs their judgement, or a result worth watching live.",
      inputSchema: { target: z.string().min(1).describe(sessionIdInputDescription) },
      outputSchema: outputShape(z.object({ sessionId: z.string() })),
      annotations: {
        readOnlyHint: false,
        destructiveHint: false,
        idempotentHint: true,
        openWorldHint: false
      }
    },
    async (args) => toCallToolResult(await ctx.gateway.focusSession(ctx.client, args))
  );
};
