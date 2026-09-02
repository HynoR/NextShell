import { describe, expect, test, vi } from "vitest";
import { DEFAULT_APP_PREFERENCES } from "@nextshell/core";
import type { AppPreferences, ConnectionProfile, ScopedCommandItem } from "@nextshell/core";

import {
  AgentGateway,
  type AgentClientIdentity,
  type AgentGatewayDeps,
  type AgentSessionInfo
} from "./agent-gateway";

const TIMESTAMP = "2026-08-03T00:00:00.000Z";

const createConnection = (
  overrides: Partial<ConnectionProfile> & Pick<ConnectionProfile, "id" | "name" | "host">
): ConnectionProfile => ({
  port: 22,
  username: "root",
  authType: "password",
  credentialRef: "secret://conn-secret",
  sshKeyId: "key-1",
  proxyId: "proxy-1",
  hostFingerprint: "SHA256:deadbeef",
  notes: "master password is hunter2",
  strictHostKeyChecking: false,
  terminalEncoding: "utf-8",
  backspaceMode: "ascii-backspace",
  deleteMode: "vt220-delete",
  groupPath: "/server",
  tags: [],
  favorite: false,
  monitorSession: false,
  createdAt: TIMESTAMP,
  updatedAt: TIMESTAMP,
  ...overrides
});

const CLIENT: AgentClientIdentity = {
  id: "session-1",
  name: "claude-code",
  version: "1.0.0",
  transport: "socket"
};

const prodHk = createConnection({
  id: "11111111-1111-1111-1111-111111111111",
  name: "prod-hk",
  host: "10.0.0.1",
  resourceId: "local-default-11111111-1111-1111-1111-111111111111"
});

const stageHk = createConnection({
  id: "22222222-2222-2222-2222-222222222222",
  name: "stage-hk",
  host: "10.0.0.2",
  resourceId: "local-default-22222222-2222-2222-2222-222222222222"
});

const SESSIONS: AgentSessionInfo[] = [
  {
    id: "sess-full",
    connectionId: prodHk.id,
    title: "prod-hk",
    status: "connected",
    type: "terminal",
    createdAt: TIMESTAMP,
    cwd: "/var/www",
    lastCommand: "pwd"
  },
  {
    id: "sess-stage",
    connectionId: stageHk.id,
    title: "stage-hk",
    status: "connected",
    type: "terminal",
    createdAt: TIMESTAMP,
    cwd: null,
    lastCommand: null
  },
  {
    id: "sess-local",
    connectionId: null,
    title: "local shell",
    status: "connected",
    type: "terminal",
    createdAt: TIMESTAMP,
    cwd: null,
    lastCommand: null
  },
  {
    id: "sess-connecting",
    connectionId: prodHk.id,
    title: "prod-hk (2)",
    status: "connecting",
    type: "terminal",
    createdAt: TIMESTAMP,
    cwd: null,
    lastCommand: null
  }
];

const LIBRARY: ScopedCommandItem[] = [
  {
    id: "cmd-1",
    name: "nginx status",
    group: "ops",
    command: "sudo systemctl status nginx",
    appendCr: true,
    scope: "local",
    createdAt: TIMESTAMP,
    updatedAt: TIMESTAMP
  },
  {
    id: "cmd-2",
    name: "deploy",
    group: "ops",
    command: "ssh deploy@10.0.0.3 systemctl restart app",
    scope: "workspace",
    workspaceId: "ws-1",
    workspaceName: "team",
    createdAt: TIMESTAMP,
    updatedAt: TIMESTAMP
  }
];

interface Harness {
  gateway: AgentGateway;
  deps: AgentGatewayDeps;
}

