import { describe, expect, test } from "bun:test";
import type { ConnectionProfile } from "@nextshell/core";
import {
  buildBatchTargetTree,
  getBatchTargetConnectionIds,
  type BatchTargetTreeNode
} from "./batchTargets";

const conn = (id: string, name: string, host: string, groupPath: string): ConnectionProfile =>
  ({
    id,
    name,
    host,
    port: 22,
    username: "root",
    authType: "password",
    groupPath,
    strictHostKeyChecking: false,
    terminalEncoding: "utf-8",
    backspaceMode: "ascii-backspace",
    deleteMode: "vt220-delete",
    favorite: false,
    createdAt: "2026-01-01T00:00:00.000Z",
    updatedAt: "2026-01-01T00:00:00.000Z"
  }) as unknown as ConnectionProfile;

const leafValues = (nodes: BatchTargetTreeNode[]): string[] =>
  nodes.flatMap((n) => (n.children && n.children.length ? leafValues(n.children) : [n.value]));

const allValues = (nodes: BatchTargetTreeNode[]): string[] =>
  nodes.flatMap((n) => [n.value, ...(n.children ? allValues(n.children) : [])]);

describe("getBatchTargetConnectionIds", () => {
  test("returns unique connection ids from open sessions in order", () => {
    const ids = getBatchTargetConnectionIds([
      { connectionId: "a" },
      { connectionId: "b" },
      { connectionId: "a" },
      { connectionId: undefined }
    ] as never);
    expect(ids).toEqual(["a", "b"]);
  });
});

describe("buildBatchTargetTree", () => {
  test("nests connections under their group path segments", () => {
    const tree = buildBatchTargetTree([
      conn("c1", "web-1", "10.0.0.1", "/server/prod"),
      conn("c2", "web-2", "10.0.0.2", "/server/prod"),
      conn("c3", "db-1", "10.0.0.3", "/server/staging")
    ]);

    expect(tree.length).toBe(1);
    expect(tree[0]?.title).toBe("server");
    expect(tree[0]?.value).toBe("grp:/server");

    const prod = tree[0]?.children?.find((n) => n.title === "prod");
    const staging = tree[0]?.children?.find((n) => n.title === "staging");
    expect(prod?.children?.map((n) => n.value)).toEqual(["c1", "c2"]);
    expect(staging?.children?.map((n) => n.value)).toEqual(["c3"]);
  });

  test("group node values use the grp: prefix; only connection ids are leaves", () => {
    const tree = buildBatchTargetTree([conn("c1", "x", "h", "/a/b")]);
    const groupValues = allValues(tree).filter((v) => v.startsWith("grp:"));
    expect(groupValues).toEqual(["grp:/a", "grp:/a/b"]);
    expect(leafValues(tree)).toEqual(["c1"]);
  });

  test("falls back to a default group for empty group paths", () => {
    const tree = buildBatchTargetTree([conn("c1", "x", "h", "")]);
    expect(tree[0]?.title).toBe("server");
    expect(leafValues(tree)).toEqual(["c1"]);
  });

  test("leaf titles show name and host; connections are sorted by name", () => {
    const tree = buildBatchTargetTree([
      conn("c2", "zeta", "10.0.0.9", "/g"),
      conn("c1", "alpha", "10.0.0.1", "/g")
    ]);
    const leaves = tree[0]?.children ?? [];
    expect(leaves.map((n) => n.title)).toEqual(["alpha (10.0.0.1)", "zeta (10.0.0.9)"]);
  });
});
