import { describe, expect, test } from "vitest";
import {
  assertFolderMoveAllowed,
  normalizeFolderName,
  planConnectionFolderBackfill,
  splitLegacyGroupPath,
  type LegacyConnectionFolderRow
} from "./index";
import { LOCAL_DEFAULT_SCOPE_KEY, type ConnectionFolder } from "../../core/src/index";

// better-sqlite3 在本仓库是按 Electron ABI 编译的,测试进程加载不了,所以这里验证的是迁移与
// 目录守卫的**决策逻辑**(纯函数);SQL 层只是照计划顺序执行,没有额外分支。

const CLOUD_SCOPE = "cloud:https://sync.example.com:team-a";

const legacyRow = (
  id: string,
  groupPath: string | null,
  scopeKey?: string
): LegacyConnectionFolderRow => ({
  id,
  group_path: groupPath,
  origin_scope_key: scopeKey ?? LOCAL_DEFAULT_SCOPE_KEY
});

const counterIds = () => {
  let next = 0;
  return () => `f${++next}`;
};

const folder = (patch: Partial<ConnectionFolder> & { id: string }): ConnectionFolder => ({
  scopeKey: LOCAL_DEFAULT_SCOPE_KEY,
  name: patch.id,
  sortIndex: 0,
  createdAt: "2026-01-01T00:00:00.000Z",
  updatedAt: "2026-01-01T00:00:00.000Z",
  ...patch
});

const lookupFrom = (folders: ConnectionFolder[]) => (id: string) =>
  folders.find((item) => item.id === id);

describe("splitLegacyGroupPath", () => {
  test("strips the server zone prefix", () => {
    expect(splitLegacyGroupPath("/server/prod/db")).toEqual(["prod", "db"]);
  });

  test("promotes import-zone paths to the top level", () => {
    expect(splitLegacyGroupPath("/import/finalshell/hk")).toEqual(["finalshell", "hk"]);
  });

  test("strips both the workspace zone and its slug", () => {
    expect(splitLegacyGroupPath("/workspace/team-a/prod")).toEqual(["prod"]);
    expect(splitLegacyGroupPath("/workspace/team-a")).toEqual([]);
  });

  test("keeps every segment of a path that carries no zone prefix", () => {
    expect(splitLegacyGroupPath("/mygroup/foo")).toEqual(["mygroup", "foo"]);
  });

  test("treats a bare zone root, empty and missing input as the top level", () => {
    expect(splitLegacyGroupPath("/server")).toEqual([]);
    expect(splitLegacyGroupPath("")).toEqual([]);
    expect(splitLegacyGroupPath(null)).toEqual([]);
    expect(splitLegacyGroupPath(undefined)).toEqual([]);
  });
});

