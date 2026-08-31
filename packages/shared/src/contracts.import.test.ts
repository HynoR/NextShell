import { describe, expect, test } from "vitest";
import { connectionImportExecuteSchema } from "./contracts";

const baseEntry = {
  name: "prod-a",
  host: "10.0.0.1",
  port: 22,
  username: "root",
  authType: "privateKey" as const,
  groupPath: "/import/prod",
  tags: [],
  favorite: false,
  monitorSession: false
};

describe("connectionImportExecuteSchema", () => {
  test("accepts an optional sshKeyId so preview can rebind a key", () => {
    const parsed = connectionImportExecuteSchema.safeParse({
      entries: [
        {
          ...baseEntry,
          sshKeyId: "11111111-1111-4111-8111-111111111111",
          sshKeyRef: { name: "ops-ed25519", fingerprint: "SHA256:abc" }
        }
      ],
      conflictPolicy: "skip"
    });
    expect(parsed.success).toBe(true);
    if (parsed.success) {
      expect(parsed.data.entries[0]?.sshKeyId).toBe("11111111-1111-4111-8111-111111111111");
      expect(parsed.data.entries[0]?.sshKeyRef).toEqual({
        name: "ops-ed25519",
        fingerprint: "SHA256:abc"
      });
    }
  });

  test("rejects a malformed sshKeyId", () => {
    const parsed = connectionImportExecuteSchema.safeParse({
      entries: [{ ...baseEntry, sshKeyId: "not-a-uuid" }],
      conflictPolicy: "skip"
    });
    expect(parsed.success).toBe(false);
  });

  test("accepts an optional targetFolderId as the import landing folder", () => {
    const parsed = connectionImportExecuteSchema.safeParse({
      entries: [baseEntry],
      conflictPolicy: "skip",
      targetFolderId: "33333333-3333-4333-8333-333333333333"
    });
    expect(parsed.success).toBe(true);
    if (parsed.success) {
      expect(parsed.data.targetFolderId).toBe("33333333-3333-4333-8333-333333333333");
    }
  });

  test("defaults targetFolderId to undefined so imports land at the root", () => {
    const parsed = connectionImportExecuteSchema.safeParse({
      entries: [baseEntry],
      conflictPolicy: "skip"
    });
    expect(parsed.success).toBe(true);
    if (parsed.success) {
      expect(parsed.data.targetFolderId).toBeUndefined();
    }
  });

  // 目录扫描导入的 groupPath 是磁盘相对路径,没有线格式前缀;省略该字段时按导出文件的
  // 线格式处理,这是老调用方的既有行为。
  test("carries the groupPath provenance so the importer knows whether to strip the wire prefix", () => {
    const literal = connectionImportExecuteSchema.safeParse({
      entries: [baseEntry],
      conflictPolicy: "skip",
      groupPathFormat: "literal"
    });
    expect(literal.success).toBe(true);
    if (literal.success) {
      expect(literal.data.groupPathFormat).toBe("literal");
    }

    const omitted = connectionImportExecuteSchema.safeParse({
      entries: [baseEntry],
      conflictPolicy: "skip"
    });
    expect(omitted.success).toBe(true);
    if (omitted.success) {
      expect(omitted.data.groupPathFormat).toBeUndefined();
    }

    expect(
      connectionImportExecuteSchema.safeParse({
        entries: [baseEntry],
        conflictPolicy: "skip",
        groupPathFormat: "disk"
      }).success
    ).toBe(false);
  });

  test("rejects a malformed targetFolderId instead of silently dropping it", () => {
    const parsed = connectionImportExecuteSchema.safeParse({
      entries: [baseEntry],
      conflictPolicy: "skip",
      targetFolderId: "root"
    });
    expect(parsed.success).toBe(false);
  });
});
