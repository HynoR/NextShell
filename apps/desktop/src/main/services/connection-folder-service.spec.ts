import { describe, expect, test } from "vitest";
import { LOCAL_DEFAULT_SCOPE_KEY, buildScopeKey } from "@nextshell/core";
import type {
  CloudSyncWorkspaceProfile,
  ConnectionFolder,
  ConnectionProfile
} from "@nextshell/core";
import type { ConnectionFolderRepository } from "@nextshell/storage";
import {
  ConnectionFolderService,
  type FolderProjectionConnectionStore
} from "./connection-folder-service";

// ── 内存假仓储 ───────────────────────────────────────────────
// better-sqlite3 是 Electron ABI，测试里连不上真实库；这里复刻两条库语义：
//   1. 删目录时子目录级联删除、其中连接的 folder_id 置空（ON DELETE CASCADE / SET NULL）；
//   2. 连接仓储带缓存，① 里那次 folder_id 置空绕过缓存，只有 invalidate 之后才看得见。

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

const connection = (
  id: string,
  groupPath: string,
  folderId?: string,
  originScopeKey = LOCAL_DEFAULT_SCOPE_KEY
): ConnectionProfile =>
  ({
    id,
    name: id,
    host: `${id}.example.com`,
    port: 22,
    username: "root",
    authType: "password",
    strictHostKeyChecking: false,
    groupPath,
    folderId,
    tags: [],
    favorite: false,
    monitorSession: false,
    terminalEncoding: "utf-8",
    backspaceMode: "ascii-backspace",
    deleteMode: "vt220-delete",
    createdAt: "2026-08-31T00:00:00.000Z",
    updatedAt: "2026-08-31T00:00:00.000Z",
    originKind: originScopeKey === LOCAL_DEFAULT_SCOPE_KEY ? "local" : "cloud",
    originScopeKey
  }) as ConnectionProfile;

const createWorld = (folders: ConnectionFolder[], connections: ConnectionProfile[]) => {
  const folderRows = [...folders];
  const connectionRows = connections.map((row) => ({ ...row }));
  const updates: Array<{ id: string; groupPath: string }> = [];
  let cache: ConnectionProfile[] | undefined;

  const subtreeIds = (rootId: string): string[] => {
    const ids = [rootId];
    for (let index = 0; index < ids.length; index++) {
      const current = ids[index];
      for (const row of folderRows) {
        if (row.parentId === current) {
          ids.push(row.id);
        }
      }
    }
    return ids;
  };

  const folderRepo: ConnectionFolderRepository = {
    list: (scopeKey?: string) =>
      scopeKey ? folderRows.filter((row) => row.scopeKey === scopeKey) : [...folderRows],
    getById: (id: string) => folderRows.find((row) => row.id === id),
    create: () => {
      throw new Error("not used");
    },
    rename: (id: string, name: string) => {
      const row = folderRows.find((item) => item.id === id);
      if (!row) throw new Error("目录不存在");
      row.name = name;
      return { ...row };
    },
    move: (id: string, parentId: string | undefined) => {
      const row = folderRows.find((item) => item.id === id);
      if (!row) throw new Error("目录不存在");
      row.parentId = parentId;
      return { ...row };
    },
    reorder: (id: string, sortIndex: number) => {
      const row = folderRows.find((item) => item.id === id);
      if (!row) throw new Error("目录不存在");
      row.sortIndex = sortIndex;
      return { ...row };
    },
    remove: (id: string) => {
      const doomed = new Set(subtreeIds(id));
      for (let index = folderRows.length - 1; index >= 0; index--) {
        if (doomed.has(folderRows[index]!.id)) {
          folderRows.splice(index, 1);
        }
      }
      // 外键 ON DELETE SET NULL：库里直接改，连接缓存看不见这一步。
      for (const row of connectionRows) {
        if (row.folderId && doomed.has(row.folderId)) {
          row.folderId = undefined;
        }
      }
    },
    countConnections: () => 0
  };

  const connectionStore: FolderProjectionConnectionStore = {
    list: () => {
      cache ??= connectionRows.map((row) => ({ ...row }));
      return cache;
    },
    updateConnectionGroupPath: (id: string, groupPath: string) => {
      updates.push({ id, groupPath });
      const row = connectionRows.find((item) => item.id === id);
      if (row) row.groupPath = groupPath;
      const cached = cache?.find((item) => item.id === id);
      if (cached) cached.groupPath = groupPath;
    },
    invalidateConnections: () => {
      cache = undefined;
    }
  };

  const service = new ConnectionFolderService({
    folders: folderRepo,
    connections: connectionStore,
    listCloudWorkspaces: () => [WORKSPACE]
  });

  const groupPathOf = (id: string) => connectionRows.find((row) => row.id === id)?.groupPath;
  const folderIdOf = (id: string) => connectionRows.find((row) => row.id === id)?.folderId;
  /** 服务的读口径(带缓存那一层),用来验证级联清空后缓存确实重读过。 */
  const visible = (id: string) => connectionStore.list({}).find((row) => row.id === id);

  return { service, updates, groupPathOf, folderIdOf, visible, folderRows, connectionRows };
};