const createHarness = (overrides: Partial<AgentGatewayDeps> = {}): Harness => {
  const deps: AgentGatewayDeps = {
    listConnections: () => [prodHk, stageHk],
    listSessions: () => SESSIONS,
    listScopedCommands: () => LIBRARY,
    saveCommand: (input) => ({
      id: input.id ?? "cmd-new",
      name: input.name,
      group: input.group ?? "默认",
      command: input.command,
      appendCr: input.appendCr,
      createdAt: TIMESTAMP,
      updatedAt: TIMESTAMP
    }),
    getSessionHistory: () => null,
    readSessionScreen: async () => null,
    writeSession: () => undefined,
    lastUserInputAt: () => null,
    waitForCommandCompletion: async () => null,
    focusSession: () => undefined,
    setSessionAgentControlled: () => undefined,
    clearSessionAgentControlled: () => undefined,
    execCommand: async () => ({ stdout: "", stderr: "", exitCode: 0, executedAt: TIMESTAMP }),
    retainConnection: () => () => undefined,
    closeConnectionIfIdle: async () => undefined,
    emitActivity: () => undefined,
    getPreferences: () => DEFAULT_APP_PREFERENCES,
    ...overrides
  };
  return { gateway: new AgentGateway(deps), deps };
};

const withAgentPrefs = (patch: Partial<AppPreferences["agent"]>): AppPreferences => ({
  ...DEFAULT_APP_PREFERENCES,
  agent: { ...DEFAULT_APP_PREFERENCES.agent, ...patch }
});

describe("session discovery", () => {
  test("session_list exposes every open remote tab with its connection name and host", async () => {
    const { gateway } = createHarness();
    const result = await gateway.listSessions(CLIENT);

    expect(result.ok).toBe(true);
    if (!result.ok) return;
    // Local shells have no connection and stay invisible; everything the user
    // opened over SSH is listed, since opening the tab is the grant.
    expect(result.data.sessions.map((session) => session.id)).toEqual([
      "sess-full",
      "sess-stage",
      "sess-connecting"
    ]);
    expect(result.data.sessions[0]).toMatchObject({
      connectionName: "prod-hk",
      host: "10.0.0.1"
    });
    // Connection metadata joins must never leak credential material.
    const serialized = JSON.stringify(result);
    for (const forbidden of ["credentialRef", "secret://", "hunter2", "hostFingerprint"]) {
      expect(serialized).not.toContain(forbidden);
    }
  });

  test("session_read and session_history report unknown or local sessions as not_found", async () => {
    const { gateway } = createHarness();

    const missing = await gateway.readSessionScreen(CLIENT, { target: "sess-gone" });
    expect(missing.ok).toBe(false);
    if (!missing.ok) expect(missing.error.code).toBe("not_found");

    // A local shell has no connection and does not exist as far as MCP knows.
    const local = await gateway.sessionHistory(CLIENT, { target: "sess-local" });
    expect(local.ok).toBe(false);
    if (!local.ok) expect(local.error.code).toBe("not_found");
  });

  test("session_history redacts credentials from commands and output", async () => {
    const { gateway } = createHarness({
      getSessionHistory: () => ({
        integrationAvailable: true,
        entries: [
          {
            command: "mysql -u root password=hunter2",
            exitCode: 0,
            startedAt: TIMESTAMP,
            finishedAt: TIMESTAMP,
            output: "Authorization: Bearer abc123def",
            truncated: false
          }
        ]
      })
    });
    const result = await gateway.sessionHistory(CLIENT, { target: "sess-full" });

    expect(result.ok).toBe(true);
    if (!result.ok) return;
    const serialized = JSON.stringify(result.data);
    expect(serialized).not.toContain("hunter2");
    expect(serialized).not.toContain("abc123def");
  });
});

