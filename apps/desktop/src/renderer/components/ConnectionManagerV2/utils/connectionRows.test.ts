import { describe, expect, test } from "vitest";
import type { ConnectionProfile, SshKeyProfile } from "@nextshell/core";
import {
  buildConnectionRow,
  filterConnectionRows,
  selectRecentConnections,
  sortConnectionRows,
  type ConnectionRow
} from "./connectionRows";

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

const key = (id: string, name: string): SshKeyProfile =>
  ({
    id,
    name,
    keyContentRef: "secret://x",
    createdAt: "2026-01-01T00:00:00.000Z",
    updatedAt: "2026-01-01T00:00:00.000Z"
  }) as SshKeyProfile;

const rowsOf = (connections: ConnectionProfile[], keys: SshKeyProfile[] = []): ConnectionRow[] =>
  connections.map((connection) => buildConnectionRow(connection, keys));

describe("buildConnectionRow", () => {
  test("labels password and agent auth plainly", () => {
    expect(buildConnectionRow(conn({ id: "a" }), []).authLabel).toBe("密码");
    expect(buildConnectionRow(conn({ id: "a", authType: "agent" }), []).authLabel).toBe("Agent");
  });

  test("names the bound key for private-key auth", () => {
    const row = buildConnectionRow(
      conn({ id: "a", authType: "privateKey", sshKeyId: "k1" }),
      [key("k1", "deploy")]
    );
    expect(row.authLabel).toBe("私钥 · deploy");
    expect(row.authMissing).toBe(false);
  });

  test("flags private-key auth with no key at all", () => {
    const row = buildConnectionRow(conn({ id: "a", authType: "privateKey" }), []);
    expect(row.authLabel).toBe("私钥 · 未绑定");
    expect(row.authMissing).toBe(true);
  });

  test("flags a key id that no longer resolves", () => {
    // Deleting the key elsewhere leaves the id behind; connecting would fail just the same.
    const row = buildConnectionRow(
      conn({ id: "a", authType: "privateKey", sshKeyId: "gone" }),
      [key("k1", "deploy")]
    );
    expect(row.authMissing).toBe(true);
  });

  test("composes the address from host and port", () => {
    expect(buildConnectionRow(conn({ id: "a", host: "example.com", port: 2222 }), []).address).toBe(
      "example.com:2222"
    );
  });
});

describe("sortConnectionRows", () => {
  const rows = rowsOf([
    conn({ id: "b", name: "beta", lastConnectedAt: "2026-01-02T00:00:00.000Z" }),
    conn({ id: "a", name: "alpha", lastConnectedAt: "2026-01-03T00:00:00.000Z" }),
    conn({ id: "c", name: "gamma" })
  ]);

  test("sorts by name in both directions", () => {
    expect(
      sortConnectionRows(rows, { key: "name", direction: "asc" }).map((r) => r.connection.name)
    ).toEqual(["alpha", "beta", "gamma"]);
    expect(
      sortConnectionRows(rows, { key: "name", direction: "desc" }).map((r) => r.connection.name)
    ).toEqual(["gamma", "beta", "alpha"]);
  });

  test("sorts numerically inside addresses", () => {
    const numeric = rowsOf([
      conn({ id: "a", host: "10.0.0.10" }),
      conn({ id: "b", host: "10.0.0.9" })
    ]);
    expect(
      sortConnectionRows(numeric, { key: "address", direction: "asc" }).map((r) => r.address)
    ).toEqual(["10.0.0.9:22", "10.0.0.10:22"]);
  });

  test("puts never-connected rows last in both directions", () => {
    expect(
      sortConnectionRows(rows, { key: "lastConnected", direction: "asc" }).map(
        (r) => r.connection.name
      )
    ).toEqual(["alpha", "beta", "gamma"]);
    expect(
      sortConnectionRows(rows, { key: "lastConnected", direction: "desc" }).map(
        (r) => r.connection.name
      )
    ).toEqual(["beta", "alpha", "gamma"]);
  });

  test("does not mutate the input", () => {
    const original = rows.map((row) => row.connection.name);
    sortConnectionRows(rows, { key: "name", direction: "desc" });
    expect(rows.map((row) => row.connection.name)).toEqual(original);
  });

  // A8/D17：创建时间排序。它永远有值，所以 asc 就是字面的"最早在前"，和表头箭头一致。
  describe("createdAt", () => {
    const byCreated = rowsOf([
      conn({ id: "mid", name: "mid", createdAt: "2026-02-01T00:00:00.000Z" }),
      conn({ id: "old", name: "old", createdAt: "2026-01-01T00:00:00.000Z" }),
      conn({ id: "new", name: "new", createdAt: "2026-03-01T00:00:00.000Z" })
    ]);

    test("asc 最早在前，desc 反过来", () => {
      expect(
        sortConnectionRows(byCreated, { key: "createdAt", direction: "asc" }).map(
          (r) => r.connection.id
        )
      ).toEqual(["old", "mid", "new"]);
      expect(
        sortConnectionRows(byCreated, { key: "createdAt", direction: "desc" }).map(
          (r) => r.connection.id
        )
      ).toEqual(["new", "mid", "old"]);
    });

    test("同一时刻按名称兜底，顺序不会随输入抖动", () => {
      const sameMoment = rowsOf([
        conn({ id: "b", name: "beta", createdAt: "2026-01-01T00:00:00.000Z" }),
        conn({ id: "a", name: "alpha", createdAt: "2026-01-01T00:00:00.000Z" })
      ]);
      expect(
        sortConnectionRows(sameMoment, { key: "createdAt", direction: "asc" }).map(
          (r) => r.connection.name
        )
      ).toEqual(["alpha", "beta"]);
    });

    test("解析不出来的时间戳不会把顺序搅乱", () => {
      const broken = rowsOf([
        conn({ id: "ok", name: "ok", createdAt: "2026-01-01T00:00:00.000Z" }),
        conn({ id: "bad", name: "bad", createdAt: "not-a-date" })
      ]);
      expect(
        sortConnectionRows(broken, { key: "createdAt", direction: "asc" }).map(
          (r) => r.connection.id
        )
      ).toEqual(["bad", "ok"]);
    });
  });
});

