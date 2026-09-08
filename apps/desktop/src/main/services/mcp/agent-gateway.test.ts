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
  transport: "http"
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
    pendingInput: async () => null,
    lastUserInputAt: () => null,
    waitForCommandCompletion: async () => null,
    focusSession: () => undefined,
    setSessionAgentControlled: () => undefined,
    clearSessionAgentControlled: () => undefined,
    execCommand: async () => ({ stdout: "", stderr: "", exitCode: 0, executedAt: TIMESTAMP }),
    retainConnection: () => () => undefined,
    closeConnectionIfIdle: async () => undefined,
    emitActivity: () => undefined,
    requestOpenSession: () => undefined,
    getPreferences: () => DEFAULT_APP_PREFERENCES,
    ...overrides
  };
  return { gateway: new AgentGateway(deps), deps };
};

const withAgentPrefs = (patch: Partial<AppPreferences["agent"]>): AppPreferences => ({
  ...DEFAULT_APP_PREFERENCES,
  agent: { ...DEFAULT_APP_PREFERENCES.agent, ...patch }
});

describe("host discovery and opening", () => {
  const recent = (id: string, name: string, lastConnectedAt: string) =>
    createConnection({ id, name, host: `10.9.0.${id.slice(0, 1)}`, lastConnectedAt });

  test("host_list without a query returns at most the 10 most recent hosts", async () => {
    const many = Array.from({ length: 14 }, (_, i) =>
      recent(
        `${i}0000000-0000-0000-0000-000000000000`,
        `h${i}`,
        `2026-01-${String(i + 1).padStart(2, "0")}T00:00:00.000Z`
      )
    );
    const never = createConnection({ id: "never", name: "never-opened", host: "10.9.9.9" });
    const { gateway } = createHarness({ listConnections: () => [...many, never] });

    const result = await gateway.hostList(CLIENT, {});
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.data.mode).toBe("recent");
    expect(result.data.hosts).toHaveLength(10);
    expect(result.data.hosts[0]?.name).toBe("h13");
    expect(result.data.hosts.map((h) => h.name)).not.toContain("never-opened");
    const serialized = JSON.stringify(result);
    for (const forbidden of ["credentialRef", "secret://", "hunter2", "hostFingerprint"]) {
      expect(serialized).not.toContain(forbidden);
    }
  });

  test("host_list with a query searches and reports open tabs", async () => {
    const { gateway } = createHarness();
    const result = await gateway.hostList(CLIENT, { query: "PROD" });
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.data.mode).toBe("search");
    expect(result.data.hosts.map((h) => h.name)).toEqual(["prod-hk"]);
    expect(result.data.hosts[0]?.openSessions).toBe(1);
  });

  test("session_open reuses a connected tab without asking", async () => {
    const requests: unknown[] = [];
    const { gateway } = createHarness({ requestOpenSession: (r) => requests.push(r) });
    const result = await gateway.openSession(CLIENT, { target: prodHk.id });
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.data).toEqual({ status: "opened", sessionId: "sess-full", reused: true });
    expect(requests).toHaveLength(0);
  });

  test("session_open waits for the user: pending → approved with the session id", async () => {
    const closed = createConnection({
      id: "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
      name: "cold",
      host: "10.0.0.9"
    });
    const sent: Array<{ id: string; reason: string | null }> = [];
    const { gateway } = createHarness({
      listConnections: () => [prodHk, closed],
      requestOpenSession: (r) => sent.push({ id: r.id, reason: r.reason }),
      openWaitMs: 20
    });

    const first = await gateway.openSession(CLIENT, { target: closed.id, reason: "check disk" });
    expect(first.ok).toBe(true);
    if (!first.ok) return;
    expect(first.data.status).toBe("pending");
    expect(sent).toHaveLength(1);
    expect(sent[0]?.reason).toBe("check disk");
    const requestId = first.data.requestId!;

    const again = gateway.openSession(CLIENT, { requestId });
    gateway.respondOpen({ id: requestId, approved: true, sessionId: "sess-new" });
    const opened = await again;
    expect(opened.ok).toBe(true);
    if (!opened.ok) return;
    expect(opened.data).toEqual({ status: "opened", sessionId: "sess-new" });
    expect(sent).toHaveLength(1);

    const stale = await gateway.openSession(CLIENT, { requestId });
    expect(stale.ok).toBe(false);
    if (!stale.ok) expect(stale.error.code).toBe("not_found");
  });

  test("session_open is denied on refusal and on the request timeout", async () => {
    const closed = createConnection({
      id: "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
      name: "cold",
      host: "10.0.0.9"
    });
    const { gateway } = createHarness({
      listConnections: () => [closed],
      openWaitMs: 50,
      openRequestTimeoutMs: 20
    });

    const timedOut = await gateway.openSession(CLIENT, { target: closed.id });
    expect(timedOut.ok).toBe(false);
    if (!timedOut.ok) expect(timedOut.error.code).toBe("denied");

    const { gateway: g2 } = createHarness({
      listConnections: () => [closed],
      openWaitMs: 50,
      requestOpenSession: (r) => {
        setTimeout(() => g2.respondOpen({ id: r.id, approved: false }), 5);
      }
    });
    const refused = await g2.openSession(CLIENT, { target: closed.id });
    expect(refused.ok).toBe(false);
    if (!refused.ok) expect(refused.error.code).toBe("denied");

    const unknown = await g2.openSession(CLIENT, { target: "nope" });
    expect(unknown.ok).toBe(false);
    if (!unknown.ok) expect(unknown.error.code).toBe("not_found");
  });
});