describe("exec", () => {
  test("borrows the session's connection and inherits its OSC cwd", async () => {
    const exec = vi.fn(async () => ({
      stdout: "/var/www\n",
      stderr: "",
      exitCode: 0,
      executedAt: TIMESTAMP
    }));
    const { gateway } = createHarness({ execCommand: exec });
    const result = await gateway.execCommand(CLIENT, { target: "sess-full", command: "pwd" });

    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.data).toMatchObject({
      sessionId: "sess-full",
      connectionId: prodHk.id,
      exitCode: 0,
      actualCwd: "/var/www"
    });
    expect(exec).toHaveBeenCalledWith(
      prodHk.id,
      "pwd",
      expect.objectContaining({ cwd: "/var/www" })
    );
  });

  test("an explicit cwd wins over the session cwd and must be absolute", async () => {
    const exec = vi.fn(async () => ({
      stdout: "",
      stderr: "",
      exitCode: 0,
      executedAt: TIMESTAMP
    }));
    const { gateway } = createHarness({ execCommand: exec });

    const ok = await gateway.execCommand(CLIENT, {
      target: "sess-full",
      command: "ls",
      cwd: "/etc"
    });
    expect(ok.ok).toBe(true);
    expect(exec).toHaveBeenCalledWith(
      prodHk.id,
      "ls",
      expect.objectContaining({ cwd: "/etc" })
    );

    const bad = await gateway.execCommand(CLIENT, {
      target: "sess-full",
      command: "ls",
      cwd: "relative/path"
    });
    expect(bad.ok).toBe(false);
    if (!bad.ok) expect(bad.error.code).toBe("invalid_argument");
  });

  test("an unknown, local or not-yet-connected session is refused", async () => {
    const { gateway } = createHarness();

    const missing = await gateway.execCommand(CLIENT, { target: "sess-gone", command: "ls" });
    expect(missing.ok).toBe(false);
    if (!missing.ok) expect(missing.error.code).toBe("not_found");

    const local = await gateway.execCommand(CLIENT, { target: "sess-local", command: "ls" });
    expect(local.ok).toBe(false);
    if (!local.ok) expect(local.error.code).toBe("not_found");

    const connecting = await gateway.execCommand(CLIENT, {
      target: "sess-connecting",
      command: "ls"
    });
    expect(connecting.ok).toBe(false);
    if (!connecting.ok) expect(connecting.error.code).toBe("unavailable");
  });

  test("the preset dangerous list blocks without any dialog", async () => {
    const exec = vi.fn();
    const { gateway } = createHarness({ execCommand: exec });
    const result = await gateway.execCommand(CLIENT, {
      target: "sess-full",
      command: "rm -rf /"
    });

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error.code).toBe("forbidden");
      expect(result.error.message).toContain("blacklist");
    }
    expect(exec).not.toHaveBeenCalled();
  });

  test("user blacklist entries match as substring and as regex", async () => {
    const exec = vi.fn(async () => ({
      stdout: "",
      stderr: "",
      exitCode: 0,
      executedAt: TIMESTAMP
    }));
    const { gateway } = createHarness({
      execCommand: exec,
      getPreferences: () =>
        withAgentPrefs({ blacklist: ["kubectl delete", String.raw`^docker\s+system\s+prune`] })
    });

    const substring = await gateway.execCommand(CLIENT, {
      target: "sess-full",
      command: "kubectl delete pod app"
    });
    expect(substring.ok).toBe(false);
    if (!substring.ok) {
      expect(substring.error.code).toBe("forbidden");
      expect(substring.error.message).toContain("kubectl delete");
    }

    const regex = await gateway.execCommand(CLIENT, {
      target: "sess-full",
      command: "docker   system   prune -af"
    });
    expect(regex.ok).toBe(false);
    if (!regex.ok) expect(regex.error.code).toBe("forbidden");

    const allowed = await gateway.execCommand(CLIENT, { target: "sess-full", command: "ls" });
    expect(allowed.ok).toBe(true);
    expect(exec).toHaveBeenCalledTimes(1);
  });

  test("human keystrokes after the last agent call fail the next one exactly once", async () => {
    let clock = 1_000;
    let lastUserInput: number | null = null;
    const exec = vi.fn(async () => ({
      stdout: "",
      stderr: "",
      exitCode: 0,
      executedAt: TIMESTAMP
    }));
    const { gateway } = createHarness({
      execCommand: exec,
      now: () => clock,
      lastUserInputAt: () => lastUserInput
    });

    // First takeover establishes the baseline and succeeds.
    const first = await gateway.execCommand(CLIENT, { target: "sess-full", command: "ls" });
    expect(first.ok).toBe(true);

    // The human types into the tab; the agent's next call reports intervention…
    lastUserInput = 1_500;
    clock = 2_000;
    const intervened = await gateway.execCommand(CLIENT, { target: "sess-full", command: "ls" });
    expect(intervened.ok).toBe(false);
    if (!intervened.ok) expect(intervened.error.code).toBe("human_intervention");

    // …and the error is one-shot: the call after it becomes the new baseline.
    clock = 3_000;
    const recovered = await gateway.execCommand(CLIENT, { target: "sess-full", command: "ls" });
    expect(recovered.ok).toBe(true);
    expect(exec).toHaveBeenCalledTimes(2);
  });
});

