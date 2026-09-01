import { afterEach, beforeEach, describe, expect, test } from "vitest";
import { DEFAULT_APP_PREFERENCES } from "@nextshell/core";
import type { ConnectionProfile } from "@nextshell/core";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

import { AgentGateway, type AgentClientIdentity, type AgentGatewayDeps } from "../agent-gateway";
import { registerAgentTools } from "./index";

const TIMESTAMP = "2026-08-03T00:00:00.000Z";

const createConnection = (
  overrides: Partial<ConnectionProfile> & Pick<ConnectionProfile, "id" | "name" | "host">
): ConnectionProfile => ({
  port: 22,
  username: "ops",
  authType: "password",
  credentialRef: "secret://conn-11111111",
  sshKeyId: "key-1",
  proxyId: "proxy-1",
  hostFingerprint: "SHA256:deadbeef",
  notes: "password: hunter2",
  strictHostKeyChecking: false,
  terminalEncoding: "utf-8",
  backspaceMode: "ascii-backspace",
  deleteMode: "vt220-delete",
  groupPath: "/server",
  tags: ["prod"],
  favorite: false,
  monitorSession: false,
  createdAt: TIMESTAMP,
  updatedAt: TIMESTAMP,
  ...overrides
});

const granted = createConnection({
  id: "11111111-1111-1111-1111-111111111111",
  name: "prod-hk",
  host: "10.0.0.1",
  agentAccess: "readonly",
  resourceId: "local-default-11111111-1111-1111-1111-111111111111"
});

const denied = createConnection({
  id: "22222222-2222-2222-2222-222222222222",
  name: "secret-vault",
  host: "10.0.0.2",
  resourceId: "local-default-22222222-2222-2222-2222-222222222222"
});

const CLIENT_IDENTITY: AgentClientIdentity = {
  id: "session-tools",
  name: "vitest",
  version: "1.0.0",
  transport: "socket",
  rateKey: "socket:vitest"
};

const deps: AgentGatewayDeps = {
  listConnections: () => [granted, denied],
  isConnectionOnline: () => true,
  listSessions: () => [],
  getMonitorSnapshot: async () => null,
  listRemoteFiles: async () => [
    {
      name: "app.log",
      path: "/var/log/app.log",
      type: "file",
      size: 42,
      permissions: "0644",
      owner: "root",
      group: "root",
      modifiedAt: TIMESTAMP
    }
  ],
  statRemoteFile: async (_connectionId, remotePath) => ({
    path: remotePath,
    type: "file",
    size: 42,
    permissions: "0644",
    uid: 0,
    gid: 0,
    modifiedAt: TIMESTAMP,
    accessedAt: TIMESTAMP
  }),
  readRemoteFile: async () => ({ bytes: Buffer.from("hello world"), truncated: false }),
  getSessionHistory: () => null,
  execCommand: async () => ({ stdout: "", stderr: "", exitCode: 0, executedAt: TIMESTAMP }),
  retainConnection: () => () => undefined,
  closeConnectionIfIdle: async () => undefined,
  promptUser: async () => ({
    id: "00000000-0000-4000-8000-000000000000",
    canceled: false,
    value: "approved"
  }),
  notifyUser: () => undefined,
  emitActivity: () => undefined,
  listSavedCommands: () => [
    {
      id: "cmd-1",
      name: "nginx status",
      group: "ops",
      command: "sudo systemctl status nginx",
      isTemplate: false,
      createdAt: TIMESTAMP,
      updatedAt: TIMESTAMP
    }
  ],
  readSessionScreen: async () => null,
  writeSession: () => undefined,
  lastUserInputAt: () => null,
  waitForCommandCompletion: async () => null,
  openSession: async () => ({ id: "sess-agent", title: "agent", status: "connected" as const }),
  closeSession: async () => undefined,
  focusSession: () => undefined,
  setSessionAgentControlled: () => undefined,
  clearSessionAgentControlled: () => undefined,
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
  startUpload: (input) => ({
    taskId: "task-stub",
    direction: "upload" as const,
    connectionId: input.connectionId,
    localPath: input.localPath,
    remotePath: input.remotePath,
    packed: input.packed,
    state: "running" as const,
    progress: 0,
    transferredBytes: 0,
    totalBytes: null,
    startedAt: TIMESTAMP,
    finishedAt: null,
    error: null
  }),
  startDownload: (input) => ({
    taskId: "task-stub",
    direction: "download" as const,
    connectionId: input.connectionId,
    localPath: input.localPath,
    remotePath: input.remotePath,
    packed: false,
    state: "running" as const,
    progress: 0,
    transferredBytes: 0,
    totalBytes: null,
    startedAt: TIMESTAMP,
    finishedAt: null,
    error: null
  }),
  getTransfer: () => undefined,
  cancelTransfer: () => false,
  runningTransferCount: () => 0,
  getPreferences: () => DEFAULT_APP_PREFERENCES
};

let client: Client;
let server: McpServer;

beforeEach(async () => {
  const gateway = new AgentGateway(deps);
  server = new McpServer({ name: "nextshell", version: "0.0.0" });
  registerAgentTools(server, { gateway, client: CLIENT_IDENTITY });

  const [clientTransport, serverTransport] = InMemoryTransport.createLinkedPair();
  await server.connect(serverTransport);
  client = new Client({ name: "vitest", version: "1.0.0" });
  await client.connect(clientTransport);
});

