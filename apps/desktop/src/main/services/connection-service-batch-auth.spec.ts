import { describe, expect, test } from "bun:test";
import {
  LOCAL_DEFAULT_SCOPE_KEY,
  type ConnectionProfile,
  type SshKeyProfile
} from "@nextshell/core";
import type {
  CachedConnectionRepository,
  CachedProxyRepository,
  CachedSshKeyRepository
} from "@nextshell/storage";
import type { EncryptedSecretVault } from "@nextshell/security";
import { ConnectionService } from "./connection-service";

const now = "2026-06-15T00:00:00.000Z";

const createConnection = (
  id: string,
  patch: Partial<ConnectionProfile> = {}
): ConnectionProfile => ({
  id,
  name: `conn-${id}`,
  host: `10.0.0.${id.slice(-1)}`,
  port: 22,
  username: "root",
  authType: "password",
  credentialRef: `secret://conn-${id}`,
  strictHostKeyChecking: false,
  terminalEncoding: "utf-8",
  backspaceMode: "ascii-backspace",
  deleteMode: "vt220-delete",
  groupPath: "/import/finalshell",
  tags: [],
  favorite: false,
  monitorSession: false,
  createdAt: now,
  updatedAt: now,
  originKind: "local",
  originScopeKey: LOCAL_DEFAULT_SCOPE_KEY,
  ...patch
});

const createSshKey = (id: string, patch: Partial<SshKeyProfile> = {}): SshKeyProfile => ({
  id,
  name: `key-${id}`,
  keyContentRef: `secret://sshkey-${id}`,
  createdAt: now,
  updatedAt: now,
  originKind: "local",
  originScopeKey: LOCAL_DEFAULT_SCOPE_KEY,
  ...patch
});

const createService = (connections: ConnectionProfile[], sshKeys: SshKeyProfile[] = []) => {
  const connectionMap = new Map(connections.map((connection) => [connection.id, connection]));
  const sshKeyMap = new Map(sshKeys.map((key) => [key.id, key]));
  const secrets = new Map<string, string>();
  for (const connection of connections) {
    if (connection.credentialRef) {
      secrets.set(connection.credentialRef, "old-secret");
    }
  }
  for (const key of sshKeys) {
    secrets.set(key.keyContentRef, "PRIVATE KEY");
    if (key.passphraseRef) {
      secrets.set(key.passphraseRef, "passphrase");
    }
  }
  const audit: Array<{ action: string; level: "info" | "warn" | "error"; message: string }> = [];

  const service = new ConnectionService({
    connections: {
      list: () => Array.from(connectionMap.values()),
      getById: (id: string) => connectionMap.get(id),
      save: (connection: ConnectionProfile) => {
        connectionMap.set(connection.id, connection);
        return connection;
      }
    } as unknown as CachedConnectionRepository,
    sshKeyRepo: {
      getById: (id: string) => sshKeyMap.get(id)
    } as unknown as CachedSshKeyRepository,
    proxyRepo: {
      getById: () => undefined
    } as unknown as CachedProxyRepository,
    vault: {
      storeCredential: async (key: string, secret: string) => {
        const ref = `secret://${key}`;
        secrets.set(ref, secret);
        return ref;
      },
      readCredential: async (ref: string) => secrets.get(ref),
      deleteCredential: async (ref: string) => {
        secrets.delete(ref);
      }
    } as unknown as EncryptedSecretVault,
    activeSessions: new Map(),
    disposeAllMonitorSessions: async () => undefined,
    closeConnectionIfIdle: async () => undefined,
    remoteEditManager: {} as any,
    monitorStates: new Map(),
    appendAuditLogIfEnabled: (payload) => {
      audit.push(payload);
    },
    sendSessionStatus: () => undefined
  });

  return { service, connectionMap, secrets, audit };
};