describe("ConnectionFolderService.rename", () => {
  // groupPath 是云同步线协议 / MCP schema / 导出文件共同读的投影：改完目录名不重投影，
  // 导出文件继续写旧路径，云快照把旧名推给整个 workspace 的其他设备。
  test("re-projects every connection in the renamed subtree", () => {
    const world = createWorld(
      [folder("f-prod", "prod"), folder("f-asia", "asia", "f-prod")],
      [
        connection("c-top", "/server/prod", "f-prod"),
        connection("c-leaf", "/server/prod/asia", "f-asia"),
        connection("c-root", "/server")
      ]
    );

    world.service.rename("f-prod", "生产");

    expect(world.groupPathOf("c-top")).toBe("/server/生产");
    expect(world.groupPathOf("c-leaf")).toBe("/server/生产/asia");
    // 值没变的不写。
    expect(world.updates.map((update) => update.id)).toEqual(["c-top", "c-leaf"]);
  });

  test("projects a cloud scope under its workspace slug and leaves other scopes alone", () => {
    const world = createWorld(
      [folder("c-prod", "prod", undefined, CLOUD_SCOPE_KEY), folder("f-prod", "prod")],
      [
        connection("cloud-1", "/workspace/team-a/prod", "c-prod", CLOUD_SCOPE_KEY),
        connection("local-1", "/server/prod", "f-prod")
      ]
    );

    world.service.rename("c-prod", "生产");

    expect(world.groupPathOf("cloud-1")).toBe("/workspace/team-a/生产");
    expect(world.groupPathOf("local-1")).toBe("/server/prod");
    expect(world.updates).toHaveLength(1);
  });
});

describe("ConnectionFolderService.move", () => {
  test("re-projects the moved subtree under its new parent", () => {
    const world = createWorld(
      [folder("f-a", "a"), folder("f-b", "b"), folder("f-leaf", "leaf", "f-a")],
      [connection("c-leaf", "/server/a/leaf", "f-leaf")]
    );

    world.service.move("f-leaf", "f-b");

    expect(world.groupPathOf("c-leaf")).toBe("/server/b/leaf");
  });

  test("re-projects to the scope root when the folder moves to the top level", () => {
    const world = createWorld(
      [folder("f-a", "a"), folder("f-leaf", "leaf", "f-a")],
      [connection("c-leaf", "/server/a/leaf", "f-leaf")]
    );

    world.service.move("f-leaf", undefined);

    expect(world.groupPathOf("c-leaf")).toBe("/server/leaf");
  });
});

describe("ConnectionFolderService.remove", () => {
  // 目录删除只解绑连接（连接本身不删）：解绑后的连接必须投影回作用域根，
  // 否则 groupPath 里还留着一个已经不存在的目录名。
  test("projects detached connections back to the scope root", () => {
    const world = createWorld(
      [folder("f-prod", "prod"), folder("f-asia", "asia", "f-prod")],
      [
        connection("c-top", "/server/prod", "f-prod"),
        connection("c-leaf", "/server/prod/asia", "f-asia"),
        connection("c-other", "/server")
      ]
    );

    // 先读一遍把连接缓存热起来——真实会话里目录删除之前必然已经列过连接。
    expect(world.visible("c-top")?.folderId).toBe("f-prod");

    world.service.remove("f-prod");

    expect(world.folderIdOf("c-top")).toBeUndefined();
    expect(world.folderIdOf("c-leaf")).toBeUndefined();
    expect(world.groupPathOf("c-top")).toBe("/server");
    expect(world.groupPathOf("c-leaf")).toBe("/server");
    expect(world.updates.map((update) => update.id)).toEqual(["c-top", "c-leaf"]);
    // 级联把 folder_id 置空是绕过连接缓存做的:不让缓存重读,树上这两条还挂在已删除的目录下。
    expect(world.visible("c-top")?.folderId).toBeUndefined();
    expect(world.visible("c-leaf")?.folderId).toBeUndefined();
  });

  test("does nothing when the folder is already gone", () => {
    const world = createWorld([], [connection("c-root", "/server")]);

    world.service.remove("missing");

    expect(world.updates).toEqual([]);
  });
});

describe("ConnectionFolderService.reorder", () => {
  // 排序不进 groupPath（投影只由目录名链决定），不该产生任何写。
  test("never re-projects", () => {
    const world = createWorld(
      [folder("f-prod", "prod")],
      [connection("c-top", "/server/prod", "f-prod")]
    );

    world.service.reorder("f-prod", 3);

    expect(world.updates).toEqual([]);
    expect(world.folderRows[0]?.sortIndex).toBe(3);
  });
});