describe("planConnectionFolderBackfill", () => {
  test("builds each chain once and links connections to the leaf", () => {
    const plan = planConnectionFolderBackfill(
      [
        legacyRow("c1", "/server/prod/db"),
        legacyRow("c2", "/server/prod/db"),
        legacyRow("c3", "/server/prod")
      ],
      counterIds()
    );

    expect(plan.folders).toEqual([
      { id: "f1", scopeKey: LOCAL_DEFAULT_SCOPE_KEY, parentId: null, name: "prod" },
      { id: "f2", scopeKey: LOCAL_DEFAULT_SCOPE_KEY, parentId: "f1", name: "db" }
    ]);
    expect(plan.assignments).toEqual([
      { connectionId: "c1", folderId: "f2" },
      { connectionId: "c2", folderId: "f2" },
      { connectionId: "c3", folderId: "f1" }
    ]);
  });

  test("emits parents before children so the plan can be inserted in order", () => {
    const plan = planConnectionFolderBackfill([legacyRow("c1", "/server/a/b/c")], counterIds());
    const positionOf = (name: string) => plan.folders.findIndex((f) => f.name === name);

    expect(positionOf("a")).toBeLessThan(positionOf("b"));
    expect(positionOf("b")).toBeLessThan(positionOf("c"));
    for (const item of plan.folders) {
      if (item.parentId) {
        expect(plan.folders.findIndex((f) => f.id === item.parentId)).toBeLessThan(
          plan.folders.findIndex((f) => f.id === item.id)
        );
      }
    }
  });

  test("keeps identically named folders of different scopes apart", () => {
    const plan = planConnectionFolderBackfill(
      [legacyRow("c1", "/server/prod"), legacyRow("c2", "/workspace/team-a/prod", CLOUD_SCOPE)],
      counterIds()
    );

    expect(plan.folders).toHaveLength(2);
    expect(plan.folders.map((f) => f.scopeKey)).toEqual([LOCAL_DEFAULT_SCOPE_KEY, CLOUD_SCOPE]);
    expect(plan.assignments.map((a) => a.folderId)).toEqual(["f1", "f2"]);
  });

  test("merges legacy zones that collide once their prefixes are stripped", () => {
    const plan = planConnectionFolderBackfill(
      [legacyRow("c1", "/server/prod"), legacyRow("c2", "/import/prod")],
      counterIds()
    );

    expect(plan.folders).toHaveLength(1);
    expect(plan.assignments).toEqual([
      { connectionId: "c1", folderId: "f1" },
      { connectionId: "c2", folderId: "f1" }
    ]);
  });

  test("leaves zone-root and pathless connections unassigned", () => {
    const plan = planConnectionFolderBackfill(
      [legacyRow("c1", "/server"), legacyRow("c2", null), legacyRow("c3", "/workspace/team-a")],
      counterIds()
    );

    expect(plan.folders).toEqual([]);
    expect(plan.assignments).toEqual([]);
  });

  test("returns an empty plan for an empty database", () => {
    expect(planConnectionFolderBackfill([], counterIds())).toEqual({
      folders: [],
      assignments: []
    });
  });
});

describe("normalizeFolderName", () => {
  test("trims surrounding whitespace", () => {
    expect(normalizeFolderName("  prod  ")).toBe("prod");
  });

  test("rejects blank names and path separators", () => {
    expect(() => normalizeFolderName("   ")).toThrow("目录名称不能为空");
    expect(() => normalizeFolderName("a/b")).toThrow("不能包含");
    expect(() => normalizeFolderName("a\\b")).toThrow("不能包含");
  });
});

describe("assertFolderMoveAllowed", () => {
  const a = folder({ id: "a" });
  const b = folder({ id: "b", parentId: "a" });
  const c = folder({ id: "c", parentId: "b" });
  const lookup = lookupFrom([a, b, c]);

  test("allows moving to the top level", () => {
    expect(() => assertFolderMoveAllowed(c, undefined, lookup)).not.toThrow();
  });

  test("allows moving under an unrelated folder in the same scope", () => {
    const sibling = folder({ id: "s" });
    expect(() => assertFolderMoveAllowed(c, "s", lookupFrom([a, b, c, sibling]))).not.toThrow();
  });

  test("rejects moving a folder into itself", () => {
    expect(() => assertFolderMoveAllowed(a, "a", lookup)).toThrow("自身");
  });

  test("rejects moving a folder into its own descendant", () => {
    expect(() => assertFolderMoveAllowed(a, "b", lookup)).toThrow("子目录");
    expect(() => assertFolderMoveAllowed(a, "c", lookup)).toThrow("子目录");
  });

  test("rejects moving across scopes", () => {
    const cloud = folder({ id: "z", scopeKey: CLOUD_SCOPE });
    expect(() => assertFolderMoveAllowed(cloud, "a", lookupFrom([a, cloud]))).toThrow("跨来源范围");
  });

  test("rejects a missing target", () => {
    expect(() => assertFolderMoveAllowed(c, "nope", lookup)).toThrow("不存在");
  });
});
