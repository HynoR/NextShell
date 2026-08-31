import { describe, expect, test } from "vitest";
import {
  LOCAL_DEFAULT_SCOPE_KEY,
  type ConnectionFolder,
  type ConnectionProfile
} from "@nextshell/core";
import type {
  CachedConnectionRepository,
  CachedProxyRepository,
  CachedSshKeyRepository,
  ConnectionFolderRepository
} from "@nextshell/storage";
import type { EncryptedSecretVault } from "@nextshell/security";
import { ConnectionService } from "./connection-service";

/**
 * `folderId` 的三态语义(拖拽移动连接的地基):
 *   uuid → 移过去,`groupPath` 跟着重投影;
 *   null → 显式移回顶层;
 *   省略 → 别动目录(导入/快连/认证改写这些老调用方靠它)。
 * 只测这一条:少了 `null` 这一档,"拖回根目录"会被 `?? current.folderId` 静默吃掉。
 */

const NOW = "2026-06-15T00:00:00.000Z";
const CONNECTION_ID = "00000000-0000-4000-8000-000000000001";
const FOLDER_ID = "aaaaaaaa-0000-4000-8000-000000000001";
const CHILD_FOLDER_ID = "aaaaaaaa-0000-4000-8000-000000000002";

const folder = (id: string, name: string, parentId?: string): ConnectionFolder => ({
  id,
  scopeKey: LOCAL_DEFAULT_SCOPE_KEY,
  parentId,
  name,
  sortIndex: 0,
  createdAt: NOW,
  updatedAt: NOW
});

const createConnection = (patch: Partial<ConnectionProfile> = {}): ConnectionProfile => ({
  id: CONNECTION_ID,
  name: "prod-db",
  host: "10.0.0.1",
  port: 22,
  username: "root",
  authType: "password",
  credentialRef: `secret://conn-${CONNECTION_ID}`,
  strictHostKeyChecking: false,
  terminalEncoding: "utf-8",
  backspaceMode: "ascii-backspace",
  deleteMode: "vt220-delete",
  groupPath: "/server/prod",
  folderId: FOLDER_ID,
  tags: [],
  favorite: false,
  monitorSession: false,
  createdAt: NOW,
  updatedAt: NOW,
  originKind: "local",
  originScopeKey: LOCAL_DEFAULT_SCOPE_KEY,
  ...patch
});

const createService = (connection: ConnectionProfile, folders: ConnectionFolder[]) => {
  const connectionMap = new Map([[connection.id, connection]]);
  const secrets = new Map<string, string>();
  if (connection.credentialRef) {
    secrets.set(connection.credentialRef, "stored-password");
  }

  const service = new ConnectionService({
    connections: {
      list: () => Array.from(connectionMap.values()),
      getById: (id: string) => connectionMap.get(id),
      save: (next: ConnectionProfile) => {
        connectionMap.set(next.id, next);
        return next;
      }
    } as unknown as CachedConnectionRepository,
    connectionFolders: {
      list: () => folders
    } as unknown as ConnectionFolderRepository,
    sshKeyRepo: { getById: () => undefined } as unknown as CachedSshKeyRepository,
    proxyRepo: { getById: () => undefined } as unknown as CachedProxyRepository,
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
    remoteEditManager: {} as never,
    monitorStates: new Map(),
    appendAuditLogIfEnabled: () => undefined,
    sendSessionStatus: () => undefined
  });

  return { service, connectionMap, secrets };
};

/** 复刻渲染层 `profileToUpsertPayload` 的形状:全量字段、绝不带 password。 */
const movePayload = (connection: ConnectionProfile, folderId: string | null) => ({
  id: connection.id,
  name: connection.name,
  host: connection.host,
  port: connection.port,
  username: connection.username,
  authType: connection.authType,
  folderId,
  strictHostKeyChecking: connection.strictHostKeyChecking,
  terminalEncoding: connection.terminalEncoding,
  backspaceMode: connection.backspaceMode,
  deleteMode: connection.deleteMode,
  groupPath: connection.groupPath,
  tags: connection.tags,
  favorite: connection.favorite,
  monitorSession: connection.monitorSession
});

describe("upsertConnection — folderId 三态", () => {
  const folders = [folder(FOLDER_ID, "prod"), folder(CHILD_FOLDER_ID, "asia", FOLDER_ID)];

  test("给了 uuid:移到该目录，groupPath 跟着重投影", async () => {
    const connection = createConnection();
    const { service } = createService(connection, folders);

    const saved = await service.upsertConnection(movePayload(connection, CHILD_FOLDER_ID));

    expect(saved.folderId).toBe(CHILD_FOLDER_ID);
    expect(saved.groupPath).toBe("/server/prod/asia");
  });

  test("给了 null:移回顶层，groupPath 退回根", async () => {
    const connection = createConnection();
    const { service } = createService(connection, folders);

    const saved = await service.upsertConnection(movePayload(connection, null));

    expect(saved.folderId).toBeUndefined();
    expect(saved.groupPath).toBe("/server");
  });

  test("省略字段:目录纹丝不动(导入/快连/认证改写这些老调用方依赖它)", async () => {
    const connection = createConnection();
    const { service } = createService(connection, folders);
    const { folderId: _dropped, ...withoutFolder } = movePayload(connection, null);

    const saved = await service.upsertConnection(withoutFolder);

    expect(saved.folderId).toBe(FOLDER_ID);
  });

  test("不带 password 的移动不会动已保存的凭据", async () => {
    const connection = createConnection();
    const { service, secrets } = createService(connection, folders);

    const saved = await service.upsertConnection(movePayload(connection, CHILD_FOLDER_ID));

    expect(saved.credentialRef).toBe(`secret://conn-${CONNECTION_ID}`);
    expect(secrets.get(`secret://conn-${CONNECTION_ID}`)).toBe("stored-password");
  });

  test("目标目录不存在时拒绝保存，而不是静默落到一个查不到的目录", async () => {
    const connection = createConnection();
    const { service, connectionMap } = createService(connection, folders);

    await expect(
      service.upsertConnection(movePayload(connection, "aaaaaaaa-0000-4000-8000-00000000dead"))
    ).rejects.toThrow();
    expect(connectionMap.get(CONNECTION_ID)?.folderId).toBe(FOLDER_ID);
  });
});
