import { describe, expect, test } from "vitest";
import { cloudSyncSyncNowSchema } from "./contracts";

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