describe("session_send_keys", () => {
  test("types into the PTY with the badge held only for the write", async () => {
    const writes: string[] = [];
    const badges: Array<{ sessionId: string; controlled: boolean }> = [];
    const { gateway } = createHarness({
      writeSession: (_sessionId, data) => writes.push(data),
      setSessionAgentControlled: (sessionId) => badges.push({ sessionId, controlled: true }),
      clearSessionAgentControlled: (sessionId) => badges.push({ sessionId, controlled: false })
    });

    const result = await gateway.sendKeys(CLIENT, {
      target: "sess-full",
      text: "ls -la",
      submit: true
    });

    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.data.bytes).toBe(Buffer.byteLength("ls -la\r", "utf8"));
    expect(result.data.submitted).toBe(true);
    expect(writes).toEqual(["ls -la\r"]);
    expect(badges).toEqual([
      { sessionId: "sess-full", controlled: true },
      { sessionId: "sess-full", controlled: false }
    ]);
  });

  test("blacklisted text is refused before any byte is written", async () => {
    const write = vi.fn();
    const { gateway } = createHarness({ writeSession: write });
    const result = await gateway.sendKeys(CLIENT, {
      target: "sess-full",
      text: "reboot",
      submit: true
    });

    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.error.code).toBe("forbidden");
    expect(write).not.toHaveBeenCalled();
  });

  test("human intervention is an error, not a wait", async () => {
    let clock = 10_000;
    let lastUserInput: number | null = null;
    const write = vi.fn();
    const { gateway } = createHarness({
      writeSession: write,
      now: () => clock,
      lastUserInputAt: () => lastUserInput
    });

    const first = await gateway.sendKeys(CLIENT, { target: "sess-full", text: "ls", submit: true });
    expect(first.ok).toBe(true);

    lastUserInput = 10_500;
    clock = 11_000;
    const intervened = await gateway.sendKeys(CLIENT, {
      target: "sess-full",
      text: "ls",
      submit: true
    });
    expect(intervened.ok).toBe(false);
    if (!intervened.ok) {
      expect(intervened.error.code).toBe("human_intervention");
      expect(intervened.error.message).toContain("manual keyboard input");
    }
    expect(write).toHaveBeenCalledTimes(1);
  });

  test("waitForPrompt returns the OSC 133 completion when integration is live", async () => {
    const { gateway } = createHarness({
      waitForCommandCompletion: async () => ({
        command: "ls",
        exitCode: 0,
        output: "file-a",
        truncated: false
      })
    });

    const result = await gateway.sendKeys(CLIENT, {
      target: "sess-full",
      text: "ls",
      submit: true,
      waitForPrompt: true
    });

    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.data.completed).toMatchObject({ command: "ls", exitCode: 0, output: "file-a" });
    expect(result.data.waitTimedOut).toBe(false);
  });
});

describe("session_send_signal and session_focus", () => {
  test("interrupt writes the Ctrl-C byte", async () => {
    const writes: string[] = [];
    const { gateway } = createHarness({
      writeSession: (_sessionId, data) => writes.push(data)
    });
    const result = await gateway.sendSignal(CLIENT, { target: "sess-full", signal: "interrupt" });

    expect(result.ok).toBe(true);
    expect(writes).toEqual([""]);
  });

  test("signals never hit the blacklist or intervention gate", async () => {
    let clock = 1_000;
    const { gateway } = createHarness({
      now: () => clock,
      // A keystroke landed, and no agent op ever happened — signals still pass,
      // because Ctrl-C is how a runaway agent command gets stopped.
      lastUserInputAt: () => 2_000
    });
    const result = await gateway.sendSignal(CLIENT, { target: "sess-full", signal: "interrupt" });
    expect(result.ok).toBe(true);
    clock += 1;
  });

  test("focus brings the tab forward", async () => {
    const focused: string[] = [];
    const { gateway } = createHarness({
      focusSession: (sessionId) => focused.push(sessionId)
    });

    const ok = await gateway.focusSession(CLIENT, { target: "sess-full" });
    expect(ok.ok).toBe(true);
    expect(focused).toEqual(["sess-full"]);

    const missing = await gateway.focusSession(CLIENT, { target: "sess-gone" });
    expect(missing.ok).toBe(false);
    if (!missing.ok) expect(missing.error.code).toBe("not_found");
  });
});

