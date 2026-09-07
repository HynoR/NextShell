import { describe, expect, test } from "vitest";

import { pickRecentConnections, searchConnections, type ConnectionProfile } from "./index";

const conn = (overrides: Partial<ConnectionProfile> & Pick<ConnectionProfile, "id" | "name">) =>
  ({
    host: "10.0.0.1",
    port: 22,
    username: "root",
    authType: "password",
    strictHostKeyChecking: false,
    terminalEncoding: "utf-8",
    backspaceMode: "auto",
    deleteMode: "auto",
    groupPath: "/",
    tags: [],
    favorite: false,
    monitorSession: false,
    createdAt: "2026-01-01T00:00:00.000Z",
    updatedAt: "2026-01-01T00:00:00.000Z",
    ...overrides
  }) as ConnectionProfile;

describe("pickRecentConnections", () => {
  test("orders by lastConnectedAt desc, skips never-opened, caps", () => {
    const list = [
      conn({ id: "a", name: "a", lastConnectedAt: "2026-01-01T00:00:00.000Z" }),
      conn({ id: "b", name: "b" }),
      conn({ id: "c", name: "c", lastConnectedAt: "2026-02-01T00:00:00.000Z" })
    ];
    expect(pickRecentConnections(list, 10).map((c) => c.id)).toEqual(["c", "a"]);
    expect(pickRecentConnections(list, 1).map((c) => c.id)).toEqual(["c"]);
  });
});

describe("searchConnections", () => {
  test("matches name, host, tags, group and notes case-insensitively; empty query matches nothing", () => {
    const list = [
      conn({ id: "a", name: "web-1", host: "10.0.0.1", tags: ["prod"], groupPath: "/hk" }),
      conn({ id: "b", name: "db", host: "10.0.0.2", notes: "Postgres primary" })
    ];
    expect(searchConnections(list, "PROD", 10).map((c) => c.id)).toEqual(["a"]);
    expect(searchConnections(list, "postgres", 10).map((c) => c.id)).toEqual(["b"]);
    expect(searchConnections(list, "/hk", 10).map((c) => c.id)).toEqual(["a"]);
    expect(searchConnections(list, "10.0.0", 1)).toHaveLength(1);
    expect(searchConnections(list, "  ", 10)).toEqual([]);
  });
});
