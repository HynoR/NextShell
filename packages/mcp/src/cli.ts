#!/usr/bin/env node
import { parseArgs } from "node:util";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { createStdioServer } from "./server";

async function main() {
  const { values } = parseArgs({
    options: { port: { type: "string", default: "41777" }, help: { type: "boolean", short: "h" } }
  });
  if (values.help) {
    process.stderr.write(
      "Usage: nextshell-mcp [--port 41777]\nRequires Node.js 24+. NextShell is needed only when calling tools.\n"
    );
    return;
  }
  const port = Number(values.port);
  if (!/^\d+$/.test(values.port!) || !Number.isInteger(port) || port < 1 || port > 65535)
    throw new Error("--port must be an integer between 1 and 65535");
  const { server, close } = createStdioServer(port);
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