describe("session discovery", () => {
  test("session_list exposes every open remote tab with its connection name and host", async () => {
    const { gateway } = createHarness();
    const result = await gateway.listSessions(CLIENT);

    expect(result.ok).toBe(true);
    if (!result.ok) return;
    // Every open tab is listed, local shells included: opening it is the grant.
    expect(result.data.sessions.map((session) => session.id)).toEqual([
      "sess-full",
      "sess-stage",
      "sess-local",
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

  test("unknown sessions are not_found; local shells are visible", async () => {
    const { gateway } = createHarness();

    const missing = await gateway.readSessionScreen(CLIENT, { target: "sess-gone" });
    expect(missing.ok).toBe(false);
    if (!missing.ok) expect(missing.error.code).toBe("not_found");

    // Visible: whatever the history source says, it is not "no such session".
    const local = await gateway.sessionHistory(CLIENT, { target: "sess-local" });
    expect(local.ok ? "ok" : local.error.code).not.toBe("not_found");
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

const BACKGROUND = () => withAgentPrefs({ execMode: "background" });

describe("exec in the foreground (default)", () => {
  test("focuses the tab, types the command with the badge held, and returns the OSC 133 result", async () => {
    const writes: string[] = [];
    const focused: string[] = [];
    const badges: Array<{ sessionId: string; controlled: boolean }> = [];
    const exec = vi.fn();
    const { gateway } = createHarness({
      execCommand: exec,
      writeSession: (_sessionId, data) => writes.push(data),
      focusSession: (sessionId) => focused.push(sessionId),
      setSessionAgentControlled: (sessionId) => badges.push({ sessionId, controlled: true }),
      clearSessionAgentControlled: (sessionId) => badges.push({ sessionId, controlled: false }),
      waitForCommandCompletion: async () => ({
        command: "ls -la",
        exitCode: 0,
        output: "file-a",
        truncated: false
      })
    });

    const result = await gateway.execCommand(CLIENT, { target: "sess-full", command: "ls -la" });

    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.data).toEqual({
      sessionId: "sess-full",
      mode: "foreground",
      command: "ls -la",
      exitCode: 0,
      output: "file-a",
      stderr: null,
      waitTimedOut: false,
      actualCwd: null
    });
    expect(writes).toEqual(["ls -la\r"]);
    expect(focused).toEqual(["sess-full"]);
    expect(badges).toEqual([
      { sessionId: "sess-full", controlled: true },
      { sessionId: "sess-full", controlled: false }
    ]);
    // The exec channel is never touched in the foreground.
    expect(exec).not.toHaveBeenCalled();
  });

  test("reports waitTimedOut with an unknown exit code when no completion mark arrives", async () => {
    const { gateway } = createHarness();
    const result = await gateway.execCommand(CLIENT, { target: "sess-full", command: "ls" });
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.data).toMatchObject({ exitCode: null, output: "", waitTimedOut: true });
  });

  test("waits the promised 120s by default; timeoutSec shortens it, execTimeoutSec does not", async () => {
    const waits: number[] = [];
    const { gateway } = createHarness({
      waitForCommandCompletion: async (_sessionId, timeoutMs) => {
        waits.push(timeoutMs);
        return null;
      },
      getPreferences: () => withAgentPrefs({ execTimeoutSec: 30 })
    });
    await gateway.execCommand(CLIENT, { target: "sess-full", command: "ls" });
    await gateway.execCommand(CLIENT, { target: "sess-full", command: "ls", timeoutSec: 10 });
    expect(waits).toEqual([120_000, 10_000]);
  });

  test("a wait that runs out settles the activity as unsettled, not succeeded", async () => {
    const events: Array<{ status: string; summary: string }> = [];
    const { gateway } = createHarness({
      emitActivity: (event) => events.push({ status: event.status, summary: event.summary })
    });
    await gateway.execCommand(CLIENT, { target: "sess-full", command: "sleep 999" });
    expect(events.at(-1)?.status).toBe("unsettled");
    expect(events.at(-1)?.summary).toContain("仍在标签页中运行");
  });

  test("local shells run in the foreground too", async () => {
    const writes: string[] = [];
    const { gateway } = createHarness({ writeSession: (_id, data) => writes.push(data) });
    const result = await gateway.execCommand(CLIENT, { target: "sess-local", command: "ls" });
    expect(result.ok).toBe(true);
    expect(writes).toEqual(["ls\r"]);
  });

  test("an unsubmitted line of the user's fails the call with what they typed", async () => {
    const write = vi.fn();
    let pending: string | null = "vim /etc/nginx.conf";
    const { gateway } = createHarness({
      writeSession: write,
      pendingInput: async () => pending
    });

    const intervened = await gateway.execCommand(CLIENT, { target: "sess-full", command: "ls" });
    expect(intervened.ok).toBe(false);
    if (!intervened.ok) {
      expect(intervened.error.code).toBe("human_intervention");
      expect(intervened.error.message).toContain("vim /etc/nginx.conf");
    }
    expect(write).not.toHaveBeenCalled();

    // The user submits or clears their line: the very next call goes through.
    pending = "";
    const clean = await gateway.execCommand(CLIENT, { target: "sess-full", command: "ls" });
    expect(clean.ok).toBe(true);
    expect(write).toHaveBeenCalledTimes(1);
  });

  test("the unsubmitted text is redacted and capped in the error", async () => {
    const { gateway } = createHarness({
      pendingInput: async () => `mysql -u root -phunter2 ${"x".repeat(100)}`
    });
    const result = await gateway.execCommand(CLIENT, { target: "sess-full", command: "ls" });
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error.message).not.toContain("hunter2");
      expect(result.error.message.length).toBeLessThan(160);
    }
  });

  test("without a prompt mark, keystrokes after the last agent call fail the next one exactly once", async () => {
    let clock = 1_000;
    let lastUserInput: number | null = null;
    const write = vi.fn();
    const { gateway } = createHarness({
      writeSession: write,
      now: () => clock,
      pendingInput: async () => null,
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
    expect(write).toHaveBeenCalledTimes(2);
  });
});

describe("exec in the background (user's choice)", () => {
  test("borrows the session's connection, inherits its OSC cwd and never touches the tab", async () => {
    const exec = vi.fn(async () => ({
      stdout: "/var/www\n",
      stderr: "warn",
      exitCode: 0,
      executedAt: TIMESTAMP
    }));
    const write = vi.fn();
    const focus = vi.fn();
    const pendingInput = vi.fn(async () => "half-typed");
    const { gateway } = createHarness({
      execCommand: exec,
      writeSession: write,
      focusSession: focus,
      pendingInput,
      getPreferences: BACKGROUND
    });
    const result = await gateway.execCommand(CLIENT, { target: "sess-full", command: "pwd" });

    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.data).toEqual({
      sessionId: "sess-full",
      mode: "background",
      command: "pwd",
      exitCode: 0,
      output: "/var/www\n",
      stderr: "warn",
      waitTimedOut: false,
      actualCwd: "/var/www"
    });
    expect(exec).toHaveBeenCalledWith(
      prodHk.id,
      "pwd",
      expect.objectContaining({ cwd: "/var/www" })
    );
    expect(write).not.toHaveBeenCalled();
    expect(focus).not.toHaveBeenCalled();
    // A background run cannot type over anyone, so the line is not even looked at.
    expect(pendingInput).not.toHaveBeenCalled();
  });

  test("a local shell has no exec channel and falls back to the foreground", async () => {
    const exec = vi.fn();
    const writes: string[] = [];
    const { gateway } = createHarness({
      execCommand: exec,
      writeSession: (_id, data) => writes.push(data),
      getPreferences: BACKGROUND
    });
    const result = await gateway.execCommand(CLIENT, { target: "sess-local", command: "ls" });
    expect(result.ok).toBe(true);
    if (result.ok && !("status" in result.data)) expect(result.data.mode).toBe("foreground");
    expect(writes).toEqual(["ls\r"]);
    expect(exec).not.toHaveBeenCalled();
  });
});

describe("exec approval (the Permission mode)", () => {
  test("every command waits for the user's click: pending → approved → runs", async () => {
    const sent: Array<{ kind?: string; command?: string; mode?: string }> = [];
    const writes: string[] = [];
    const { gateway } = createHarness({
      getPreferences: () => withAgentPrefs({ execApproval: "permission" }),
      requestOpenSession: (r) => sent.push({ kind: r.kind, command: r.command, mode: r.mode }),
      writeSession: (_id, data) => writes.push(data),
      openWaitMs: 20
    });

    const first = await gateway.execCommand(CLIENT, { target: "sess-full", command: "uptime" });
    expect(first.ok).toBe(true);
    if (!first.ok || !("status" in first.data)) return;
    expect(first.data.status).toBe("pending");
    expect(sent).toEqual([{ kind: "exec", command: "uptime", mode: "foreground" }]);
    expect(writes).toEqual([]);
    const requestId = first.data.requestId;

    const again = gateway.execCommand(CLIENT, { requestId });
    gateway.respondOpen({ id: requestId, approved: true });
    const ran = await again;
    expect(ran.ok).toBe(true);
    if (!ran.ok || "status" in ran.data) return;
    expect(ran.data.mode).toBe("foreground");
    expect(writes).toEqual(["uptime\r"]);
    expect(sent).toHaveLength(1);

    const stale = await gateway.execCommand(CLIENT, { requestId });
    expect(stale.ok).toBe(false);
    if (!stale.ok) expect(stale.error.code).toBe("not_found");
  });

  test("a refusal is denied and nothing is written", async () => {
    const write = vi.fn();
    const { gateway } = createHarness({
      getPreferences: () => withAgentPrefs({ execApproval: "permission" }),
      writeSession: write,
      openWaitMs: 20
    });
    const first = await gateway.execCommand(CLIENT, { target: "sess-full", command: "uptime" });
    if (!first.ok || !("status" in first.data)) throw new Error("expected pending");
    const again = gateway.execCommand(CLIENT, { requestId: first.data.requestId });
    gateway.respondOpen({ id: first.data.requestId, approved: false });
    const denied = await again;
    expect(denied.ok).toBe(false);
    if (!denied.ok) expect(denied.error.code).toBe("denied");
    expect(write).not.toHaveBeenCalled();
  });

  test("the blacklist and consent gate run before the user is even asked", async () => {
    const sent = vi.fn();
    const { gateway } = createHarness({
      getPreferences: () => withAgentPrefs({ execApproval: "permission" }),
      requestOpenSession: sent
    });
    const blocked = await gateway.execCommand(CLIENT, { target: "sess-full", command: "rm -rf /" });
    expect(blocked.ok).toBe(false);
    const env = await gateway.execCommand(CLIENT, { target: "sess-full", command: "cat .env" });
    expect(env.ok).toBe(false);
    expect(sent).not.toHaveBeenCalled();
  });

  test("Auto (the default) runs straight away with no request", async () => {
    const sent = vi.fn();
    const { gateway } = createHarness({ requestOpenSession: sent });
    const result = await gateway.execCommand(CLIENT, { target: "sess-full", command: "uptime" });
    expect(result.ok).toBe(true);
    expect(sent).not.toHaveBeenCalled();
  });
});

describe("exec policy gates (both modes)", () => {
  test(".env files need explicit consent", async () => {
    const { gateway } = createHarness();

    const refused = await gateway.execCommand(CLIENT, { target: "sess-full", command: "cat .env" });
    expect(refused.ok).toBe(false);
    if (!refused.ok) {
      expect(refused.error.code).toBe("consent_required");
      expect(refused.error.message).toContain(".env");
    }

    const typed = await gateway.execCommand(CLIENT, {
      target: "sess-full",
      command: "vim .env.prod"
    });
    expect(typed.ok).toBe(false);
    if (!typed.ok) expect(typed.error.code).toBe("consent_required");

    const agreed = await gateway.execCommand(CLIENT, {
      target: "sess-full",
      command: "cat .env",
      allowSensitive: true
    });
    expect(agreed.ok).toBe(true);
  });

  test("an unknown or not-yet-connected session is refused", async () => {
    const { gateway } = createHarness();

    const missing = await gateway.execCommand(CLIENT, { target: "sess-gone", command: "ls" });
    expect(missing.ok).toBe(false);
    if (!missing.ok) expect(missing.error.code).toBe("not_found");

    const connecting = await gateway.execCommand(CLIENT, {
      target: "sess-connecting",
      command: "ls"
    });
    expect(connecting.ok).toBe(false);
    if (!connecting.ok) expect(connecting.error.code).toBe("unavailable");
  });

  test("the preset dangerous list blocks without any dialog", async () => {
    const exec = vi.fn();
    const write = vi.fn();
    const { gateway } = createHarness({ execCommand: exec, writeSession: write });
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
    expect(write).not.toHaveBeenCalled();
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
        withAgentPrefs({
          execMode: "background",
          blacklist: ["kubectl delete", String.raw`^docker\s+system\s+prune`]
        })
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
      // No completion mark in this harness: the call returns, the command does not.
      { tool: "exec", status: "unsettled" }
    ]);
  });
});
