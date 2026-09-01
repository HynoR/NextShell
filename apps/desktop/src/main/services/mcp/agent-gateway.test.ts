import { beforeEach, describe, expect, test, vi } from "vitest";
import { DEFAULT_APP_PREFERENCES } from "@nextshell/core";
import type { ConnectionProfile, MonitorSnapshot, SavedCommand } from "@nextshell/core";
import type { AgentPromptResponse } from "@nextshell/shared";

import {
  AgentGateway,
  normalizeRemotePath,
  type AgentClientIdentity,
  type AgentGatewayDeps,
  type AgentRemoteFileStat,
  type AgentSessionInfo
} from "./agent-gateway";
import type { AgentTransferSnapshot } from "./transfers";

const TIMESTAMP = "2026-08-03T00:00:00.000Z";

/** Minimal Tier 2 transfer stub; the tracker itself is covered in transfers.test.ts. */
const stubTransfer = (
  direction: "upload" | "download",
  input: { connectionId: string; localPath: string; remotePath: string }
): AgentTransferSnapshot => ({
  taskId: "task-stub",
  direction,
  connectionId: input.connectionId,
  localPath: input.localPath,
  remotePath: input.remotePath,
  packed: false,
  state: "running",
  progress: 0,
  transferredBytes: 0,
  totalBytes: null,
  startedAt: TIMESTAMP,
  finishedAt: null,
  error: null
});


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
  transport: "socket",
  rateKey: "socket:claude-code"
};

const grantedFull = createConnection({
  id: "11111111-1111-1111-1111-111111111111",
  name: "prod-hk",
  host: "10.0.0.1",
  agentAccess: "full",
  resourceId: "local-default-11111111-1111-1111-1111-111111111111"
});

const grantedReadonly = createConnection({
  id: "22222222-2222-2222-2222-222222222222",
  name: "stage-hk",
  host: "10.0.0.2",
  agentAccess: "readonly",
  resourceId: "local-default-22222222-2222-2222-2222-222222222222"
});

const deniedExplicit = createConnection({
  id: "33333333-3333-3333-3333-333333333333",
  name: "secret-vault",
  host: "10.0.0.3",
  agentAccess: "off",
  resourceId: "local-default-33333333-3333-3333-3333-333333333333"
});

const deniedByOmission = createConnection({
  id: "44444444-4444-4444-4444-444444444444",
  name: "legacy-box",
  host: "10.0.0.4",
  resourceId: "local-default-44444444-4444-4444-4444-444444444444"
});

const ambiguousA = createConnection({
  id: "55555555-5555-5555-5555-555555555555",
  name: "shared",
  host: "10.0.0.5",
  agentAccess: "readonly",
  resourceId: "local-default-55555555-5555-5555-5555-555555555555"
});

const ambiguousB = createConnection({
  id: "66666666-6666-6666-6666-666666666666",
  name: "shared",
  host: "10.0.0.6",
  agentAccess: "readonly",
  resourceId: "local-default-66666666-6666-6666-6666-666666666666"
});

const SESSIONS: AgentSessionInfo[] = [
  {
    id: "sess-full",
    connectionId: grantedFull.id,
    title: "prod-hk",
    status: "connected",
    type: "terminal",
    createdAt: TIMESTAMP,
    cwd: "/var/www",
    lastCommand: "pwd"
  },
  {
    id: "sess-denied",
    connectionId: deniedExplicit.id,
    title: "secret-vault",
    status: "connected",
    type: "terminal",
    createdAt: TIMESTAMP,
    cwd: null,
    lastCommand: null
  }
];

const SNAPSHOT: MonitorSnapshot = {
  connectionId: grantedFull.id,
  loadAverage: [0.1, 0.2, 0.3],
  cpuPercent: 12,
  memoryPercent: 33,
  memoryUsedMb: 1024,
  memoryTotalMb: 4096,
  swapPercent: 0,
  swapUsedMb: 0,
  swapTotalMb: 0,
  diskPercent: 40,
  diskUsedGb: 20,
  diskTotalGb: 50,
  networkInMbps: 1,
  networkOutMbps: 2,
  networkInterface: "eth0",
  networkInterfaceOptions: ["eth0"],
  processes: [],
  capturedAt: TIMESTAMP
};

const fileStat = (overrides: Partial<AgentRemoteFileStat> = {}): AgentRemoteFileStat => ({
  path: "/etc/hosts",
  type: "file",
  size: 12,
  permissions: "0644",
  uid: 0,
  gid: 0,
  modifiedAt: TIMESTAMP,
  accessedAt: TIMESTAMP,
  ...overrides
});

interface Harness {
  gateway: AgentGateway;
  deps: AgentGatewayDeps;
}

const createHarness = (overrides: Partial<AgentGatewayDeps> = {}, limits = {}): Harness => {
  const deps: AgentGatewayDeps = {
    listConnections: () => [
      grantedFull,
      grantedReadonly,
      deniedExplicit,
      deniedByOmission,
      ambiguousA,
      ambiguousB
    ],
    isConnectionOnline: (id) => id === grantedFull.id,
    listSessions: () => SESSIONS,
    getMonitorSnapshot: async () => SNAPSHOT,
    listRemoteFiles: async () => [
      {
        name: "hosts",
        path: "/etc/hosts",
        type: "file",
        size: 12,
        permissions: "0644",
        owner: "root",
        group: "root",
        modifiedAt: TIMESTAMP
      }
    ],
    statRemoteFile: async () => fileStat(),
    readRemoteFile: async () => ({ bytes: Buffer.from("127.0.0.1 x"), truncated: false }),
    listSavedCommands: () => [],
    readSessionScreen: async () => null,
    writeSession: () => undefined,
    lastUserInputAt: () => null,
    waitForCommandCompletion: async () => null,
    openSession: async () => ({ id: "sess-agent", title: "agent", status: "connected" as const }),
    closeSession: async () => undefined,
    focusSession: () => undefined,
    setSessionAgentControlled: () => undefined,
    clearSessionAgentControlled: () => undefined,
    getSessionHistory: () => null,
    execCommand: async (_connectionId, _command) => ({
      stdout: "",
      stderr: "",
      exitCode: 0,
      executedAt: TIMESTAMP
    }),
    writeRemoteFile: async () => undefined,
    makeRemoteDirectory: async () => undefined,
    renameRemotePath: async () => undefined,
    deleteRemotePath: async () => undefined,
    statLocalPath: () => null,
    localPathContext: () => ({
      homeDir: "/home/tester",
      appDataDir: "/home/tester/.nextshell",
      allowedRoots: []
    }),
    startUpload: (input) => stubTransfer("upload", input),
    startDownload: (input) => stubTransfer("download", input),
    getTransfer: () => undefined,
    cancelTransfer: () => false,
    runningTransferCount: () => 0,
    retainConnection: () => () => undefined,
    closeConnectionIfIdle: async () => undefined,
    promptUser: async (request) => ({
      id: "00000000-0000-4000-8000-000000000000",
      canceled: false,
      value: request.kind === "confirm" ? "approved" : "answer"
    }),
    notifyUser: () => undefined,
    emitActivity: () => undefined,
    getPreferences: () => DEFAULT_APP_PREFERENCES,
    ...overrides
  };
  return { gateway: new AgentGateway(deps, { limits }), deps };
};

