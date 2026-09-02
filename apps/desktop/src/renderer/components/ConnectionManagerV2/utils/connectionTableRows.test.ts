import { describe, expect, test } from "vitest";
import type { ConnectionFolder, ConnectionProfile } from "@nextshell/core";
import { buildConnectionRow, type ConnectionRow } from "./connectionRows";
import {
  buildConnectionTableRows,
  connectionIdFromRowKey,
  connectionRowKey,
  folderAncestorRowKeys,
  folderRowKey,
  mergeCollapsedKeys,
  type ConnectionTableRow
} from "./connectionTableRows";

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

const nameOf = (row: ConnectionTableRow): string =>
  row.kind === "folder" ? row.folder.name : row.row.connection.name;

const ASC = { key: "name", direction: "asc" } as const;

//   prod ── asia ── db
//     └── edge
//   staging(空目录)
const folders = [
  folder("prod", "prod"),
  folder("asia", "asia", "prod"),
  folder("db", "db", "asia"),
  folder("edge", "edge", "prod"),
  folder("staging", "staging")
];

describe("buildConnectionTableRows — 平铺", () => {
  test("「含子目录」关闭/搜索中只出连接行，一个目录行都没有", () => {
    const rows = buildConnectionTableRows({
      rows: rowsOf([conn({ id: "b", folderId: "db" }), conn({ id: "a" })]),
      folders,
      sort: ASC,
      flat: true
    });
    expect(rows.every((row) => row.kind === "connection")).toBe(true);
    expect(rows.map(nameOf)).toEqual(["a", "b"]);
  });

  test("平铺时忽略 currentFolderId，输入给什么就排什么", () => {
    const rows = buildConnectionTableRows({
      rows: rowsOf([conn({ id: "only", folderId: "db" })]),
      folders,
      currentFolderId: "staging",
      sort: ASC,
      flat: true
    });
    expect(rows.map(nameOf)).toEqual(["only"]);
  });
});

describe("buildConnectionTableRows — 层级", () => {
  test("目录行恒在连接行之前", () => {
    const rows = buildConnectionTableRows({
      rows: rowsOf([conn({ id: "aaa-root" }), conn({ id: "zzz-in-prod", folderId: "prod" })]),
      folders,
      sort: ASC,
      flat: false
    });
    // 顶层：prod / staging 两个目录行在前，根连接在后——即使连接名字排序更靠前。
    expect(rows.map(nameOf)).toEqual(["prod", "staging", "aaa-root"]);
  });

  test("多层嵌套按目录链摆放，计数是递归的", () => {
    const rows = buildConnectionTableRows({
      rows: rowsOf([
        conn({ id: "c-db", folderId: "db" }),
        conn({ id: "c-asia", folderId: "asia" }),
        conn({ id: "c-edge", folderId: "edge" })
      ]),
      folders,
      sort: ASC,
      flat: false
    });
    const prod = rows[0];
    expect(prod?.kind).toBe("folder");
    if (prod?.kind !== "folder") {
      throw new Error("第一行应当是 prod 目录行");
    }
    expect(prod.count).toBe(3);
    expect(prod.children?.map(nameOf)).toEqual(["asia", "edge"]);
    const asia = prod.children?.[0];
    if (asia?.kind !== "folder") {
      throw new Error("asia 应当是目录行");
    }
    expect(asia.count).toBe(2);
    // asia 层内：子目录 db 在前，直属连接 c-asia 在后。
    expect(asia.children?.map(nameOf)).toEqual(["db", "c-asia"]);
  });

  test("空目录仍然出现在列表里，但不给 children(免得画出点不开的箭头)", () => {
    const rows = buildConnectionTableRows({ rows: [], folders, sort: ASC, flat: false });
    const staging = rows.find((row) => nameOf(row) === "staging");
    expect(staging?.kind).toBe("folder");
    if (staging?.kind !== "folder") {
      throw new Error("staging 应当是目录行");
    }
    expect(staging.count).toBe(0);
    expect(staging.children).toBeUndefined();
  });

  test("currentFolderId 决定顶层：只画它的子目录与直属连接", () => {
    const rows = buildConnectionTableRows({
      rows: rowsOf([
        conn({ id: "c-prod", folderId: "prod" }),
        conn({ id: "c-db", folderId: "db" })
      ]),
      folders,
      currentFolderId: "prod",
      sort: ASC,
      flat: false
    });
    // prod 自身不再是一行；它的直属连接升到顶层。
    expect(rows.map(nameOf)).toEqual(["asia", "edge", "c-prod"]);
  });

  test("排序键作用于每一层内部的连接行，目录顺序不受影响", () => {
    const rows = buildConnectionTableRows({
      rows: rowsOf([
        conn({ id: "r-b", name: "b-root" }),
        conn({ id: "r-a", name: "a-root" }),
        conn({ id: "p-b", name: "b-prod", folderId: "prod" }),
        conn({ id: "p-a", name: "a-prod", folderId: "prod" })
      ]),
      folders,
      sort: { key: "name", direction: "desc" },
      flat: false
    });
    expect(rows.map(nameOf)).toEqual(["prod", "staging", "b-root", "a-root"]);
    const prod = rows[0];
    if (prod?.kind !== "folder") {
      throw new Error("第一行应当是 prod 目录行");
    }
    // 目录(asia/edge)不跟着排序方向翻转，只有连接行按 name desc 排。
    expect(prod.children?.map(nameOf)).toEqual(["asia", "edge", "b-prod", "a-prod"]);
  });

  test("同一份输入两次调用结果一致(排序稳定)", () => {
    const input = {
      rows: rowsOf([
        conn({ id: "x", name: "same", folderId: "prod" }),
        conn({ id: "y", name: "same", folderId: "prod" })
      ]),
      folders,
      sort: ASC,
      flat: false
    };
    const first = buildConnectionTableRows(input);
    const second = buildConnectionTableRows(input);
    const keysOf = (rows: ConnectionTableRow[]): string[] =>
      rows.flatMap((row) => [
        row.key,
        ...(row.kind === "folder" ? keysOf(row.children ?? []) : [])
      ]);
    expect(keysOf(first)).toEqual(keysOf(second));
  });

  test("folderId 悬空的连接挂到顶层而不是消失", () => {
    const rows = buildConnectionTableRows({
      rows: rowsOf([conn({ id: "ghost", folderId: "deleted-folder" })]),
      folders,
      sort: ASC,
      flat: false
    });
    expect(rows.map(nameOf)).toContain("ghost");
  });

  test("currentFolderId 指向一个不存在的目录时不崩，连接全部落到顶层", () => {
    const rows = buildConnectionTableRows({
      rows: rowsOf([conn({ id: "c1", folderId: "db" })]),
      folders,
      currentFolderId: "gone",
      sort: ASC,
      flat: false
    });
    expect(rows.map(nameOf)).toEqual(["c1"]);
  });

  test("没有任何目录时退化成一串连接行", () => {
    const rows = buildConnectionTableRows({
      rows: rowsOf([conn({ id: "a" }), conn({ id: "b" })]),
      folders: [],
      sort: ASC,
      flat: false
    });
    expect(rows.map(nameOf)).toEqual(["a", "b"]);
  });
});

