import { describe, expect, test } from "vitest";
import { persistConnectionWrite } from "./useConnectionManager.helpers";

describe("persistConnectionWrite", () => {
  test("returns the write result on the success path", async () => {
    const recovered: string[] = [];
    const result = await persistConnectionWrite(
      async () => "saved",
      () => {
        recovered.push("recover");
      }
    );

    expect(result).toBe("saved");
    expect(recovered).toEqual([]);
  });

  test("recovers then rethrows so callers do not toast success", async () => {
    const recovered: string[] = [];
    const failure = new Error("禁止跨来源引用 SSH 密钥");

    await expect(
      persistConnectionWrite(
        async () => {
          throw failure;
        },
        () => {
          recovered.push("recover");
        }
      )
    ).rejects.toThrow("禁止跨来源引用 SSH 密钥");

    expect(recovered).toEqual(["recover"]);
  });
});
