import { isRecord } from "./json-rpc.js";

export interface ToolDescriptor {
  name: string;
  title?: string;
  description?: string;
  inputSchema?: Record<string, unknown>;
  annotations?: Record<string, unknown>;
}

/** Answered by the bridge itself; never forwarded upstream. */
export const BRIDGE_STATUS_TOOL = "nextshell_bridge_status";

const emptySchema = (): Record<string, unknown> => ({ type: "object", properties: {} });

export const BRIDGE_STATUS_TOOL_DESCRIPTOR: ToolDescriptor = {
  name: BRIDGE_STATUS_TOOL,
  title: "NextShell 桥接状态",
  description:
    "检查 NextShell 桌面应用是否可达、Agent 接入是否已开启。NextShell 未运行时其他工具会失败，先用它确认。",
  inputSchema: emptySchema(),
  annotations: { readOnlyHint: true, openWorldHint: false }
};

const targetSchema = (extra: Record<string, unknown> = {}): Record<string, unknown> => ({
  type: "object",
  properties: { target: { type: "string" }, ...extra },
  required: ["target"]
});

/**
 * Fallback manifest used only while NextShell is unreachable, so that a client
 * that starts before the app still completes `initialize` / `tools/list`. The
 * running app is authoritative: the first successful dial replaces this list and
 * emits `notifications/tools/list_changed`.
 */
export const STATIC_TOOLS: ToolDescriptor[] = [
  BRIDGE_STATUS_TOOL_DESCRIPTOR,
  {
    name: "session_list",
    title: "列出会话",
    description: "列出用户在 NextShell 里已打开的终端标签页（会话 id、连接名、host、状态与 cwd）。",
    inputSchema: emptySchema(),
    annotations: { readOnlyHint: true }
  },
  {
    name: "session_history",
    title: "读取会话历史",
    description: "读取活动会话的命令、退出码与有界输出。",
    inputSchema: targetSchema(),
    annotations: { readOnlyHint: true }
  },
  {
    name: "session_read",
    title: "读取会话屏幕",
    description:
      "读取活动会话渲染后的屏幕（含后台标签）。全屏程序（top / vim / 交互式安装器）返回人眼所见的那一帧，而非一堆光标定位转义序列。",
    inputSchema: targetSchema({
      mode: { type: "string", enum: ["screen", "scrollback"] },
      lines: { type: "integer", minimum: 1, maximum: 2000 },
      stripAnsi: { type: "boolean" }
    }),
    annotations: { readOnlyHint: true }
  },
  {
    name: "command_search",
    title: "检索命令库",
    description: "检索用户保存的快速命令（本地 + workspace）。",
    inputSchema: emptySchema(),
    annotations: { readOnlyHint: true }
  },
  {
    name: "command_save",
    title: "保存快速命令",
    description: "保存或更新一条快速命令（name/group/command/appendCr）。",
    inputSchema: {
      type: "object",
      properties: {
        id: { type: "string" },
        workspaceId: { type: "string" },
        name: { type: "string" },
        group: { type: "string" },
        command: { type: "string" },
        appendCr: { type: "boolean" }
      },
      required: ["name", "command"]
    },
    annotations: { idempotentHint: true }
  },
  {
    name: "exec",
    title: "执行远程命令",
    description:
      "在某个已打开会话的既有连接上执行一条命令并返回输出；命中黑名单直接报错。",
    inputSchema: {
      type: "object",
      properties: {
        target: { type: "string", description: "session_list 返回的活动会话 id" },
        command: { type: "string" },
        cwd: { type: "string" },
        timeoutSec: { type: "integer", minimum: 1, maximum: 3600 }
      },
      required: ["target", "command"]
    },
    annotations: { destructiveHint: true }
  },
  {
    name: "session_send_keys",
    title: "向会话注入输入",
    description:
      "像用户一样往活动会话的 PTY 里打字。除非状态就活在那个 shell 里（TUI、交互式提示、已进入的 venv 或容器），否则优先用 exec。检测到用户在该标签页手动操作时报 human_intervention。",
    inputSchema: targetSchema({
      text: { type: "string" },
      submit: { type: "boolean" },
      waitForPrompt: { type: "boolean" },
      timeoutSec: { type: "integer", minimum: 1, maximum: 600 }
    }),
    annotations: { destructiveHint: true }
  },
  {
    name: "session_send_signal",
    title: "向会话发送控制信号",
    description: "向活动会话发送控制字符：interrupt（Ctrl-C）/ eof / suspend / quit。",
    inputSchema: targetSchema({
      signal: { type: "string", enum: ["interrupt", "eof", "suspend", "quit"] }
    }),
    annotations: { destructiveHint: true }
  },
  {
    name: "session_focus",
    title: "聚焦终端标签",
    description: "把 NextShell 窗口置顶并切到该会话的标签，用于把事情交回给人。",
    inputSchema: targetSchema(),
    annotations: { idempotentHint: true }
  }
];

export const parseToolDescriptors = (value: unknown): ToolDescriptor[] | null => {
  if (!isRecord(value) || !Array.isArray(value.tools)) {
    return null;
  }
  const tools: ToolDescriptor[] = [];
  for (const entry of value.tools) {
    if (isRecord(entry) && typeof entry.name === "string" && entry.name.length > 0) {
      // Kept by reference so client-visible fields the bridge does not model survive.
      tools.push(entry as unknown as ToolDescriptor);
    }
  }
  return tools;
};

export const toolListSignature = (tools: ToolDescriptor[]): string =>
  JSON.stringify(
    tools.map((tool) => [tool.name, tool.description ?? "", tool.inputSchema ?? null])
  );
