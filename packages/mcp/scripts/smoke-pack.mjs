import { execFileSync } from "node:child_process";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import assert from "node:assert/strict";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";

const root = fileURLToPath(new URL("..", import.meta.url));
const dir = await mkdtemp(path.join(tmpdir(), "nextshell-mcp-pack-"));
const npmCommand = process.platform === "win32" ? process.execPath : "npm";
const npmArgs =
  process.platform === "win32"
    ? [path.join(path.dirname(process.execPath), "node_modules/npm/bin/npm-cli.js")]
    : [];
const runNpm = (args) =>
  execFileSync(npmCommand, [...npmArgs, ...args], { cwd: dir, encoding: "utf8" });
const client = new Client({ name: "packed-smoke", version: "1" });
try {
  const packed = JSON.parse(runNpm(["pack", root, "--ignore-scripts", "--json"]));
  runNpm([
    "install",
    "--ignore-scripts",
    "--no-audit",
    "--no-fund",
    path.join(dir, packed[0].filename)
  ]);
  const installed = path.join(dir, "node_modules/@nextshell/mcp");
  const manifest = JSON.parse(await readFile(path.join(installed, "package.json"), "utf8"));
  assert.ok(!Object.values(manifest.dependencies).some((value) => value.startsWith("workspace:")));
  await client.connect(
    new StdioClientTransport({
      command: npmCommand,
      args: [...npmArgs, "exec", "--offline", "--", "nextshell-mcp", "--port", "1"],
      cwd: dir
    })
  );
  assert.ok((await client.listTools()).tools.some((tool) => tool.name === "exec"));
  await client.ping();
  const result = await client.callTool({ name: "session_list", arguments: {} });
  assert.equal(result.structuredContent.error.code, "app_unavailable");
  console.log("Packed MCP install: offline discovery and tool failure passed");
} finally {
  await client.close();
  await rm(dir, { recursive: true, force: true });
}
