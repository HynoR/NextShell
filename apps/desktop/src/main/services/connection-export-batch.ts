import fs from "node:fs";
import path from "node:path";
import type {
  ConnectionExportFile,
  ExportedConnection
} from "../../../../../packages/core/src/index";
import type { ConnectionExportBatchResult } from "../../../../../packages/shared/src/index";
import { encryptConnectionExportPayload } from "./connection-export-crypto";
import {
  buildBaseFileName,
  resolveUniqueFileName,
  sanitizeFileName
} from "./connection-export-filename";

const ENCRYPTED_EXPORT_PREFIX = "b64##";

export interface BatchExportConnectionIdentity {
  id: string;
  name: string;
  host: string;
  port: number;
}

interface ExportConnectionsBatchToDirectoryOptions<
  TConnection extends BatchExportConnectionIdentity
> {
  connections: TConnection[];
  directoryPath: string;
  encryptionPassword?: string;
  buildExportedConnection: (connection: TConnection) => Promise<ExportedConnection>;
}

export const exportConnectionsBatchToDirectory = async <
  TConnection extends BatchExportConnectionIdentity
>(
  options: ExportConnectionsBatchToDirectoryOptions<TConnection>
): Promise<ConnectionExportBatchResult> => {
  const encrypted = typeof options.encryptionPassword === "string";
  const targetPath = path.resolve(options.directoryPath);

  const result: ConnectionExportBatchResult = {
    total: options.connections.length,
    exported: 0,
    failed: 0,
    encrypted,
    directoryPath: targetPath,
    files: [],
    errors: []
  };

  if (options.connections.length === 0) {
    return result;
  }

  let directoryAvailable = false;
  try {
    const stat = fs.statSync(targetPath);
    directoryAvailable = stat.isDirectory();
  } catch {
    directoryAvailable = false;
  }

  if (!directoryAvailable) {
    for (const connection of options.connections) {
      result.failed += 1;
      result.errors.push(`${connection.name}(${connection.host}:${connection.port}): 导出目录不可用`);
    }
    return result;
  }

  const reservedFileNames = new Set<string>();
  const existsInDir = (candidate: string): boolean => {
    if (reservedFileNames.has(candidate)) {
      return true;
    }
    return fs.existsSync(path.join(targetPath, candidate));
  };

  for (const connection of options.connections) {
    const baseFileName = buildBaseFileName({ name: connection.name, host: connection.host });
    const desiredName = sanitizeFileName(baseFileName);
    const finalFileName = resolveUniqueFileName(desiredName, existsInDir);
    const filePath = path.join(targetPath, finalFileName);

    try {
      const exportedConnection = await options.buildExportedConnection(connection);
      const exportFile: ConnectionExportFile = {
        format: "nextshell-connections",
        version: 1,
        exportedAt: new Date().toISOString(),
        connections: [exportedConnection]
      };
      const plainJson = JSON.stringify(exportFile, null, 2);
      const fileContent = encrypted
        ? `${ENCRYPTED_EXPORT_PREFIX}${encryptConnectionExportPayload(
            plainJson,
            options.encryptionPassword as string
          )}`
        : plainJson;

      fs.writeFileSync(filePath, fileContent, "utf-8");
      reservedFileNames.add(finalFileName);
      result.exported += 1;
      result.files.push({
        connectionId: connection.id,
        filePath,
        fileName: finalFileName
      });
    } catch (error) {
      result.failed += 1;
      const reason = error instanceof Error ? error.message : "未知错误";
      result.errors.push(`${connection.name}(${connection.host}:${connection.port}): ${reason}`);
    }
  }

  return result;
};
