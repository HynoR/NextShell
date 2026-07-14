import fsp from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { describe, expect, test } from "bun:test";
import {
  buildImportGroupPathFromRelativeFile,
  scanConnectionImportDirectory
} from "./connection-import-directory";

describe("connection import directory scanning", () => {
  test("maps relative file directories into the import zone without selected root", () => {
    expect(buildImportGroupPathFromRelativeFile("alpha.json")).toBe("/import");
    expect(buildImportGroupPathFromRelativeFile("prod/alpha.json")).toBe("/import/prod");
    expect(buildImportGroupPathFromRelativeFile("客户A/prod/alpha.json")).toBe(
      "/import/客户A/prod"
    );
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
      expect(result.files.map((file) => file.groupPath)).toEqual(["/import/prod", "/import"]);
      if (symlinkCreated) {
        expect(result.warnings.some((warning) => warning.includes("linked.json"))).toBe(true);
      }
    } finally {
      await fsp.rm(root, { recursive: true, force: true });
    }
  });
});
