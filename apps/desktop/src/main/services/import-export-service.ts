import fs from "node:fs";
import { BrowserWindow, dialog } from "electron";
import type { WebContents } from "electron";
import type {
  ConnectionExportFile,
  ConnectionImportEntry,
  ConnectionImportResult,
  ConnectionProfile,
  ExportedConnection,
} from "@nextshell/core";
import type {
  ConnectionExportInput,
  ConnectionExportBatchInput,
  ConnectionExportBatchResult,
  ConnectionImportPreviewInput,
  ConnectionImportFinalShellPreviewInput,
  ConnectionImportExecuteInput,
  ConnectionUpsertInput,
} from "@nextshell/shared";
import { CONNECTION_IMPORT_DECRYPT_PROMPT_PREFIX } from "@nextshell/shared";
import type { EncryptedSecretVault } from "@nextshell/security";
import type { CachedConnectionRepository } from "@nextshell/storage";
import {
  isFinalShellFormat,
  isNextShellFormat,
  parseFinalShellImport,
  parseNextShellImport,
} from "./import-export";
import {
  decryptConnectionExportPayload,
  encryptConnectionExportPayload,
  obfuscatePassword,
} from "./connection-export-crypto";
import { exportConnectionsBatchToDirectory } from "./connection-export-batch";

interface ImportExportServiceOptions {
  connections: CachedConnectionRepository;
  vault: EncryptedSecretVault;
  upsertConnection: (input: ConnectionUpsertInput) => Promise<ConnectionProfile>;
  appendAuditLogIfEnabled: (payload: {
    action: string;
    level: "info" | "warn" | "error";
    connectionId?: string;
    message: string;
    metadata?: Record<string, unknown>;
  }) => void;
}

const ENCRYPTED_EXPORT_PREFIX = "b64##";

const trimBomAndWhitespace = (value: string): string => value.replace(/^\uFEFF/, "").trim();

export class ImportExportService {
  private readonly connections: CachedConnectionRepository;
  private readonly vault: EncryptedSecretVault;
  private readonly upsertConnection: (input: ConnectionUpsertInput) => Promise<ConnectionProfile>;
  private readonly appendAuditLogIfEnabled: ImportExportServiceOptions["appendAuditLogIfEnabled"];

  constructor(options: ImportExportServiceOptions) {
    this.connections = options.connections;
    this.vault = options.vault;
    this.upsertConnection = options.upsertConnection;
    this.appendAuditLogIfEnabled = options.appendAuditLogIfEnabled;
  }

  // ---------------------------------------------------------------------------
  // Public methods
  // ---------------------------------------------------------------------------

  async exportConnections(
    sender: WebContents,
    input: ConnectionExportInput,
  ): Promise<{ ok: true; filePath: string } | { ok: false; canceled: true }> {
    const owner = BrowserWindow.fromWebContents(sender);
    const saveOptions = {
      title: "导出连接",
      defaultPath: "nextshell-connections.json",
      filters: [{ name: "JSON", extensions: ["json"] }],
    };
    const result = owner
      ? await dialog.showSaveDialog(owner, saveOptions)
      : await dialog.showSaveDialog(saveOptions);
    if (result.canceled || !result.filePath) {
      return { ok: false, canceled: true };
    }

    const allConnections = this.connections.list({});
    const idSet = new Set(input.connectionIds);
    const filtered = allConnections.filter((c) => idSet.has(c.id));

    const exportedConnections: ExportedConnection[] = [];
    for (const conn of filtered) {
      exportedConnections.push(await this.buildExportedConnection(conn));
    }

    const encryptionPassword = input.encryptionPassword;
    const encrypted = typeof encryptionPassword === "string";

    const exportedConnectionsFinal = encrypted
      ? exportedConnections
      : exportedConnections.map((c) => ({
          ...c,
          password:
            c.password !== undefined
              ? obfuscatePassword(c.password, c.name, c.host, c.port)
              : undefined,
        }));

    const exportFile: ConnectionExportFile = {
      format: "nextshell-connections",
      version: 1,
      exportedAt: new Date().toISOString(),
      ...(encrypted ? {} : { passwordsObfuscated: true }),
      connections: exportedConnectionsFinal,
    };

    const plainJson = JSON.stringify(exportFile, null, 2);
    const fileContent = encrypted
      ? `${ENCRYPTED_EXPORT_PREFIX}${await encryptConnectionExportPayload(plainJson, encryptionPassword)}`
      : plainJson;

    fs.writeFileSync(result.filePath, fileContent, "utf-8");

    this.appendAuditLogIfEnabled({
      action: "connection.export",
      level: "info",
      message: `Exported ${exportedConnections.length} connections`,
      metadata: { filePath: result.filePath, count: exportedConnections.length, encrypted },
    });

    return { ok: true, filePath: result.filePath };
  }

