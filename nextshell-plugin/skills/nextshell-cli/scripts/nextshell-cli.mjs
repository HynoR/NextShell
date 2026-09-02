#!/usr/bin/env node

// src/constants.ts
var LATEST_PROTOCOL_VERSION = "2025-11-25";
var DEFAULT_CONNECT_TIMEOUT_MS = 4e3;
var DEFAULT_REQUEST_TIMEOUT_MS = 3e4;
var DEFAULT_CALL_TIMEOUT_MS = 3e5;
var UNAVAILABLE_MESSAGE =
  "\u65E0\u6CD5\u8FDE\u63A5\u5230 NextShell\uFF1A\u5E94\u7528\u672A\u8FD0\u884C\uFF0C\u6216\u672A\u5728\u8BBE\u7F6E\u4E2D\u5FC3\u5F00\u542F Agent \u63A5\u5165\u3002\u8BF7\u542F\u52A8 NextShell \u684C\u9762\u5E94\u7528\uFF0C\u5728\u300C\u8BBE\u7F6E\u4E2D\u5FC3 \u2192 Agent \u63A5\u5165\u300D\u4E2D\u5F00\u542F\u540E\u91CD\u8BD5\u3002";

// src/json-rpc.ts
var isRecord = (value) => typeof value === "object" && value !== null && !Array.isArray(value);