describe("command library", () => {
  test("command_search returns local and workspace entries in the Stage 6 shape", async () => {
    const { gateway } = createHarness();
    const result = await gateway.searchCommands(CLIENT, {});

    expect(result.ok).toBe(true);
    if (!result.ok) return;
    // Local and workspace entries are both reachable.
    expect(result.data.matches.map((match) => match.id)).toEqual(["cmd-1", "cmd-2"]);
    expect(result.data.matches[0]).toMatchObject({
      name: "nginx status",
      group: "ops",
      appendCr: true,
      scope: "local"
    });
    expect(result.data.matches[1]).toMatchObject({
      scope: "workspace",
      workspaceId: "ws-1",
      workspaceName: "team"
    });
  });

  test("command_search matches on name and command text", async () => {
    const { gateway } = createHarness();
    const byName = await gateway.searchCommands(CLIENT, { query: "nginx" });
    expect(byName.ok && byName.data.total).toBe(1);

    const noHit = await gateway.searchCommands(CLIENT, { query: "nonexistent" });
    expect(noHit.ok && noHit.data.total).toBe(0);
  });

  test("command_save upserts local and workspace entries", async () => {
    const saved: Array<Record<string, unknown>> = [];
    const { gateway } = createHarness({
      saveCommand: (input) => {
        saved.push({ ...input });
        return {
          id: input.id ?? "cmd-new",
          name: input.name,
          group: input.group ?? "默认",
          command: input.command,
          appendCr: input.appendCr,
          createdAt: TIMESTAMP,
          updatedAt: TIMESTAMP
        };
      }
    });

    const created = await gateway.saveCommand(CLIENT, {
      name: "disk usage",
      command: "df -h",
      appendCr: true
    });
    expect(created.ok).toBe(true);
    if (created.ok) expect(created.data.command.id).toBe("cmd-new");

    const updated = await gateway.saveCommand(CLIENT, {
      id: "cmd-1",
      workspaceId: "ws-1",
      name: "nginx status",
      command: "sudo systemctl status nginx"
    });
    expect(updated.ok).toBe(true);
    expect(saved[1]).toMatchObject({ id: "cmd-1", workspaceId: "ws-1" });

    const invalid = await gateway.saveCommand(CLIENT, { name: " ", command: "" });
    expect(invalid.ok).toBe(false);
    if (!invalid.ok) expect(invalid.error.code).toBe("invalid_argument");
  });
});

describe("kill switch", () => {
  test("a halted gateway refuses every tool without touching a dep", async () => {
    const exec = vi.fn();
    const { gateway } = createHarness({ execCommand: exec });
    gateway.setHalted(true);

    const listed = await gateway.listSessions(CLIENT);
    expect(listed.ok).toBe(false);
    if (!listed.ok) expect(listed.error.code).toBe("forbidden");

    const executed = await gateway.execCommand(CLIENT, { target: "sess-full", command: "ls" });
    expect(executed.ok).toBe(false);
    expect(exec).not.toHaveBeenCalled();

    gateway.setHalted(false);
    const recovered = await gateway.listSessions(CLIENT);
    expect(recovered.ok).toBe(true);
  });
});

describe("activity stream", () => {
  test("tool calls emit running and settled events", async () => {
    const events: Array<{ tool: string; status: string }> = [];
    const { gateway } = createHarness({
      emitActivity: (event) => events.push({ tool: event.tool, status: event.status })
    });

    await gateway.listSessions(CLIENT);
    await gateway.execCommand(CLIENT, { target: "sess-full", command: "ls" });

    expect(events).toEqual([
      { tool: "session_list", status: "running" },
      { tool: "session_list", status: "succeeded" },
      { tool: "exec", status: "running" },
      { tool: "exec", status: "succeeded" }
    ]);
  });
});
