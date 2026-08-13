import { describe, expect, test } from "vitest";
import type { CloudSyncWorkspaceProfile } from "@nextshell/core";
import { LOCAL_DEFAULT_SCOPE_KEY } from "@nextshell/core";
import { buildManagerScopes, resolveActiveScope } from "./scopes";

const workspace = (
  id: string,
  workspaceName: string,
  displayName = ""
): CloudSyncWorkspaceProfile => ({
  id,
  apiBaseUrl: "https://sync.example.com",
  workspaceName,
  displayName,
  pullIntervalSec: 60,
  ignoreTlsErrors: false,
  enabled: true,
  createdAt: "2026-01-01T00:00:00.000Z",
  updatedAt: "2026-01-01T00:00:00.000Z",
  lastSyncAt: null,
  lastError: null
});

describe("buildManagerScopes", () => {
  test("always starts with the local scope", () => {
    const scopes = buildManagerScopes([]);
    expect(scopes).toHaveLength(1);
    expect(scopes[0]).toMatchObject({ key: LOCAL_DEFAULT_SCOPE_KEY, kind: "local" });
  });

  test("derives cloud scope keys the same way the main process does", () => {
    const scopes = buildManagerScopes([workspace("ws-1", "team-a")]);
    expect(scopes[1]).toMatchObject({
      key: "sync.example.com-team-a",
      kind: "cloud",
      workspaceId: "ws-1"
    });
  });

  test("prefers the display name but falls back to the workspace name", () => {
    const scopes = buildManagerScopes([
      workspace("ws-1", "team-a", "Team A"),
      workspace("ws-2", "team-b")
    ]);
    expect(scopes.map((scope) => scope.label)).toEqual(["本地", "Team A", "team-b"]);
  });
});

describe("resolveActiveScope", () => {
  const scopes = buildManagerScopes([workspace("ws-1", "team-a")]);

  test("returns the selected scope", () => {
    expect(resolveActiveScope(scopes, "sync.example.com-team-a").workspaceId).toBe("ws-1");
  });

  test("falls back to local when the workspace has gone away", () => {
    // Unsubscribing or a revoked token removes the workspace while the selection lingers.
    expect(resolveActiveScope(scopes, "sync.example.com-removed").kind).toBe("local");
    expect(resolveActiveScope(scopes, undefined).kind).toBe("local");
  });

  test("never returns undefined even with an empty scope list", () => {
    expect(resolveActiveScope([], "anything").key).toBe(LOCAL_DEFAULT_SCOPE_KEY);
  });
});
