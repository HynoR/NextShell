import { describe, expect, test } from "vitest";
import { sshKeyGenerateSchema, sshKeyUsageSchema } from "./contracts";

const UUID = "11111111-1111-4111-8111-111111111111";

describe("sshKeyGenerateSchema", () => {
  test("defaults to ed25519 and leaves the comment out", () => {
    const parsed = sshKeyGenerateSchema.parse({ name: "deploy" });
    expect(parsed.algorithm).toBe("ed25519");
    expect(parsed.comment).toBeUndefined();
  });

  test("trims the name and the comment", () => {
    const parsed = sshKeyGenerateSchema.parse({
      name: "  deploy  ",
      comment: "  ops@laptop  ",
      algorithm: "rsa-4096"
    });
    expect(parsed).toMatchObject({ name: "deploy", comment: "ops@laptop", algorithm: "rsa-4096" });
  });

  test("treats a whitespace-only comment as absent", () => {
    expect(sshKeyGenerateSchema.parse({ name: "deploy", comment: "   " }).comment).toBeUndefined();
  });

  test("rejects a blank name and an unknown algorithm", () => {
    expect(sshKeyGenerateSchema.safeParse({ name: "   " }).success).toBe(false);
    expect(sshKeyGenerateSchema.safeParse({ name: "k", algorithm: "dsa" }).success).toBe(false);
  });
});

describe("sshKeyUsageSchema", () => {
  test("requires a uuid", () => {
    expect(sshKeyUsageSchema.parse({ id: UUID }).id).toBe(UUID);
    expect(sshKeyUsageSchema.safeParse({ id: "key-1" }).success).toBe(false);
  });
});
