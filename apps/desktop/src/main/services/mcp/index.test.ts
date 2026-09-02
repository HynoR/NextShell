import { afterEach, describe, expect, test } from "vitest";
import { mkdtemp, rm, stat } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { randomUUID } from "node:crypto";
import { DEFAULT_APP_PREFERENCES } from "@nextshell/core";
import type { AppPreferences } from "@nextshell/core";

import { createAgentMcpService, type AgentMcpService, type AgentMcpServiceDeps } from "./index";

const TIMESTAMP = "2026-08-03T00:00:00.000Z";

const tempDirs: string[] = [];
let service: AgentMcpService | null = null;

const createTempDir = async (): Promise<string> => {
  const dir = await mkdtemp(path.join(os.tmpdir(), "nsmcp-service-"));
  tempDirs.push(dir);
  return dir;
};

const baseDeps = (
  userDataDir: string,
  preferences: () => AppPreferences,
  overrides: Partial<AgentMcpServiceDeps> = {}
): AgentMcpServiceDeps => ({
  userDataDir,
  appVersion: "9.9.9",
  socketPath: path.join(os.tmpdir(), `nsmcp-svc-${randomUUID().slice(0, 8)}`, "s"),
  listConnections: () => [],
  listSessions: () => [],
  listScopedCommands: () => [],
  saveCommand: (input) => ({
    id: input.id ?? "cmd-new",
    name: input.name,
    group: input.group ?? "默认",
    command: input.command,
    appendCr: input.appendCr,
    createdAt: TIMESTAMP,
    updatedAt: TIMESTAMP
  }),
  readSessionScreen: async () => null,
  writeSession: () => undefined,
  lastUserInputAt: () => null,
  waitForCommandCompletion: async () => null,
  focusSession: () => undefined,
  setSessionAgentControlled: () => undefined,
  clearSessionAgentControlled: () => undefined,
  getSessionHistory: () => null,
  execCommand: async () => ({
    stdout: "",
    stderr: "",
    exitCode: 0,
    executedAt: new Date().toISOString()
  }),
  retainConnection: () => () => undefined,
  closeConnectionIfIdle: async () => undefined,
  emitActivity: () => undefined,
  getPreferences: preferences,
  ...overrides
});

const withAgent = (patch: Partial<AppPreferences["agent"]>): AppPreferences => ({
  ...DEFAULT_APP_PREFERENCES,
  agent: { ...DEFAULT_APP_PREFERENCES.agent, ...patch }
});

afterEach(async () => {
  await service?.dispose();
  service = null;
  while (tempDirs.length > 0) {
    const dir = tempDirs.pop();
    if (dir) {
      await rm(dir, { recursive: true, force: true });
    }
  }
});