describe("filterConnectionRows", () => {
  const rows = rowsOf([
    conn({ id: "a", name: "prod-db", host: "10.0.3.21", tags: ["prod", "hk"] }),
    conn({ id: "b", name: "staging-web", host: "10.0.4.9", notes: "临时机器" }),
    conn({ id: "c", name: "gateway", username: "ops" })
  ]);

  test("returns everything for a blank keyword", () => {
    expect(filterConnectionRows(rows, "   ")).toHaveLength(3);
  });

  test("matches name, address, username, tags and notes", () => {
    expect(filterConnectionRows(rows, "prod").map((r) => r.connection.id)).toEqual(["a"]);
    expect(filterConnectionRows(rows, "10.0.4").map((r) => r.connection.id)).toEqual(["b"]);
    expect(filterConnectionRows(rows, "ops").map((r) => r.connection.id)).toEqual(["c"]);
    expect(filterConnectionRows(rows, "临时").map((r) => r.connection.id)).toEqual(["b"]);
  });

  test("requires every token to match", () => {
    expect(filterConnectionRows(rows, "prod hk").map((r) => r.connection.id)).toEqual(["a"]);
    expect(filterConnectionRows(rows, "prod gateway")).toEqual([]);
  });

  test("ignores case", () => {
    expect(filterConnectionRows(rows, "PROD-DB").map((r) => r.connection.id)).toEqual(["a"]);
  });
});

describe("selectRecentConnections", () => {
  const conn = (id: string, lastConnectedAt?: string, createdAt = "2026-01-01T00:00:00Z") =>
    ({ id, name: id, lastConnectedAt, createdAt }) as never;

  test("连过的按最后连接时间倒序排在前面", () => {
    const picked = selectRecentConnections(
      [
        conn("old", "2026-01-01T00:00:00Z"),
        conn("new", "2026-08-01T00:00:00Z"),
        conn("mid", "2026-05-01T00:00:00Z")
      ],
      2
    );
    expect(picked.map((c: { id: string }) => c.id)).toEqual(["new", "mid"]);
  });

  test("不足上限时用最近创建的补齐", () => {
    const picked = selectRecentConnections(
      [
        conn("never-old", undefined, "2026-02-01T00:00:00Z"),
        conn("connected", "2026-06-01T00:00:00Z"),
        conn("never-new", undefined, "2026-07-01T00:00:00Z")
      ],
      2
    );
    expect(picked.map((c: { id: string }) => c.id)).toEqual(["connected", "never-new"]);
  });

  test("坏时间戳不炸,按 0 处理并落到名称序", () => {
    const picked = selectRecentConnections(
      [conn("b", "not-a-date"), conn("a", "also-bad")],
      5
    );
    expect(picked.map((c: { id: string }) => c.id)).toEqual(["a", "b"]);
  });
});
