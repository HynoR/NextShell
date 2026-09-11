import { beforeAll, expect, test } from "vitest";
import { build } from "esbuild";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import { execFileSync } from "node:child_process";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";
import { version, name } from "../package.json";
import { MCP_PACKAGE_SPEC, buildStdioConfig } from "../../shared/src/mcp-config";
import pluginConfig from "../../../nextshell-plugin/.mcp.json";

const entry = fileURLToPath(new URL("../dist/index.js", import.meta.url));
beforeAll(async () => {
  await build({
    entryPoints: [fileURLToPath(new URL("./cli.ts", import.meta.url))],
    outfile: entry,
    bundle: true,
    platform: "node",
    format: "esm",
    packages: "external"
  });
});
test("published version and plugin configuration stay aligned", () => {
  expect(MCP_PACKAGE_SPEC).toBe(`${name}@${version}`);
  expect(pluginConfig.mcpServers.nextshell).toEqual(buildStdioConfig(41777));
});
test("real stdio process discovers tools offline and exits cleanly", async () => {
  const transport = new StdioClientTransport({
    command: process.execPath,
    args: [entry, "--port", "1"],
    stderr: "pipe",
    cwd: tmpdir()
  });
  let stderr = "";
  transport.stderr?.on("data", (chunk) => {
    stderr += String(chunk);
  });
  const client = new Client({ name: "cli-test", version: "1" });
  try {
    await client.connect(transport);
    expect((await client.listTools()).tools.some((tool) => tool.name === "exec")).toBe(true);
    await client.ping();
    expect(await client.callTool({ name: "session_list", arguments: {} })).toMatchObject({
      isError: true,
      structuredContent: { error: { code: "app_unavailable" } }
    });
    await client.ping();
    expect(stderr).toBe("");
  } finally {
    await client.close();
  }
});
test.each(["0", "65536", "1.5", "bad", "", "-1"])(
  "invalid port %s exits without protocol output",
  (port) => {
    try {
      execFileSync(process.execPath, [entry, "--port", port], { stdio: "pipe" });
      throw new Error("Expected invalid port to fail");
    } catch (error) {
      expect(error).toMatchObject({ status: 1, stdout: Buffer.alloc(0) });
    }
  }
);
