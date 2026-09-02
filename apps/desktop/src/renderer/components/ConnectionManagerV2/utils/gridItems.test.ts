import { describe, expect, test } from "vitest";
import type { ConnectionFolder, ConnectionProfile } from "@nextshell/core";
import { buildConnectionRow, type ConnectionRow } from "./connectionRows";
import {
  buildGridSections,
  collectGridConnectionIds,
  gridSortValue,
  resolveGridSort,
  type GridItem,
  type GridSection
} from "./gridItems";

const folder = (id: string, name: string, parentId?: string, sortIndex = 0): ConnectionFolder => ({
  id,
  scopeKey: "local-default",
  parentId,
  name,
  sortIndex,
  createdAt: "2026-01-01T00:00:00.000Z",
  updatedAt: "2026-01-01T00:00:00.000Z"
});

const conn = (patch: Partial<ConnectionProfile> & { id: string }): ConnectionProfile =>
  ({
    name: patch.id,
    host: "10.0.0.1",
    port: 22,
    username: "root",
    authType: "password",
    strictHostKeyChecking: false,
    groupPath: "/server",
    tags: [],
    favorite: false,
    monitorSession: false,
    terminalEncoding: "utf-8",
    backspaceMode: "ascii-backspace",
    deleteMode: "vt220-delete",
    createdAt: "2026-01-01T00:00:00.000Z",
    updatedAt: "2026-01-01T00:00:00.000Z",
    ...patch
  }) as ConnectionProfile;

const rowsOf = (connections: ConnectionProfile[]): ConnectionRow[] =>
  connections.map((connection) => buildConnectionRow(connection, []));

const nameOf = (item: GridItem): string =>
  item.kind === "folder" ? item.folder.name : item.row.connection.name;

const namesOf = (section: GridSection | undefined): string[] => (section?.items ?? []).map(nameOf);

const ASC = { key: "name", direction: "asc" } as const;

//   prod ── asia ── db
//     └── edge
//   staging(空目录)
const folders = [
  folder("prod", "prod"),
  folder("asia", "asia", "prod"),
  folder("db", "db", "asia"),
  folder("edge", "edge", "prod"),
  folder("staging", "staging", undefined, 1)
];

describe("buildGridSections — 搜索", () => {
  test("搜索中平铺成单段,无视目录层级", () => {
    const rows = rowsOf([
      conn({ id: "c1", name: "beta", folderId: "db" }),
      conn({ id: "c2", name: "alpha" }),
      conn({ id: "c3", name: "gamma", folderId: "prod" })
    ]);
    const sections = buildGridSections({
      rows,
      folders,
      currentFolderId: undefined,
      searching: true,
      mode: "browse",
      sort: ASC
    });
    expect(sections).toHaveLength(1);
    expect(sections[0]?.key).toBe("search");
    // 排序生效,且没有任何目录磁贴混进来。
    expect(namesOf(sections[0])).toEqual(["alpha", "beta", "gamma"]);
    expect(sections[0]?.items.every((item) => item.kind === "connection")).toBe(true);
  });

  test("搜索无命中时不产出空段", () => {
    expect(
      buildGridSections({ rows: [], folders, searching: true, mode: "browse", sort: ASC })
    ).toEqual([]);
  });

  test("搜索中不追加「最近连接」段——那是根目录浏览态才有的东西", () => {
    const rows = rowsOf([conn({ id: "c1", name: "alpha" })]);
    const sections = buildGridSections({
      rows,
      folders,
      searching: true,
      mode: "browse",
      sort: ASC
    });
    expect(sections.map((section) => section.key)).toEqual(["search"]);
  });
});

