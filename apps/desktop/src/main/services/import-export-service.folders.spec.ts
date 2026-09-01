import { beforeEach, describe, expect, test, vi } from "vitest";
import { LOCAL_DEFAULT_SCOPE_KEY } from "@nextshell/core";
import type { ConnectionFolder, ConnectionProfile } from "@nextshell/core";
import type { ConnectionUpsertInput } from "@nextshell/shared";
import type {
  CachedConnectionRepository,
  CachedSshKeyRepository,
  ConnectionFolderRepository
} from "@nextshell/storage";
import type { EncryptedSecretVault } from "@nextshell/security";

// 服务模块在顶层 import electron（导出对话框用），单测里只走导入执行链路，给个空壳即可。
vi.mock("electron", () => ({
  BrowserWindow: { fromWebContents: () => null },
  dialog: { showSaveDialog: async () => ({ canceled: true }) }
}));

const { ImportExportService } = await import("./import-export-service");

// ── 内存假仓储 ───────────────────────────────────────────────
// better-sqlite3 是 Electron ABI，测试里连不上真实库；这里只需要目录表的语义。

interface FakeFolders extends ConnectionFolderRepository {
  rows: ConnectionFolder[];
  createCalls: Array<{ name: string; parentId?: string; scopeKey: string }>;
}

const createFolderRepo = (seed: ConnectionFolder[] = []): FakeFolders => {
  const rows = [...seed];
  const createCalls: FakeFolders["createCalls"] = [];
  let counter = rows.length;
  const repo: FakeFolders = {
    rows,
    createCalls,
    list: (scopeKey?: string) =>
      scopeKey ? rows.filter((row) => row.scopeKey === scopeKey) : [...rows],
    getById: (id: string) => rows.find((row) => row.id === id),
    create: ({ scopeKey, name, parentId }) => {
      // 库里 (scope_key, parent_id, name) 上有唯一索引；假实现也要拦，否则重复创建的
      // 回归会在测试里悄悄通过。
      if (
        rows.some(
          (row) =>
            row.scopeKey === scopeKey &&
            (row.parentId ?? undefined) === parentId &&
            row.name === name
        )
      ) {
        throw new Error(`duplicate folder: ${name}`);
      }
      createCalls.push({ name, parentId, scopeKey });
      counter += 1;
      const folder: ConnectionFolder = {
        id: `folder-${counter}`,
        scopeKey,
        parentId,
        name,
        sortIndex: 0,
        createdAt: "2026-08-31T00:00:00.000Z",
        updatedAt: "2026-08-31T00:00:00.000Z"
      };
      rows.push(folder);
      return folder;
    },
    rename: () => {
      throw new Error("not used");
    },
    move: () => {
      throw new Error("not used");
    },
    reorder: () => {
      throw new Error("not used");
    },
    remove: () => undefined,
    countConnections: () => 0
  };
  return repo;
};

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

const entry = (patch: Partial<{ name: string; host: string; groupPath: string }> = {}) => ({
  name: patch.name ?? "prod-a",
  host: patch.host ?? "10.0.0.1",
  port: 22,
  username: "root",
  authType: "password" as const,
  groupPath: patch.groupPath ?? "/server/prod/asia",
  tags: [] as string[],
  favorite: false,
  terminalEncoding: "utf-8" as const,
  backspaceMode: "ascii-backspace" as const,
  deleteMode: "vt220-delete" as const,
  monitorSession: false
});

const createService = (folders: ConnectionFolderRepository, existing: ConnectionProfile[] = []) => {
  const upserts: ConnectionUpsertInput[] = [];
  const service = new ImportExportService({
    connections: { list: () => existing } as unknown as CachedConnectionRepository,
    sshKeyRepo: { list: () => [] } as unknown as CachedSshKeyRepository,
    connectionFolders: folders,
    vault: {} as unknown as EncryptedSecretVault,
    upsertConnection: async (input) => {
      upserts.push(input);
      return { id: "saved" } as unknown as ConnectionProfile;
    }
  });
  return { service, upserts };
};