describe("host authorization", () => {
  test("host_list only exposes granted hosts and carries no credential fields", async () => {
    const { gateway } = createHarness();
    const result = await gateway.listHosts(CLIENT, {});

    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.data.hosts.map((host) => host.name)).toEqual([
      "prod-hk",
      "stage-hk",
      "shared",
      "shared"
    ]);

    const serialized = JSON.stringify(result.data);
    for (const forbidden of [
      "credentialRef",
      "secret://",
      "sshKeyId",
      "proxyId",
      "hostFingerprint",
      "password",
      "privateKey",
      "passphrase",
      "hunter2"
    ]) {
      expect(serialized).not.toContain(forbidden);
    }
  });

  test("an unauthorized host is not found even when addressed by exact name or host", async () => {
    const { gateway } = createHarness();

    for (const target of ["secret-vault", "10.0.0.3", "legacy-box", "10.0.0.4"]) {
      const result = await gateway.describeHost(CLIENT, { target });
      expect(result.ok).toBe(false);
      if (result.ok) continue;
      // "unauthorized" must be indistinguishable from "does not exist".
      expect(result.error.code).toBe("not_found");
      expect(result.error.message).not.toContain("access");
    }
  });

  test("readonly hosts reject calls that require write access", () => {
    const { gateway } = createHarness();

    expect(gateway.resolveTarget("stage-hk", "read").ok).toBe(true);
    const write = gateway.resolveTarget("stage-hk", "write");
    expect(write.ok).toBe(false);
    if (write.ok) return;
    expect(write.error.code).toBe("forbidden");

    const full = gateway.resolveTarget("prod-hk", "write");
    expect(full.ok).toBe(true);
  });

  test("ambiguous targets return candidates instead of a guess", async () => {
    const { gateway } = createHarness();
    const result = await gateway.describeHost(CLIENT, { target: "shared" });

    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.error.code).toBe("ambiguous");
    expect(result.error.candidates?.map((candidate) => candidate.host)).toEqual([
      "10.0.0.5",
      "10.0.0.6"
    ]);
  });

  test("sessions on unauthorized hosts are invisible", async () => {
    const { gateway } = createHarness();
    const result = await gateway.listSessions(CLIENT, {});

    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.data.sessions.map((session) => session.id)).toEqual(["sess-full"]);
  });
});

describe("limits and failure paths", () => {
  test("a new MCP session must be approved before it can see hosts", async () => {
    const promptUser = vi.fn<() => Promise<AgentPromptResponse>>(async () => ({
      id: "00000000-0000-4000-8000-000000000000",
      canceled: true
    }));
    const { gateway } = createHarness({ promptUser });
    const result = await gateway.listHosts(CLIENT, {});
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.error.code).toBe("forbidden");
    expect(promptUser).toHaveBeenCalledOnce();
  });

  test("rate limiting kicks in per client", async () => {
    const { gateway } = createHarness({}, { callsPerMinute: 2 });

    expect((await gateway.listHosts(CLIENT, {})).ok).toBe(true);
    expect((await gateway.listHosts(CLIENT, {})).ok).toBe(true);
    const third = await gateway.listHosts(CLIENT, {});

    expect(third.ok).toBe(false);
    if (third.ok) return;
    expect(third.error.code).toBe("rate_limited");

    // Re-running `initialize` mints a new session id; the budget must not follow it.
    const sameClientNewSession = await gateway.listHosts({ ...CLIENT, id: "session-2" }, {});
    expect(sameClientNewSession.ok).toBe(false);

    const otherClient = await gateway.listHosts(
      { ...CLIENT, id: "session-3", name: "cursor", rateKey: "socket:cursor" },
      {}
    );
    expect(otherClient.ok).toBe(true);
  });

  test("pruneRateLimits keeps a live budget and drops aged-out buckets", async () => {
    let now = 1_000_000;
    const { gateway } = createHarness({ now: () => now }, { callsPerMinute: 1 });

    expect((await gateway.listHosts(CLIENT, {})).ok).toBe(true);
    gateway.pruneRateLimits();
    // A disconnect/reconnect inside the window must not refill the budget.
    expect((await gateway.listHosts(CLIENT, {})).ok).toBe(false);

    now += 61_000;
    gateway.pruneRateLimits();
    expect((await gateway.listHosts(CLIENT, {})).ok).toBe(true);
  });

  test("per-host concurrency is enforced", async () => {
    let release: (() => void) | undefined;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    const { gateway } = createHarness(
      {
        listRemoteFiles: async () => {
          await gate;
          return [];
        }
      },
      { perHostConcurrency: 1 }
    );

    const first = gateway.listFiles(CLIENT, { target: "prod-hk", path: "/etc" });
    const second = await gateway.listFiles(CLIENT, { target: "prod-hk", path: "/etc" });

    expect(second.ok).toBe(false);
    if (!second.ok) {
      expect(second.error.code).toBe("busy");
    }
    release?.();
    expect((await first).ok).toBe(true);
  });

  test("call timeouts surface as a timeout error rather than hanging", async () => {
    vi.useFakeTimers();
    try {
      const { gateway } = createHarness({
        getPreferences: () => ({
          ...DEFAULT_APP_PREFERENCES,
          agent: { ...DEFAULT_APP_PREFERENCES.agent, execTimeoutSec: 1 }
        }),
        listRemoteFiles: () => new Promise(() => undefined)
      });

      const pending = gateway.listFiles(CLIENT, { target: "prod-hk", path: "/etc" });
      await vi.advanceTimersByTimeAsync(1_500);
      const result = await pending;

      expect(result.ok).toBe(false);
      if (result.ok) return;
      expect(result.error.code).toBe("timeout");
    } finally {
      vi.useRealTimers();
    }
  });

  test("raw exception text never reaches the agent", async () => {
    const { gateway } = createHarness({
      listRemoteFiles: async () => {
        throw new Error("connect ECONNREFUSED root@10.0.0.1:22 using /Users/me/.ssh/id_rsa");
      }
    });

    const result = await gateway.listFiles(CLIENT, { target: "prod-hk", path: "/etc" });
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.error.code).toBe("unavailable");
    expect(result.error.message).not.toContain("id_rsa");
    expect(result.error.message).not.toContain("10.0.0.1");
  });
});

