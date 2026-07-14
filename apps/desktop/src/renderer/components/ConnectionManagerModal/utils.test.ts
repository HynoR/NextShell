import { describe, expect, test } from "bun:test";
import type { CloudSyncWorkspaceProfile, ConnectionProfile } from "@nextshell/core";
import { CONNECTION_ZONES } from "@nextshell/shared";
import {
  buildManagerTree,
  collectFlatLeafIds,
  collectGroupLeafIds,
  normalizeGroupPath,
  sortMgrChildren
} from "./utils/tree";
import {
  toConnectionPayload,
  toQuickUpsertInput,
  type ConnectionFormValues
} from "./utils/connectionForm";

const makeConnection = (overrides: Partial<ConnectionProfile>): ConnectionProfile => ({
  id: "11111111-1111-4111-8111-111111111111",
  name: "alpha",
  host: "alpha.example.com",
  port: 22,
  username: "root",
  authType: "password",
  strictHostKeyChecking: false,
  terminalEncoding: "utf-8",
  backspaceMode: "ascii-backspace",
  deleteMode: "vt220-delete",
  groupPath: "/server/default",
  tags: [],
  favorite: false,
  monitorSession: false,
  createdAt: "2026-03-15T00:00:00.000Z",
  updatedAt: "2026-03-15T00:00:00.000Z",
  ...overrides
});

describe("ConnectionManagerModal tree helpers", () => {
  test("normalizes group paths and enforces a valid zone prefix", () => {
    expect(normalizeGroupPath("prod/api/")).toBe("/server/prod/api");
    expect(normalizeGroupPath("/workspace/team-a")).toBe("/workspace/team-a");
    expect(normalizeGroupPath(undefined)).toBe("/server");
  });

  test("builds sorted trees with zone roots, folders, and visible leaf ids", () => {
    const alpha = makeConnection({
      id: "11111111-1111-4111-8111-111111111111",
      name: "zulu",
      groupPath: "/server/prod"
    });
    const beta = makeConnection({
      id: "22222222-2222-4222-8222-222222222222",
      name: "alpha",
      host: "beta.example.com",
      groupPath: "/server/prod"
    });
    const gamma = makeConnection({
      id: "33333333-3333-4333-8333-333333333333",
      name: "workspace-node",
      groupPath: "/workspace/team-a"
    });

    const workspaces: CloudSyncWorkspaceProfile[] = [
      {
        id: "workspace-1",
        apiBaseUrl: "https://sync.example.com",
        workspaceName: "team-a",
        displayName: "Team A",
        pullIntervalSec: 300,
        ignoreTlsErrors: false,
        enabled: true,
        createdAt: "2026-03-15T00:00:00.000Z",
        updatedAt: "2026-03-15T00:00:00.000Z",
        lastSyncAt: null,
        lastError: null
      }
    ];

    const tree = sortMgrChildren(
      buildManagerTree([alpha, beta, gamma], "", workspaces, ["/import/archive"]),
      "name"
    );

    expect(
      tree.children.map((child) => (child.type === "group" ? child.key : child.connection.id))
    ).toEqual(["mgr-group:server", "mgr-group:workspace/team-a", "mgr-group:import"]);

    const serverZone = tree.children[0];
    if (serverZone?.type !== "group") {
      throw new Error("expected server zone");
    }
    const prodGroup = serverZone.children[0];
    if (prodGroup?.type !== "group") {
      throw new Error("expected prod group");
    }

    expect(collectGroupLeafIds(prodGroup)).toEqual([
      "22222222-2222-4222-8222-222222222222",
      "11111111-1111-4111-8111-111111111111"
    ]);
    expect(
      collectFlatLeafIds(tree, new Set(["root", "mgr-group:server", "mgr-group:server/prod"]), 0)
    ).toEqual(["22222222-2222-4222-8222-222222222222", "11111111-1111-4111-8111-111111111111"]);
  });
});

describe("ConnectionManagerModal connection form helpers", () => {
  test("creates normalized payloads from form values", () => {
    const values: ConnectionFormValues = {
      name: "  ",
      host: " example.com ",
      port: 22,
      username: " admin ",
      authType: "privateKey",
      password: "  secret  ",
      sshKeyId: "key-1",
      hostFingerprint: "  SHA256:abc  ",
      strictHostKeyChecking: true,
      proxyId: "proxy-1",
      keepAliveEnabled: true,
      keepAliveIntervalSec: 30,
      terminalEncoding: "gbk",
      backspaceMode: "ascii-delete",
      deleteMode: "ascii-backspace",
      groupPath: "/server/ignored",
      groupZone: CONNECTION_ZONES.WORKSPACE,
      groupSubPath: " team-a/prod ",
      tags: [" web ", " ", "ops"],
      notes: "  note me  ",
      favorite: true,
      monitorSession: true
    };

    expect(
      toConnectionPayload(values, {
        selectedConnectionId: "44444444-4444-4444-8444-444444444444",
        generateId: () => "55555555-5555-4555-8555-555555555555"
      })
    ).toEqual({
      id: "44444444-4444-4444-8444-444444444444",
      name: "example.com:22",
      host: "example.com",
      port: 22,
      username: "admin",
      authType: "privateKey",
      password: "secret",
      sshKeyId: "key-1",
      hostFingerprint: "SHA256:abc",
      strictHostKeyChecking: true,
      proxyId: "proxy-1",
      keepAliveEnabled: true,
      keepAliveIntervalSec: 30,
      terminalEncoding: "gbk",
      backspaceMode: "ascii-delete",
      deleteMode: "ascii-backspace",
      groupPath: "/workspace/team-a/prod",
      tags: ["web", "ops"],
      notes: "note me",
      favorite: true,
      monitorSession: true
    });
  });

  test("creates quick upsert payloads by preserving existing fields", () => {
    const connection = makeConnection({
      sshKeyId: "key-1",
      notes: "before",
      favorite: false,
      monitorSession: true
    });

    expect(
      toQuickUpsertInput(connection, {
        name: "renamed",
        favorite: true
      })
    ).toMatchObject({
      id: connection.id,
      name: "renamed",
      host: connection.host,
      sshKeyId: "key-1",
      notes: "before",
      favorite: true,
      monitorSession: true
    });
  });
});
