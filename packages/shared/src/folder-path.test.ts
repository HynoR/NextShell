import { describe, expect, test } from "vitest";
import {
  deriveGroupPath,
  parseGroupPathSegments,
  resolveFolderNames,
  workspaceSlug
} from "./folder-path";
import { LOCAL_DEFAULT_SCOPE_KEY } from "../../core/src/index";

const CLOUD_SCOPE = "sync.example.com-team-a";

const folders = [
  { id: "a", name: "prod" },
  { id: "b", name: "asia", parentId: "a" },
  { id: "c", name: "db", parentId: "b" }
];

describe("resolveFolderNames", () => {
  test("returns the chain root-first", () => {
    expect(resolveFolderNames("c", folders)).toEqual(["prod", "asia", "db"]);
  });

  test("returns nothing for a top-level connection", () => {
    expect(resolveFolderNames(undefined, folders)).toEqual([]);
  });

  test("stops at the first unknown ancestor instead of throwing", () => {
    expect(resolveFolderNames("c", [{ id: "c", name: "db", parentId: "missing" }])).toEqual(["db"]);
    expect(resolveFolderNames("nope", folders)).toEqual([]);
  });

  test("truncates instead of looping forever when the data contains a cycle", () => {
    const cyclic = [
      { id: "x", name: "x", parentId: "y" },
      { id: "y", name: "y", parentId: "x" }
    ];
    expect(resolveFolderNames("x", cyclic)).toEqual(["y", "x"]);
  });
});

describe("deriveGroupPath", () => {
  test("projects a local connection under the /server wire prefix", () => {
    expect(
      deriveGroupPath({ scopeKey: LOCAL_DEFAULT_SCOPE_KEY, folderNames: ["prod", "db"] })
    ).toBe("/server/prod/db");
  });

  test("projects a top-level local connection to the bare root", () => {
    expect(deriveGroupPath({ scopeKey: LOCAL_DEFAULT_SCOPE_KEY, folderNames: [] })).toBe("/server");
  });

  test("projects a cloud connection under its workspace slug", () => {
    expect(
      deriveGroupPath({
        scopeKey: CLOUD_SCOPE,
        workspaceName: "Team A",
        folderNames: ["prod"]
      })
    ).toBe("/workspace/team-a/prod");
  });

  test("falls back to the local root when a cloud scope has no workspace name", () => {
    // A bare /workspace would make the tree invent a root literally named "workspace".
    expect(deriveGroupPath({ scopeKey: CLOUD_SCOPE, folderNames: ["prod"] })).toBe("/server/prod");
  });

  test("drops blank segments so the path never doubles a separator", () => {
    expect(
      deriveGroupPath({ scopeKey: LOCAL_DEFAULT_SCOPE_KEY, folderNames: ["prod", "  ", "db"] })
    ).toBe("/server/prod/db");
  });
});

describe("parseGroupPathSegments — stripWirePrefix: true", () => {
  const parse = (path: string | undefined) =>
    parseGroupPathSegments(path, { stripWirePrefix: true });

  test("strips the local wire prefix", () => {
    expect(parse("/server/prod/db")).toEqual(["prod", "db"]);
    expect(parse("/server")).toEqual([]);
  });

  test("strips the workspace prefix together with its slug", () => {
    expect(parse("/workspace/team-a/prod")).toEqual(["prod"]);
    expect(parse("/workspace/team-a")).toEqual([]);
    expect(parse("/workspace")).toEqual([]);
  });

  // 旧导出文件里的 /import 前缀不是用户建的目录,别把它物化成一层。
  test("strips the legacy import prefix", () => {
    expect(parse("/import/customer-a/prod")).toEqual(["customer-a", "prod"]);
    expect(parse("/import/finalshell")).toEqual(["finalshell"]);
  });

  test("keeps every segment when the prefix is unknown", () => {
    expect(parse("/prod/db")).toEqual(["prod", "db"]);
    expect(parse("prod")).toEqual(["prod"]);
  });

  test("keeps a folder that happens to be named like the root", () => {
    expect(parse("/server/server")).toEqual(["server"]);
  });

  test("survives empty, blank and doubled separators", () => {
    expect(parse(undefined)).toEqual([]);
    expect(parse("")).toEqual([]);
    expect(parse("/")).toEqual([]);
    expect(parse("/server//prod/  /db")).toEqual(["prod", "db"]);
  });

  test("round-trips with deriveGroupPath", () => {
    const names = ["prod", "asia"];
    const path = deriveGroupPath({ scopeKey: LOCAL_DEFAULT_SCOPE_KEY, folderNames: names });
    expect(parse(path)).toEqual(names);
  });
});

describe("parseGroupPathSegments — stripWirePrefix: false", () => {
  const parse = (path: string | undefined) =>
    parseGroupPathSegments(path, { stripWirePrefix: false });

  // 目录扫描导入的 groupPath 来自磁盘相对路径,没有线格式前缀:用户顶层目录就叫 server 时
  // 剥掉首段会把整整一层目录吞掉。
  test("keeps a literal path whose first segment looks like a wire prefix", () => {
    expect(parse("/server/prod")).toEqual(["server", "prod"]);
    expect(parse("/import/finalshell")).toEqual(["import", "finalshell"]);
    expect(parse("/workspace/team-a/prod")).toEqual(["workspace", "team-a", "prod"]);
  });

  test("keeps ordinary literal paths intact", () => {
    expect(parse("/客户 A/prod")).toEqual(["客户 A", "prod"]);
    expect(parse("/")).toEqual([]);
    expect(parse(undefined)).toEqual([]);
  });
});

describe("parseGroupPathSegments — separators", () => {
  // 反斜杠按 normalizeGroupPath 同款规则折算,否则整条路径会变成一个名字带 \\ 的目录,
  // 而 normalizeFolderName 根本不允许目录名里出现 \\。
  test("treats backslashes as separators in both modes", () => {
    expect(parseGroupPathSegments("\\server\\prod\\db", { stripWirePrefix: true })).toEqual([
      "prod",
      "db"
    ]);
    expect(parseGroupPathSegments("a\\b/c", { stripWirePrefix: false })).toEqual(["a", "b", "c"]);
  });
});

describe("workspaceSlug", () => {
  test("lowercases and collapses separators", () => {
    expect(workspaceSlug("Team A")).toBe("team-a");
    expect(workspaceSlug("  Team   A  ")).toBe("team-a");
    expect(workspaceSlug("team_a.v2")).toBe("team_a.v2");
  });

  test("never yields an empty slug", () => {
    expect(workspaceSlug("   ")).toBe("workspace");
    expect(workspaceSlug("///")).toBe("workspace");
  });
});