describe("file reads", () => {
  test("rejects non-regular files such as /dev/zero", async () => {
    const { gateway } = createHarness({
      statRemoteFile: async () => fileStat({ path: "/dev/zero", type: "other", size: 0 })
    });

    const result = await gateway.readFile(CLIENT, { target: "prod-hk", path: "/dev/zero" });
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.error.code).toBe("invalid_argument");
  });

  test("refuses files above the read limit before transferring them", async () => {
    const readRemoteFile = vi.fn(async () => ({ bytes: Buffer.alloc(0), truncated: false }));
    const { gateway } = createHarness({
      statRemoteFile: async () => fileStat({ path: "/var/log/huge.log", size: 900 * 1024 }),
      readRemoteFile
    });

    const result = await gateway.readFile(CLIENT, { target: "prod-hk", path: "/var/log/huge.log" });
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.error.code).toBe("too_large");
    expect(readRemoteFile).not.toHaveBeenCalled();
  });

  test("a procfs file reporting size 0 is still bounded by the byte budget", async () => {
    const budgets: number[] = [];
    const { gateway } = createHarness({
      // /proc and /sys report st_size = 0 for regular files of any length, so
      // the stat gate lets them through: the budget has to reach the reader.
      statRemoteFile: async () => fileStat({ path: "/proc/kallsyms", size: 0 }),
      readRemoteFile: async (_connectionId, _remotePath, maxBytes) => {
        budgets.push(maxBytes);
        return { bytes: Buffer.alloc(maxBytes + 1, 0x41), truncated: true };
      }
    });

    const result = await gateway.readFile(CLIENT, {
      target: "prod-hk",
      path: "/proc/kallsyms",
      maxBytes: 128
    });

    expect(budgets).toEqual([128]);
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.data.bytes).toBe(128);
    expect(result.data.truncated).toBe(true);
  });

  test("a timed-out read aborts the underlying transfer instead of leaving it running", async () => {
    let aborted = false;
    const { gateway } = createHarness(
      {
        readRemoteFile: (_connectionId, _remotePath, _maxBytes, signal) =>
          new Promise((_resolve, reject) => {
            signal.addEventListener("abort", () => {
              aborted = true;
              reject(new Error("SFTP read aborted"));
            });
          })
      },
      { maxCallTimeoutMs: 20 }
    );

    const result = await gateway.readFile(CLIENT, { target: "prod-hk", path: "/etc/hosts" });

    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.error.code).toBe("timeout");
    expect(aborted).toBe(true);
  });

  test("binary content comes back base64 encoded", async () => {
    const { gateway } = createHarness({
      readRemoteFile: async () => ({ bytes: Buffer.from([0x00, 0x01, 0x02]), truncated: false })
    });

    const result = await gateway.readFile(CLIENT, { target: "prod-hk", path: "/tmp/blob" });
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.data.encoding).toBe("base64");
    expect(result.data.content).toBe(Buffer.from([0x00, 0x01, 0x02]).toString("base64"));
  });

  test("relative and empty paths are rejected", async () => {
    const { gateway } = createHarness();

    for (const path of ["etc/hosts", "~/.ssh/id_rsa", "   "]) {
      const result = await gateway.readFile(CLIENT, { target: "prod-hk", path });
      expect(result.ok).toBe(false);
      if (result.ok) continue;
      expect(result.error.code).toBe("invalid_argument");
    }
  });

  test("normalizeRemotePath collapses traversal segments", () => {
    const result = normalizeRemotePath("/var/www/../log/./app.log");
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.data).toBe("/var/log/app.log");
  });
});

