/**
 * nextshell-cli — one-shot command wrapper around the NextShell MCP endpoint.
 *
 * Same trust model as the stdio bridge: this process never holds credentials,
 * it only forwards one tools/call and prints the result. Meant for coding
 * agents that speak shell but not MCP (see nextshell-plugin/skills/nextshell-cli).
 */
import {
  DEFAULT_CALL_TIMEOUT_MS,
  DEFAULT_CONNECT_TIMEOUT_MS,
  DEFAULT_REQUEST_TIMEOUT_MS,
  LATEST_PROTOCOL_VERSION,
  UNAVAILABLE_MESSAGE
} from "./constants.js";
import { HELP_TEXT, parseCliInvocation, renderCallResult, renderToolList } from "./cli-core.js";
import { discoverEndpointTargets } from "./endpoint.js";
import { parseToolDescriptors, STATIC_TOOLS } from "./tools.js";
import { UpstreamError, UpstreamRpcError, UpstreamSession, type UpstreamLike } from "./upstream.js";

const CLI_NAME = "nextshell-cli";
/** Keep in sync with apps/mcp-bridge/package.json. */
const CLI_VERSION = "0.1.0";

const EXIT_TOOL_ERROR = 1;
const EXIT_USAGE = 2;
const EXIT_UNREACHABLE = 3;

const fail = (code: number, message: string): never => {
  process.stderr.write(`${message}\n`);
  process.exit(code);
};

const connect = async (): Promise<UpstreamLike | null> => {
  const targets = discoverEndpointTargets();
  for (const target of targets) {
    try {
      return await UpstreamSession.open(target, {
        clientInfo: { name: CLI_NAME, version: CLI_VERSION },
        protocolVersion: LATEST_PROTOCOL_VERSION,
        connectTimeoutMs: DEFAULT_CONNECT_TIMEOUT_MS,
        requestTimeoutMs: DEFAULT_REQUEST_TIMEOUT_MS
      });
    } catch {
      // A stale endpoint file is normal after a crash; try the next candidate.
    }
  }
  return null;
};

const main = async (): Promise<void> => {
  const invocation = parseCliInvocation(process.argv.slice(2));

  if (invocation.kind === "help") {
    process.stdout.write(`${HELP_TEXT}\n`);
    return;
  }
  if (invocation.kind === "usage-error") {
    fail(EXIT_USAGE, `${invocation.message}\n\n${HELP_TEXT}`);
    return;
  }

  if (invocation.kind === "status") {
    const session = await connect();
    if (session === null) {
      process.stdout.write(`${JSON.stringify({ running: false, message: UNAVAILABLE_MESSAGE })}\n`);
      process.exit(EXIT_UNREACHABLE);
    }
    process.stdout.write(
      `${JSON.stringify({ running: true, transport: session.transport, server: session.serverInfo })}\n`
    );
    await session.close();
    return;
  }

  if (invocation.kind === "tools") {
    const session = await connect();
    let tools = STATIC_TOOLS;
    if (session !== null) {
      try {
        tools = parseToolDescriptors(await session.request("tools/list")) ?? STATIC_TOOLS;
      } finally {
        await session.close();
      }
    } else {
      process.stderr.write("NextShell 未连接，以下是静态清单（实际可用性以运行中的应用为准）\n");
    }
    process.stdout.write(`${renderToolList(tools, invocation.full)}\n`);
    return;
  }

  const session = await connect();
  if (session === null) {
    fail(EXIT_UNREACHABLE, UNAVAILABLE_MESSAGE);
    return;
  }
  try {
    const timeoutMs =
      invocation.timeoutSec !== null ? invocation.timeoutSec * 1000 : DEFAULT_CALL_TIMEOUT_MS;
    const result = await session.request(
      "tools/call",
      { name: invocation.tool, arguments: invocation.args },
      timeoutMs
    );
    const rendered = renderCallResult(result, invocation.full);
    process.stdout.write(`${rendered.text}\n`);
    if (rendered.isError) {
      process.exit(EXIT_TOOL_ERROR);
    }
  } catch (error) {
    if (error instanceof UpstreamRpcError) {
      fail(EXIT_TOOL_ERROR, `工具调用被拒绝：${error.body.message}`);
    }
    if (error instanceof UpstreamError) {
      fail(EXIT_UNREACHABLE, `连接 NextShell 失败（${error.code}）：${error.message}`);
    }
    throw error;
  } finally {
    await session.close();
  }
};

main().catch((error: unknown) => {
  fail(EXIT_TOOL_ERROR, `nextshell-cli 内部错误：${String(error)}`);
});
