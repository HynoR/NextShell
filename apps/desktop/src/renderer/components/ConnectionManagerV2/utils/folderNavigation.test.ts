import { describe, expect, test } from "vitest";
import type { ConnectionFolder, ConnectionProfile } from "@nextshell/core";
import {
  buildBreadcrumb,
  collectDescendantIds,
  listChildFolders,
  listVisibleConnections,
  reconcileCurrentFolder
} from "./folderNavigation";

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

describe("buildBreadcrumb", () => {
  test("walks from the root down to the current folder", () => {
    expect(buildBreadcrumb("db", folders, "本地")).toEqual([
      { label: "本地" },
      { folderId: "prod", label: "prod" },
      { folderId: "asia", label: "asia" },
      { folderId: "db", label: "db" }
    ]);
  });

  test("is just the root at the top level", () => {
    expect(buildBreadcrumb(undefined, folders, "本地")).toEqual([{ label: "本地" }]);
  });

  test("stops at an unknown ancestor instead of throwing", () => {
    expect(buildBreadcrumb("orphan", [folder("orphan", "orphan", "gone")], "本地")).toEqual([
      { label: "本地" },
      { folderId: "orphan", label: "orphan" }
    ]);
  });

  test("truncates a cyclic chain rather than hanging", () => {
    const cyclic = [folder("x", "x", "y"), folder("y", "y", "x")];
    expect(buildBreadcrumb("x", cyclic, "本地").length).toBe(3);
  });
});

describe("collectDescendantIds", () => {
  test("collects the whole subtree without the folder itself", () => {
    expect([...collectDescendantIds("prod", folders)].sort()).toEqual(["asia", "db", "edge"]);
  });

  test("is empty for a leaf", () => {
    expect(collectDescendantIds("db", folders).size).toBe(0);
  });

  test("terminates on a cycle", () => {
    const cyclic = [folder("x", "x", "y"), folder("y", "y", "x")];
    expect([...collectDescendantIds("x", cyclic)].sort()).toEqual(["x", "y"]);
  });
});

describe("listChildFolders", () => {
  test("lists only the current level, sorted by sortIndex then name", () => {
    const ordered = [
      folder("b", "b", undefined, 1),
      folder("a", "a", undefined, 2),
      folder("c", "c", undefined, 1)
    ];
    expect(listChildFolders(undefined, ordered, []).map((item) => item.folder.name)).toEqual([
      "b",
      "c",
      "a"
    ]);
  });

  test("counts connections across the whole subtree", () => {
    const [prod, staging] = listChildFolders(undefined, folders, connections);
    expect(prod?.folder.name).toBe("prod");
    expect(prod?.connectionCount).toBe(4);
    expect(prod?.hasChildren).toBe(true);
    expect(staging?.connectionCount).toBe(0);
    expect(staging?.hasChildren).toBe(false);
  });
});

describe("listVisibleConnections", () => {
  test("shows the whole scope at the root when subfolders are included", () => {
    expect(
      listVisibleConnections({
        folders,
        connections,
        includeSubfolders: true
      })
    ).toHaveLength(5);
  });

  test("shows only unfiled connections at the root when they are not", () => {
    expect(
      listVisibleConnections({
        folders,
        connections,
        includeSubfolders: false
      }).map((item) => item.id)
    ).toEqual(["c-root"]);
  });

  test("includes the subtree of the current folder", () => {
    expect(
      listVisibleConnections({
        folderId: "prod",
        folders,
        connections,
        includeSubfolders: true
      })
        .map((item) => item.id)
        .sort()
    ).toEqual(["c-asia", "c-db-1", "c-db-2", "c-edge"]);
  });

  test("restricts to direct children when subfolders are excluded", () => {
    expect(
      listVisibleConnections({
        folderId: "prod",
        folders,
        connections,
        includeSubfolders: false
      })
    ).toEqual([]);
    expect(
      listVisibleConnections({
        folderId: "db",
        folders,
        connections,
        includeSubfolders: false
      }).map((item) => item.id)
    ).toEqual(["c-db-1", "c-db-2"]);
  });
});

describe("reconcileCurrentFolder", () => {
  test("keeps a folder that still exists", () => {
    expect(reconcileCurrentFolder("db", folders)).toBe("db");
  });

  test("falls back to the root when the folder is gone", () => {
    // Happens after a delete, and after switching scope — the id belongs to the other scope.
    expect(reconcileCurrentFolder("db", [])).toBeUndefined();
    expect(reconcileCurrentFolder(undefined, folders)).toBeUndefined();
  });
});