describe("session awareness and exec policy", () => {
  test("session_history returns OSC-derived command output and can strip ANSI", async () => {
    const { gateway } = createHarness({
      getSessionHistory: () => ({
        integrationAvailable: true,
        entries: [
          {
            command: "du -sh -- *",
            exitCode: 0,
            startedAt: TIMESTAMP,
            finishedAt: TIMESTAMP,
            output: "\u001b[31m10G uploads\u001b[0m",
            truncated: false
          }
        ]
      })
    });

    const result = await gateway.sessionHistory(CLIENT, {
      target: "sess-full",
      stripAnsi: true
    });
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.data.integrationAvailable).toBe(true);
    expect(result.data.entries[0]?.command).toBe("du -sh -- *");
    expect(result.data.entries[0]?.output).toBe("10G uploads");
  });

  test("a session target inherits its OSC cwd and retains the pooled connection", async () => {
    const calls: Array<{ connectionId: string; command: string; cwd?: string }> = [];
    const release = vi.fn();
    const close = vi.fn(async () => undefined);
    const activities: string[] = [];
    const { gateway } = createHarness({
      execCommand: async (connectionId, command, options) => {
        calls.push({ connectionId, command, cwd: options.cwd });
        return {
          stdout: "10G uploads\n",
          stderr: "",
          exitCode: 0,
          cwd: options.cwd,
          executedAt: TIMESTAMP
        };
      },
      retainConnection: () => release,
      closeConnectionIfIdle: close,
      emitActivity: (activity) => activities.push(activity.status)
    });

    const result = await gateway.execCommand(CLIENT, {
      target: "sess-full",
      command: "du -sh -- *"
    });
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(calls).toEqual([
      { connectionId: grantedFull.id, command: "du -sh -- *", cwd: "/var/www" }
    ]);
    expect(result.data.actualCwd).toBe("/var/www");
    expect(result.data.risk.level).toBe("readonly");
    expect(release).toHaveBeenCalledOnce();
    expect(close).toHaveBeenCalledWith(grantedFull.id);
    expect(activities).toEqual(["running", "succeeded"]);
  });

  test("readonly hosts allow proven reads but reject unknown commands without prompting", async () => {
    const exec = vi.fn(async () => ({
      stdout: "ok",
      stderr: "",
      exitCode: 0,
      executedAt: TIMESTAMP,
      cwd: "/srv"
    }));
    const prompt = vi.fn(async () => ({
      id: "00000000-0000-4000-8000-000000000000",
      canceled: false,
      value: "approved"
    }));
    const { gateway } = createHarness({ execCommand: exec, promptUser: prompt });
    await gateway.listHosts(CLIENT, {});
    prompt.mockClear();

    expect(
      (await gateway.execCommand(CLIENT, {
        target: "stage-hk",
        command: "systemctl restart nginx"
      })).ok
    ).toBe(false);
    expect(prompt).not.toHaveBeenCalled();

    const read = await gateway.execCommand(CLIENT, {
      target: "stage-hk",
      command: "systemctl status nginx"
    });
    expect(read.ok).toBe(true);
    expect(exec).toHaveBeenCalledOnce();
  });

  test("dangerous commands require approval and a denial prevents execution", async () => {
    const exec = vi.fn();
    const prompt = vi.fn<() => Promise<AgentPromptResponse>>(async () => ({
      id: "00000000-0000-4000-8000-000000000000",
      canceled: true
    }));
    const { gateway } = createHarness({ execCommand: exec, promptUser: prompt });
    prompt.mockResolvedValueOnce({
      id: "00000000-0000-4000-8000-000000000000",
      canceled: false,
      value: "approved"
    });
    await gateway.listHosts(CLIENT, {});
    prompt.mockClear();

    const result = await gateway.execCommand(CLIENT, {
      target: "prod-hk",
      command: "rm -rf /"
    });
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.error.code).toBe("forbidden");
    expect(prompt).toHaveBeenCalledOnce();
    expect(exec).not.toHaveBeenCalled();
  });

  test("an offline unpinned host cannot use agent-driven TOFU", async () => {
    const unpinned = { ...grantedFull, hostFingerprint: undefined };
    const exec = vi.fn();
    const { gateway } = createHarness({
      listConnections: () => [unpinned],
      isConnectionOnline: () => false,
      execCommand: exec
    });
    const result = await gateway.execCommand(CLIENT, {
      target: unpinned.id,
      command: "pwd"
    });
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.error.code).toBe("unavailable");
    expect(exec).not.toHaveBeenCalled();
  });
});

describe("command search", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  const savedCommand = (command: string, id = "cmd-1"): SavedCommand => ({
    id,
    name: "deploy",
    group: "ops",
    command,
    createdAt: TIMESTAMP,
    updatedAt: TIMESTAMP
  });

  test("redacts secrets and drops commands naming unauthorized hosts", async () => {
    const { gateway } = createHarness({
      listSavedCommands: () => [
        savedCommand("curl -H 'Authorization: Bearer abcdef123456' https://10.0.0.1/deploy"),
        savedCommand("ssh ops@secret-vault", "cmd-2"),
        savedCommand("ssh ops@10.0.0.3", "cmd-3")
      ]
    });

    const result = await gateway.searchCommands(CLIENT, {});
    expect(result.ok).toBe(true);
    if (!result.ok) return;

    expect(result.data.matches).toHaveLength(1);
    const serialized = JSON.stringify(result.data);
    expect(serialized).not.toContain("abcdef123456");
    expect(serialized).not.toContain("secret-vault");
    expect(serialized).not.toContain("10.0.0.3");
    expect(result.data.source).toBe("library");
  });

  test("the global shell history is never a source", async () => {
    const historyReader = vi.fn(() => [
      { command: "export DB_PASSWORD=hunter2", useCount: 3, lastUsedAt: TIMESTAMP }
    ]);
    const { gateway } = createHarness({
      // Not part of AgentGatewayDeps at all; the cast proves the gateway cannot
      // reach shell history even when a caller tries to hand it over.
      ...({ listCommandHistory: historyReader } as Partial<AgentGatewayDeps>)
    });

    const result = await gateway.searchCommands(CLIENT, { query: "export" });
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.data.matches).toEqual([]);
    expect(historyReader).not.toHaveBeenCalled();
  });

  test("redacts common credential shapes from agent output", async () => {
    const leaky = [
      "export DB_PASSWORD=hunter2",
      "export AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI",
      "psql postgresql://app:S3cr3tOne@10.0.0.1:5432/prod",
      "git clone https://oauth2:ghp_AbCdEf123@example.com/acme/private.git",
      "sshpass -p 'S3cr3tTwo' ssh root@10.0.0.1",
      "mysql -h db -u root -p'S3cr3tThree'",
      "curl -u admin:S3cr3tFour https://api.example.com"
    ];
    const { gateway } = createHarness({
      listSavedCommands: () => leaky.map((command, index) => savedCommand(command, `cmd-${index}`))
    });

    const result = await gateway.searchCommands(CLIENT, {});
    expect(result.ok).toBe(true);
    if (!result.ok) return;

    expect(result.data.matches).toHaveLength(leaky.length);
    const serialized = JSON.stringify(result.data);
    for (const secret of [
      "hunter2",
      "wJalrXUtnFEMI",
      "S3cr3tOne",
      "ghp_AbCdEf123",
      "S3cr3tTwo",
      "S3cr3tThree",
      "S3cr3tFour"
    ]) {
      expect(serialized).not.toContain(secret);
    }
  });
});

