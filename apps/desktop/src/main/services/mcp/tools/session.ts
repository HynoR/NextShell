import { z } from "zod";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

import {
  outputShape,
  READ_ONLY_ANNOTATIONS,
  sessionIdInputDescription,
  sessionInfoSchema,
  toCallToolResult,
  type AgentToolContext
} from "./shared";

/**
 * `session_list` and `session_history` are served from OscTap (the main-process
 * OSC scanner); `session_read` from ScreenMirror (the headless emulator). The
 * split is deliberate: OscTap owns command semantics and raw output, the mirror
 * owns "what is on screen right now".
 */
export const registerSessionTools = (server: McpServer, ctx: AgentToolContext): void => {
  server.registerTool(
    "session_list",
    {
      title: "列出会话",
      description:
        "List the terminal tabs the user currently has open in NextShell — the only sessions an agent may touch. Each entry carries the session id, connection name, host and status. cwd is the directory the shell last reported via OSC 7 and is trustworthy even for background tabs; it is null only when the session never reported one (no shell integration). Pass a session id as exec's target to inherit that cwd.",
      inputSchema: {},
      outputSchema: outputShape(
        z.object({
          sessions: z.array(sessionInfoSchema),
          truncated: z.boolean()
        })
      ),
      annotations: READ_ONLY_ANNOTATIONS
    },
    async () => toCallToolResult(await ctx.gateway.listSessions(ctx.client))
  );

  server.registerTool(
    "session_history",
    {
      title: "读取会话命令记录",
      description:
        "Read OSC-tracked commands, exit codes and bounded raw output for one live session. Sessions without shell integration report limited capability instead of guessed command text.",
      inputSchema: {
        target: z.string().min(1).describe(sessionIdInputDescription),
        limit: z.number().int().min(1).max(200).optional(),
        stripAnsi: z.boolean().optional()
      },
      outputSchema: outputShape(
        z.object({
          sessionId: z.string(),
          integrationAvailable: z.boolean(),
          entries: z.array(
            z.object({
              command: z.string().nullable(),
              exitCode: z.number().nullable(),
              startedAt: z.string(),
              finishedAt: z.string().nullable(),
              output: z.string(),
              truncated: z.boolean()
            })
          ),
          truncated: z.boolean()
        })
      ),
      annotations: READ_ONLY_ANNOTATIONS
    },
    async (args) => toCallToolResult(await ctx.gateway.sessionHistory(ctx.client, args))
  );

  server.registerTool(
    "session_read",
    {
      title: "读取会话屏幕",
      description:
        "Read the rendered screen of a live session, including background tabs. This is a real terminal emulation, so a full-screen program (top, htop, vim, an interactive installer) comes back as the frame a human would see rather than a pile of cursor-addressing escapes. Use `screen` for the current viewport and `scrollback` to include what scrolled off; for command output with exit codes prefer session_history.",
      inputSchema: {
        target: z.string().min(1).describe(sessionIdInputDescription),
        mode: z
          .enum(["screen", "scrollback"])
          .optional()
          .describe("`screen` (default) is the visible viewport; `scrollback` includes history"),
        lines: z
          .number()
          .int()
          .min(1)
          .max(2000)
          .optional()
          .describe("Lines to return, counted back from the bottom"),
        stripAnsi: z
          .boolean()
          .optional()
          .describe("Defaults to true; pass false to keep colour and style sequences")
      },
      outputSchema: outputShape(
        z.object({
          sessionId: z.string(),
          mode: z.enum(["screen", "scrollback"]),
          content: z.string(),
          lines: z.number(),
          cols: z.number(),
          rows: z.number(),
          scrollbackLines: z.number(),
          truncated: z.boolean()
        })
      ),
      annotations: READ_ONLY_ANNOTATIONS
    },
    async (args) => toCallToolResult(await ctx.gateway.readSessionScreen(ctx.client, args))
  );
};
