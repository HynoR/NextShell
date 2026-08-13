import fsp from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { describe, expect, test } from "vitest";
import {
  buildImportGroupPathFromRelativeFile,
  scanConnectionImportDirectory
} from "./connection-import-directory";

describe("connection import directory scanning", () => {
  // 目录结构原样保留，不再套一层 /import：归属由导入时选的目标目录(folderId)决定，
  // groupPath 只是给云同步/MCP/导出读的投影。
  test("mirrors the source directory structure", () => {
    expect(buildImportGroupPathFromRelativeFile("alpha.json")).toBe("/");
    expect(buildImportGroupPathFromRelativeFile("prod/alpha.json")).toBe("/prod");
    expect(buildImportGroupPathFromRelativeFile("客户A/prod/alpha.json")).toBe("/客户A/prod");
  });

  test("recursively scans regular files and skips symbolic links", async () => {
    const root = await fsp.mkdtemp(path.join(os.tmpdir(), "nextshell-import-"));
    let symlinkCreated = false;
    try {
      await fsp.mkdir(path.join(root, "prod"), { recursive: true });
      await fsp.writeFile(path.join(root, "root.json"), "{}", "utf-8");
      await fsp.writeFile(path.join(root, "prod", "web.json"), "{}", "utf-8");
      try {
        await fsp.symlink(path.join(root, "root.json"), path.join(root, "linked.json"));
        symlinkCreated = true;
      } catch {
        symlinkCreated = false;
      }

      const result = await scanConnectionImportDirectory(root);

      expect(result.files.map((file) => file.relativePath)).toEqual(["prod/web.json", "root.json"]);
      expect(result.files.map((file) => file.groupPath)).toEqual(["/prod", "/"]);
      if (symlinkCreated) {
        expect(result.warnings.some((warning) => warning.includes("linked.json"))).toBe(true);
      }
    } finally {
      await fsp.rm(root, { recursive: true, force: true });
    }
  });
});
