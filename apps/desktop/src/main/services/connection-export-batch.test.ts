import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { decryptConnectionExportPayload } from "./connection-export-crypto";
import {
  type BatchExportConnectionIdentity,
  exportConnectionsBatchToDirectory
} from "./connection-export-batch";

interface MockConnection extends BatchExportConnectionIdentity {
  username: string;
}

const assertEqual = <T>(actual: T, expected: T, message: string): void => {
  if (actual !== expected) {
    throw new Error(`${message}: expected "${String(expected)}", got "${String(actual)}"`);
  }
};

const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

const createTempDir = (): string => {
  return fs.mkdtempSync(path.join(os.tmpdir(), "nextshell-export-batch-test-"));
};

const removeDir = (target: string): void => {
  try {
    fs.rmSync(target, { recursive: true, force: true });
  } catch {
    // Ignore cleanup errors in tests.
  }
};

const toExportedConnection = async (connection: MockConnection) => {
  return {
    name: connection.name,
    host: connection.host,
    port: connection.port,
    username: connection.username,
    authType: "agent" as const,
    password: undefined,
    groupPath: ["default"],
    tags: [],
    notes: undefined,
    favorite: false,
    terminalEncoding: "utf-8" as const,
    backspaceMode: "ascii-backspace" as const,
    deleteMode: "vt220-delete" as const,
    monitorSession: false
  };
};

const parsePlainJsonFile = (filePath: string): unknown => {
  return JSON.parse(fs.readFileSync(filePath, "utf-8"));
};

await (async () => {
  const exportDir = createTempDir();
  try {
    const connections: MockConnection[] = [
      { id: "c1", name: "web", host: "10.0.0.1", port: 22, username: "root" },
      { id: "c2", name: "db", host: "10.0.0.2", port: 22, username: "root" },
      { id: "c3", name: "cache", host: "10.0.0.3", port: 22, username: "root" }
    ];
    const result = await exportConnectionsBatchToDirectory({
      connections,
      directoryPath: exportDir,
      buildExportedConnection: toExportedConnection
    });

    assertEqual(result.total, 3, "success path total");
    assertEqual(result.exported, 3, "success path exported");
    assertEqual(result.failed, 0, "success path failed");
    assertEqual(result.files.length, 3, "success path file list");

    for (const file of result.files) {
      const parsed = parsePlainJsonFile(file.filePath) as { format?: string; connections?: unknown[] };
      assertEqual(parsed.format, "nextshell-connections", "exported file format");
      assert(Array.isArray(parsed.connections), "exported file should include connections array");
      assertEqual(parsed.connections?.length ?? 0, 1, "single-connection export file");
    }
  } finally {
    removeDir(exportDir);
  }
})();

await (async () => {
  const exportDir = createTempDir();
  try {
    const veryLongName = "x".repeat(300);
    const connections: MockConnection[] = [
      { id: "ok-1", name: "normal", host: "10.1.1.1", port: 22, username: "root" },
      { id: "bad-1", name: veryLongName, host: "10.1.1.2", port: 22, username: "root" }
    ];

    const result = await exportConnectionsBatchToDirectory({
      connections,
      directoryPath: exportDir,
      buildExportedConnection: toExportedConnection
    });

    assertEqual(result.total, 2, "partial-failure total");
    assertEqual(result.exported, 1, "partial-failure exported");
    assertEqual(result.failed, 1, "partial-failure failed");
    assert(result.errors.length >= 1, "partial-failure should include errors");
  } finally {
    removeDir(exportDir);
  }
})();

await (async () => {
  const exportDir = createTempDir();
  try {
    const result = await exportConnectionsBatchToDirectory({
      connections: [
        { id: "enc-1", name: "secure", host: "10.2.2.2", port: 22, username: "root" }
      ],
      directoryPath: exportDir,
      encryptionPassword: "123456",
      buildExportedConnection: toExportedConnection
    });

    assertEqual(result.exported, 1, "encrypted export count");
    const filePath = result.files[0]?.filePath;
    assert(typeof filePath === "string", "encrypted export file path should exist");
    const raw = fs.readFileSync(filePath as string, "utf-8");
    assert(raw.startsWith("b64##"), "encrypted file should start with b64##");

    const decrypted = decryptConnectionExportPayload(raw.slice("b64##".length), "123456");
    const parsed = JSON.parse(decrypted) as { format?: string; connections?: unknown[] };
    assertEqual(parsed.format, "nextshell-connections", "decrypted file format");
    assertEqual(parsed.connections?.length ?? 0, 1, "decrypted single-connection payload");
  } finally {
    removeDir(exportDir);
  }
})();