describe("ConnectionService batch auth update", () => {
  test("updates password auth for all connections under a group path", async () => {
    const { service, connectionMap, secrets } = createService([
      createConnection("00000000-0000-4000-8000-000000000001"),
      createConnection("00000000-0000-4000-8000-000000000002", {
        groupPath: "/import/finalshell/prod"
      }),
      createConnection("00000000-0000-4000-8000-000000000003", {
        groupPath: "/server/manual"
      })
    ]);

    const result = await service.batchUpdateConnectionAuth({
      target: { type: "group", groupPath: "/import/finalshell" },
      auth: { authType: "password", password: "shared-password" }
    });

    expect(result.total).toBe(2);
    expect(result.updated).toBe(2);
    expect(result.failed).toBe(0);
    expect(connectionMap.get("00000000-0000-4000-8000-000000000001")?.authType).toBe("password");
    expect(secrets.get("secret://conn-00000000-0000-4000-8000-000000000001")).toBe(
      "shared-password"
    );
    expect(secrets.get("secret://conn-00000000-0000-4000-8000-000000000002")).toBe(
      "shared-password"
    );
    expect(secrets.get("secret://conn-00000000-0000-4000-8000-000000000003")).toBe("old-secret");
  });

  test("updates selected connections to private key auth", async () => {
    const key = createSshKey("10000000-0000-4000-8000-000000000001");
    const { service, connectionMap, secrets } = createService(
      [
        createConnection("00000000-0000-4000-8000-000000000001"),
        createConnection("00000000-0000-4000-8000-000000000002")
      ],
      [key]
    );

    const result = await service.batchUpdateConnectionAuth({
      target: {
        type: "connections",
        connectionIds: [
          "00000000-0000-4000-8000-000000000001",
          "00000000-0000-4000-8000-000000000002"
        ]
      },
      auth: { authType: "privateKey", sshKeyId: key.id }
    });

    expect(result.total).toBe(2);
    expect(result.updated).toBe(2);
    expect(result.failed).toBe(0);
    const updatedConnection = connectionMap.get("00000000-0000-4000-8000-000000000001");
    expect(updatedConnection?.authType).toBe("privateKey");
    expect(updatedConnection?.sshKeyId).toBe(key.id);
    expect(updatedConnection?.credentialRef).toBe(undefined);
    expect(secrets.has("secret://conn-00000000-0000-4000-8000-000000000001")).toBe(false);
  });

  test("rejects mixed connection scopes before updating", async () => {
    const local = createConnection("00000000-0000-4000-8000-000000000001");
    const cloud = createConnection("00000000-0000-4000-8000-000000000002", {
      originKind: "cloud",
      originScopeKey: "cloud-scope-a",
      originWorkspaceId: "workspace-a"
    });
    const { service, connectionMap } = createService([local, cloud]);

    await expect(
      service.batchUpdateConnectionAuth({
        target: { type: "connections", connectionIds: [local.id, cloud.id] },
        auth: { authType: "password", password: "shared-password" }
      })
    ).rejects.toThrow("同一来源范围");

    expect(connectionMap.get(local.id)?.updatedAt).toBe(now);
    expect(connectionMap.get(cloud.id)?.updatedAt).toBe(now);
  });

  test("rejects private keys from a different scope before updating", async () => {
    const connection = createConnection("00000000-0000-4000-8000-000000000001");
    const cloudKey = createSshKey("10000000-0000-4000-8000-000000000001", {
      originKind: "cloud",
      originScopeKey: "cloud-scope-a",
      originWorkspaceId: "workspace-a"
    });
    const { service, connectionMap } = createService([connection], [cloudKey]);

    await expect(
      service.batchUpdateConnectionAuth({
        target: { type: "connections", connectionIds: [connection.id] },
        auth: { authType: "privateKey", sshKeyId: cloudKey.id }
      })
    ).rejects.toThrow("不属于同一来源范围");

    expect(connectionMap.get(connection.id)?.authType).toBe("password");
  });
});
