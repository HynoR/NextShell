#!/usr/bin/env node
import { parseArgs } from "node:util";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { resolveCredentials, TOKEN_ENV } from "./credentials";
import { createStdioServer } from "./server";

const USAGE = `Usage: nextshell-mcp [--port <n>] [--token <token>] [--config <mcp.json>]
Requires Node.js 24+. NextShell is needed only when calling tools.
Token: --token > ${TOKEN_ENV} > --config file > the mcp.json NextShell writes in its config
directory while MCP is enabled (设置 → Agent). Port: --port > that file > 41777.
Prefer ${TOKEN_ENV} or the file: command-line arguments are visible to other local users.
`;

async function main() {
  const { values } = parseArgs({
    options: {
      port: { type: "string" },
      token: { type: "string" },
      config: { type: "string" },
      help: { type: "boolean", short: "h" }
    }
  });
  if (values.help) {
    process.stderr.write(USAGE);
    return;
  }
  let port: number | undefined;
  if (values.port !== undefined) {
    port = Number(values.port);
    if (!/^\d+$/.test(values.port) || !Number.isInteger(port) || port < 1 || port > 65535)
      throw new Error("--port must be an integer between 1 and 65535");
  }
  if (values.config === "") throw new Error("--config requires a file path");
  const { server, close } = createStdioServer(() =>
    resolveCredentials({ port, token: values.token, config: values.config })
  );
  let stopping = false;
  const stop = async () => {
    if (stopping) return;
    stopping = true;
    await close();
    await server.close();
    process.exit(0);
  };
  process.once("SIGINT", () => {
    void stop();
  });
  process.once("SIGTERM", () => {
    void stop();
  });
  process.stdin.once("end", () => {
    void stop();
  });
  await server.connect(new StdioServerTransport());
}
main().catch((error) => {
  process.stderr.write(`${error instanceof Error ? error.message : String(error)}\n`);
  process.exitCode = 1;
});
