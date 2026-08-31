import { describe, expect, test } from "vitest";
import type { ConnectionFolder, ConnectionProfile } from "@nextshell/core";
import {
  buildFolderPathLabels,
  buildFolderTree,
  isSelfOrAncestor,
  listSiblingFolders,
  planSiblingReorder
} from "./folderTree";

const folder = (
  id: string,
  name: string,
  parentId?: string,
  sortIndex = 0
): ConnectionFolder => ({
  id,
  scopeKey: "local-default",
  parentId,
  name,
  sortIndex,
  createdAt: "2026-01-01T00:00:00.000Z",
  updatedAt: "2026-01-01T00:00:00.000Z"
});

const connection = (id: string, folderId?: string): ConnectionProfile =>
  ({
    id,
    name: id,
    host: "10.0.0.1",
    port: 22,
    username: "root",
    authType: "password",
    strictHostKeyChecking: false,
    groupPath: "/server",
    folderId,
    tags: [],
    favorite: false,
    monitorSession: false,
    terminalEncoding: "utf-8",
    backspaceMode: "ascii-backspace",
    deleteMode: "vt220-delete",
    createdAt: "2026-01-01T00:00:00.000Z",
    updatedAt: "2026-01-01T00:00:00.000Z"
  }) as ConnectionProfile;

//        prod ── asia ── db
//          └── edge
const folders = [
  folder("prod", "prod"),
  folder("asia", "asia", "prod"),
  folder("db", "db", "asia"),
  folder("edge", "edge", "prod"),
  folder("staging", "staging")
];

const connections = [
  connection("c-db-1", "db"),
  connection("c-db-2", "db"),
  connection("c-asia", "asia"),
  connection("c-edge", "edge"),
  connection("c-root")
];

describe("buildFolderTree", () => {
  test("builds the hierarchy with subtree counts", () => {
    const roots = buildFolderTree(folders, connections);
    expect(roots.map((node) => node.folder.name)).toEqual(["prod", "staging"]);
    const prod = roots[0];
    expect(prod?.count).toBe(4);
    expect(prod?.children.map((node) => node.folder.name)).toEqual(["asia", "edge"]);
    expect(prod?.children[0]?.count).toBe(3);
    expect(prod?.children[0]?.children[0]?.count).toBe(2);
    expect(roots[1]?.count).toBe(0);
  });

  test("sorts siblings by sortIndex then name", () => {
    const ordered = [
      folder("b", "b", undefined, 1),
      folder("a", "a", undefined, 2),
      folder("c", "c", undefined, 1)
    ];
    expect(buildFolderTree(ordered, []).map((node) => node.folder.name)).toEqual(["b", "c", "a"]);
  });

  test("treats a dangling parentId as top-level", () => {
    const dangling = [folder("orphan", "orphan", "gone")];
    expect(buildFolderTree(dangling, []).map((node) => node.folder.name)).toEqual(["orphan"]);
  });

  test("drops a cyclic cluster instead of recursing forever", () => {
    const cyclic = [folder("x", "x", "y"), folder("y", "y", "x"), folder("ok", "ok")];
    expect(buildFolderTree(cyclic, []).map((node) => node.folder.name)).toEqual(["ok"]);
  });
});

describe("buildFolderPathLabels", () => {
  test("labels each folder with its full path", () => {
    const labels = buildFolderPathLabels(folders);
    expect(labels.get("db")).toBe("prod / asia / db");
    expect(labels.get("staging")).toBe("staging");
  });

  test("does not hang on a cycle", () => {
    const cyclic = [folder("x", "x", "y"), folder("y", "y", "x")];
    const labels = buildFolderPathLabels(cyclic);
    expect(labels.get("x")).toContain("x");
  });
});