describe("prompt-flooding guards", () => {
  test("a denied client is remembered for its MCP session instead of re-prompting", async () => {
    const prompts: string[] = [];
    const { gateway } = createHarness({
      promptUser: async (request) => {
        prompts.push(request.title);
        return { id: "00000000-0000-4000-8000-000000000000", canceled: true };
      }
    });

    for (let index = 0; index < 5; index += 1) {
      const result = await gateway.listHosts(CLIENT, {});
      expect(result.ok).toBe(false);
      if (!result.ok) expect(result.error.code).toBe("forbidden");
    }

    expect(prompts).toEqual(["新的 Agent 客户端请求接入"]);
  });

  test("the approval prompt is only reachable while the client has call budget", async () => {
    const prompts: string[] = [];
    const { gateway } = createHarness(
      {
        promptUser: async (request) => {
          prompts.push(request.title);
          // Never approved and never denied: the client stays pending, which is
          // exactly the state a rate limit has to survive.
          return { id: "00000000-0000-4000-8000-000000000000", canceled: true };
        }
      },
      { callsPerMinute: 2 }
    );

    const first = await gateway.listHosts(CLIENT, {});
    const second = await gateway.listHosts(CLIENT, {});
    const third = await gateway.listHosts(CLIENT, {});

    expect(first.ok).toBe(false);
    expect(second.ok).toBe(false);
    expect(third.ok).toBe(false);
    if (!third.ok) expect(third.error.code).toBe("rate_limited");
    expect(prompts).toHaveLength(1);
  });

  test("exec confirmation costs a call-budget slot", async () => {
    const prompts: string[] = [];
    const { gateway } = createHarness(
      {
        promptUser: async (request) => {
          prompts.push(request.title);
          return {
            id: "00000000-0000-4000-8000-000000000000",
            canceled: false,
            ...(request.title === "新的 Agent 客户端请求接入" ? { value: "approved" } : {})
          };
        }
      },
      { callsPerMinute: 1 }
    );

    const first = await gateway.execCommand(CLIENT, { target: "prod-hk", command: "rm -rf /" });
    const second = await gateway.execCommand(CLIENT, { target: "prod-hk", command: "rm -rf /" });

    expect(first.ok).toBe(false);
    if (!first.ok) expect(first.error.code).toBe("forbidden");
    expect(second.ok).toBe(false);
    if (!second.ok) expect(second.error.code).toBe("rate_limited");
    expect(prompts).toEqual(["新的 Agent 客户端请求接入", "Agent 请求执行危险命令"]);
  });
});

