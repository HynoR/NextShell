import { describe, expect, test } from "bun:test";
import type { ConnectionExportFile } from "@nextshell/core";
import { parseFinalShellImport, parseNextShellImport } from "./import-export";

const createNextShellExport = (groupPath: string): ConnectionExportFile => ({
  format: "nextshell-connections",
  version: 1,
  exportedAt: "2026-06-15T00:00:00.000Z",
  connections: [
    {
      name: "prod-a",
      host: "10.0.0.1",
      port: 22,
      username: "root",
      authType: "password",
      groupPath,
      tags: [],
      favorite: false,
      terminalEncoding: "utf-8",
      backspaceMode: "ascii-backspace",
      deleteMode: "vt220-delete",
      monitorSession: false
    }
  ]
});

describe("connection import parsers", () => {
  test("single NextShell import remaps exported groupPath into import zone", () => {
    const entries = parseNextShellImport(createNextShellExport("/server/prod"));

    expect(entries[0]?.groupPath).toBe("/import/prod");
  });

  test("directory NextShell import uses directory-derived groupPath", () => {
    const entries = parseNextShellImport(createNextShellExport("/server/prod"), {
      groupPathOverride: "/import/customer-a/prod"
    });

    expect(entries[0]?.groupPath).toBe("/import/customer-a/prod");
  });

  test("directory FinalShell import uses directory-derived groupPath", () => {
    const entries = parseFinalShellImport(
      {
        name: "legacy-a",
        host: "10.0.0.2",
        port: 22,
        user_name: "admin",
        authentication_type: 2
      },
      {
        groupPathOverride: "/import/legacy/prod"
      }
    );

    expect(entries[0]?.groupPath).toBe("/import/legacy/prod");
    expect(entries[0]?.sourceFormat).toBe("finalshell");
  });
});