// src/tools.ts
var BRIDGE_STATUS_TOOL = "nextshell_bridge_status";
var emptySchema = () => ({ type: "object", properties: {} });
var BRIDGE_STATUS_TOOL_DESCRIPTOR = {
  name: BRIDGE_STATUS_TOOL,
  title: "NextShell \u6865\u63A5\u72B6\u6001",
  description:
    "\u68C0\u67E5 NextShell \u684C\u9762\u5E94\u7528\u662F\u5426\u53EF\u8FBE\u3001Agent \u63A5\u5165\u662F\u5426\u5DF2\u5F00\u542F\u3002NextShell \u672A\u8FD0\u884C\u65F6\u5176\u4ED6\u5DE5\u5177\u4F1A\u5931\u8D25\uFF0C\u5148\u7528\u5B83\u786E\u8BA4\u3002",
  inputSchema: emptySchema(),
  annotations: { readOnlyHint: true, openWorldHint: false }
};
var targetSchema = (extra = {}) => ({
  type: "object",
  properties: { target: { type: "string" }, ...extra },
  required: ["target"]
});
var STATIC_TOOLS = [
  BRIDGE_STATUS_TOOL_DESCRIPTOR,
  {
    name: "host_list",
    title: "\u5217\u51FA\u4E3B\u673A",
    description:
      "\u5217\u51FA\u5DF2\u6388\u6743 Agent \u8BBF\u95EE\u7684 NextShell \u4E3B\u673A\u6458\u8981\uFF08\u4E0D\u542B\u4EFB\u4F55\u51ED\u636E\u5B57\u6BB5\uFF09\u3002",
    inputSchema: emptySchema(),
    annotations: { readOnlyHint: true }
  },
  {
    name: "host_describe",
    title: "\u67E5\u770B\u4E3B\u673A",
    description:
      "\u67E5\u770B\u4E00\u53F0\u5DF2\u6388\u6743\u4E3B\u673A\u3001\u6D3B\u52A8\u4F1A\u8BDD\u4E0E\u76D1\u63A7\u6458\u8981\u3002",
    inputSchema: targetSchema(),
    annotations: { readOnlyHint: true }
  },
  {
    name: "session_list",
    title: "\u5217\u51FA\u4F1A\u8BDD",
    description:
      "\u5217\u51FA\u5DF2\u6388\u6743\u4E3B\u673A\u7684\u6D3B\u52A8\u4F1A\u8BDD\u4E0E OSC \u8DDF\u8E2A\u7684 cwd\u3002",
    inputSchema: emptySchema(),
    annotations: { readOnlyHint: true }
  },
  {
    name: "session_history",
    title: "\u8BFB\u53D6\u4F1A\u8BDD\u5386\u53F2",
    description:
      "\u8BFB\u53D6\u6D3B\u52A8\u4F1A\u8BDD\u7684\u547D\u4EE4\u3001\u9000\u51FA\u7801\u4E0E\u6709\u754C\u8F93\u51FA\u3002",
    inputSchema: targetSchema(),
    annotations: { readOnlyHint: true }
  },
  {
    name: "session_read",
    title: "\u8BFB\u53D6\u4F1A\u8BDD\u5C4F\u5E55",
    description:
      "\u8BFB\u53D6\u6D3B\u52A8\u4F1A\u8BDD\u6E32\u67D3\u540E\u7684\u5C4F\u5E55\uFF08\u542B\u540E\u53F0\u6807\u7B7E\uFF09\u3002\u5168\u5C4F\u7A0B\u5E8F\uFF08top / vim / \u4EA4\u4E92\u5F0F\u5B89\u88C5\u5668\uFF09\u8FD4\u56DE\u4EBA\u773C\u6240\u89C1\u7684\u90A3\u4E00\u5E27\uFF0C\u800C\u975E\u4E00\u5806\u5149\u6807\u5B9A\u4F4D\u8F6C\u4E49\u5E8F\u5217\u3002",
    inputSchema: targetSchema({
      mode: { type: "string", enum: ["screen", "scrollback"] },
      lines: { type: "integer", minimum: 1, maximum: 2e3 },
      stripAnsi: { type: "boolean" }
    }),
    annotations: { readOnlyHint: true }
  },
  {
    name: "file_list",
    title: "\u5217\u51FA\u8FDC\u7AEF\u76EE\u5F55",
    inputSchema: targetSchema({ path: { type: "string" } }),
    annotations: { readOnlyHint: true }
  },
  {
    name: "file_stat",
    title: "\u67E5\u770B\u8FDC\u7AEF\u6587\u4EF6\u5C5E\u6027",
    inputSchema: targetSchema({ path: { type: "string" } }),
    annotations: { readOnlyHint: true }
  },
  {
    name: "file_read",
    title: "\u8BFB\u53D6\u8FDC\u7AEF\u6587\u4EF6",
    inputSchema: targetSchema({ path: { type: "string" } }),
    annotations: { readOnlyHint: true }
  },
  {
    name: "monitor_snapshot",
    title: "\u8BFB\u53D6\u76D1\u63A7\u5FEB\u7167",
    inputSchema: targetSchema(),
    annotations: { readOnlyHint: true }
  },
  {
    name: "command_search",
    title: "\u68C0\u7D22\u547D\u4EE4\u5E93",
    inputSchema: emptySchema(),
    annotations: { readOnlyHint: true }
  },
  {
    name: "exec",
    title: "\u6267\u884C\u8FDC\u7A0B\u547D\u4EE4",
    description:
      "\u5728\u5DF2\u6388\u6743\u4E3B\u673A\u4E0A\u6267\u884C\u4E00\u6761\u547D\u4EE4\u5E76\u8FD4\u56DE\u8F93\u51FA\u3002",
    inputSchema: {
      type: "object",
      properties: {
        target: { type: "string", description: "\u4E3B\u673A\u6216\u6D3B\u52A8\u4F1A\u8BDD id" },
        command: { type: "string" },
        cwd: { type: "string" },
        timeoutSec: { type: "integer", minimum: 1, maximum: 3600 }
      },
      required: ["target", "command"]
    }
  },
  {
    name: "file_write",
    title: "\u5199\u5165\u8FDC\u7AEF\u6587\u4EF6",
    description:
      "\u5199\u5165\u4E00\u4E2A\u5C0F\u6587\u4EF6\uFF081MB \u4EE5\u5185\uFF09\uFF0C\u5DF2\u5B58\u5728\u5219\u8986\u76D6\u3002\u5927\u6587\u4EF6\u6216\u6574\u4E2A\u76EE\u5F55\u8BF7\u7528 transfer_upload\u3002",
    inputSchema: targetSchema({
      path: { type: "string" },
      content: { type: "string" },
      encoding: { type: "string", enum: ["utf-8", "base64"] }
    })
  },
  {
    name: "file_mkdir",
    title: "\u521B\u5EFA\u8FDC\u7AEF\u76EE\u5F55",
    inputSchema: targetSchema({ path: { type: "string" } }),
    annotations: { idempotentHint: true }
  },
  {
    name: "file_rename",
    title: "\u91CD\u547D\u540D\u8FDC\u7AEF\u8DEF\u5F84",
    inputSchema: targetSchema({ from: { type: "string" }, to: { type: "string" } })
  },
  {
    name: "file_delete",
    title: "\u5220\u9664\u8FDC\u7AEF\u8DEF\u5F84",
    description:
      "\u5220\u9664\u8FDC\u7AEF\u8DEF\u5F84\u3002\u76EE\u5F55\u4F1A\u88AB\u9012\u5F52\u5220\u9664\u4E14\u4E0D\u53EF\u6062\u590D\uFF0C\u59CB\u7EC8\u9700\u8981\u7528\u6237\u5728 NextShell \u5185\u786E\u8BA4\u3002",
    inputSchema: targetSchema({
      path: { type: "string" },
      type: { type: "string", enum: ["file", "directory", "link"] }
    }),
    annotations: { destructiveHint: true }
  },
  {
    name: "transfer_upload",
    title: "\u4E0A\u4F20\u5230\u8FDC\u7AEF",
    description:
      "\u628A\u672C\u673A\u6587\u4EF6\u6216\u76EE\u5F55\u4F20\u5230\u4E3B\u673A\uFF08\u76EE\u5F55\u81EA\u52A8\u6253\u5305\u4E3A tar.gz \u5E76\u5728\u8FDC\u7AEF\u89E3\u5305\uFF09\u3002\u7ACB\u5373\u8FD4\u56DE taskId\uFF0C\u7528 transfer_status \u8F6E\u8BE2\u3002",
    inputSchema: targetSchema({
      localPath: { type: "string" },
      remotePath: { type: "string" }
    })
  },
  {
    name: "transfer_download",
    title: "\u4E0B\u8F7D\u5230\u672C\u673A",
    description:
      "\u628A\u8FDC\u7AEF\u6587\u4EF6\u4E0B\u8F7D\u5230\u672C\u673A\u3002\u7ACB\u5373\u8FD4\u56DE taskId\uFF0C\u7528 transfer_status \u8F6E\u8BE2\u3002",
    inputSchema: targetSchema({
      remotePath: { type: "string" },
      localPath: { type: "string" }
    })
  },
  {
    name: "transfer_status",
    title: "\u67E5\u8BE2\u4F20\u8F93\u8FDB\u5EA6",
    inputSchema: {
      type: "object",
      properties: { taskId: { type: "string" } },
      required: ["taskId"]
    },
    annotations: { readOnlyHint: true }
  },
  {
    name: "transfer_cancel",
    title: "\u53D6\u6D88\u4F20\u8F93",
    inputSchema: {
      type: "object",
      properties: { taskId: { type: "string" } },
      required: ["taskId"]
    },
    annotations: { idempotentHint: true }
  },
  {
    name: "session_send_keys",
    title: "\u5411\u4F1A\u8BDD\u6CE8\u5165\u8F93\u5165",
    description:
      "\u50CF\u7528\u6237\u4E00\u6837\u5F80\u6D3B\u52A8\u4F1A\u8BDD\u7684 PTY \u91CC\u6253\u5B57\u3002\u9664\u975E\u72B6\u6001\u5C31\u6D3B\u5728\u90A3\u4E2A shell \u91CC\uFF08TUI\u3001\u4EA4\u4E92\u5F0F\u63D0\u793A\u3001\u5DF2\u8FDB\u5165\u7684 venv \u6216\u5BB9\u5668\uFF09\uFF0C\u5426\u5219\u4F18\u5148\u7528 exec\u3002\u59CB\u7EC8\u9700\u8981\u7528\u6237\u786E\u8BA4\uFF1B\u7528\u6237\u6B63\u5728\u8BE5\u4F1A\u8BDD\u91CC\u6253\u5B57\u65F6\u4F1A\u88AB\u62D2\u7EDD\u3002",
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
    title: "\u5411\u4F1A\u8BDD\u53D1\u9001\u63A7\u5236\u4FE1\u53F7",
    description:
      "\u5411\u6D3B\u52A8\u4F1A\u8BDD\u53D1\u9001\u63A7\u5236\u5B57\u7B26\uFF1Ainterrupt\uFF08Ctrl-C\uFF09/ eof / suspend / quit\u3002",
    inputSchema: targetSchema({
      signal: { type: "string", enum: ["interrupt", "eof", "suspend", "quit"] }
    }),
    annotations: { destructiveHint: true }
  },
  {
    name: "session_open",
    title: "\u6253\u5F00\u7EC8\u7AEF\u6807\u7B7E",
    description:
      "\u5728\u4E3B\u673A\u4E0A\u6253\u5F00\u4E00\u4E2A\u771F\u5B9E\u53EF\u89C1\u7684\u7EC8\u7AEF\u6807\u7B7E\uFF0C\u5E76\u6807\u8BB0\u4E3A Agent \u63A7\u5236\u4E2D\u3002",
    inputSchema: targetSchema()
  },
  {
    name: "session_close",
    title: "\u5173\u95ED\u7EC8\u7AEF\u6807\u7B7E",
    inputSchema: targetSchema(),
    annotations: { destructiveHint: true }
  },
  {
    name: "session_focus",
    title: "\u805A\u7126\u7EC8\u7AEF\u6807\u7B7E",
    description:
      "\u628A NextShell \u7A97\u53E3\u7F6E\u9876\u5E76\u5207\u5230\u8BE5\u4F1A\u8BDD\u7684\u6807\u7B7E\uFF0C\u7528\u4E8E\u628A\u4E8B\u60C5\u4EA4\u56DE\u7ED9\u4EBA\u3002",
    inputSchema: targetSchema(),
    annotations: { idempotentHint: true }
  },
  {
    name: "ask_user",
    title: "\u8BE2\u95EE\u7528\u6237",
    description:
      "\u5728 NextShell \u5185\u5F39\u51FA\u786E\u8BA4\u3001\u9009\u62E9\u6216\u6587\u672C\u95EE\u8BE2\u3002",
    inputSchema: {
      type: "object",
      properties: {
        question: { type: "string" },
        choices: { type: "array", items: { type: "string" } }
      },
      required: ["question"]
    }
  },
  {
    name: "notify_user",
    title: "\u901A\u77E5\u7528\u6237",
    inputSchema: {
      type: "object",
      properties: { title: { type: "string" }, message: { type: "string" } },
      required: ["title", "message"]
    }
  }
];
var parseToolDescriptors = (value) => {
  if (!isRecord(value) || !Array.isArray(value.tools)) {
    return null;
  }
  const tools = [];
  for (const entry of value.tools) {
    if (isRecord(entry) && typeof entry.name === "string" && entry.name.length > 0) {
      tools.push(entry);
    }
  }
  return tools;
};