describe("tier 2 writes and transfers", () => {
  const approving = (
    calls: Array<{ title: string; details?: string }>
  ): Partial<AgentGatewayDeps> => ({
    promptUser: async (request) => {
      calls.push({ title: request.title, details: request.details });
      return {
        id: "00000000-0000-4000-8000-000000000000",
        canceled: false,
        value: "approved"
      };
    }
  });

  test("a readonly host refuses every mutating tool", async () => {
    const { gateway } = createHarness();

    for (const result of [
      await gateway.writeFile(CLIENT, { target: "stage-hk", path: "/tmp/x", content: "hi" }),
      await gateway.makeDirectory(CLIENT, { target: "stage-hk", path: "/tmp/x" }),
      await gateway.renamePath(CLIENT, { target: "stage-hk", from: "/tmp/a", to: "/tmp/b" }),
      await gateway.deletePath(CLIENT, { target: "stage-hk", path: "/tmp/x", type: "file" }),
      await gateway.uploadTransfer(CLIENT, {
        target: "stage-hk",
        localPath: "/Users/tester/repo/dist.tar.gz",
        remotePath: "/opt/app"
      })
    ]) {
      expect(result.ok).toBe(false);
      if (!result.ok) expect(result.error.code).toBe("forbidden");
    }
  });

  test("file_write asks before it writes and reports the byte count", async () => {
    const prompts: Array<{ title: string; details?: string }> = [];
    const written: Array<{ path: string; content: string }> = [];
    const { gateway } = createHarness({
      ...approving(prompts),
      writeRemoteFile: async (_id, remotePath, content) => {
        written.push({ path: remotePath, content: content.toString("utf8") });
      }
    });

    const result = await gateway.writeFile(CLIENT, {
      target: "prod-hk",
      path: "/opt/app/../app/config.yaml",
      content: "key: value"
    });

    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.data).toEqual({ path: "/opt/app/config.yaml", bytes: 10 });
    expect(written).toEqual([{ path: "/opt/app/config.yaml", content: "key: value" }]);
    expect(prompts.map((prompt) => prompt.title)).toEqual([
      "新的 Agent 客户端请求接入",
      "Agent 请求写入远端文件"
    ]);
  });

  test("confirmWrites off skips the write dialog but never the delete dialog", async () => {
    const prompts: Array<{ title: string; details?: string }> = [];
    const { gateway } = createHarness({
      ...approving(prompts),
      getPreferences: () => ({
        ...DEFAULT_APP_PREFERENCES,
        agent: { ...DEFAULT_APP_PREFERENCES.agent, confirmWrites: false }
      })
    });

    await gateway.makeDirectory(CLIENT, { target: "prod-hk", path: "/opt/app" });
    await gateway.deletePath(CLIENT, { target: "prod-hk", path: "/opt/old", type: "directory" });

    expect(prompts.map((prompt) => prompt.title)).toEqual([
      "新的 Agent 客户端请求接入",
      "Agent 请求删除远端路径"
    ]);
    expect(prompts.at(-1)?.details).toContain("递归删除");
  });

  test("deleting the filesystem root is refused outright", async () => {
    const { gateway } = createHarness();
    const result = await gateway.deletePath(CLIENT, {
      target: "prod-hk",
      path: "/",
      type: "directory"
    });
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.error.code).toBe("forbidden");
  });

  test("transfer_upload refuses a denied local path before any confirmation", async () => {
    const prompts: Array<{ title: string; details?: string }> = [];
    const started: string[] = [];
    const { gateway } = createHarness({
      ...approving(prompts),
      startUpload: (input) => {
        started.push(input.localPath);
        return stubTransfer("upload", input);
      }
    });

    const byDirectory = await gateway.uploadTransfer(CLIENT, {
      target: "prod-hk",
      localPath: "/home/tester/.ssh/config",
      remotePath: "/tmp"
    });
    const byFilename = await gateway.uploadTransfer(CLIENT, {
      target: "prod-hk",
      localPath: "/home/tester/backup/id_rsa",
      remotePath: "/tmp"
    });

    expect(byDirectory.ok).toBe(false);
    if (!byDirectory.ok) {
      expect(byDirectory.error.code).toBe("forbidden");
      expect(byDirectory.error.message).toContain(".ssh");
    }
    expect(byFilename.ok).toBe(false);
    if (!byFilename.ok) {
      expect(byFilename.error.code).toBe("forbidden");
      expect(byFilename.error.message).toContain("id_rsa");
    }
    expect(started).toEqual([]);
    // A policy denial is decided before any dialog: the user is never asked to
    // approve something the policy has already refused.
    expect(prompts).toEqual([]);
  });

  test("transfer_upload shows the full local path and starts the transfer detached", async () => {
    const prompts: Array<{ title: string; details?: string }> = [];
    const { gateway } = createHarness({
      ...approving(prompts),
      statLocalPath: () => ({ type: "file", size: 4096 })
    });

    const result = await gateway.uploadTransfer(CLIENT, {
      target: "prod-hk",
      localPath: "/home/tester/repo/dist.tar.gz",
      remotePath: "/opt/app/dist.tar.gz"
    });

    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.data.state).toBe("running");
    const dialog = prompts.at(-1);
    expect(dialog?.title).toBe("Agent 请求上传本机文件");
    expect(dialog?.details).toContain("/home/tester/repo/dist.tar.gz");
    expect(dialog?.details).toContain("4096 字节");
  });

  test("a directory upload is announced as a packed transfer", async () => {
    const prompts: Array<{ title: string; details?: string }> = [];
    const packedFlags: boolean[] = [];
    const { gateway } = createHarness({
      ...approving(prompts),
      statLocalPath: () => ({ type: "directory", size: 0 }),
      startUpload: (input) => {
        packedFlags.push(input.packed);
        return stubTransfer("upload", input);
      }
    });

    const result = await gateway.uploadTransfer(CLIENT, {
      target: "prod-hk",
      localPath: "/home/tester/repo/dist",
      remotePath: "/opt/app"
    });

    expect(result.ok).toBe(true);
    expect(packedFlags).toEqual([true]);
    expect(prompts.at(-1)?.details).toContain("打包");
  });

  test("transfer_download refuses a destination that would be executed at login", async () => {
    const prompts: Array<{ title: string; details?: string }> = [];
    const { gateway } = createHarness(approving(prompts));

    const result = await gateway.downloadTransfer(CLIENT, {
      target: "prod-hk",
      remotePath: "/tmp/payload",
      localPath: "/home/tester/.zshrc"
    });

    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.error.code).toBe("forbidden");
  });

  test("transfer_status and transfer_cancel only see this client's tasks", async () => {
    const { gateway } = createHarness({
      getTransfer: (taskId, clientId) =>
        clientId === CLIENT.id && taskId === "mine"
          ? stubTransfer("upload", {
              connectionId: grantedFull.id,
              localPath: "/tmp/a",
              remotePath: "/tmp/b"
            })
          : undefined,
      cancelTransfer: () => true
    });

    const mine = await gateway.transferStatus(CLIENT, { taskId: "mine" });
    const theirs = await gateway.transferStatus(CLIENT, { taskId: "theirs" });
    const cancelled = await gateway.cancelTransfer(CLIENT, { taskId: "mine" });

    expect(mine.ok).toBe(true);
    expect(theirs.ok).toBe(false);
    if (!theirs.ok) expect(theirs.error.code).toBe("not_found");
    expect(cancelled.ok).toBe(true);
    if (cancelled.ok) expect(cancelled.data.cancelRequested).toBe(true);
  });

  test("a client cannot exceed its concurrent transfer budget", async () => {
    const { gateway } = createHarness(
      { runningTransferCount: () => 4, statLocalPath: () => ({ type: "file", size: 1 }) },
      { maxConcurrentTransfers: 4 }
    );

    const result = await gateway.uploadTransfer(CLIENT, {
      target: "prod-hk",
      localPath: "/home/tester/repo/dist.tar.gz",
      remotePath: "/opt/app"
    });

    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.error.code).toBe("busy");
  });

  test("an oversized inline write is rejected before it reaches SFTP", async () => {
    const writes: number[] = [];
    const { gateway } = createHarness(
      {
        writeRemoteFile: async (_id, _path, content) => {
          writes.push(content.byteLength);
        }
      },
      { maxWriteBytes: 16 }
    );

    const result = await gateway.writeFile(CLIENT, {
      target: "prod-hk",
      path: "/tmp/big",
      content: "x".repeat(64)
    });

    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.error.code).toBe("too_large");
    expect(writes).toEqual([]);
  });
});

describe("upload destination resolution", () => {
  const uploadHarness = (remoteType: AgentRemoteFileStat["type"]) => {
    const started: Array<{ remotePath: string; packed: boolean }> = [];
    const { gateway } = createHarness({
      statLocalPath: () => ({ type: "file", size: 10 }),
      statRemoteFile: async (_id, remotePath) => fileStat({ path: remotePath, type: remoteType }),
      startUpload: (input) => {
        started.push({ remotePath: input.remotePath, packed: input.packed });
        return stubTransfer("upload", input);
      }
    });
    return { gateway, started };
  };

  test("a file addressed at a remote directory lands inside it", async () => {
    const { gateway, started } = uploadHarness("directory");

    await gateway.uploadTransfer(CLIENT, {
      target: "prod-hk",
      localPath: "/home/tester/repo/dist/app-1.0.tar.gz",
      remotePath: "/opt/app/"
    });

    expect(started).toEqual([{ remotePath: "/opt/app/app-1.0.tar.gz", packed: false }]);
  });

  test("an explicit remote file path is used verbatim", async () => {
    const { gateway, started } = uploadHarness("file");

    await gateway.uploadTransfer(CLIENT, {
      target: "prod-hk",
      localPath: "/home/tester/repo/dist/app-1.0.tar.gz",
      remotePath: "/opt/app/release.tar.gz"
    });

    expect(started).toEqual([{ remotePath: "/opt/app/release.tar.gz", packed: false }]);
  });
});