describe("ImportExportService.importConnectionsExecute — folder materialization", () => {
  let folders: FakeFolders;

  beforeEach(() => {
    folders = createFolderRepo();
  });

  // V2 的树完全按 folderId 摆放：不物化目录，导入进来的连接会全部平铺到根。
  test("materializes the whole groupPath chain and binds the leaf folderId", async () => {
    const { service, upserts } = createService(folders);

    const result = await service.importConnectionsExecute({
      entries: [entry()],
      conflictPolicy: "skip"
    });

    expect(result.created).toBe(1);
    expect(folders.createCalls.map((call) => call.name)).toEqual(["prod", "asia"]);
    expect(folders.rows[1]?.parentId).toBe(folders.rows[0]?.id);
    expect(upserts[0]?.folderId).toBe(folders.rows[1]?.id);
  });

  test("hangs the materialized chain under targetFolderId", async () => {
    folders = createFolderRepo([folder("root-1", "导入")]);
    const { service, upserts } = createService(folders);

    await service.importConnectionsExecute({
      entries: [entry({ groupPath: "/server/prod" })],
      conflictPolicy: "skip",
      targetFolderId: "root-1"
    });

    const created = folders.rows.find((row) => row.name === "prod");
    expect(created?.parentId).toBe("root-1");
    expect(upserts[0]?.folderId).toBe(created?.id);
  });

  test("puts the connections straight into targetFolderId when the path carries no folders", async () => {
    folders = createFolderRepo([folder("root-1", "导入")]);
    const { service, upserts } = createService(folders);

    await service.importConnectionsExecute({
      entries: [entry({ groupPath: "/server" })],
      conflictPolicy: "skip",
      targetFolderId: "root-1"
    });

    expect(folders.createCalls).toEqual([]);
    expect(upserts[0]?.folderId).toBe("root-1");
  });

  // 同一批里的两条 entry 共享前缀，第二条必须看见第一条刚建出来的目录。
  test("reuses folders across entries in the same batch and across repeated imports", async () => {
    const first = createService(folders);
    await first.service.importConnectionsExecute({
      entries: [entry({ host: "10.0.0.1" }), entry({ host: "10.0.0.2" })],
      conflictPolicy: "skip"
    });
    expect(folders.createCalls.map((call) => call.name)).toEqual(["prod", "asia"]);

    const second = createService(folders);
    await second.service.importConnectionsExecute({
      entries: [entry({ host: "10.0.0.3" })],
      conflictPolicy: "skip"
    });

    expect(folders.createCalls.map((call) => call.name)).toEqual(["prod", "asia"]);
    expect(second.upserts[0]?.folderId).toBe(folders.rows[1]?.id);
  });

  test("merges into an existing folder tree instead of duplicating it", async () => {
    folders = createFolderRepo([folder("f-prod", "prod"), folder("f-asia", "asia", "f-prod")]);
    const { service, upserts } = createService(folders);

    await service.importConnectionsExecute({
      entries: [entry()],
      conflictPolicy: "skip"
    });

    expect(folders.createCalls).toEqual([]);
    expect(upserts[0]?.folderId).toBe("f-asia");
  });

  // 一份云导出文件被导进本地：/workspace/<slug> 是线格式前缀，不能物化成一层目录，
  // 也不能原样写进本地库的 groupPath。
  test("strips the workspace wire prefix and never lets it reach the local groupPath", async () => {
    const { service, upserts } = createService(folders);

    await service.importConnectionsExecute({
      entries: [entry({ groupPath: "/workspace/team-a" })],
      conflictPolicy: "skip"
    });

    expect(folders.createCalls).toEqual([]);
    // 显式 null 而不是省略:省略在 upsert 里是"目录别动",与同一次写入把 groupPath 压回
    // 根的意图相反,overwrite 一条已在目录里的连接时两边会分叉。
    expect(upserts[0]?.folderId).toBeNull();
    expect(upserts[0]?.groupPath).toBe("/server");
  });

  test("rejects the whole batch when targetFolderId does not exist", async () => {
    const { service, upserts } = createService(folders);

    await expect(
      service.importConnectionsExecute({
        entries: [entry()],
        conflictPolicy: "skip",
        targetFolderId: "missing"
      })
    ).rejects.toThrow("目标目录不存在或不属于本地作用域");
    expect(upserts).toEqual([]);
  });

  test("rejects a target folder that belongs to a cloud scope", async () => {
    folders = createFolderRepo([folder("cloud-1", "prod", undefined, "sync.example.com-team-a")]);
    const { service, upserts } = createService(folders);

    await expect(
      service.importConnectionsExecute({
        entries: [entry()],
        conflictPolicy: "skip",
        targetFolderId: "cloud-1"
      })
    ).rejects.toThrow("目标目录不存在或不属于本地作用域");
    expect(upserts).toEqual([]);
  });
});

