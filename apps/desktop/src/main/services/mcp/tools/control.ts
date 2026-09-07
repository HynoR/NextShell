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
 * Driving the PTY the user is looking at.
 *
 * `session_send_keys` is the default way to run anything: the user watches the
 * command echo and its output scroll in the tab, exactly as if they had typed
 * it. `exec` is the out-of-band fallback, used only when the user asks for
 * background / do-not-disturb execution.
 * Injected text passes the command blacklist, and a human keystroke since the
 * agent's last operation fails the call with `human_intervention` — the agent
 * should then stop and report instead of retrying.
 */
export const registerControlTools = (server: McpServer, ctx: AgentToolContext): void => {
  server.registerTool(
    "session_send_keys",
    {
      title: "向会话注入输入",
      description:
        "DEFAULT way to run commands: type into the live session's PTY exactly as if the user had typed it, so they watch the command and its output appear in the tab in real time. Use this unless the user explicitly asks for background / quiet execution (then use exec). Set submit: true to press Enter and waitForPrompt: true to get the exit code and output back once the shell reports completion (OSC 133); without shell integration no mark arrives, waitTimedOut comes back true, and you should session_read the screen instead. Fails with human_intervention when the user typed into the tab after your last operation — stop and report instead of retrying.",
      inputSchema: {
        target: z.string().min(1).describe(sessionIdInputDescription),
        text: z.string().max(4096).describe("Characters to type"),
        submit: z.boolean().optional().describe("Append a carriage return to run it"),
        waitForPrompt: z
          .boolean()
          .optional()
          .describe("Wait for the shell's next OSC 133 completion mark"),
        timeoutSec: z.number().int().min(1).max(600).optional(),
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
          bytes: z.number(),
          submitted: z.boolean(),
          completed: z
            .object({
              command: z.string().nullable(),
              exitCode: z.number().nullable(),
              output: z.string(),
              truncated: z.boolean()
            })
            .nullable(),
          waitTimedOut: z.boolean()
        })
      ),
      annotations: DESTRUCTIVE_ANNOTATIONS
    },
    async (args) => toCallToolResult(await ctx.gateway.sendKeys(ctx.client, args))
  );

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