afterEach(async () => {
  await client.close();
  await server.close();
});

const structured = (result: unknown): { ok: boolean; data?: any; error?: any } =>
  (result as { structuredContent: { ok: boolean; data?: unknown; error?: unknown } })
    .structuredContent as { ok: boolean; data?: any; error?: any };

describe("tool registration", () => {
  test("exposes read, exec, write, transfer and interaction tools with honest annotations", async () => {
    const listed = await client.listTools();

    expect(listed.tools.map((tool) => tool.name).sort()).toEqual([
      "ask_user",
      "command_search",
      "exec",
      "file_delete",
      "file_list",
      "file_mkdir",
      "file_read",
      "file_rename",
      "file_stat",
      "file_write",
      "host_describe",
      "host_list",
      "monitor_snapshot",
      "notify_user",
      "session_close",
      "session_focus",
      "session_history",
      "session_list",
      "session_open",
      "session_read",
      "session_send_keys",
      "session_send_signal",
      "transfer_cancel",
      "transfer_download",
      "transfer_status",
      "transfer_upload"
    ]);
    const readOnly = listed.tools.filter((tool) =>
      [
        "command_search",
        "file_list",
        "file_read",
        "file_stat",
        "host_describe",
        "host_list",
        "monitor_snapshot",
        "session_history",
        "session_list",
        "session_read",
        "transfer_status"
      ].includes(tool.name)
    );
    for (const tool of readOnly) {
      expect(tool.annotations?.readOnlyHint).toBe(true);
      expect(tool.annotations?.destructiveHint).toBe(false);
    }
    // Only the two tools that actually destroy data claim destructiveHint;
    // a client that escalates on that flag must not be crying wolf for mkdir.
    expect(
      listed.tools
        .filter((tool) => tool.annotations?.destructiveHint === true)
        .map((tool) => tool.name)
        .sort()
    ).toEqual([
      "exec",
      "file_delete",
      "session_close",
      "session_send_keys",
      "session_send_signal"
    ]);
    for (const name of ["file_write", "file_mkdir", "file_rename", "transfer_upload"]) {
      expect(listed.tools.find((tool) => tool.name === name)?.annotations).toMatchObject({
        readOnlyHint: false,
        destructiveHint: false
      });
    }
  });
});

describe("tool responses", () => {
  test("exec reaches the policy-gated gateway and returns structured output", async () => {
    const result = await client.callTool({
      name: "exec",
      arguments: { target: "prod-hk", command: "pwd" }
    });
    const payload = structured(result);
    expect(payload.ok).toBe(true);
    expect(payload.data).toMatchObject({ exitCode: 0, command: "pwd", risk: { level: "readonly" } });
  });

  test("host_list returns granted hosts and no credential material", async () => {
    const result = await client.callTool({ name: "host_list", arguments: {} });
    const payload = structured(result);

    expect(payload.ok).toBe(true);
    expect(payload.data.hosts).toHaveLength(1);
    expect(payload.data.hosts[0]).toMatchObject({
      name: "prod-hk",
      host: "10.0.0.1",
      user: "ops",
      access: "readonly"
    });

    const serialized = JSON.stringify(result);
    for (const forbidden of [
      "credentialRef",
      "secret://",
      "sshKeyId",
      "proxyId",
      "hostFingerprint",
      "hunter2",
      "privateKey",
      "passphrase"
    ]) {
      expect(serialized).not.toContain(forbidden);
    }
  });

  test("an unauthorized host is reported as not found", async () => {
    const result = await client.callTool({
      name: "host_describe",
      arguments: { target: "secret-vault" }
    });

    expect((result as { isError?: boolean }).isError).toBe(true);
    const payload = structured(result);
    expect(payload.ok).toBe(false);
    expect(payload.error.code).toBe("not_found");
  });

  test("file_list and file_read pass their output schema", async () => {
    const listed = await client.callTool({
      name: "file_list",
      arguments: { target: "prod-hk", path: "/var/log" }
    });
    expect(structured(listed).data.entries[0].name).toBe("app.log");

    const read = await client.callTool({
      name: "file_read",
      arguments: { target: "prod-hk", path: "/var/log/app.log" }
    });
    const payload = structured(read);
    expect(payload.data.encoding).toBe("utf-8");
    expect(payload.data.content).toBe("hello world");
  });

  test("monitor_snapshot reports unavailability instead of throwing", async () => {
    const result = await client.callTool({
      name: "monitor_snapshot",
      arguments: { target: "prod-hk" }
    });

    expect(structured(result).error.code).toBe("unavailable");
  });

  test("command_search returns library entries only", async () => {
    const result = await client.callTool({ name: "command_search", arguments: {} });
    const payload = structured(result);

    expect(payload.data.matches[0].command).toContain("systemctl");
    expect(payload.data.matches[0].source).toBe("library");
    expect(payload.data.source).toBe("library");
  });

  test("invalid arguments are rejected by the input schema", async () => {
    const result = await client.callTool({ name: "host_describe", arguments: { target: "" } });

    expect((result as { isError?: boolean }).isError).toBe(true);
    expect(JSON.stringify(result)).toContain("Input validation error");
  });
});