const localConnection = (patch: Partial<ConnectionProfile> = {}): ConnectionProfile =>
  ({
    id: "existing-local",
    name: "prod-a",
    host: "10.0.0.1",
    port: 22,
    username: "root",
    authType: "password",
    strictHostKeyChecking: false,
    groupPath: "/server/prod",
    folderId: "f-prod",
    tags: [],
    favorite: false,
    monitorSession: false,
    terminalEncoding: "utf-8",
    backspaceMode: "ascii-backspace",
    deleteMode: "vt220-delete",
    createdAt: "2026-08-31T00:00:00.000Z",
    updatedAt: "2026-08-31T00:00:00.000Z",
    originKind: "local",
    originScopeKey: LOCAL_DEFAULT_SCOPE_KEY,
    ...patch
  }) as ConnectionProfile;

describe("ImportExportService.importConnectionsExecute — conflict handling", () => {
  // overwrite 到根:folderId 必须显式落 null,不能省略。省略 = "目录别动"，连接会留在
  // 旧目录里，而 groupPath 已经被写成 /server —— folderId 与 groupPath 分叉。
  test("moves an overwritten connection out of its old folder when the path carries none", async () => {
    const folders = createFolderRepo([folder("f-prod", "prod")]);
    const { service, upserts } = createService(folders, [localConnection()]);

    const result = await service.importConnectionsExecute({
      entries: [entry({ groupPath: "/server" })],
      conflictPolicy: "overwrite"
    });

    expect(result.overwritten).toBe(1);
    expect(upserts[0]?.id).toBe("existing-local");
    expect(upserts[0]?.folderId).toBeNull();
    expect(upserts[0]?.groupPath).toBe("/server");
  });

  // 导入只写本地作用域:一条同 host 的云连接不是本地的冲突对象。以前跨作用域匹配到它之后，
  // overwrite 会拿本地目录去改写云资源，投影链路直接抛"目标目录不存在"，整条 entry 失败。
  test("ignores a cloud-scope connection with the same host and creates a local one", async () => {
    const folders = createFolderRepo();
    const cloudTwin = localConnection({
      id: "existing-cloud",
      groupPath: "/workspace/team-a/prod",
      folderId: "cloud-folder",
      originKind: "cloud",
      originScopeKey: "sync.example.com-team-a",
      originWorkspaceId: "ws-1"
    } as Partial<ConnectionProfile>);
    const { service, upserts } = createService(folders, [cloudTwin]);

    const result = await service.importConnectionsExecute({
      entries: [entry({ groupPath: "/server/prod" })],
      conflictPolicy: "overwrite"
    });

    expect(result.failed).toBe(0);
    expect(result.overwritten).toBe(0);
    expect(result.created).toBe(1);
    expect(upserts[0]?.id).toBeUndefined();
    expect(upserts[0]?.folderId).toBe(folders.rows[0]?.id);
  });

  // 目录物化排在冲突判定之后:整批 skip 的导入不该在树上留下任何空目录。
  test("materializes no folder at all when every entry is skipped", async () => {
    const folders = createFolderRepo();
    const { service, upserts } = createService(folders, [localConnection()]);

    const result = await service.importConnectionsExecute({
      entries: [entry({ groupPath: "/server/prod/asia" })],
      conflictPolicy: "skip"
    });

    expect(result.skipped).toBe(1);
    expect(folders.createCalls).toEqual([]);
    expect(folders.rows).toEqual([]);
    expect(upserts).toEqual([]);
  });
});

