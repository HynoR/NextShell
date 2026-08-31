import { describe, expect, test } from "vitest";
import { LOCAL_DEFAULT_SCOPE_KEY, buildScopeKey } from "@nextshell/core";
import type {
  CloudSyncWorkspaceProfile,
  ConnectionFolder,
  ConnectionProfile
} from "@nextshell/core";
import type {
  CachedConnectionRepository,
  CachedProxyRepository,
  CachedSshKeyRepository,
  ConnectionFolderRepository
} from "@nextshell/storage";
import type { EncryptedSecretVault } from "@nextshell/security";
import { ResourceOperationsService } from "./resource-operations-service";
import type { CloudSyncManager } from "./cloud-sync-manager";

// ── 夹具 ────────────────────────────────────────────────────
// 内存假仓储：better-sqlite3 是 Electron ABI，测试里连不上真实库。

const WORKSPACE: CloudSyncWorkspaceProfile = {
  id: "ws-1",
  apiBaseUrl: "https://sync.example.com",
  workspaceName: "Team A",
  displayName: "Team A",
  pullIntervalSec: 60,
  ignoreTlsErrors: false,
  enabled: true,
  createdAt: "2026-08-31T00:00:00.000Z",
  updatedAt: "2026-08-31T00:00:00.000Z",
  lastSyncAt: null,
  lastError: null
} as CloudSyncWorkspaceProfile;

const CLOUD_SCOPE_KEY = buildScopeKey({
  kind: "cloud",
  apiBaseUrl: WORKSPACE.apiBaseUrl,
  workspaceName: WORKSPACE.workspaceName
});

const folder = (
  id: string,
  name: string,
  parentId?: string,
  scopeKey = LOCAL_DEFAULT_SCOPE_KEY
): ConnectionFolder => ({
  id,
  scopeKey,
  parentId,
  name,
  sortIndex: 0,
  createdAt: "2026-08-31T00:00:00.000Z",
  updatedAt: "2026-08-31T00:00:00.000Z"
});

const source: ConnectionProfile = {
  id: "src-1",
  name: "prod-a",
  host: "10.0.0.1",
  port: 22,
  username: "root",
  authType: "password",
  strictHostKeyChecking: false,
  groupPath: "/server/old",
  folderId: "local-old",
  tags: [],
  favorite: false,
  monitorSession: false,
  terminalEncoding: "utf-8",
  backspaceMode: "ascii-backspace",
  deleteMode: "vt220-delete",
  createdAt: "2026-08-31T00:00:00.000Z",
  updatedAt: "2026-08-31T00:00:00.000Z",
  originKind: "local",
  originScopeKey: LOCAL_DEFAULT_SCOPE_KEY
} as ConnectionProfile;

const createService = (folders: ConnectionFolder[]) => {
  const saved: ConnectionProfile[] = [];
  const folderRepo = {
    list: (scopeKey?: string) =>
      scopeKey ? folders.filter((item) => item.scopeKey === scopeKey) : [...folders],
    getById: (id: string) => folders.find((item) => item.id === id)
  } as unknown as ConnectionFolderRepository;

  const service = new ResourceOperationsService({
    connections: {
      getById: (id: string) => (id === source.id ? source : undefined),
      save: (profile: ConnectionProfile) => saved.push(profile)
    } as unknown as CachedConnectionRepository,
    sshKeyRepo: { list: () => [], getById: () => undefined } as unknown as CachedSshKeyRepository,
    proxyRepo: { list: () => [], getById: () => undefined } as unknown as CachedProxyRepository,
    connectionFolders: folderRepo,
    vault: {} as unknown as EncryptedSecretVault,
    cloudSyncManager: {
      listWorkspaces: () => [WORKSPACE],
      pushConnectionUpsert: () => undefined
    } as unknown as CloudSyncManager,
    saveRecycleBinEntry: () => undefined,
    listRecycleBinEntries: () => [],
    removeRecycleBinEntry: () => undefined,
    appendAuditLog: () => undefined
  });

  return { service, saved };
};

describe("ResourceOperationsService.copyConnection — target folder", () => {
  // C1 的核心：旧实现只传目录名，选中嵌套目录 a/b 时只带上 "b"，落点与所选不符。
  test("projects the full folder chain, not just the leaf name", async () => {
    const { service, saved } = createService([folder("f-a", "a"), folder("f-b", "b", "f-a")]);

    await service.copyConnection({
      sourceId: source.id,
      targetOriginKind: "local",
      targetFolderId: "f-b"
    });

    expect(saved[0]?.folderId).toBe("f-b");
    expect(saved[0]?.groupPath).toBe("/server/a/b");
  });

  test("projects a cloud target under its workspace slug", async () => {
    const { service, saved } = createService([
      folder("c-a", "a", undefined, CLOUD_SCOPE_KEY),
      folder("c-b", "b", "c-a", CLOUD_SCOPE_KEY)
    ]);

    await service.copyConnection({
      sourceId: source.id,
      targetOriginKind: "cloud",
      targetWorkspaceId: WORKSPACE.id,
      targetFolderId: "c-b"
    });

    expect(saved[0]?.folderId).toBe("c-b");
    expect(saved[0]?.groupPath).toBe("/workspace/team-a/a/b");
  });

  // 目录 id 按 scope 分区：拿本地目录去投影一条云连接会算出 /server/...，同步会把它
  // 看成换了分组。
  test("refuses a folder that does not belong to the target scope", async () => {
    const { service, saved } = createService([folder("f-a", "a")]);

    await expect(
      service.copyConnection({
        sourceId: source.id,
        targetOriginKind: "cloud",
        targetWorkspaceId: WORKSPACE.id,
        targetFolderId: "f-a"
      })
    ).rejects.toThrow("目标目录不存在或不属于目标作用域");
    expect(saved).toEqual([]);
  });

  test("refuses an unknown folder id", async () => {
    const { service, saved } = createService([folder("f-a", "a")]);

    await expect(
      service.copyConnection({
        sourceId: source.id,
        targetOriginKind: "local",
        targetFolderId: "missing"
      })
    ).rejects.toThrow("目标目录不存在或不属于目标作用域");
    expect(saved).toEqual([]);
  });

  // 源目录属于源作用域，原样带过去会指向目标域里不存在的 id。
  test("drops the source folderId when no target folder is given", async () => {
    const { service, saved } = createService([folder("f-a", "a")]);

    await service.copyConnection({
      sourceId: source.id,
      targetOriginKind: "local"
    });

    expect(saved[0]?.folderId).toBeUndefined();
    expect(saved[0]?.groupPath).toBe("/server");
  });

  // 云作用域的根是 `/workspace/<slug>`；拼出没有 slug 的 `/workspace` 会在树上造出一个
  // 名为 "workspace" 的幽灵根节点（folder-path.ts 明令禁止的形状）。
  test("projects a cloud target without a folder onto the workspace root", async () => {
    const { service, saved } = createService([]);

    await service.copyConnection({
      sourceId: source.id,
      targetOriginKind: "cloud",
      targetWorkspaceId: WORKSPACE.id
    });

    expect(saved[0]?.folderId).toBeUndefined();
    expect(saved[0]?.groupPath).toBe("/workspace/team-a");
  });

  test("keeps the legacy sub-path behaviour for callers that still send one", async () => {
    const { service, saved } = createService([]);

    await service.copyConnection({
      sourceId: source.id,
      targetOriginKind: "local",
      targetGroupSubPath: "legacy"
    });

    expect(saved[0]?.groupPath).toBe("/server/legacy");
    expect(saved[0]?.folderId).toBeUndefined();
  });
});