// src/cli-core.ts
var RESERVED_FLAGS = /* @__PURE__ */ new Set(["json", "full", "timeout", "help"]);
var coerceFlagValue = (raw) => {
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
    return JSON.parse(trimmed);
  } catch {
    return raw;
  }
};
var usage = (message) => ({ kind: "usage-error", message });
var parseCliInvocation = (argv) => {
  const [command, ...rest] = argv;
  if (command === void 0 || command === "help" || command === "--help" || command === "-h") {
    return { kind: "help" };
  }
  if (command === "status") {
    return rest.length === 0 ? { kind: "status" } : usage("status \u4E0D\u63A5\u53D7\u53C2\u6570");
  }
  if (command === "tools") {
    if (rest.every((flag) => flag === "--full")) {
      return { kind: "tools", full: rest.includes("--full") };
    }
    return usage("tools \u53EA\u63A5\u53D7 --full");
  }
  if (command.startsWith("-")) {
    return usage(
      `\u672A\u77E5\u9009\u9879\uFF1A${command}\uFF08\u5DE5\u5177\u540D\u5FC5\u987B\u662F\u7B2C\u4E00\u4E2A\u53C2\u6570\uFF09`
    );
  }
  const args = {};
  let full = false;
  let timeoutSec = null;
  for (let i = 0; i < rest.length; i += 1) {
    const token = rest[i] ?? "";
    if (!token.startsWith("--")) {
      return usage(
        `\u591A\u4F59\u7684\u4F4D\u7F6E\u53C2\u6570\uFF1A${token}\uFF08\u5DE5\u5177\u53C2\u6570\u5199\u6210 --key value\uFF09`
      );
    }
    const body = token.slice(2);
    const eq = body.indexOf("=");
    const key = eq === -1 ? body : body.slice(0, eq);
    let value = eq === -1 ? null : body.slice(eq + 1);
    if (key.length === 0) {
      return usage(`\u65E0\u6CD5\u89E3\u6790\u7684\u9009\u9879\uFF1A${token}`);
    }
    if (value === null) {
      const next = rest[i + 1];
      if (next !== void 0 && !next.startsWith("--")) {
        value = next;
        i += 1;
      }
    }
    if (key === "help") {
      return { kind: "help" };
    }
    if (key === "full") {
      if (value !== null) {
        return usage("--full \u4E0D\u63A5\u53D7\u503C");
      }
      full = true;
      continue;
    }
    if (key === "timeout") {
      const parsed = value === null ? Number.NaN : Number.parseInt(value, 10);
      if (!Number.isInteger(parsed) || parsed <= 0) {
        return usage("--timeout \u9700\u8981\u4E00\u4E2A\u6B63\u6574\u6570\u79D2\u6570");
      }
      timeoutSec = parsed;
      continue;
    }
    if (key === "json") {
      if (value === null) {
        return usage("--json \u9700\u8981\u4E00\u4E2A JSON \u5BF9\u8C61\u5B57\u7B26\u4E32");
      }
      let parsed;
      try {
        parsed = JSON.parse(value);
      } catch {
        return usage("--json \u7684\u503C\u4E0D\u662F\u5408\u6CD5 JSON");
      }
      if (!isRecord(parsed)) {
        return usage("--json \u7684\u503C\u5FC5\u987B\u662F\u4E00\u4E2A JSON \u5BF9\u8C61");
      }
      Object.assign(args, parsed);
      continue;
    }
    if (RESERVED_FLAGS.has(key)) {
      return usage(`\u9009\u9879 --${key} \u7528\u6CD5\u4E0D\u5BF9`);
    }
    args[key] = value === null ? true : coerceFlagValue(value);
  }
  return { kind: "call", tool: command, args, full, timeoutSec };
};
var renderCallResult = (result, full) => {
  if (!isRecord(result)) {
    return { text: JSON.stringify(result), isError: false };
  }
  const isError = result.isError === true;
  if (full) {
    return { text: JSON.stringify(result, null, 2), isError };
  }
  const content = Array.isArray(result.content) ? result.content : [];
  const texts = [];
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
var renderToolList = (tools, full) => {
  if (full) {
    return JSON.stringify(tools, null, 2);
  }
  const width = Math.max(...tools.map((tool) => tool.name.length));
  return tools
    .map((tool) => {
      const label = tool.title ?? "";
      const summary = tool.description ?? "";
      const doc = label && summary ? `${label} \u2014 ${summary}` : label || summary;
      return `${tool.name.padEnd(width)}  ${doc}`.trimEnd();
    })
    .join("\n");
};
var HELP_TEXT = `nextshell-cli \u2014 \u901A\u8FC7\u672C\u673A\u8FD0\u884C\u7684 NextShell \u684C\u9762\u5E94\u7528\u64CD\u4F5C\u8FDC\u7A0B\u670D\u52A1\u5668

\u7528\u6CD5:
  nextshell-cli status                     \u68C0\u67E5 NextShell \u662F\u5426\u53EF\u8FBE
  nextshell-cli tools [--full]             \u5217\u51FA\u53EF\u7528\u5DE5\u5177\uFF08--full \u8F93\u51FA\u5B8C\u6574 JSON schema\uFF09
  nextshell-cli <tool> [--key value ...]   \u8C03\u7528\u4E00\u4E2A\u5DE5\u5177
  nextshell-cli <tool> --json '{...}'      \u7528 JSON \u5BF9\u8C61\u4F20\u53C2\uFF08\u4E0E --key \u53EF\u6DF7\u7528\uFF09

\u901A\u7528\u9009\u9879:
  --full           \u8F93\u51FA\u5B8C\u6574\u7ED3\u679C JSON\uFF08\u9ED8\u8BA4\u53EA\u8F93\u51FA\u6587\u672C\u5185\u5BB9\uFF09
  --timeout <sec>  \u8986\u76D6\u672C\u6B21\u8C03\u7528\u7684\u7B49\u5F85\u79D2\u6570

\u793A\u4F8B:
  nextshell-cli host_list
  nextshell-cli exec --target web-1 --command "systemctl status nginx"
  nextshell-cli file_read --target web-1 --path /var/log/nginx/error.log
  nextshell-cli transfer_upload --target web-1 --localPath ./dist.tar.gz --remotePath /opt/app/

\u9000\u51FA\u7801: 0 \u6210\u529F\uFF1B1 \u5DE5\u5177\u6267\u884C\u51FA\u9519\uFF1B2 \u7528\u6CD5\u9519\u8BEF\uFF1B3 \u65E0\u6CD5\u8FDE\u63A5 NextShell`;

// src/endpoint.ts
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
var ENDPOINT_ENV_VAR = "NEXTSHELL_MCP_ENDPOINT";
var ENDPOINT_TOKEN_ENV_VAR = "NEXTSHELL_MCP_TOKEN";
var DEFAULT_HTTP_PATH = "/mcp";
var ENDPOINT_DIRECTORY_NAME = "mcp";
var ENDPOINT_FILE_NAME = "endpoint.json";
var APP_DIRECTORY_CANDIDATES = ["NextShell", "nextshell"];
var WINDOWS_PIPE_PREFIX = "\\\\.\\pipe\\";
var isNamedPipe = (value) =>
  value.startsWith(WINDOWS_PIPE_PREFIX) || value.startsWith("\\\\?\\pipe\\");
var isProcessAlive = (pid) => {
  if (!Number.isInteger(pid) || pid <= 0) {
    return false;
  }
  try {
    process.kill(pid, 0);
    return true;
  } catch (error) {
    return isRecord(error) && error.code === "EPERM";
  }
};
var defaultFileExists = (file) => {
  try {
    fs.statSync(file);
    return true;
  } catch {
    return false;
  }
};
var defaultReadDir = (dir) => {
  try {
    return fs.readdirSync(dir);
  } catch {
    return [];
  }
};
var defaultReadFile = (file) => fs.readFileSync(file, "utf8");
var readString = (source, keys) => {
  for (const key of keys) {
    const value = source[key];
    if (typeof value === "string" && value.trim().length > 0) {
      return value.trim();
    }
  }
  return null;
};
var readPort = (source, keys) => {
  for (const key of keys) {
    const value = source[key];
    if (typeof value === "number" && Number.isInteger(value) && value > 0 && value <= 65535) {
      return value;
    }
  }
  return null;
};
var readTimestamp = (source, keys) => {
  for (const key of keys) {
    const value = source[key];
    if (typeof value === "number" && Number.isFinite(value) && value > 0) {
      return value;
    }
    if (typeof value === "string") {
      const parsed = Date.parse(value);
      if (Number.isFinite(parsed)) {
        return parsed;
      }
    }
  }
  return 0;
};
var normalizeHttpPath = (value) => {
  if (value === null) {
    return DEFAULT_HTTP_PATH;
  }
  return value.startsWith("/") ? value : `/${value}`;
};
var parseEndpointRecords = (raw, source) => {
  let entries;
  if (Array.isArray(raw)) {
    entries = raw;
  } else if (isRecord(raw)) {
    const nested = raw.endpoints ?? raw.instances;
    entries = Array.isArray(nested) ? nested : [raw];
  } else {
    entries = [];
  }
  const records = [];
  for (const entry of entries) {
    if (!isRecord(entry)) {
      continue;
    }
    const socketPath = readString(entry, ["socketPath", "socket", "pipePath", "pipe"]);
    const tcpPort = readPort(entry, ["httpPort", "tcpPort", "port"]);
    if (socketPath === null && tcpPort === null) {
      continue;
    }
    const pidValue = entry.pid;
    records.push({
      socketPath,
      host: readString(entry, ["host", "address"]) ?? "127.0.0.1",
      tcpPort,
      token: readString(entry, ["token"]),
      httpPath: normalizeHttpPath(readString(entry, ["httpPath", "path", "endpointPath"])),
      pid:
        typeof pidValue === "number" && Number.isInteger(pidValue) && pidValue > 0
          ? pidValue
          : null,
      updatedAt: readTimestamp(entry, ["updatedAt", "startedAt", "timestamp", "mtime"]),
      source
    });
  }
  return records;
};
var pathFor = (platform) => (platform === "win32" ? path.win32 : path.posix);
var resolveUserDataDirs = (deps = {}) => {
  const env = deps.env ?? process.env;
  const platform = deps.platform ?? process.platform;
  const homeDir = deps.homeDir ?? (() => os.homedir());
  const join = pathFor(platform).join;
  const bases = [];
  if (platform === "win32") {
    const appData = env.APPDATA;
    bases.push(
      appData !== void 0 && appData.length > 0
        ? appData
        : join(env.USERPROFILE ?? homeDir(), "AppData", "Roaming")
    );
  } else if (platform === "darwin") {
    bases.push(join(env.HOME ?? homeDir(), "Library", "Application Support"));
  } else {
    const xdg = env.XDG_CONFIG_HOME;
    bases.push(xdg !== void 0 && xdg.length > 0 ? xdg : join(env.HOME ?? homeDir(), ".config"));
  }
  const dirs = [];
  for (const base of bases) {
    for (const appDir of APP_DIRECTORY_CANDIDATES) {
      const candidate = join(base, appDir);
      if (!dirs.includes(candidate)) {
        dirs.push(candidate);
      }
    }
  }
  return dirs;
};
var recordKey = (record) =>
  `${record.socketPath ?? ""}|${record.host}|${record.tcpPort ?? 0}|${record.pid ?? 0}`;
var readEndpointRecords = (deps = {}) => {
  const readFile = deps.readFile ?? defaultReadFile;
  const readDir = deps.readDir ?? defaultReadDir;
  const join = pathFor(deps.platform ?? process.platform).join;
  const records = [];
  const seen = /* @__PURE__ */ new Set();
  for (const userDataDir of resolveUserDataDirs(deps)) {
    const dir = join(userDataDir, ENDPOINT_DIRECTORY_NAME);
    const names = readDir(dir).filter((name) => /^endpoint.*\.json$/i.test(name));
    if (!names.includes(ENDPOINT_FILE_NAME)) {
      names.unshift(ENDPOINT_FILE_NAME);
    }
    for (const name of names) {
      const file = join(dir, name);
      let raw;
      try {
        raw = readFile(file);
      } catch {
        continue;
      }
      let parsed;
      try {
        parsed = parseEndpointRecords(JSON.parse(raw), file);
      } catch {
        continue;
      }
      for (const record of parsed) {
        const key = recordKey(record);
        if (seen.has(key)) {
          continue;
        }
        seen.add(key);
        records.push(record);
      }
    }
  }
  return records;
};
var selectEndpointTargets = (records, deps = {}) => {
  const alive = deps.isProcessAlive ?? isProcessAlive;
  const exists = deps.fileExists ?? defaultFileExists;
  const live = records.filter((record) => record.pid === null || alive(record.pid));
  const ordered = [...live].sort((left, right) => right.updatedAt - left.updatedAt);
  const targets = [];
  for (const record of ordered) {
    const socketPath = record.socketPath;
    if (socketPath !== null && (isNamedPipe(socketPath) || exists(socketPath))) {
      targets.push({
        transport: "socket",
        socketPath,
        host: null,
        port: null,
        token: null,
        httpPath: record.httpPath,
        source: record.source
      });
    }
    if (record.tcpPort !== null) {
      targets.push({
        transport: "tcp",
        socketPath: null,
        host: record.host,
        port: record.tcpPort,
        token: record.token,
        httpPath: record.httpPath,
        source: record.source
      });
    }
  }
  return targets;
};
var parseEndpointOverride = (value, deps = {}) => {
  const trimmed = value.trim();
  if (trimmed.length === 0) {
    return [];
  }
  const env = deps.env ?? process.env;
  const envToken = env[ENDPOINT_TOKEN_ENV_VAR];
  const token = envToken !== void 0 && envToken.length > 0 ? envToken : null;
  if (/^https?:\/\//i.test(trimmed)) {
    let url;
    try {
      url = new URL(trimmed);
    } catch {
      return [];
    }
    const port = url.port.length > 0 ? Number.parseInt(url.port, 10) : 80;
    return [
      {
        transport: "tcp",
        socketPath: null,
        host: url.hostname,
        port,
        token,
        httpPath: url.pathname.length > 1 ? url.pathname : DEFAULT_HTTP_PATH,
        source: ENDPOINT_ENV_VAR
      }
    ];
  }
  if (trimmed.toLowerCase().endsWith(".json")) {
    const readFile = deps.readFile ?? defaultReadFile;
    try {
      const records = parseEndpointRecords(JSON.parse(readFile(trimmed)), trimmed);
      return selectEndpointTargets(records, deps);
    } catch {
      return [];
    }
  }
  return [
    {
      transport: "socket",
      socketPath: trimmed,
      host: null,
      port: null,
      token,
      httpPath: DEFAULT_HTTP_PATH,
      source: ENDPOINT_ENV_VAR
    }
  ];
};
var discoverEndpointTargets = (deps = {}) => {
  const env = deps.env ?? process.env;
  const override = env[ENDPOINT_ENV_VAR];
  if (override !== void 0 && override.trim().length > 0) {
    return parseEndpointOverride(override, deps);
  }
  return selectEndpointTargets(readEndpointRecords(deps), deps);
};

// src/upstream.ts
import http from "node:http";

// src/sse.ts
var SseParser = class {
  buffer = "";
  dataLines = [];
  eventName = null;
  push(chunk) {
    this.buffer += chunk;
    const messages = [];
    let newlineIndex = this.buffer.indexOf("\n");
    while (newlineIndex !== -1) {
      let line = this.buffer.slice(0, newlineIndex);
      this.buffer = this.buffer.slice(newlineIndex + 1);
      if (line.endsWith("\r")) {
        line = line.slice(0, -1);
      }
      this.handleLine(line, messages);
      newlineIndex = this.buffer.indexOf("\n");
    }
    return messages;
  }
  handleLine(line, sink) {
    if (line === "") {
      this.dispatch(sink);
      return;
    }
    if (line.startsWith(":")) {
      return;
    }
    const colonIndex = line.indexOf(":");
    const field = colonIndex === -1 ? line : line.slice(0, colonIndex);
    let value = colonIndex === -1 ? "" : line.slice(colonIndex + 1);
    if (value.startsWith(" ")) {
      value = value.slice(1);
    }
    if (field === "event") {
      this.eventName = value;
      return;
    }
    if (field === "data") {
      this.dataLines.push(value);
    }
  }
  dispatch(sink) {
    const payload = this.dataLines.join("\n");
    const eventName = this.eventName;
    this.dataLines = [];
    this.eventName = null;
    if (payload === "") {
      return;
    }
    if (eventName !== null && eventName !== "message") {
      return;
    }
    let parsed;
    try {
      parsed = JSON.parse(payload);
    } catch {
      return;
    }
    const entries = Array.isArray(parsed) ? parsed : [parsed];
    for (const entry of entries) {
      if (isRecord(entry)) {
        sink.push(entry);
      }
    }
  }
};

// src/upstream.ts
var MAX_RESPONSE_BYTES = 32 * 1024 * 1024;
var MAX_ERROR_BODY_CHARS = 512;
var UpstreamError = class extends Error {
  code;
  status;
  constructor(message, code, status = null) {
    super(message);
    this.name = "UpstreamError";
    this.code = code;
    this.status = status;
  }
};
var UpstreamRpcError = class extends UpstreamError {
  body;
  constructor(body) {
    super(body.message, "RPC_ERROR");
    this.name = "UpstreamRpcError";
    this.body = body;
  }
};
var toUpstreamError = (error) => {
  if (error instanceof UpstreamError) {
    return error;
  }
  const code = isRecord(error) && typeof error.code === "string" ? error.code : "";
  const message = error instanceof Error ? error.message : String(error);
  if (code === "ECONNREFUSED") {
    return new UpstreamError(message, "REFUSED");
  }
  if (code === "ETIMEDOUT" || code === "ECONNRESET" || code === "EPIPE") {
    return new UpstreamError(message, "TIMEOUT");
  }
  return new UpstreamError(message, "UNREACHABLE");
};
var headerValue = (value) => {
  if (typeof value === "string") {
    return value;
  }
  if (Array.isArray(value)) {
    return value[0] ?? null;
  }
  return null;
};
var buildRequestOptions = (target, method, headers) => {
  const options = { method, path: target.httpPath, headers };
  if (target.transport === "socket" && target.socketPath !== null) {
    options.socketPath = target.socketPath;
  } else {
    options.host = target.host ?? "127.0.0.1";
    options.port = target.port ?? 0;
  }
  return options;
};
var buildHeaders = (target, sessionId, protocolVersion, extra = {}) => {
  const headers = {
    accept: "application/json, text/event-stream",
    ...extra
  };
  if (sessionId !== null) {
    headers["mcp-session-id"] = sessionId;
  }
  if (protocolVersion !== null) {
    headers["mcp-protocol-version"] = protocolVersion;
  }
  if (target.token !== null && target.token.length > 0) {
    headers.authorization = `Bearer ${target.token}`;
  }
  return headers;
};
var postJsonRpc = (args) =>
  new Promise((resolve, reject) => {
    const payload = Buffer.from(JSON.stringify(args.message), "utf8");
    const headers = buildHeaders(args.target, args.sessionId, args.protocolVersion, {
      "content-type": "application/json",
      "content-length": String(payload.byteLength)
    });
    const request = http.request(buildRequestOptions(args.target, "POST", headers));
    let settled = false;
    const timer = setTimeout(() => {
      if (settled) {
        return;
      }
      settled = true;
      request.destroy();
      reject(new UpstreamError("upstream request timed out", "TIMEOUT"));
    }, args.timeoutMs);
    timer.unref();
    const succeed = (outcome) => {
      if (settled) {
        return;
      }
      settled = true;
      clearTimeout(timer);
      resolve(outcome);
    };
    const fail2 = (error) => {
      if (settled) {
        return;
      }
      settled = true;
      clearTimeout(timer);
      reject(toUpstreamError(error));
    };
    request.on("error", fail2);
    request.on("response", (response) => {
      const status = response.statusCode ?? 0;
      const sessionId = headerValue(response.headers["mcp-session-id"]);
      const contentType = (headerValue(response.headers["content-type"]) ?? "").toLowerCase();
      response.setEncoding("utf8");
      if (status >= 400) {
        let body = "";
        response.on("data", (chunk) => {
          if (body.length < MAX_ERROR_BODY_CHARS) {
            body += chunk;
          }
        });
        response.on("end", () => {
          const detail = body.slice(0, MAX_ERROR_BODY_CHARS).replace(/\s+/g, " ").trim();
          if (status === 401 || status === 403) {
            fail2(
              new UpstreamError(`upstream rejected the bridge: ${detail}`, "UNAUTHORIZED", status)
            );
            return;
          }
          if (status === 404) {
            fail2(new UpstreamError("upstream session is gone", "SESSION_EXPIRED", status));
            return;
          }
          fail2(
            new UpstreamError(`upstream returned HTTP ${status}: ${detail}`, "HTTP_ERROR", status)
          );
        });
        response.on("error", fail2);
        return;
      }
      if (args.awaitId === null) {
        response.resume();
        response.on("end", () => succeed({ status, sessionId, response: null }));
        response.on("error", fail2);
        return;
      }
      const consume = (message) => {
        if (!isRecord(message)) {
          return false;
        }
        if (typeof message.method === "string") {
          if (message.id === void 0) {
            args.onNotification?.(message);
          } else {
            args.log?.(`ignoring unsupported upstream request: ${message.method}`);
          }
          return false;
        }
        if (message.id === args.awaitId) {
          succeed({ status, sessionId, response: message });
          response.destroy();
          request.destroy();
          return true;
        }
        return false;
      };
      let received = 0;
      if (contentType.startsWith("text/event-stream")) {
        const parser = new SseParser();
        response.on("data", (chunk) => {
          received += chunk.length;
          if (received > MAX_RESPONSE_BYTES) {
            fail2(new UpstreamError("upstream response exceeded the size limit", "PROTOCOL"));
            response.destroy();
            request.destroy();
            return;
          }
          for (const message of parser.push(chunk)) {
            if (consume(message)) {
              return;
            }
          }
        });
      } else {
        let body = "";
        response.on("data", (chunk) => {
          received += chunk.length;
          if (received > MAX_RESPONSE_BYTES) {
            fail2(new UpstreamError("upstream response exceeded the size limit", "PROTOCOL"));
            response.destroy();
            request.destroy();
            return;
          }
          body += chunk;
        });
        response.on("end", () => {
          if (settled) {
            return;
          }
          let parsed;
          try {
            parsed = JSON.parse(body);
          } catch {
            fail2(new UpstreamError("upstream returned malformed JSON", "PROTOCOL"));
            return;
          }
          const entries = Array.isArray(parsed) ? parsed : [parsed];
          for (const entry of entries) {
            if (consume(entry)) {
              return;
            }
          }
          fail2(new UpstreamError("upstream response carried no matching reply", "PROTOCOL"));
        });
      }
      response.on("end", () => {
        if (!settled) {
          fail2(new UpstreamError("upstream closed the stream before replying", "PROTOCOL"));
        }
      });
      response.on("error", fail2);
    });
    request.end(payload);
  });
var UpstreamSession = class _UpstreamSession {
  constructor(target, options, sessionId, protocolVersion, serverInfo) {
    this.target = target;
    this.options = options;
    this.sessionId = sessionId;
    this.protocolVersion = protocolVersion;
    this.serverInfo = serverInfo;
  }
  target;
  options;
  sessionId;
  protocolVersion;
  serverInfo;
  nextId = 1;
  closed = false;
  get transport() {
    return this.target.transport;
  }
  static async open(target, options) {
    const outcome = await postJsonRpc({
      target,
      message: {
        jsonrpc: "2.0",
        id: 0,
        method: "initialize",
        params: {
          protocolVersion: options.protocolVersion,
          capabilities: {},
          clientInfo: options.clientInfo
        }
      },
      awaitId: 0,
      sessionId: null,
      protocolVersion: null,
      timeoutMs: options.connectTimeoutMs,
      onNotification: options.onNotification,
      log: options.log
    });
    const response = outcome.response;
    if (response === null) {
      throw new UpstreamError("upstream did not answer initialize", "PROTOCOL");
    }
    if (response.error !== void 0) {
      throw new UpstreamRpcError(response.error);
    }
    const result = isRecord(response.result) ? response.result : {};
    const protocolVersion =
      typeof result.protocolVersion === "string" ? result.protocolVersion : options.protocolVersion;
    const info = isRecord(result.serverInfo) ? result.serverInfo : null;
    const serverInfo =
      info !== null && typeof info.name === "string"
        ? { name: info.name, version: typeof info.version === "string" ? info.version : "" }
        : null;
    const session = new _UpstreamSession(
      target,
      options,
      outcome.sessionId,
      protocolVersion,
      serverInfo
    );
    await session.notify("notifications/initialized");
    return session;
  }
  async notify(method, params) {
    await postJsonRpc({
      target: this.target,
      message: { jsonrpc: "2.0", method, ...(params === void 0 ? {} : { params }) },
      awaitId: null,
      sessionId: this.sessionId,
      protocolVersion: this.protocolVersion,
      timeoutMs: this.options.connectTimeoutMs,
      log: this.options.log
    });
  }
  async request(method, params, timeoutMs) {
    if (this.closed) {
      throw new UpstreamError("upstream session is closed", "SESSION_EXPIRED");
    }
    const id = this.nextId++;
    const outcome = await postJsonRpc({
      target: this.target,
      message: { jsonrpc: "2.0", id, method, ...(params === void 0 ? {} : { params }) },
      awaitId: id,
      sessionId: this.sessionId,
      protocolVersion: this.protocolVersion,
      timeoutMs: timeoutMs ?? this.options.requestTimeoutMs,
      onNotification: this.options.onNotification,
      log: this.options.log
    });
    const response = outcome.response;
    if (response === null) {
      throw new UpstreamError(`upstream did not answer ${method}`, "PROTOCOL");
    }
    if (response.error !== void 0) {
      throw new UpstreamRpcError(response.error);
    }
    return response.result;
  }
  async close() {
    if (this.closed) {
      return;
    }
    this.closed = true;
    if (this.sessionId === null) {
      return;
    }
    await new Promise((resolve) => {
      const headers = buildHeaders(this.target, this.sessionId, this.protocolVersion);
      const request = http.request(buildRequestOptions(this.target, "DELETE", headers));
      const done = () => resolve();
      request.on("error", done);
      request.on("response", (response) => {
        response.resume();
        response.on("end", done);
        response.on("error", done);
      });
      request.setTimeout(this.options.connectTimeoutMs, () => request.destroy());
      request.end();
    });
  }
};

// src/cli.ts
var CLI_NAME = "nextshell-cli";
var CLI_VERSION = "0.1.0";
var EXIT_TOOL_ERROR = 1;
var EXIT_USAGE = 2;
var EXIT_UNREACHABLE = 3;
var fail = (code, message) => {
  process.stderr.write(`${message}
`);
  process.exit(code);
};
var connect = async () => {
  const targets = discoverEndpointTargets();
  for (const target of targets) {
    try {
      return await UpstreamSession.open(target, {
        clientInfo: { name: CLI_NAME, version: CLI_VERSION },
        protocolVersion: LATEST_PROTOCOL_VERSION,
        connectTimeoutMs: DEFAULT_CONNECT_TIMEOUT_MS,
        requestTimeoutMs: DEFAULT_REQUEST_TIMEOUT_MS
      });
    } catch {}
  }
  return null;
};
var main = async () => {
  const invocation = parseCliInvocation(process.argv.slice(2));
  if (invocation.kind === "help") {
    process.stdout.write(`${HELP_TEXT}
`);
    return;
  }
  if (invocation.kind === "usage-error") {
    fail(
      EXIT_USAGE,
      `${invocation.message}

${HELP_TEXT}`
    );
    return;
  }
  if (invocation.kind === "status") {
    const session2 = await connect();
    if (session2 === null) {
      process.stdout.write(`${JSON.stringify({ running: false, message: UNAVAILABLE_MESSAGE })}
`);
      process.exit(EXIT_UNREACHABLE);
    }
    process.stdout.write(
      `${JSON.stringify({ running: true, transport: session2.transport, server: session2.serverInfo })}
`
    );
    await session2.close();
    return;
  }
  if (invocation.kind === "tools") {
    const session2 = await connect();
    let tools = STATIC_TOOLS;
    if (session2 !== null) {
      try {
        tools = parseToolDescriptors(await session2.request("tools/list")) ?? STATIC_TOOLS;
      } finally {
        await session2.close();
      }
    } else {
      process.stderr.write(
        "NextShell \u672A\u8FDE\u63A5\uFF0C\u4EE5\u4E0B\u662F\u9759\u6001\u6E05\u5355\uFF08\u5B9E\u9645\u53EF\u7528\u6027\u4EE5\u8FD0\u884C\u4E2D\u7684\u5E94\u7528\u4E3A\u51C6\uFF09\n"
      );
    }
    process.stdout.write(`${renderToolList(tools, invocation.full)}
`);
    return;
  }
  const session = await connect();
  if (session === null) {
    fail(EXIT_UNREACHABLE, UNAVAILABLE_MESSAGE);
    return;
  }
  try {
    const timeoutMs =
      invocation.timeoutSec !== null ? invocation.timeoutSec * 1e3 : DEFAULT_CALL_TIMEOUT_MS;
    const result = await session.request(
      "tools/call",
      { name: invocation.tool, arguments: invocation.args },
      timeoutMs
    );
    const rendered = renderCallResult(result, invocation.full);
    process.stdout.write(`${rendered.text}
`);
    if (rendered.isError) {
      process.exit(EXIT_TOOL_ERROR);
    }
  } catch (error) {
    if (error instanceof UpstreamRpcError) {
      fail(
        EXIT_TOOL_ERROR,
        `\u5DE5\u5177\u8C03\u7528\u88AB\u62D2\u7EDD\uFF1A${error.body.message}`
      );
    }
    if (error instanceof UpstreamError) {
      fail(
        EXIT_UNREACHABLE,
        `\u8FDE\u63A5 NextShell \u5931\u8D25\uFF08${error.code}\uFF09\uFF1A${error.message}`
      );
    }
    throw error;
  } finally {
    await session.close();
  }
};
main().catch((error) => {
  fail(EXIT_TOOL_ERROR, `nextshell-cli \u5185\u90E8\u9519\u8BEF\uFF1A${String(error)}`);
});
