import { describe, expect, test } from "vitest";
import {
  CLOUD_SYNC_WORKSPACE_PASSWORD_MIN_LENGTH,
  cloudSyncSyncNowSchema,
  cloudSyncTestConnectionSchema,
  cloudSyncWorkspaceAddSchema,
  cloudSyncWorkspaceTokenDraftSchema,
  cloudSyncWorkspaceUpdateSchema
} from "./contracts";

const base = {
  apiBaseUrl: "https://sync.example.com",
  workspaceName: "team"
};

describe("cloud sync workspace password length", () => {
  const shortPassword = "a".repeat(CLOUD_SYNC_WORKSPACE_PASSWORD_MIN_LENGTH - 1);
  const okPassword = "a".repeat(CLOUD_SYNC_WORKSPACE_PASSWORD_MIN_LENGTH);

  test("workspaceAdd / testConnection reject passwords shorter than the server minimum", () => {
    expect(
      cloudSyncWorkspaceAddSchema.safeParse({ ...base, workspacePassword: shortPassword }).success
    ).toBe(false);
    expect(
      cloudSyncTestConnectionSchema.safeParse({ ...base, workspacePassword: shortPassword }).success
    ).toBe(false);
    expect(
      cloudSyncWorkspaceAddSchema.safeParse({ ...base, workspacePassword: okPassword }).success
    ).toBe(true);
  });

  test("workspaceUpdate allows omitting the password but still enforces the minimum", () => {
    expect(cloudSyncWorkspaceUpdateSchema.safeParse({ id: "ws-1", ...base }).success).toBe(true);
    expect(
      cloudSyncWorkspaceUpdateSchema.safeParse({
        id: "ws-1",
        ...base,
        workspacePassword: shortPassword
      }).success
    ).toBe(false);
  });

  test("token draft stays lenient so pasted tokens can still be parsed and shown", () => {
    expect(
      cloudSyncWorkspaceTokenDraftSchema.safeParse({
        ...base,
        displayName: "",
        workspacePassword: shortPassword,
        pullIntervalSec: 300,
        ignoreTlsErrors: false,
        enabled: true
      }).success
    ).toBe(true);
  });
});

describe("cloudSyncSyncNowSchema", () => {
  test("accepts an explicit LWW decision", () => {
    expect(cloudSyncSyncNowSchema.parse({ workspaceId: "ws-1", mode: "local-wins" })).toEqual({
      workspaceId: "ws-1",
      mode: "local-wins"
    });
  });

  test("keeps omitted mode in automatic divergence detection", () => {
    expect(cloudSyncSyncNowSchema.parse({ workspaceId: "ws-1" })).toEqual({ workspaceId: "ws-1" });
    expect(cloudSyncSyncNowSchema.safeParse({ mode: "merge" }).success).toBe(false);
  });
});
