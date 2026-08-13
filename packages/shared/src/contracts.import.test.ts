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
});
