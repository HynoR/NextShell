import { describe, expect, test } from "vitest";
import { recycleBinRestoreSchema } from "./contracts";

describe("recycleBinRestoreSchema", () => {
  test("leaves the target unset so the entry's own scope decides", () => {
    const parsed = recycleBinRestoreSchema.parse({ recycleBinEntryId: "entry-1" });
    expect(parsed.targetOriginKind).toBeUndefined();
    expect(parsed.targetWorkspaceId).toBeUndefined();
  });

  test("still accepts an explicit override", () => {
    expect(
      recycleBinRestoreSchema.parse({
        recycleBinEntryId: "entry-1",
        targetOriginKind: "cloud",
        targetWorkspaceId: "ws-1"
      })
    ).toMatchObject({ targetOriginKind: "cloud", targetWorkspaceId: "ws-1" });
  });

  test("rejects a blank entry id and an unknown origin kind", () => {
    expect(recycleBinRestoreSchema.safeParse({ recycleBinEntryId: "  " }).success).toBe(false);
    expect(
      recycleBinRestoreSchema.safeParse({ recycleBinEntryId: "e", targetOriginKind: "remote" })
        .success
    ).toBe(false);
  });
});
