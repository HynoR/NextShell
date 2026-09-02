import { isRecord } from "./json-rpc.js";
import { STATIC_TOOLS, type ToolDescriptor } from "./tools.js";

/**
 * Pure argv → invocation parsing for the nextshell CLI. Kept free of I/O so the
 * flag grammar (the part agents actually script against) is unit-testable.
 */

export interface CliUsageError {
  kind: "usage-error";
  message: string;
}

export interface CliHelpCommand {
  kind: "help";
}

export interface CliStatusCommand {
  kind: "status";
}

export interface CliToolsCommand {
  kind: "tools";
  full: boolean;
}

export interface CliCallCommand {
  kind: "call";
  tool: string;
  args: Record<string, unknown>;
  full: boolean;
  timeoutSec: number | null;
}

export type CliInvocation =
  CliUsageError | CliHelpCommand | CliStatusCommand | CliToolsCommand | CliCallCommand;

/** Flags consumed by the CLI itself; everything else becomes a tool argument. */
const RESERVED_FLAGS = new Set(["json", "full", "timeout", "help"]);

/**
 * `true` / `false` / numbers / `{...}` / `[...]` / `null` become their JSON
 * value; anything that does not parse stays a string. A literal string that
 * happens to look like JSON must be passed via --json.
 */
export const coerceFlagValue = (raw: string): unknown => {
  const trimmed = raw.trim();
  if (trimmed.length === 0) {
    return raw;
  }
  const looksJson =
    trimmed === "true" ||
    trimmed === "false" ||
    trimmed === "null" ||
    /^-?\d+(\.\d+)?$/.test(trimmed) ||
    trimmed.startsWith("{") ||
    trimmed.startsWith("[") ||
    trimmed.startsWith('"');
  if (!looksJson) {
    return raw;
  }
  try {
    return JSON.parse(trimmed) as unknown;
  } catch {
    return raw;
  }
};

const usage = (message: string): CliUsageError => ({ kind: "usage-error", message });

export const parseCliInvocation = (argv: string[]): CliInvocation => {
  const [command, ...rest] = argv;
  if (command === undefined || command === "help" || command === "--help" || command === "-h") {
    return { kind: "help" };
  }
  if (command === "status") {
    return rest.length === 0 ? { kind: "status" } : usage("status 不接受参数");
  }
  if (command === "tools") {
    if (rest.every((flag) => flag === "--full")) {
      return { kind: "tools", full: rest.includes("--full") };
    }
    return usage("tools 只接受 --full");
  }
  if (command.startsWith("-")) {
    return usage(`未知选项：${command}（工具名必须是第一个参数）`);
  }

  const args: Record<string, unknown> = {};
  let full = false;
  let timeoutSec: number | null = null;

  for (let i = 0; i < rest.length; i += 1) {
    const token = rest[i] ?? "";
    if (!token.startsWith("--")) {
      return usage(`多余的位置参数：${token}（工具参数写成 --key value）`);
    }
    const body = token.slice(2);
    const eq = body.indexOf("=");
    const key = eq === -1 ? body : body.slice(0, eq);
    let value: string | null = eq === -1 ? null : body.slice(eq + 1);
    if (key.length === 0) {
      return usage(`无法解析的选项：${token}`);
    }
    if (value === null) {
      const next = rest[i + 1];
      if (next !== undefined && !next.startsWith("--")) {
        value = next;
        i += 1;
      }
    }

    if (key === "help") {
      return { kind: "help" };
    }
    if (key === "full") {
      if (value !== null) {
        return usage("--full 不接受值");
      }
      full = true;
      continue;
    }
    if (key === "timeout") {
      const parsed = value === null ? Number.NaN : Number.parseInt(value, 10);
      if (!Number.isInteger(parsed) || parsed <= 0) {
        return usage("--timeout 需要一个正整数秒数");
      }
      timeoutSec = parsed;
      continue;
    }
    if (key === "json") {
      if (value === null) {
        return usage("--json 需要一个 JSON 对象字符串");
      }
      let parsed: unknown;
      try {
        parsed = JSON.parse(value);
      } catch {
        return usage("--json 的值不是合法 JSON");
      }
      if (!isRecord(parsed)) {
        return usage("--json 的值必须是一个 JSON 对象");
      }
      Object.assign(args, parsed);
      continue;
    }
    if (RESERVED_FLAGS.has(key)) {
      return usage(`选项 --${key} 用法不对`);
    }
    args[key] = value === null ? true : coerceFlagValue(value);
  }

  return { kind: "call", tool: command, args, full, timeoutSec };
};

/** MCP CallToolResult → printable text: concatenated text parts, or raw JSON. */
export const renderCallResult = (
  result: unknown,
  full: boolean
): { text: string; isError: boolean } => {
  if (!isRecord(result)) {
    return { text: JSON.stringify(result), isError: false };
  }
  const isError = result.isError === true;
  if (full) {
    return { text: JSON.stringify(result, null, 2), isError };
  }
  const content = Array.isArray(result.content) ? result.content : [];
  const texts: string[] = [];
  for (const part of content) {
    if (isRecord(part) && part.type === "text" && typeof part.text === "string") {
      texts.push(part.text);
    }
  }
  if (texts.length === 0) {
    return { text: JSON.stringify(result, null, 2), isError };
  }
  return { text: texts.join("\n"), isError };
};

export const renderToolList = (tools: ToolDescriptor[], full: boolean): string => {
  if (full) {
    return JSON.stringify(tools, null, 2);
  }
  const width = Math.max(...tools.map((tool) => tool.name.length));
  return tools
    .map((tool) => {
      const label = tool.title ?? "";
      const summary = tool.description ?? "";
      const doc = label && summary ? `${label} — ${summary}` : label || summary;
      return `${tool.name.padEnd(width)}  ${doc}`.trimEnd();
    })
    .join("\n");
};

export const HELP_TEXT = `nextshell-cli — 通过本机运行的 NextShell 桌面应用操作远程服务器

用法:
  nextshell-cli status                     检查 NextShell 是否可达
  nextshell-cli tools [--full]             列出可用工具（--full 输出完整 JSON schema）
  nextshell-cli <tool> [--key value ...]   调用一个工具
  nextshell-cli <tool> --json '{...}'      用 JSON 对象传参（与 --key 可混用）

通用选项:
  --full           输出完整结果 JSON（默认只输出文本内容）
  --timeout <sec>  覆盖本次调用的等待秒数

示例:
  nextshell-cli session_list
  nextshell-cli exec --target 3f9c… --command "systemctl status nginx"
  nextshell-cli session_read --target 3f9c… --mode scrollback --lines 200
  nextshell-cli command_save --name "重启 nginx" --command "sudo systemctl restart nginx"

退出码: 0 成功；1 工具执行出错；2 用法错误；3 无法连接 NextShell`;

export { STATIC_TOOLS };