  async exportConnectionsBatch(
    input: ConnectionExportBatchInput,
  ): Promise<ConnectionExportBatchResult> {
    const allConnections = this.connections.list({});
    const idSet = new Set(input.connectionIds);
    const filtered = allConnections.filter((conn) => idSet.has(conn.id));

    const buildExportedConnection = this.buildExportedConnection.bind(this);
    const result = await exportConnectionsBatchToDirectory({
      connections: filtered,
      directoryPath: input.directoryPath,
      encryptionPassword: input.encryptionPassword,
      buildExportedConnection,
    });

    this.appendAuditLogIfEnabled({
      action: "connection.export.batch",
      level: "info",
      message: `Batch exported ${result.exported}/${result.total} connections`,
      metadata: { ...result },
    });

    return result;
  }

  async importConnectionsPreview(
    input: ConnectionImportPreviewInput,
  ): Promise<ConnectionImportEntry[]> {
    const raw = fs.readFileSync(input.filePath, "utf-8");
    const data = await this.parseImportPayloadText(raw, input.decryptionPassword);
    if (isNextShellFormat(data)) {
      return parseNextShellImport(data);
    }
    throw new Error(
      "该文件不是 NextShell 导出格式，请使用\u201c导入 FinalShell 文件\u201d按钮导入 FinalShell 配置",
    );
  }

  async importFinalShellConnectionsPreview(
    input: ConnectionImportFinalShellPreviewInput,
  ): Promise<ConnectionImportEntry[]> {
    const raw = fs.readFileSync(input.filePath, "utf-8");
    const data = parseJsonPayloadText(raw);
    if (!isFinalShellFormat(data)) {
      throw new Error("该文件不是 FinalShell 配置格式");
    }
    return parseFinalShellImport(data);
  }

  async importConnectionsExecute(
    input: ConnectionImportExecuteInput,
  ): Promise<ConnectionImportResult> {
    const result: ConnectionImportResult = {
      created: 0,
      skipped: 0,
      overwritten: 0,
      failed: 0,
      passwordsUnavailable: 0,
      errors: [],
    };

    const allConnections = this.connections.list({});

    for (const entry of input.entries) {
      try {
        const existing = allConnections.find(
          (c) => c.host === entry.host && c.port === entry.port && c.username === entry.username,
        );

        if (existing) {
          if (input.conflictPolicy === "skip") {
            result.skipped++;
            continue;
          }
          if (input.conflictPolicy === "overwrite") {
            await this.upsertConnection({
              id: existing.id,
              name: entry.name,
              host: entry.host,
              port: entry.port,
              username: entry.username,
              authType: entry.authType,
              password: entry.password,
              strictHostKeyChecking: false,
              keepAliveEnabled: entry.keepAliveEnabled,
              keepAliveIntervalSec: entry.keepAliveIntervalSec,
              groupPath: entry.groupPath,
              tags: entry.tags,
              notes: entry.notes,
              favorite: entry.favorite,
              terminalEncoding: entry.terminalEncoding,
              backspaceMode: entry.backspaceMode,
              deleteMode: entry.deleteMode,
              monitorSession: entry.monitorSession,
            });
            result.overwritten++;
            if (
              !entry.password &&
              (entry.authType === "password" || entry.authType === "interactive")
            ) {
              result.passwordsUnavailable++;
            }
            continue;
          }
        }

        await this.upsertConnection({
          name: entry.name,
          host: entry.host,
          port: entry.port,
          username: entry.username,
          authType: entry.authType,
          password: entry.password,
          strictHostKeyChecking: false,
          keepAliveEnabled: entry.keepAliveEnabled,
          keepAliveIntervalSec: entry.keepAliveIntervalSec,
          groupPath: entry.groupPath,
          tags: entry.tags,
          notes: entry.notes,
          favorite: entry.favorite,
          terminalEncoding: entry.terminalEncoding,
          backspaceMode: entry.backspaceMode,
          deleteMode: entry.deleteMode,
          monitorSession: entry.monitorSession,
        });
        result.created++;
        if (
          !entry.password &&
          (entry.authType === "password" || entry.authType === "interactive")
        ) {
          result.passwordsUnavailable++;
        }
      } catch (error) {
        result.failed++;
        const reason = error instanceof Error ? error.message : "未知错误";
        result.errors.push(`${entry.name} (${entry.host}:${entry.port}): ${reason}`);
      }
    }

    this.appendAuditLogIfEnabled({
      action: "connection.import",
      level: "info",
      message: `Imported connections: ${result.created} created, ${result.overwritten} overwritten, ${result.skipped} skipped, ${result.failed} failed`,
      metadata: { ...result },
    });

    return result;
  }