describe("buildGridSections — 根目录三段结构", () => {
  const rows = rowsOf([
    conn({ id: "root-b", name: "root-b", lastConnectedAt: "2026-02-01T00:00:00.000Z" }),
    conn({ id: "root-a", name: "root-a" }),
    conn({ id: "deep", name: "deep", folderId: "db", lastConnectedAt: "2026-03-01T00:00:00.000Z" }),
    conn({ id: "mid", name: "mid", folderId: "prod", lastConnectedAt: "2026-01-01T00:00:00.000Z" })
  ]);

  test("顶层目录磁贴在前、根直属连接在后,同属第一段且无标题", () => {
    const sections = buildGridSections({
      rows,
      folders,
      currentFolderId: undefined,
      searching: false,
      mode: "browse",
      sort: ASC
    });
    expect(sections[0]?.key).toBe("browse");
    expect(sections[0]?.title).toBeUndefined();
    expect(namesOf(sections[0])).toEqual(["prod", "staging", "root-a", "root-b"]);
  });

  test("浏览态不再追加「最近连接」段——那是独立视图,重复出现只是噪音", () => {
    const sections = buildGridSections({
      rows,
      folders,
      currentFolderId: undefined,
      searching: false,
      mode: "browse",
      sort: ASC
    });
    expect(sections.map((section) => section.key)).toEqual(["browse"]);
  });

  test("「最近连接」视图:单段、按最近序、无目录磁贴", () => {
    const sections = buildGridSections({
      rows,
      folders,
      currentFolderId: undefined,
      searching: false,
      mode: "recent",
      sort: ASC
    });
    expect(sections).toHaveLength(1);
    expect(sections[0]?.key).toBe("recent");
    // 连过的按最近序在前(deep > root-b > mid),没连过的按创建补位。
    expect(namesOf(sections[0])).toEqual(["deep", "root-b", "mid", "root-a"]);
  });

  test("「最近连接」视图的磁贴数与连接总量脱钩(≤recentLimit)", () => {
    const many = rowsOf(
      Array.from({ length: 400 }, (_, index) =>
        conn({
          id: `c${index}`,
          name: `c${index}`,
          folderId: "db",
          lastConnectedAt: `2026-01-01T00:00:${String(index % 60).padStart(2, "0")}.000Z`
        })
      )
    );
    const sections = buildGridSections({
      rows: many,
      folders,
      currentFolderId: undefined,
      searching: false,
      mode: "recent",
      sort: ASC,
      recentLimit: 30
    });
    const total = sections.reduce((sum, section) => sum + section.items.length, 0);
    expect(total).toBe(30);
  });

  test("空作用域:两种视图都不产出任何段", () => {
    expect(
      buildGridSections({ rows: [], folders: [], searching: false, mode: "browse", sort: ASC })
    ).toEqual([]);
    expect(
      buildGridSections({ rows: [], folders: [], searching: false, mode: "recent", sort: ASC })
    ).toEqual([]);
  });
});

describe("buildGridSections — 目录层", () => {
  const rows = rowsOf([
    conn({ id: "c1", name: "in-prod", folderId: "prod" }),
    conn({ id: "c2", name: "in-asia", folderId: "asia" }),
    conn({ id: "c3", name: "in-db", folderId: "db" }),
    conn({ id: "c4", name: "at-root" })
  ]);

  test("只给出当前层的子目录与直属连接", () => {
    const sections = buildGridSections({
      rows,
      folders,
      currentFolderId: "prod",
      searching: false,
      mode: "browse",
      sort: ASC
    });
    expect(sections).toHaveLength(1);
    // asia / edge 是 prod 的子目录,in-prod 是它的直属连接;更深的 in-asia/in-db 不出现。
    expect(namesOf(sections[0])).toEqual(["asia", "edge", "in-prod"]);
  });

  test("目录层不追加「最近连接」段", () => {
    const sections = buildGridSections({
      rows,
      folders,
      currentFolderId: "prod",
      searching: false,
      mode: "browse",
      sort: ASC
    });
    expect(sections.map((section) => section.key)).toEqual(["browse"]);
  });

  test("目录磁贴的计数是递归子树数,不是直属数", () => {
    const sections = buildGridSections({
      rows,
      folders,
      currentFolderId: undefined,
      searching: false,
      mode: "browse",
      sort: ASC
    });
    const prodTile = sections[0]?.items.find(
      (item): item is Extract<GridItem, { kind: "folder" }> =>
        item.kind === "folder" && item.folder.id === "prod"
    );
    // prod 直属 1 + asia 1 + db 1 = 3;edge 为空不贡献。
    expect(prodTile?.count).toBe(3);
  });

  test("空目录产出空段列表,由渲染层给「此目录为空」", () => {
    const sections = buildGridSections({
      rows,
      folders,
      currentFolderId: "edge",
      searching: false,
      mode: "browse",
      sort: ASC
    });
    expect(sections).toEqual([]);
  });

  test("目录磁贴按 sortIndex 再按名称排,不受连接排序影响", () => {
    const ordered = [folder("z", "z-folder", undefined, 0), folder("a", "a-folder", undefined, 1)];
    const sections = buildGridSections({
      rows: [],
      folders: ordered,
      currentFolderId: undefined,
      searching: false,
      mode: "browse",
      // 连接按名称降序,目录仍旧走 sortIndex。
      sort: { key: "name", direction: "desc" }
    });
    expect(namesOf(sections[0])).toEqual(["z-folder", "a-folder"]);
  });

  test("folderId 指向已不存在的目录时,连接落回根直属而不是永久消失", () => {
    const orphaned = rowsOf([conn({ id: "ghost", name: "ghost", folderId: "deleted-folder" })]);
    const sections = buildGridSections({
      rows: orphaned,
      folders,
      currentFolderId: undefined,
      searching: false,
      mode: "browse",
      sort: ASC
    });
    expect(namesOf(sections[0])).toContain("ghost");
  });
});

