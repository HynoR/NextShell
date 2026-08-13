import { describe, expect, test } from "vitest";
import { deriveGroupPath, resolveFolderNames, workspaceSlug } from "./folder-path";
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
