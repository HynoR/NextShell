import { describe, expect, test } from "vitest";
import type { ConnectionFolder, ConnectionProfile } from "@nextshell/core";
import { buildFolderPathLabels, buildFolderTree, isSelfOrAncestor } from "./folderTree";

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
