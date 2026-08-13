import { describe, expect, test } from "vitest";
import {
  connectionFolderCreateSchema,
  connectionFolderListSchema,
  connectionFolderMoveSchema,
  connectionFolderRemoveSchema,
  connectionFolderRenameSchema,
  connectionFolderReorderSchema
} from "./contracts";

const UUID = "11111111-1111-4111-8111-111111111111";
const OTHER_UUID = "22222222-2222-4222-8222-222222222222";

describe("connectionFolderListSchema", () => {
  test("accepts an empty payload as 'every scope'", () => {
    expect(connectionFolderListSchema.parse({})).toEqual({});
  });

  test("rejects a blank scopeKey rather than silently listing everything", () => {
    expect(connectionFolderListSchema.safeParse({ scopeKey: "   " }).success).toBe(false);
  });
});

describe("connectionFolderCreateSchema", () => {
  test("trims the name and defaults to the top level", () => {
    const parsed = connectionFolderCreateSchema.parse({
      scopeKey: "local-default",
      name: "  prod  "
    });
    expect(parsed.name).toBe("prod");
    expect(parsed.parentId).toBeUndefined();
  });

  test("accepts a parent and an explicit sort index", () => {
    const parsed = connectionFolderCreateSchema.parse({
      scopeKey: "local-default",
      name: "db",
      parentId: UUID,
      sortIndex: 2
    });
    expect(parsed).toMatchObject({ parentId: UUID, sortIndex: 2 });
  });

  test.each(["", "   ", "a/b", "a\\b"])("rejects the unusable name %j", (name) => {
    expect(
      connectionFolderCreateSchema.safeParse({ scopeKey: "local-default", name }).success
    ).toBe(false);
  });

  test("rejects a missing scope and a non-uuid parent", () => {
    expect(connectionFolderCreateSchema.safeParse({ name: "prod" }).success).toBe(false);
    expect(
      connectionFolderCreateSchema.safeParse({
        scopeKey: "local-default",
        name: "prod",
        parentId: "not-a-uuid"
      }).success
    ).toBe(false);
  });

  test("rejects a negative sort index", () => {
    expect(
      connectionFolderCreateSchema.safeParse({
        scopeKey: "local-default",
        name: "prod",
        sortIndex: -1
      }).success
    ).toBe(false);
  });
});

describe("connectionFolderRenameSchema", () => {
  test("trims the new name", () => {
    expect(connectionFolderRenameSchema.parse({ id: UUID, name: " staging " }).name).toBe(
      "staging"
    );
  });

  test("rejects separators so the derived path stays unambiguous", () => {
    expect(connectionFolderRenameSchema.safeParse({ id: UUID, name: "a/b" }).success).toBe(false);
  });
});

describe("connectionFolderMoveSchema", () => {
  test("treats an absent parentId as 'move to the top level'", () => {
    expect(connectionFolderMoveSchema.parse({ id: UUID }).parentId).toBeUndefined();
  });

  test("accepts a uuid parent and rejects anything else", () => {
    expect(connectionFolderMoveSchema.parse({ id: UUID, parentId: OTHER_UUID }).parentId).toBe(
      OTHER_UUID
    );
    expect(connectionFolderMoveSchema.safeParse({ id: UUID, parentId: "root" }).success).toBe(
      false
    );
  });
});

describe("connectionFolderReorderSchema", () => {
  test("coerces a numeric string index", () => {
    expect(connectionFolderReorderSchema.parse({ id: UUID, sortIndex: "4" }).sortIndex).toBe(4);
  });

  test("rejects a fractional index", () => {
    expect(connectionFolderReorderSchema.safeParse({ id: UUID, sortIndex: 1.5 }).success).toBe(
      false
    );
  });
});

describe("connectionFolderRemoveSchema", () => {
  test("requires a uuid", () => {
    expect(connectionFolderRemoveSchema.parse({ id: UUID }).id).toBe(UUID);
    expect(connectionFolderRemoveSchema.safeParse({ id: "1" }).success).toBe(false);
  });
});