describe("buildGridSections — 连接排序", () => {
  const rows = rowsOf([
    conn({ id: "a", name: "alpha", lastConnectedAt: "2026-01-01T00:00:00.000Z" }),
    conn({ id: "b", name: "beta", lastConnectedAt: "2026-03-01T00:00:00.000Z" }),
    conn({ id: "c", name: "gamma" })
  ]);

  test("「最近连接」把最近连过的排最前,从未连接的沉底", () => {
    const sections = buildGridSections({
      rows,
      folders: [],
      currentFolderId: undefined,
      searching: true,
      mode: "browse",
      sort: resolveGridSort("lastConnected")
    });
    expect(namesOf(sections[0])).toEqual(["beta", "alpha", "gamma"]);
  });

  test("「创建时间」把最新创建的排最前", () => {
    const byCreated = rowsOf([
      conn({ id: "old", name: "old", createdAt: "2025-01-01T00:00:00.000Z" }),
      conn({ id: "new", name: "new", createdAt: "2026-06-01T00:00:00.000Z" })
    ]);
    const sections = buildGridSections({
      rows: byCreated,
      folders: [],
      searching: true,
      mode: "browse",
      sort: resolveGridSort("createdAt")
    });
    expect(namesOf(sections[0])).toEqual(["new", "old"]);
  });

  test("排序值双向映射;网格没有的排序键落回名称", () => {
    expect(gridSortValue(resolveGridSort("lastConnected"))).toBe("lastConnected");
    expect(gridSortValue(resolveGridSort("createdAt"))).toBe("createdAt");
    expect(gridSortValue({ key: "address", direction: "asc" })).toBe("name");
  });
});

describe("collectGridConnectionIds", () => {
  test("只收连接磁贴的 id,跨段合并,目录不算", () => {
    const rows = rowsOf([
      conn({ id: "root-a", name: "root-a" }),
      conn({
        id: "deep",
        name: "deep",
        folderId: "db",
        lastConnectedAt: "2026-03-01T00:00:00.000Z"
      })
    ]);
    const browse = buildGridSections({
      rows,
      folders,
      currentFolderId: undefined,
      searching: false,
      mode: "browse",
      sort: ASC
    });
    // 浏览态:目录磁贴(db)不算,只有根直属的连接。
    expect(collectGridConnectionIds(browse)).toEqual(["root-a"]);

    const recent = buildGridSections({
      rows,
      folders,
      currentFolderId: undefined,
      searching: false,
      mode: "recent",
      sort: ASC
    });
    // 最近视图:无视目录层级,两台都露面。
    expect(collectGridConnectionIds(recent).sort()).toEqual(["deep", "root-a"]);
  });

  test("没有磁贴时返回空数组", () => {
    expect(collectGridConnectionIds([])).toEqual([]);
  });
});