describe("ImportExportService.importConnectionsExecute — groupPathFormat", () => {
  // 目录扫描导入的 groupPath 是磁盘相对路径:用户顶层目录就叫 server 时，按线格式剥首段
  // 会把整整一层吞掉。
  test("keeps every segment of a literal path", async () => {
    const folders = createFolderRepo();
    const { service, upserts } = createService(folders);

    await service.importConnectionsExecute({
      entries: [entry({ groupPath: "/server/prod" })],
      conflictPolicy: "skip",
      groupPathFormat: "literal"
    });

    expect(folders.createCalls.map((call) => call.name)).toEqual(["server", "prod"]);
    expect(upserts[0]?.folderId).toBe(folders.rows[1]?.id);
  });

  test("strips the wire prefix when the entries come from an export file", async () => {
    const folders = createFolderRepo();
    const { service } = createService(folders);

    await service.importConnectionsExecute({
      entries: [entry({ groupPath: "/server/prod" })],
      conflictPolicy: "skip",
      groupPathFormat: "wire"
    });

    expect(folders.createCalls.map((call) => call.name)).toEqual(["prod"]);
  });
});

describe("ImportExportService.importConnectionsExecute — stale folder cache", () => {
  /** create 撞唯一索引(别处刚建了同名目录),重读之后才看得见那一条。 */
  const createRacingFolderRepo = (appearsOnRefresh: boolean) => {
    const rows: ConnectionFolder[] = [];
    let attempts = 0;
    const repo = {
      list: (scopeKey?: string) =>
        scopeKey ? rows.filter((row) => row.scopeKey === scopeKey) : [...rows],
      getById: (id: string) => rows.find((row) => row.id === id),
      create: ({
        name,
        parentId,
        scopeKey
      }: {
        name: string;
        parentId?: string;
        scopeKey: string;
      }) => {
        attempts += 1;
        if (appearsOnRefresh) {
          rows.push({ ...folder("f-race", name, parentId, scopeKey) });
        }
        throw new Error("UNIQUE constraint failed: idx_connection_folders_sibling_name");
      },
      rename: () => {
        throw new Error("not used");
      },
      move: () => {
        throw new Error("not used");
      },
      reorder: () => {
        throw new Error("not used");
      },
      remove: () => undefined,
      countConnections: () => 0
    } as unknown as ConnectionFolderRepository;
    return { repo, rows, attempts: () => attempts };
  };

  test("reuses the folder that appeared behind the cache", async () => {
    const racing = createRacingFolderRepo(true);
    const { service, upserts } = createService(racing.repo);

    const result = await service.importConnectionsExecute({
      entries: [entry({ groupPath: "/server/prod" })],
      conflictPolicy: "skip"
    });

    expect(result.created).toBe(1);
    expect(result.errors).toEqual([]);
    expect(racing.attempts()).toBe(1);
    expect(upserts[0]?.folderId).toBe("f-race");
  });

  test("reports a readable error when the conflict does not resolve", async () => {
    const racing = createRacingFolderRepo(false);
    const { service, upserts } = createService(racing.repo);

    const result = await service.importConnectionsExecute({
      entries: [entry({ groupPath: "/server/prod" })],
      conflictPolicy: "skip"
    });

    expect(result.created).toBe(0);
    expect(result.failed).toBe(1);
    expect(result.errors[0]).toContain("目录「prod」创建冲突，请重试导入");
    expect(upserts).toEqual([]);
  });
});