describe("agent mcp service", () => {
  test("stays silent until the preference is enabled", async () => {
    const userDataDir = await createTempDir();
    let preferences = withAgent({ enabled: false });
    service = createAgentMcpService(baseDeps(userDataDir, () => preferences));

    const idle = await service.start();
    expect(idle.enabled).toBe(false);
    expect(idle.listening).toBe(false);
    expect(idle.socketPath).toBeNull();
    await expect(stat(idle.endpointFilePath)).rejects.toThrow();

    preferences = withAgent({ enabled: true });
    const running = await service.applyPreferences();
    expect(running.listening).toBe(true);
    expect(running.socketPath).not.toBeNull();
    await expect(stat(running.endpointFilePath)).resolves.toBeDefined();
    await expect(stat(running.socketPath!)).resolves.toBeDefined();
  });

  test("start and stop are idempotent and clean up the discovery file", async () => {
    const userDataDir = await createTempDir();
    const preferences = withAgent({ enabled: true });
    service = createAgentMcpService(baseDeps(userDataDir, () => preferences));

    const first = await service.start();
    const second = await service.start();
    expect(second.socketPath).toBe(first.socketPath);
    expect(second.listening).toBe(true);

    const stopped = await service.stop();
    expect(stopped.listening).toBe(false);
    expect(stopped.socketPath).toBeNull();
    await expect(stat(first.endpointFilePath)).rejects.toThrow();
    await expect(stat(first.socketPath!)).rejects.toThrow();

    const stoppedAgain = await service.stop();
    expect(stoppedAgain.listening).toBe(false);
  });

  test("client config points at the bundled bridge, never at npx or a credential", async () => {
    const userDataDir = await createTempDir();
    const preferences = withAgent({ enabled: true });
    const clipboard: string[] = [];
    const bridgeEntry = path.join(userDataDir, "mcp-bridge", "index.js");
    service = createAgentMcpService(
      baseDeps(userDataDir, () => preferences, {
        writeClipboard: (text) => clipboard.push(text),
        resolveBridgeEntry: () => bridgeEntry,
        bridgeRuntimePath: "/Applications/NextShell.app/Contents/MacOS/NextShell"
      })
    );
    await service.start();

    const config = service.buildClientConfig("claude-code");
    expect(config.ok).toBe(true);
    expect(config.command).toContain("claude mcp add nextshell");
    expect(config.command).toContain("NEXTSHELL_MCP_ENDPOINT=");
    // The bridge is not published to npm; an `npx` config would 404 for users.
    expect(config.command).not.toContain("npx");
    expect(config.json).not.toContain("npx");
    expect(config.json).toContain(bridgeEntry);
    expect(config.json).toContain("ELECTRON_RUN_AS_NODE");
    expect(config.json).not.toContain("Authorization");
    expect(clipboard).toEqual([config.command]);

    const cursor = service.buildClientConfig("cursor");
    expect(clipboard.at(-1)).toBe(cursor.json);
  });

  test("a missing bundled bridge fails loudly instead of emitting a broken config", async () => {
    const userDataDir = await createTempDir();
    const preferences = withAgent({ enabled: true });
    const clipboard: string[] = [];
    service = createAgentMcpService(
      baseDeps(userDataDir, () => preferences, {
        writeClipboard: (text) => clipboard.push(text),
        resolveBridgeEntry: () => null
      })
    );
    await service.start();

    expect(() => service?.buildClientConfig("claude-code")).toThrow(/桥接程序/);
    expect(clipboard).toEqual([]);
  });

  test("installCursor opens a deeplink that carries the bridge config", async () => {
    const userDataDir = await createTempDir();
    const preferences = withAgent({ enabled: true });
    const opened: string[] = [];
    const bridgeEntry = path.join(userDataDir, "mcp-bridge", "index.js");
    service = createAgentMcpService(
      baseDeps(userDataDir, () => preferences, {
        resolveBridgeEntry: () => bridgeEntry,
        bridgeRuntimePath: "/apps/NextShell",
        openExternal: async (url) => {
          opened.push(url);
        }
      })
    );
    await service.start();

    const result = await service.installCursor();
    expect(result.ok).toBe(true);
    expect(opened).toEqual([result.deeplink]);
    expect(result.deeplink).toContain(
      "cursor://anysphere.cursor-deeplink/mcp/install?name=nextshell"
    );
    const encoded = decodeURIComponent(result.deeplink.split("config=")[1]!);
    const decoded = JSON.parse(Buffer.from(encoded, "base64").toString("utf8"));
    expect(decoded.command).toBe("/apps/NextShell");
    expect(decoded.args).toEqual([bridgeEntry]);
  });

  test("installClaudeDesktop merges the stdio bridge into the config file", async () => {
    const userDataDir = await createTempDir();
    const preferences = withAgent({ enabled: true });
    const bridgeEntry = path.join(userDataDir, "mcp-bridge", "index.js");
    const claudeDir = path.join(userDataDir, "Claude");
    const configPath = path.join(claudeDir, "claude_desktop_config.json");
    const { mkdir, readFile } = await import("node:fs/promises");
    await mkdir(claudeDir, { recursive: true });
    service = createAgentMcpService(
      baseDeps(userDataDir, () => preferences, {
        resolveBridgeEntry: () => bridgeEntry,
        bridgeRuntimePath: "/apps/NextShell",
        claudeDesktopConfigPath: configPath
      })
    );
    await service.start();

    const result = service.installClaudeDesktop();
    expect(result.configPath).toBe(configPath);
    const written = JSON.parse(await readFile(configPath, "utf8"));
    expect(written.mcpServers.nextshell.command).toBe("/apps/NextShell");
    expect(written.mcpServers.nextshell.env.ELECTRON_RUN_AS_NODE).toBe("1");
  });

  test("exportMcpb writes a bundle and honours dialog cancellation", async () => {
    const userDataDir = await createTempDir();
    const preferences = withAgent({ enabled: true });
    const bridgeEntry = path.join(userDataDir, "bridge.js");
    const savePath = path.join(userDataDir, "nextshell.mcpb");
    const { writeFile, readFile } = await import("node:fs/promises");
    await writeFile(bridgeEntry, "#!/usr/bin/env node\n// bridge\n");
    let nextSavePath: string | null = savePath;
    service = createAgentMcpService(
      baseDeps(userDataDir, () => preferences, {
        resolveBridgeEntry: () => bridgeEntry,
        chooseSavePath: async () => nextSavePath
      })
    );
    await service.start();

    const exported = await service.exportMcpb();
    expect(exported).toEqual({ ok: true, filePath: savePath });
    const bundle = await readFile(savePath);
    expect(bundle.readUInt32LE(0)).toBe(0x04034b50); // ZIP local header
    expect(bundle.toString("latin1")).toContain("manifest.json");

    nextSavePath = null;
    const canceled = await service.exportMcpb();
    expect(canceled).toEqual({ ok: false, canceled: true });
  });

  test("the halt breaker is reflected in the status", async () => {
    const userDataDir = await createTempDir();
    const preferences = withAgent({ enabled: true });
    service = createAgentMcpService(baseDeps(userDataDir, () => preferences));
    await service.start();

    expect(service.getStatus().halted).toBe(false);
    const halted = service.setHalted(true);
    expect(halted.halted).toBe(true);
    expect(halted.listening).toBe(true);
    expect(service.setHalted(false).halted).toBe(false);
  });
});
