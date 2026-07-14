import { describe, expect, test } from "bun:test";
import type { ConnectionProfile } from "@nextshell/core";
import {
  buildConnectionSearchIndex,
  buildManagerTreeResult,
  collectGroupLeafIds,
  countMgrLeaves
} from "./utils/tree";

const makeConnection = (overrides: Partial<ConnectionProfile>): ConnectionProfile => ({
  id: "11111111-1111-4111-8111-111111111111",
  name: "alpha",
  host: "alpha.example.com",
  port: 22,
  username: "root",
  authType: "password",
  strictHostKeyChecking: false,
  terminalEncoding: "utf-8",
  backspaceMode: "ascii-backspace",
  deleteMode: "vt220-delete",
  groupPath: "/server/default",
  tags: [],
  favorite: false,
  monitorSession: false,
  createdAt: "2026-03-15T00:00:00.000Z",
  updatedAt: "2026-03-15T00:00:00.000Z",
  ...overrides
});

describe("ConnectionManagerModal search helpers", () => {
  test("searches host, connection name, folder names, and notes only", () => {
    const alpha = makeConnection({
      id: "11111111-1111-4111-8111-111111111111",
      name: "Alpha Gateway",
      host: "10.20.30.40",
      port: 2222,
      username: "deploy-user",
      groupPath: "/server/prod/api",
      tags: ["tag-only-hit"],
      notes: "owned by database team"
    });
    const beta = makeConnection({
      id: "22222222-2222-4222-8222-222222222222",
      name: "Beta Worker",
      host: "worker.example.com",
      username: "root",
      groupPath: "/server/staging",
      notes: "background jobs"
    });
    const connections = [alpha, beta];
    const searchIndex = buildConnectionSearchIndex(connections);
    const search = (keyword: string) =>
      collectGroupLeafIds(
        buildManagerTreeResult(connections, keyword, [], undefined, searchIndex).tree
      );

    expect(search("10.20")).toEqual([alpha.id]);
    expect(search("gateway")).toEqual([alpha.id]);
    expect(search("prod api")).toEqual([alpha.id]);
    expect(search("database team")).toEqual([alpha.id]);
    expect(search("deploy-user")).toEqual([]);
    expect(search("tag-only-hit")).toEqual([]);
    expect(search("2222")).toEqual([]);
  });

  test("caps broad search results while retaining total match count", () => {
    const connections = Array.from({ length: 5 }, (_, index) =>
      makeConnection({
        id: `connection-${index}`,
        name: `prod-${index}`,
        host: `10.0.0.${index}`,
        groupPath: "/server/prod"
      })
    );
    const result = buildManagerTreeResult(
      connections,
      "prod",
      [],
      undefined,
      buildConnectionSearchIndex(connections),
      2
    );

    expect(result.totalMatches).toBe(5);
    expect(result.visibleMatches).toBe(2);
    expect(result.limited).toBe(true);
    expect(countMgrLeaves(result.tree)).toBe(2);
    expect(collectGroupLeafIds(result.tree)).toEqual(["connection-0", "connection-1"]);
  });
});
