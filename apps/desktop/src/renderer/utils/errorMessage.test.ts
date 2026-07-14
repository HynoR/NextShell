import { describe, expect, test } from "bun:test";
import { formatErrorMessage } from "./errorMessage";

describe("formatErrorMessage", () => {
  test("removes Electron invoke wrappers with a channel name", () => {
    expect(
      formatErrorMessage(
        new Error("Error invoking remote method 'nextshell:sftp:list': Error: Permission denied")
      )
    ).toBe("权限不足：Permission denied");
  });

  test("removes the wrapper variant without a channel name", () => {
    expect(formatErrorMessage("Error invoking remote method: Error: 连接配置不存在")).toBe(
      "连接配置不存在"
    );
  });
});
