import { afterEach, beforeEach, describe, expect, test } from "vitest";
import { DEFAULT_APP_PREFERENCES } from "@nextshell/core";
import type { ConnectionProfile } from "@nextshell/core";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

import {
  AgentGateway,
  type AgentClientIdentity,
  type AgentGatewayDeps,
  type AgentSessionInfo
} from "../agent-gateway";
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
  resourceId: "local-default-11111111-1111-1111-1111-111111111111"
});

const liveSession: AgentSessionInfo = {
  id: "sess-1",
  connectionId: granted.id,
  title: "prod-hk",
  status: "connected",
  type: "terminal",
  createdAt: TIMESTAMP,
  cwd: "/var/log",
  lastCommand: null
};

const CLIENT_IDENTITY: AgentClientIdentity = {
  id: "session-tools",
  name: "vitest",
  version: "1.0.0",
  transport: "http"
};

const deps: AgentGatewayDeps = {
  listConnections: () => [granted],
  listSessions: () => [liveSession],
  listScopedCommands: () => [
    {
      id: "cmd-1",
      name: "nginx status",
      group: "ops",
      command: "sudo systemctl status nginx",
      appendCr: true,
      scope: "local",
      createdAt: TIMESTAMP,
      updatedAt: TIMESTAMP
    }
  ],
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
  test("exposes exactly the nine session-centric tools with honest annotations", async () => {
    const listed = await client.listTools();

    expect(listed.tools.map((tool) => tool.name).sort()).toEqual([
      "command_save",
      "command_search",
      "exec",
      "session_focus",
      "session_history",
      "session_list",
      "session_read",
      "session_send_keys",
      "session_send_signal"
    ]);
    const readOnly = listed.tools.filter((tool) =>
      ["command_search", "session_history", "session_list", "session_read"].includes(tool.name)
    );
    for (const tool of readOnly) {
      expect(tool.annotations?.readOnlyHint).toBe(true);
      expect(tool.annotations?.destructiveHint).toBe(false);
    }
    // Only the tools that actually change remote state claim destructiveHint;
    // a client that escalates on that flag must not be crying wolf.
    expect(
      listed.tools
        .filter((tool) => tool.annotations?.destructiveHint === true)
        .map((tool) => tool.name)
        .sort()
    ).toEqual(["exec", "session_send_keys", "session_send_signal"]);
  });
});

describe("tool responses", () => {
  test("session_list returns the open tabs and no credential material", async () => {
    const result = await client.callTool({ name: "session_list", arguments: {} });
    const payload = structured(result);

    expect(payload.ok).toBe(true);
    expect(payload.data.sessions).toHaveLength(1);
    expect(payload.data.sessions[0]).toMatchObject({ id: "sess-1", title: "prod-hk" });

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

  test("exec borrows the live session's connection and returns structured output", async () => {
    const result = await client.callTool({
      name: "exec",
      arguments: { target: "sess-1", command: "pwd" }
    });
    const payload = structured(result);
    expect(payload.ok).toBe(true);
    expect(payload.data).toMatchObject({
      sessionId: "sess-1",
      connectionId: granted.id,
      exitCode: 0,
      command: "pwd",
      actualCwd: "/var/log"
    });
  });

  test("exec on an unknown session id is not_found", async () => {
    const result = await client.callTool({
      name: "exec",
      arguments: { target: "sess-gone", command: "pwd" }
    });

    expect((result as { isError?: boolean }).isError).toBe(true);
    const payload = structured(result);
    expect(payload.ok).toBe(false);
    expect(payload.error.code).toBe("not_found");
  });

  test("exec on a preset-blacklisted command is a hard error with the reason", async () => {
    const result = await client.callTool({
      name: "exec",
      arguments: { target: "sess-1", command: "rm -rf /" }
    });

    expect((result as { isError?: boolean }).isError).toBe(true);
    const payload = structured(result);
    expect(payload.ok).toBe(false);
    expect(payload.error.code).toBe("forbidden");
    expect(payload.error.message).toContain("blacklist");
  });

  test("session_send_keys on a preset-blacklisted text is refused", async () => {
    const result = await client.callTool({
      name: "session_send_keys",
      arguments: { target: "sess-1", text: "shutdown -h now", submit: true }
    });

    const payload = structured(result);
    expect(payload.ok).toBe(false);
    expect(payload.error.code).toBe("forbidden");
  });

  test("command_search returns scoped entries with the Stage 6 shape", async () => {
    const result = await client.callTool({ name: "command_search", arguments: {} });
    const payload = structured(result);

    expect(payload.ok).toBe(true);
    expect(payload.data.matches[0]).toMatchObject({
      id: "cmd-1",
      name: "nginx status",
      group: "ops",
      appendCr: true,
      scope: "local"
    });
    expect(payload.data.matches[0].command).toContain("systemctl");
  });

  test("command_save upserts an entry and echoes it back", async () => {
    const result = await client.callTool({
      name: "command_save",
      arguments: { name: "disk usage", command: "df -h", appendCr: true }
    });
    const payload = structured(result);

    expect(payload.ok).toBe(true);
    expect(payload.data.command).toMatchObject({
      id: "cmd-new",
      name: "disk usage",
      command: "df -h",
      appendCr: true
    });
  });

  test("session_read on an unknown session id is not_found", async () => {
    const result = await client.callTool({ name: "session_read", arguments: { target: "nope" } });
    expect(structured(result).error.code).toBe("not_found");
  });

  test("invalid arguments are rejected by the input schema", async () => {
    const result = await client.callTool({
      name: "session_history",
      arguments: { target: "" }
    });

    expect((result as { isError?: boolean }).isError).toBe(true);
    expect(JSON.stringify(result)).toContain("Input validation error");
  });
});
