import { describe, expect, test } from "vitest";
import { resourceCopyConnectionSchema } from "./contracts";

const FOLDER_ID = "44444444-4444-4444-8444-444444444444";

describe("resourceCopyConnectionSchema", () => {
  test("accepts targetFolderId as the copy landing folder", () => {
    const parsed = resourceCopyConnectionSchema.safeParse({
      sourceId: "src-1",
      targetOriginKind: "cloud",
      targetWorkspaceId: "ws-1",
      targetFolderId: FOLDER_ID
    });
    expect(parsed.success).toBe(true);
    if (parsed.success) {
      expect(parsed.data.targetFolderId).toBe(FOLDER_ID);
    }
  });

  // 旧调用方还在传路径字符串，通道不能因为新增字段就把它们拒掉。
  test("still accepts the legacy targetGroupSubPath", () => {
    const parsed = resourceCopyConnectionSchema.safeParse({
      sourceId: "src-1",
      targetOriginKind: "local",
      targetGroupSubPath: "legacy"
    });
    expect(parsed.success).toBe(true);
    if (parsed.success) {
      expect(parsed.data.targetGroupSubPath).toBe("legacy");
      expect(parsed.data.targetFolderId).toBeUndefined();
    }
  });

  test("rejects a malformed targetFolderId rather than falling back to the root", () => {
    const parsed = resourceCopyConnectionSchema.safeParse({
      sourceId: "src-1",
      targetOriginKind: "local",
      targetFolderId: "prod"
    });
    expect(parsed.success).toBe(false);
  });
});