describe("listSiblingFolders", () => {
  test("按树上的显示顺序列出同级", () => {
    expect(listSiblingFolders(undefined, folders).map((f) => f.name)).toEqual(["prod", "staging"]);
    expect(listSiblingFolders("prod", folders).map((f) => f.name)).toEqual(["asia", "edge"]);
  });

  test("parentId 悬空的目录算顶层", () => {
    const dangling = [folder("orphan", "orphan", "gone")];
    expect(listSiblingFolders(undefined, dangling).map((f) => f.name)).toEqual(["orphan"]);
  });
});

describe("planSiblingReorder", () => {
  // 同级三个，sortIndex 全是 0(create 的默认值)——正是线上数据的真实样子。
  const level = [folder("a", "a"), folder("b", "b"), folder("c", "c")];

  // 三个的 sortIndex 都已经是 0，所以"新下标恰好也是 0"的那一个会被过滤掉，
  // 剩下的两条足以把整层排成 c / a / b。
  test("拖到某个节点之前", () => {
    expect(planSiblingReorder({ folders: level, dragId: "c", dropId: "a", placeAfter: false })).toEqual([
      { id: "a", sortIndex: 1 },
      { id: "b", sortIndex: 2 }
    ]);
  });

  test("拖到某个节点之后", () => {
    expect(planSiblingReorder({ folders: level, dragId: "a", dropId: "b", placeAfter: true })).toEqual([
      { id: "a", sortIndex: 1 },
      { id: "c", sortIndex: 2 }
    ]);
  });

  test("整层重编号，但只返回真正变了的项", () => {
    // b 已经在 1 号位，重编号后仍是 1，不该为它白发一次 IPC。
    const steps = planSiblingReorder({
      folders: [folder("a", "a", undefined, 0), folder("b", "b", undefined, 1), folder("c", "c", undefined, 2)],
      dragId: "a",
      dropId: "c",
      placeAfter: false
    });
    expect(steps).toEqual([
      { id: "b", sortIndex: 0 },
      { id: "a", sortIndex: 1 }
    ]);
  });

  test("跨层拖到别人的缝隙里:按目标层重编号，被拖的那个也在计划里", () => {
    // 目标层是 prod 的孩子 [asia, edge]，staging 插到 asia 之后 → asia / staging / edge。
    const steps = planSiblingReorder({ folders, dragId: "staging", dropId: "asia", placeAfter: true });
    expect(steps).toEqual([
      { id: "staging", sortIndex: 1 },
      { id: "edge", sortIndex: 2 }
    ]);
  });

  test("拖到自己身上、或落点目录不存在时给空计划", () => {
    expect(planSiblingReorder({ folders: level, dragId: "a", dropId: "a", placeAfter: true })).toEqual([]);
    expect(planSiblingReorder({ folders: level, dragId: "a", dropId: "gone", placeAfter: true })).toEqual(
      []
    );
    expect(planSiblingReorder({ folders: level, dragId: "gone", dropId: "a", placeAfter: true })).toEqual(
      []
    );
  });

  test("重编号的结果与 buildFolderTree 的显示顺序一致", () => {
    const steps = planSiblingReorder({ folders: level, dragId: "c", dropId: "a", placeAfter: false });
    const applied = level.map((f) => {
      const step = steps.find((candidate) => candidate.id === f.id);
      return step ? { ...f, sortIndex: step.sortIndex } : f;
    });
    expect(buildFolderTree(applied, []).map((node) => node.folder.name)).toEqual(["c", "a", "b"]);
  });
});

describe("isSelfOrAncestor", () => {
  test("blocks moving a folder into its own subtree", () => {
    expect(isSelfOrAncestor("prod", "db", folders)).toBe(true);
    expect(isSelfOrAncestor("asia", "asia", folders)).toBe(true);
  });

  test("allows moves to unrelated folders and to the root", () => {
    expect(isSelfOrAncestor("asia", "staging", folders)).toBe(false);
    expect(isSelfOrAncestor("asia", undefined, folders)).toBe(false);
  });
});
