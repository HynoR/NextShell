import { afterEach, describe, expect, test } from "vitest";
import { createServer, type Server } from "node:net";
import { DEFAULT_APP_PREFERENCES } from "@nextshell/core";
import type { AppPreferences } from "@nextshell/core";

import { createAgentMcpService, type AgentMcpService, type AgentMcpServiceDeps } from "./index";

const TIMESTAMP = "2026-08-03T00:00:00.000Z";

let service: AgentMcpService | null = null;
let blocker: Server | null = null;

/** Grabs a free loopback port and releases it so the service can bind it. */
const freePort = (): Promise<number> =>
  new Promise((resolve, reject) => {
    const probe = createServer();
    probe.once("error", reject);
    probe.listen(0, "127.0.0.1", () => {
      const { port } = probe.address() as { port: number };
      probe.close(() => resolve(port));
    });
  });

const baseDeps = (preferences: () => AppPreferences): AgentMcpServiceDeps => ({
  appVersion: "9.9.9",
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
  getPreferences: preferences
});

const withAgent = (patch: Partial<AppPreferences["agent"]>): AppPreferences => ({
  ...DEFAULT_APP_PREFERENCES,
  agent: { ...DEFAULT_APP_PREFERENCES.agent, ...patch }
});

afterEach(async () => {
  await service?.dispose();
  service = null;
  await new Promise<void>((resolve) => (blocker ? blocker.close(() => resolve()) : resolve()));
  blocker = null;
});

describe("agent mcp service", () => {
  test("stays silent until the preference is enabled", async () => {
    const port = await freePort();
    let preferences = withAgent({ enabled: false, port });
    service = createAgentMcpService(baseDeps(() => preferences));

    const idle = await service.start();
    expect(idle.enabled).toBe(false);
    expect(idle.listening).toBe(false);
    expect(idle.url).toBeNull();
    expect(idle.port).toBe(port);

    preferences = withAgent({ enabled: true, port });
    const running = await service.applyPreferences();
    expect(running.listening).toBe(true);
    expect(running.url).toBe(`http://127.0.0.1:${port}/mcp`);
  });

  test("start and stop are idempotent", async () => {
    const port = await freePort();
    const preferences = withAgent({ enabled: true, port });
    service = createAgentMcpService(baseDeps(() => preferences));

    const first = await service.start();
    const second = await service.start();
    expect(second.url).toBe(first.url);
    expect(second.listening).toBe(true);

    const stopped = await service.stop();
    expect(stopped.listening).toBe(false);
    expect(stopped.url).toBeNull();

    const stoppedAgain = await service.stop();
    expect(stoppedAgain.listening).toBe(false);
  });

  test("changing the port preference rebinds the endpoint", async () => {
    const portA = await freePort();
    const portB = await freePort();
    let preferences = withAgent({ enabled: true, port: portA });
    service = createAgentMcpService(baseDeps(() => preferences));

    const onA = await service.start();
    expect(onA.url).toBe(`http://127.0.0.1:${portA}/mcp`);

    preferences = withAgent({ enabled: true, port: portB });
    const onB = await service.applyPreferences();
    expect(onB.listening).toBe(true);
    expect(onB.url).toBe(`http://127.0.0.1:${portB}/mcp`);
  });

  test("a busy port is reported through lastError instead of throwing", async () => {
    const port = await freePort();
    blocker = createServer();
    await new Promise<void>((resolve) => blocker!.listen(port, "127.0.0.1", resolve));
    const preferences = withAgent({ enabled: true, port });
    service = createAgentMcpService(baseDeps(() => preferences));

    const status = await service.start();
    expect(status.enabled).toBe(true);
    expect(status.listening).toBe(false);
    expect(status.lastError).toContain(`端口 ${port} 已被占用`);
  });

  test("the halt breaker is reflected in the status", async () => {
    const preferences = withAgent({ enabled: true, port: await freePort() });
    service = createAgentMcpService(baseDeps(() => preferences));
    await service.start();

    expect(service.getStatus().halted).toBe(false);
    const halted = service.setHalted(true);
    expect(halted.halted).toBe(true);
    expect(halted.listening).toBe(true);
    expect(service.setHalted(false).halted).toBe(false);
  });
});
