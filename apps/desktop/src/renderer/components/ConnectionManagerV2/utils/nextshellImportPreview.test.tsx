import { describe, expect, test } from "vitest";
import { CONNECTION_IMPORT_DECRYPT_PROMPT_PREFIX } from "../../../../../../../packages/shared/src/index";
import { buildNextShellImportPreviewQueue, getImportFileName } from "./nextshellImportPreview";

const createEntry = (name: string, host: string, username: string) => ({
  name,
  host,
  port: 22,
  username,
  authType: "password" as const,
  groupPath: "/import",
  tags: [],
  favorite: false,
  terminalEncoding: "utf-8" as const,
  backspaceMode: "ascii-backspace" as const,
  deleteMode: "vt220-delete" as const,
  monitorSession: false,
  sourceFormat: "nextshell" as const
});

describe("nextshell import preview queue", () => {
  test("collects valid preview entries into the queue", async () => {
    const result = await buildNextShellImportPreviewQueue({
      filePaths: ["/tmp/alpha.json"],
      importPreview: async ({ filePath }) => [createEntry(filePath, "127.0.0.1", "root")],
      promptImportDecryptionPassword: async () => {
        throw new Error("should not prompt");
      }
    });

    expect(result.queue.length).toBe(1);
    expect(result.queue[0]).toMatchObject({
      fileName: "alpha.json"
    });
    expect(result.warnings).toEqual([]);
  });

  test("keeps importing remaining files when one file is unsupported", async () => {
    const result = await buildNextShellImportPreviewQueue({
      filePaths: ["/tmp/bad.json", "/tmp/good.json"],
      importPreview: async ({ filePath }) => {
        if (filePath.endsWith("bad.json")) {
          throw new Error("该文件不是 NextShell 导出格式");
        }
        return [createEntry("good", "10.0.0.1", "admin")];
      },
      promptImportDecryptionPassword: async () => null
    });

    expect(result.queue.length).toBe(1);
    expect(result.queue[0]?.fileName).toBe("good.json");
    expect(result.warnings).toEqual(["bad.json：该文件不是 NextShell 导出格式"]);
  });

  test("retries encrypted files after prompting for a password", async () => {
    const calls: Array<{ filePath: string; decryptionPassword?: string }> = [];

    const result = await buildNextShellImportPreviewQueue({
      filePaths: ["/tmp/secure.json"],
      importPreview: async (payload) => {
        calls.push(payload);
        if (!payload.decryptionPassword) {
          throw new Error(`${CONNECTION_IMPORT_DECRYPT_PROMPT_PREFIX} 请输入密码`);
        }
        return [createEntry("secure", "10.0.0.2", "ops")];
      },
      promptImportDecryptionPassword: async (fileName, promptText) => {
        expect(fileName).toBe("secure.json");
        expect(promptText).toBe("请输入密码");
        return "secret123";
      }
    });

    expect(calls).toEqual([
      { filePath: "/tmp/secure.json", decryptionPassword: undefined },
      { filePath: "/tmp/secure.json", decryptionPassword: "secret123" }
    ]);
    expect(result.queue.length).toBe(1);
    expect(result.warnings).toEqual([]);
  });

  test("skips encrypted files when the user cancels decryption", async () => {
    const result = await buildNextShellImportPreviewQueue({
      filePaths: ["/tmp/secure.json"],
      importPreview: async () => {
        throw new Error(`${CONNECTION_IMPORT_DECRYPT_PROMPT_PREFIX} 请输入密码`);
      },
      promptImportDecryptionPassword: async () => null
    });

    expect(result.queue).toEqual([]);
    expect(result.warnings).toEqual(["secure.json：用户取消解密，已跳过该文件"]);
  });

  test("derives filenames from both posix and windows paths", () => {
    expect(getImportFileName("/tmp/a.json")).toBe("a.json");
    expect(getImportFileName("C:\\temp\\b.json")).toBe("b.json");
  });
});
