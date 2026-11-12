import { afterEach, beforeAll, expect, test } from "vitest";
import { build } from "esbuild";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { execFileSync } from "node:child_process";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";
import { version, name } from "../package.json";
import { MCP_PACKAGE_SPEC } from "../../shared/src/mcp-config";
import pluginConfig from "../../../nextshell-plugin/.mcp.json";
import { defaultConfigPath } from "./credentials";
import { FakeApp, success } from "./fake-app";

const entry = fileURLToPath(new URL("../dist/index.js", import.meta.url));
const TOKEN = "cli-token-BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB";
const cleanup: (() => Promise<unknown>)[] = [];
afterEach(async () => {
  for (const close of cleanup.splice(0).reverse()) await close();
});
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

/** Starts the real built bridge over stdio. Its environment is clean unless a test adds to it. */
async function bridge(args: string[], env: Record<string, string> = {}) {
  const home = await mkdtemp(path.join(tmpdir(), "nextshell-mcp-cli-"));
  cleanup.push(() => rm(home, { recursive: true, force: true }));
  const transport = new StdioClientTransport({
    command: process.execPath,
    args: [entry, ...args],
    stderr: "pipe",
    cwd: tmpdir(),
    env: {
      PATH: process.env.PATH ?? "",
      HOME: home,
      USERPROFILE: home,
      AppData: path.join(home, "AppData", "Roaming"),
      XDG_CONFIG_HOME: "",
      ...env
    }
  });
  let stderr = "";
  transport.stderr?.on("data", (chunk) => {
    stderr += String(chunk);
  });
  const client = new Client({ name: "cli-test", version: "1" });
  cleanup.push(() => client.close());
  await client.connect(transport);
  return { client, home, stderr: () => stderr };
}
async function fakeApp() {
  const calls: { name: string; args: Record<string, unknown> }[] = [];
  const fake = new FakeApp({
    token: () => TOKEN,
    invoke: async (toolName, args) => {
      calls.push({ name: toolName, args });
      return success({ sessions: [] });
    }
  });
  await fake.start();
  cleanup.push(() => fake.stop());
  return { fake, calls };
}
const writeDiscovery = async (file: string, port: number, token = TOKEN) => {
  await mkdir(path.dirname(file), { recursive: true });
  await writeFile(file, JSON.stringify({ port, token }));
};

test("published version and plugin configuration stay aligned", () => {
  expect(version).toBe("0.2.0");
  expect(MCP_PACKAGE_SPEC).toBe(`${name}@${version}`);
  // NextShell 2 releases attach the bridge as a tarball (it is not on npm), run by URL. npm 12
  // refuses URL packages unless --allow-remote permits the one on the command line.
  // No port and no token: both come from the mcp.json the app writes.
  expect(pluginConfig.mcpServers.nextshell.command).toBe("npx");
  expect(pluginConfig.mcpServers.nextshell.args).toEqual([
    "--allow-remote=root",
    "-y",
    expect.stringMatching(
      new RegExp(
        `^https://github\\.com/HynoR/NextShell/releases/download/v2\\.[^/]+/nextshell-mcp-${version.replaceAll(".", "\\.")}\\.tgz$`
      )
    )
  ]);
  expect(JSON.stringify(pluginConfig)).not.toMatch(/token|--port/i);
});
test("real stdio process discovers tools offline and exits cleanly", async () => {
  const { client, stderr } = await bridge([]);
  expect((await client.listTools()).tools.some((tool) => tool.name === "exec")).toBe(true);
  await client.ping();
  expect(await client.callTool({ name: "session_list", arguments: {} })).toMatchObject({
    isError: true,
    structuredContent: { error: { code: "token_missing" } }
  });
  await client.ping();
  expect(stderr()).toBe("");
});
test("with no token anywhere the tool error tells the user how to enable MCP", async () => {
  const { client } = await bridge([]);
  const result = await client.callTool({ name: "session_list", arguments: {} });
  expect(JSON.stringify(result)).toContain("在 NextShell 设置 → Agent 中开启 MCP");
});
test("uses the default mcp.json of the OS when it appears after the bridge started", async () => {
  const { fake, calls } = await fakeApp();
  const { client, home } = await bridge([]);
  expect((await client.callTool({ name: "session_list", arguments: {} })).isError).toBe(true);
  const file = defaultConfigPath({
    platform: process.platform,
    env: { AppData: path.join(home, "AppData", "Roaming") },
    home
  });
  await writeDiscovery(file, fake.port); // the app is enabled later: no restart of the bridge
  expect(await client.callTool({ name: "session_list", arguments: {} })).toMatchObject({
    structuredContent: { ok: true }
  });
  expect(calls).toHaveLength(1);
});
test("--config, the environment and --token take precedence in that order", async () => {
  const { fake, calls } = await fakeApp();
  const { client, home } = await bridge(["--config", "unused"]);
  await client.close();

  const config = path.join(home, "custom.json");
  await writeDiscovery(config, fake.port);
  const viaConfig = await bridge(["--config", config]);
  expect((await viaConfig.client.callTool({ name: "host_list", arguments: {} })).isError).not.toBe(
    true
  );

  const viaEnv = await bridge(["--port", String(fake.port)], { NEXTSHELL_MCP_TOKEN: TOKEN });
  expect((await viaEnv.client.callTool({ name: "host_list", arguments: {} })).isError).not.toBe(
    true
  );

  // A wrong --token must win over a correct environment token and be refused by the app.
  const viaFlag = await bridge(["--port", String(fake.port), "--token", "wrong"], {
    NEXTSHELL_MCP_TOKEN: TOKEN
  });
  expect(await viaFlag.client.callTool({ name: "host_list", arguments: {} })).toMatchObject({
    structuredContent: { error: { code: "unauthorized" } }
  });
  expect(viaFlag.stderr() + viaEnv.stderr() + viaConfig.stderr()).toBe("");
  expect(calls).toHaveLength(2);
  expect(fake.requests.every((r) => r.authorization?.startsWith("Bearer "))).toBe(true);
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
test("--help documents the token sources without starting the server", () => {
  const out = execFileSync(process.execPath, [entry, "--help"], { stdio: "pipe" });
  expect(out).toEqual(Buffer.alloc(0)); // usage goes to stderr, stdout stays protocol-clean
});