describe("tier 3 session takeover", () => {
  const liveSession: AgentSessionInfo = {
    id: "sess-full",
    connectionId: grantedFull.id,
    title: "prod-hk",
    status: "connected",
    type: "terminal",
    createdAt: TIMESTAMP,
    cwd: "/var/www",
    lastCommand: "pwd"
  };

  const takeoverHarness = (overrides: Partial<AgentGatewayDeps> = {}, limits = {}) => {
    const prompts: Array<{ title: string; details?: string }> = [];
    const written: Array<{ sessionId: string; data: string }> = [];
    const badges: Array<{ sessionId: string; controlled: boolean }> = [];
    const harness = createHarness(
      {
        listSessions: () => [liveSession],
        writeSession: (sessionId, data) => {
          written.push({ sessionId, data });
        },
        setSessionAgentControlled: (sessionId) => badges.push({ sessionId, controlled: true }),
        clearSessionAgentControlled: (sessionId) => badges.push({ sessionId, controlled: false }),
        promptUser: async (request) => {
          prompts.push({ title: request.title, details: request.details });
          return {
            id: "00000000-0000-4000-8000-000000000000",
            canceled: false,
            value: "approved"
          };
        },
        ...overrides
      },
      limits
    );
    return { ...harness, prompts, written, badges };
  };

  test("injection asks first, shows the keys, and badges the tab", async () => {
    const { gateway, prompts, written, badges } = takeoverHarness();

    const result = await gateway.sendKeys(CLIENT, {
      target: "sess-full",
      text: "make deploy",
      submit: true
    });

    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.data).toMatchObject({ sessionId: "sess-full", submitted: true });
    expect(written).toEqual([{ sessionId: "sess-full", data: "make deploy\r" }]);
    expect(prompts.at(-1)?.title).toBe("Agent 请求向终端注入输入");
    expect(prompts.at(-1)?.details).toContain("make deploy");
    // The badge goes up before the write and comes back down afterwards.
    expect(badges).toEqual([
      { sessionId: "sess-full", controlled: true },
      { sessionId: "sess-full", controlled: false }
    ]);
  });

  test("control bytes hidden inside the text are named in the dialog", async () => {
    const { gateway, prompts } = takeoverHarness();

    await gateway.sendKeys(CLIENT, {
      target: "sess-full",
      text: "echo saferm -rf /tmp/x"
    });

    const details = prompts.at(-1)?.details ?? "";
    expect(details).toContain("<Ctrl-C>");
    expect(details).not.toContain("");
  });

  test("a denied injection writes nothing", async () => {
    const { gateway, written } = takeoverHarness({
      promptUser: async () => ({ id: "00000000-0000-4000-8000-000000000000", canceled: true })
    });

    const result = await gateway.sendKeys(CLIENT, { target: "sess-full", text: "rm -rf /" });
    expect(result.ok).toBe(false);
    expect(written).toEqual([]);
  });

  test("the user typing preempts injection", async () => {
    let clock = 1_000_000;
    // A single keystroke half a second ago; the clock moves, the keystroke does not.
    const lastKeystrokeAt = clock - 500;
    const { gateway, written } = takeoverHarness(
      { lastUserInputAt: () => lastKeystrokeAt, now: () => clock },
      { userInputPreemptionMs: 3000 }
    );

    const blocked = await gateway.sendKeys(CLIENT, { target: "sess-full", text: "ls" });
    expect(blocked.ok).toBe(false);
    if (!blocked.ok) expect(blocked.error.code).toBe("busy");
    expect(written).toEqual([]);

    // Once the person has stopped typing, injection is allowed again.
    clock += 5000;
    const allowed = await gateway.sendKeys(CLIENT, { target: "sess-full", text: "ls" });
    expect(allowed.ok).toBe(true);
  });

  test("waitForPrompt reports the shell's own completion, and times out honestly", async () => {
    const completed = await takeoverHarness({
      waitForCommandCompletion: async () => ({
        command: "make deploy",
        exitCode: 0,
        output: "done\n",
        truncated: false
      })
    }).gateway.sendKeys(CLIENT, {
      target: "sess-full",
      text: "make deploy",
      submit: true,
      waitForPrompt: true
    });

    expect(completed.ok).toBe(true);
    if (completed.ok) {
      expect(completed.data.completed).toEqual({
        command: "make deploy",
        exitCode: 0,
        output: "done\n",
        truncated: false
      });
      expect(completed.data.waitTimedOut).toBe(false);
    }

    // No shell integration: no mark ever arrives, and the result says so rather
    // than inventing an exit code.
    const timedOut = await takeoverHarness({
      waitForCommandCompletion: async () => null
    }).gateway.sendKeys(CLIENT, {
      target: "sess-full",
      text: "make deploy",
      submit: true,
      waitForPrompt: true
    });

    expect(timedOut.ok).toBe(true);
    if (timedOut.ok) {
      expect(timedOut.data.completed).toBeNull();
      expect(timedOut.data.waitTimedOut).toBe(true);
    }
  });

  test("send_signal maps names to control bytes; only interrupt skips the dialog", async () => {
    const { gateway, prompts, written } = takeoverHarness();

    await gateway.sendSignal(CLIENT, { target: "sess-full", signal: "interrupt" });
    await gateway.sendSignal(CLIENT, { target: "sess-full", signal: "eof" });

    expect(written.map((entry) => entry.data)).toEqual(["", ""]);
    expect(prompts.map((prompt) => prompt.title)).toEqual([
      "新的 Agent 客户端请求接入",
      "Agent 请求向终端发送控制信号"
    ]);
  });

  test("takeover is refused on a readonly host and on a disconnected session", async () => {
    const readonlySession: AgentSessionInfo = {
      ...liveSession,
      id: "sess-readonly",
      connectionId: grantedReadonly.id
    };
    const readonly = takeoverHarness({ listSessions: () => [readonlySession] });
    const onReadonly = await readonly.gateway.sendKeys(CLIENT, {
      target: "sess-readonly",
      text: "ls"
    });
    expect(onReadonly.ok).toBe(false);
    if (!onReadonly.ok) expect(onReadonly.error.code).toBe("forbidden");

    const dead = takeoverHarness({
      listSessions: () => [{ ...liveSession, status: "disconnected" }]
    });
    const onDead = await dead.gateway.sendKeys(CLIENT, { target: "sess-full", text: "ls" });
    expect(onDead.ok).toBe(false);
    if (!onDead.ok) expect(onDead.error.code).toBe("unavailable");
  });

  test("session_open produces a visible tab and marks it agent-controlled", async () => {
    const { gateway, badges, prompts } = takeoverHarness({
      openSession: async () => ({ id: "sess-new", title: "prod-hk", status: "connected" as const })
    });

    const result = await gateway.openSession(CLIENT, { target: "prod-hk" });

    expect(result.ok).toBe(true);
    if (result.ok) expect(result.data.sessionId).toBe("sess-new");
    expect(badges).toEqual([{ sessionId: "sess-new", controlled: true }]);
    expect(prompts.at(-1)?.title).toBe("Agent 请求打开终端标签");
  });

  test("session_focus works on a readonly host — it only moves the user's window", async () => {
    const focused: string[] = [];
    const { gateway } = takeoverHarness({
      listSessions: () => [{ ...liveSession, id: "sess-ro", connectionId: grantedReadonly.id }],
      focusSession: (sessionId) => focused.push(sessionId)
    });

    const result = await gateway.focusSession(CLIENT, { target: "sess-ro" });
    expect(result.ok).toBe(true);
    expect(focused).toEqual(["sess-ro"]);
  });
});