describe("row key", () => {
  test("目录与连接的 key 互不相干", () => {
    expect(folderRowKey("x")).not.toBe(connectionRowKey("x"));
  });

  test("只有连接行的 key 能还原出连接 id", () => {
    expect(connectionIdFromRowKey(connectionRowKey("abc"))).toBe("abc");
    expect(connectionIdFromRowKey(folderRowKey("abc"))).toBeUndefined();
    expect(connectionIdFromRowKey("garbage")).toBeUndefined();
  });
});

describe("folderAncestorRowKeys", () => {
  test("返回从根到该目录(含自身)的全部目录行 key", () => {
    expect(folderAncestorRowKeys("db", folders)).toEqual([
      folderRowKey("prod"),
      folderRowKey("asia"),
      folderRowKey("db")
    ]);
  });

  test("顶层连接与未知目录返回空", () => {
    expect(folderAncestorRowKeys(undefined, folders)).toEqual([]);
    expect(folderAncestorRowKeys("gone", folders)).toEqual([]);
  });

  test("成环时截断而不是死循环", () => {
    const cyclic = [folder("x", "x", "y"), folder("y", "y", "x")];
    expect(folderAncestorRowKeys("x", cyclic)).toHaveLength(2);
  });
});

describe("mergeCollapsedKeys", () => {
  const visible = ["folder:a", "folder:b"];

  test("视图内展开的移出折叠集、收起的进折叠集", () => {
    // 两个都展开 → 折叠集空。
    expect(mergeCollapsedKeys(["folder:a"], visible, new Set(visible))).toEqual([]);
    // 只报告 a 展开 → b 算收起。
    expect(mergeCollapsedKeys([], visible, new Set(["folder:a"]))).toEqual(["folder:b"]);
  });

  // 这条就是那次回归的守门人:钻进 b 再退回来，原先在别处收起的 z 不该被整体重算抹掉。
  test("视图外的折叠状态原样保留", () => {
    expect(mergeCollapsedKeys(["folder:z"], visible, new Set(visible))).toEqual(["folder:z"]);
    expect(mergeCollapsedKeys(["folder:z", "folder:a"], visible, new Set(["folder:b"]))).toEqual([
      "folder:z",
      "folder:a"
    ]);
  });

  test("当前视图一个目录都没有时折叠集原封不动", () => {
    expect(mergeCollapsedKeys(["folder:z"], [], new Set())).toEqual(["folder:z"]);
  });

  test("结果不会重复计入同一个 key", () => {
    const merged = mergeCollapsedKeys(["folder:a"], visible, new Set());
    expect(merged).toEqual(["folder:a", "folder:b"]);
    expect(new Set(merged).size).toBe(merged.length);
  });
});
