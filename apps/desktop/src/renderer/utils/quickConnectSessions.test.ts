import { describe, expect, it } from "vitest";
import type { ConnectionProfile, SessionDescriptor } from "@nextshell/core";
import { buildQuickConnectSessionResults } from "./quickConnectSessions";

function makeConnection(overrides: Partial<ConnectionProfile> & { id: string }): ConnectionProfile {
  return {
    name: "conn",
    host: "host",
    port: 22,
    username: "root",
    authType: "password",
    strictHostKeyChecking: true,
    terminalEncoding: "utf-8",
    backspaceMode: "auto",
    groupPath: "/",
    tags: [],
    favorite: false,
    ...overrides
  } as ConnectionProfile;
}

function makeSession(overrides: Partial<SessionDescriptor> & { id: string }): SessionDescriptor {
  return {
    target: "remote",
    title: "session",
    status: "connected",
    type: "terminal",
    createdAt: new Date().toISOString(),
    reconnectable: true,
    ...overrides
  } as SessionDescriptor;
}

describe("buildQuickConnectSessionResults", () => {
  const connA = makeConnection({
    id: "conn-a",
    name: "Prod Web",
    host: "1.2.3.4",
    username: "deploy"
  });
  const connB = makeConnection({
    id: "conn-b",
    name: "Staging DB",
    host: "db.internal",
    username: "root"
  });
  const connections = [connA, connB];

  const sA = makeSession({ id: "s-a", title: "Prod Web #1", connectionId: "conn-a" });
  const sB = makeSession({ id: "s-b", title: "Staging DB #1", connectionId: "conn-b" });
  const sC = makeSession({
    id: "s-c",
    title: "Prod Web #2",
    connectionId: "conn-a",
    status: "disconnected"
  });
  const sessions = [sA, sB, sC];

  it("returns sessions in MRU order, skipping the active session, when keyword is empty", () => {
    const result = buildQuickConnectSessionResults({
      sessions,
      sessionMruIds: ["s-c", "s-a", "s-b"],
      activeSessionId: "s-a",
      connections,
      keyword: ""
    });

    expect(result.map((r) => r.session.id)).toEqual(["s-c", "s-b"]);
    expect(result[0]?.connection?.id).toBe("conn-a");
  });

  it("caps empty-keyword results and appends sessions missing from MRU list", () => {
    const manySessions = Array.from({ length: 12 }, (_, i) =>
      makeSession({ id: `sess-${i}`, title: `Sess ${i}` })
    );
    const result = buildQuickConnectSessionResults({
      sessions: manySessions,
      sessionMruIds: [],
      activeSessionId: undefined,
      connections: [],
      keyword: ""
    });

    expect(result).toHaveLength(8);
    expect(result[0]?.session.id).toBe("sess-0");
  });

  it("filters by session title and connection host/name when typing", () => {
    const result = buildQuickConnectSessionResults({
      sessions,
      sessionMruIds: [],
      activeSessionId: undefined,
      connections,
      keyword: "db.internal"
    });

    expect(result.map((r) => r.session.id)).toEqual(["s-b"]);
  });

  it("keeps the active session in filtered (typing) results", () => {
    const result = buildQuickConnectSessionResults({
      sessions,
      sessionMruIds: [],
      activeSessionId: "s-a",
      connections,
      keyword: "prod"
    });

    expect(result.map((r) => r.session.id).sort()).toEqual(["s-a", "s-c"]);
    expect(result.find((r) => r.session.id === "s-a")?.isActive).toBe(true);
  });
});