describe("global halt", () => {
  test("halting rejects every call without a dialog and forgets remembered approvals", async () => {
    const prompts: string[] = [];
    const { gateway } = createHarness({
      promptUser: async (request) => {
        prompts.push(request.title);
        return {
          id: "00000000-0000-4000-8000-000000000000",
          canceled: false,
          value: "approved",
          rememberForSession: true
        };
      }
    });

    // Approve the client and remember a dangerous command.
    const before = await gateway.execCommand(CLIENT, { target: "prod-hk", command: "rm -rf /" });
    expect(before.ok).toBe(true);
    const promptsBeforeHalt = prompts.length;

    gateway.setHalted(true);
    expect(gateway.isHalted).toBe(true);

    const halted = await gateway.execCommand(CLIENT, { target: "prod-hk", command: "rm -rf /" });
    expect(halted.ok).toBe(false);
    if (!halted.ok) expect(halted.error.code).toBe("forbidden");
    const listBlocked = await gateway.listHosts(CLIENT, {});
    expect(listBlocked.ok).toBe(false);
    expect(prompts).toHaveLength(promptsBeforeHalt);

    // Resuming must not silently restore "always allow this command".
    gateway.setHalted(false);
    const after = await gateway.execCommand(CLIENT, { target: "prod-hk", command: "rm -rf /" });
    expect(after.ok).toBe(true);
    expect(prompts.length).toBeGreaterThan(promptsBeforeHalt);
  });
});

describe("agent-controlled tab badges", () => {
  const openedSession: AgentSessionInfo = {
    id: "sess-agent",
    connectionId: grantedFull.id,
    title: "prod-hk",
    status: "connected",
    type: "terminal",
    createdAt: TIMESTAMP,
    cwd: "/root",
    lastCommand: null
  };

  const badgeHarness = (overrides: Partial<AgentGatewayDeps> = {}) => {
    const badges: Array<{ sessionId: string; controlled: boolean }> = [];
    const harness = createHarness({
      listSessions: () => [openedSession],
      writeSession: () => undefined,
      openSession: async () => ({ id: "sess-agent", title: "prod-hk", status: "connected" }),
      setSessionAgentControlled: (sessionId) => badges.push({ sessionId, controlled: true }),
      clearSessionAgentControlled: (sessionId) => badges.push({ sessionId, controlled: false }),
      promptUser: async () => ({
        id: "00000000-0000-4000-8000-000000000000",
        canceled: false,
        value: "approved"
      }),
      ...overrides
    });
    return { ...harness, badges };
  };

  test("a session the agent opened stays badged across injections", async () => {
    const { gateway, badges } = badgeHarness();

    await gateway.openSession(CLIENT, { target: "prod-hk" });
    badges.length = 0;
    await gateway.sendKeys(CLIENT, { target: "sess-agent", text: "ls", submit: true });

    // Owned session: injecting neither re-raises nor lowers the badge.
    expect(badges).toEqual([]);

    await gateway.closeSession(CLIENT, { target: "sess-agent" });
    expect(badges).toEqual([{ sessionId: "sess-agent", controlled: false }]);
  });

  test("a failed injection into a user's session still lowers the badge", async () => {
    const { gateway, badges } = badgeHarness({
      writeSession: () => {
        throw new Error("channel closed");
      }
    });

    const result = await gateway.sendKeys(CLIENT, { target: "sess-agent", text: "ls" });

    expect(result.ok).toBe(false);
    expect(badges).toEqual([
      { sessionId: "sess-agent", controlled: true },
      { sessionId: "sess-agent", controlled: false }
    ]);
  });

  test("halting lowers the badge on every session the agent owns", async () => {
    const { gateway, badges } = badgeHarness();

    await gateway.openSession(CLIENT, { target: "prod-hk" });
    badges.length = 0;
    gateway.setHalted(true);

    expect(badges).toEqual([{ sessionId: "sess-agent", controlled: false }]);
  });
});