  // ---------------------------------------------------------------------------
  // Private helpers
  // ---------------------------------------------------------------------------

  private async parseImportPayloadText(
    rawText: string,
    decryptionPassword?: string,
  ): Promise<unknown> {
    const normalizedText = trimBomAndWhitespace(rawText);
    const encryptedPrefix = ENCRYPTED_EXPORT_PREFIX;

    if (normalizedText.startsWith(encryptedPrefix)) {
      if (!decryptionPassword) {
        throw new Error(
          `${CONNECTION_IMPORT_DECRYPT_PROMPT_PREFIX}该导入文件已加密，请输入密码`,
        );
      }
      const encryptedB64 = normalizedText.slice(encryptedPrefix.length).trim();
      if (!encryptedB64) {
        throw new Error("导入文件加密内容为空");
      }

      let decryptedText: string;
      try {
        decryptedText = await decryptConnectionExportPayload(encryptedB64, decryptionPassword);
      } catch {
        throw new Error(
          `${CONNECTION_IMPORT_DECRYPT_PROMPT_PREFIX}密码错误或文件损坏，请重试`,
        );
      }

      try {
        return JSON.parse(decryptedText);
      } catch {
        throw new Error("解密成功，但文件内容不是合法 JSON");
      }
    }

    return JSON.parse(normalizedText);
  }

  private async buildExportedConnection(conn: ConnectionProfile): Promise<ExportedConnection> {
    let password: string | undefined;
    if (
      (conn.authType === "password" || conn.authType === "interactive") &&
      conn.credentialRef
    ) {
      try {
        password = await this.vault.readCredential(conn.credentialRef);
      } catch {
        /* If we can't read the credential, export without password */
      }
    }
    return {
      name: conn.name,
      host: conn.host,
      port: conn.port,
      username: conn.username,
      authType: conn.authType,
      password,
      keepAliveEnabled: conn.keepAliveEnabled,
      keepAliveIntervalSec: conn.keepAliveIntervalSec,
      groupPath: conn.groupPath,
      tags: conn.tags,
      notes: conn.notes,
      favorite: conn.favorite,
      terminalEncoding: conn.terminalEncoding,
      backspaceMode: conn.backspaceMode,
      deleteMode: conn.deleteMode,
      monitorSession: conn.monitorSession,
    };
  }
}

// ---------------------------------------------------------------------------
// Module-level helpers
// ---------------------------------------------------------------------------

function parseJsonPayloadText(rawText: string): unknown {
  const normalizedText = trimBomAndWhitespace(rawText);
  return JSON.parse(normalizedText);
}
