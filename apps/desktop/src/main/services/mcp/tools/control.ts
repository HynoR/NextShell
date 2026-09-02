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
 * `exec` is the default for running anything — it is bounded and never shares
 * the line editor with the person at the keyboard. These tools exist for the
 * cases where the state genuinely lives in that shell: a TUI, an interactive
 * installer, a sudo prompt, an already-entered venv or `docker exec`.
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
        "Type into a live session's PTY, as if the user had typed it. Prefer exec unless the state you need lives in that shell (a TUI, an interactive prompt, an entered venv or container). Fails with human_intervention when the user typed into the tab after your last operation — stop and report instead of retrying. With waitForPrompt the call returns once the shell reports the command finished (OSC 133), including its exit code and output; without shell integration no mark ever arrives and waitTimedOut comes back true rather than a guessed result.",
      inputSchema: {
        target: z.string().min(1).describe(sessionIdInputDescription),
        text: z.string().max(4096).describe("Characters to type"),
        submit: z.boolean().optional().describe("Append a carriage return to run it"),
        waitForPrompt: z
          .boolean()
          .optional()
          .describe("Wait for the shell's next OSC 133 completion mark"),
        timeoutSec: z.number().int().min(1).max(600).optional()
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
