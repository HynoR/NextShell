import fs from "node:fs";
import { BrowserWindow, dialog } from "electron";
import type { WebContents } from "electron";
import type {
  ConnectionExportFile,
  ConnectionImportEntry,
  ConnectionImportResult,
  ConnectionProfile,
  ExportedConnection
} from "@nextshell/core";
import type {
  ConnectionExportInput,
  ConnectionExportBatchInput,
  ConnectionExportBatchResult,
  ConnectionImportDirectoryPreviewInput,
  ConnectionImportDirectoryPreviewResult,
  ConnectionImportPreviewInput,
  ConnectionImportFinalShellPreviewInput,
  ConnectionImportExecuteInput,
  ConnectionUpsertInput
} from "@nextshell/shared";
import { CONNECTION_IMPORT_DECRYPT_PROMPT_PREFIX } from "@nextshell/shared";
import type { EncryptedSecretVault } from "@nextshell/security";
import type { CachedConnectionRepository, CachedSshKeyRepository } from "@nextshell/storage";
import {
  enrichImportEntry,
  isFinalShellFormat,
  isNextShellFormat,
  parseFinalShellImport,
  parseNextShellImport,
  resolveImportedAuth,
  type SshKeyMatchCandidate
} from "./import-export";
import {
  decryptConnectionExportPayload,
  encryptConnectionExportPayload
} from "./connection-export-crypto";
import { exportConnectionsBatchToDirectory } from "./connection-export-batch";
import { scanConnectionImportDirectory } from "./connection-import-directory";

interface ImportExportServiceOptions {
  connections: CachedConnectionRepository;
  sshKeyRepo: CachedSshKeyRepository;
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
  private readonly sshKeyRepo: CachedSshKeyRepository;
  private readonly vault: EncryptedSecretVault;
  private readonly upsertConnection: (input: ConnectionUpsertInput) => Promise<ConnectionProfile>;
  private readonly appendAuditLogIfEnabled: ImportExportServiceOptions["appendAuditLogIfEnabled"];

  constructor(options: ImportExportServiceOptions) {
    this.connections = options.connections;
    this.sshKeyRepo = options.sshKeyRepo;
    this.vault = options.vault;
    this.upsertConnection = options.upsertConnection;
    this.appendAuditLogIfEnabled = options.appendAuditLogIfEnabled;
  }

  // ---------------------------------------------------------------------------
  // Public methods
  // ---------------------------------------------------------------------------

  async exportConnections(
    sender: WebContents,
    input: ConnectionExportInput
  ): Promise<{ ok: true; filePath: string } | { ok: false; canceled: true }> {
    const owner = BrowserWindow.fromWebContents(sender);
    const saveOptions = {
      title: "导出连接",
      defaultPath: "nextshell-connections.json",
      filters: [{ name: "JSON", extensions: ["json"] }]
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

    // Unencrypted exports omit secrets entirely: the previous XOR "obfuscation"
    // was reversible from fields present in the file, so it was plaintext in
    // disguise. Use an encrypted export to include passwords.
    const exportedConnectionsFinal = encrypted
      ? exportedConnections
      : exportedConnections.map((c) => ({ ...c, password: undefined }));

    const exportFile: ConnectionExportFile = {
      format: "nextshell-connections",
      version: 1,
      exportedAt: new Date().toISOString(),
      ...(encrypted ? {} : { passwordsOmitted: true }),
      connections: exportedConnectionsFinal
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
      metadata: { filePath: result.filePath, count: exportedConnections.length, encrypted }
    });

    return { ok: true, filePath: result.filePath };
  }

  async exportConnectionsBatch(
    input: ConnectionExportBatchInput
  ): Promise<ConnectionExportBatchResult> {
    const allConnections = this.connections.list({});
    const idSet = new Set(input.connectionIds);
    const filtered = allConnections.filter((conn) => idSet.has(conn.id));

    const buildExportedConnection = this.buildExportedConnection.bind(this);
    const result = await exportConnectionsBatchToDirectory({
      connections: filtered,
      directoryPath: input.directoryPath,
      encryptionPassword: input.encryptionPassword,
      buildExportedConnection
    });

    this.appendAuditLogIfEnabled({
      action: "connection.export.batch",
      level: "info",
      message: `Batch exported ${result.exported}/${result.total} connections`,
      metadata: { ...result }
    });

    return result;
  }

  async importConnectionsPreview(
    input: ConnectionImportPreviewInput
  ): Promise<ConnectionImportEntry[]> {
    const raw = fs.readFileSync(input.filePath, "utf-8");
    const data = await this.parseImportPayloadText(raw, input.decryptionPassword);
    if (isNextShellFormat(data)) {
      return this.enrichImportEntries(parseNextShellImport(data));
    }
    throw new Error(
      "该文件不是 NextShell 导出格式，请使用\u201c导入 FinalShell 文件\u201d按钮导入 FinalShell 配置"
    );
  }

  async importFinalShellConnectionsPreview(
    input: ConnectionImportFinalShellPreviewInput
  ): Promise<ConnectionImportEntry[]> {
    const raw = fs.readFileSync(input.filePath, "utf-8");
    const data = parseJsonPayloadText(raw);
    if (!isFinalShellFormat(data)) {
      throw new Error("该文件不是 FinalShell 配置格式");
    }
    return this.enrichImportEntries(parseFinalShellImport(data));
  }

  async importConnectionsDirectoryPreview(
    input: ConnectionImportDirectoryPreviewInput
  ): Promise<ConnectionImportDirectoryPreviewResult> {
    const scan = await scanConnectionImportDirectory(input.directoryPath);
    const result: ConnectionImportDirectoryPreviewResult = {
      directoryPath: scan.directoryPath,
      source: input.source,
      totalFiles: scan.files.length,
      importedFiles: 0,
      skippedFiles: 0,
      entries: [],
      files: [],
      warnings: [...scan.warnings]
    };

    for (const file of scan.files) {
      try {
        const raw = await fs.promises.readFile(file.filePath, "utf-8");
        const entries =
          input.source === "nextshell"
            ? await this.parseNextShellDirectoryFile(raw, file.groupPath, input.decryptionPassword)
            : this.parseFinalShellDirectoryFile(raw, file.groupPath);

        if (entries.length === 0) {
          result.skippedFiles++;
          result.warnings.push(`${file.relativePath}：文件中没有可导入的连接`);
          continue;
        }

        const entriesWithSource = this.enrichImportEntries(
          entries.map((entry) => ({
            ...entry,
            sourceFileName: file.fileName,
            sourceRelativePath: file.relativePath
          }))
        );

        result.importedFiles++;
        result.entries.push(...entriesWithSource);
        result.files.push({
          filePath: file.filePath,
          fileName: file.fileName,
          relativePath: file.relativePath,
          groupPath: file.groupPath,
          entries: entriesWithSource
        });
      } catch (error) {
        const reason = error instanceof Error ? error.message : "未知错误";
        if (
          input.source === "nextshell" &&
          !input.decryptionPassword &&
          reason.startsWith(CONNECTION_IMPORT_DECRYPT_PROMPT_PREFIX)
        ) {
          throw error;
        }
        result.skippedFiles++;
        result.warnings.push(`${file.relativePath}：${stripImportPromptPrefix(reason)}`);
      }
    }

    return result;
  }

  async importConnectionsExecute(
    input: ConnectionImportExecuteInput
  ): Promise<ConnectionImportResult> {
    const result: ConnectionImportResult = {
      created: 0,
      skipped: 0,
      overwritten: 0,
      failed: 0,
      passwordsUnavailable: 0,
      errors: []
    };

    const allConnections = this.connections.list({});
    const keyCandidates = this.loadSshKeyMatchCandidates();

    for (const entry of input.entries) {
      try {
        const existing = allConnections.find(
          (c) => c.host === entry.host && c.port === entry.port && c.username === entry.username
        );
        const upsertInput = this.toImportedUpsertInput(entry, keyCandidates, existing?.id);

        if (existing) {
          if (input.conflictPolicy === "skip") {
            result.skipped++;
            continue;
          }
          if (input.conflictPolicy === "overwrite") {
            await this.upsertConnection(upsertInput);
            result.overwritten++;
            this.notePasswordUnavailable(result, upsertInput.authType, entry.password);
            continue;
          }
        }

        await this.upsertConnection({ ...upsertInput, id: undefined });
        result.created++;
        this.notePasswordUnavailable(result, upsertInput.authType, entry.password);
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
      metadata: { ...result }
    });

    return result;
  }

  // ---------------------------------------------------------------------------
  // Private helpers
  // ---------------------------------------------------------------------------

  private async parseImportPayloadText(
    rawText: string,
    decryptionPassword?: string
  ): Promise<unknown> {
    const normalizedText = trimBomAndWhitespace(rawText);
    const encryptedPrefix = ENCRYPTED_EXPORT_PREFIX;

    if (normalizedText.startsWith(encryptedPrefix)) {
      if (!decryptionPassword) {
        throw new Error(`${CONNECTION_IMPORT_DECRYPT_PROMPT_PREFIX}该导入文件已加密，请输入密码`);
      }
      const encryptedB64 = normalizedText.slice(encryptedPrefix.length).trim();
      if (!encryptedB64) {
        throw new Error("导入文件加密内容为空");
      }

      let decryptedText: string;
      try {
        decryptedText = await decryptConnectionExportPayload(encryptedB64, decryptionPassword);
      } catch {
        throw new Error(`${CONNECTION_IMPORT_DECRYPT_PROMPT_PREFIX}密码错误或文件损坏，请重试`);
      }

      try {
        return JSON.parse(decryptedText);
      } catch {
        throw new Error("解密成功，但文件内容不是合法 JSON");
      }
    }

    return JSON.parse(normalizedText);
  }

  private async parseNextShellDirectoryFile(
    rawText: string,
    groupPath: string,
    decryptionPassword?: string
  ): Promise<ConnectionImportEntry[]> {
    const data = await this.parseImportPayloadText(rawText, decryptionPassword);
    if (!isNextShellFormat(data)) {
      throw new Error("该文件不是 NextShell 导出格式");
    }
    return parseNextShellImport(data, { groupPathOverride: groupPath });
  }

  private parseFinalShellDirectoryFile(
    rawText: string,
    groupPath: string
  ): ConnectionImportEntry[] {
    const data = parseJsonPayloadText(rawText);
    if (!isFinalShellFormat(data)) {
      throw new Error("该文件不是 FinalShell 配置格式");
    }
    return parseFinalShellImport(data, { groupPathOverride: groupPath });
  }

  private async buildExportedConnection(conn: ConnectionProfile): Promise<ExportedConnection> {
    let password: string | undefined;
    if ((conn.authType === "password" || conn.authType === "interactive") && conn.credentialRef) {
      try {
        password = await this.vault.readCredential(conn.credentialRef);
      } catch {
        /* If we can't read the credential, export without password */
      }
    }
    const sshKeyRef = conn.sshKeyId ? this.buildExportedSshKeyRef(conn.sshKeyId) : undefined;
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
      sshKeyRef
    };
  }

  /**
   * 只带名称与 OpenSSH 公钥指纹,永远不带私钥内容。指纹在保存密钥时就解析好落库了,所以这里
   * 不需要碰 vault。
   */
  private buildExportedSshKeyRef(sshKeyId: string): ExportedConnection["sshKeyRef"] | undefined {
    const key = this.sshKeyRepo.getById(sshKeyId);
    if (!key) {
      return undefined;
    }
    return { name: key.name, fingerprint: key.fingerprint };
  }

  private enrichImportEntries(entries: ConnectionImportEntry[]): ConnectionImportEntry[] {
    const keys = this.loadSshKeyMatchCandidates();
    return entries.map((entry) => enrichImportEntry(entry, keys));
  }

  private loadSshKeyMatchCandidates(): SshKeyMatchCandidate[] {
    // Keys stored before fingerprints were parsed carry none; those fall back to name
    // matching, which enrichImportEntry already handles.
    return this.sshKeyRepo
      .list()
      .map((key) => ({ id: key.id, name: key.name, fingerprint: key.fingerprint }));
  }

  private toImportedUpsertInput(
    entry: ConnectionImportExecuteInput["entries"][number],
    keys: readonly SshKeyMatchCandidate[],
    existingId?: string
  ): ConnectionUpsertInput {
    const resolved = resolveImportedAuth(entry, keys);
    return {
      id: existingId,
      name: entry.name,
      host: entry.host,
      port: entry.port,
      username: entry.username,
      authType: resolved.authType,
      password: entry.password,
      sshKeyId: resolved.sshKeyId,
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
      // Imported files carry no agent authorization: overwriting also replaces the auth
      // material, so any previously granted access is revoked rather than inherited.
      agentAccess: "off"
    };
  }

  private notePasswordUnavailable(
    result: ConnectionImportResult,
    authType: ConnectionUpsertInput["authType"],
    password: string | undefined
  ): void {
    if (!password && (authType === "password" || authType === "interactive")) {
      result.passwordsUnavailable++;
    }
  }
}

// ---------------------------------------------------------------------------
// Module-level helpers
// ---------------------------------------------------------------------------

function parseJsonPayloadText(rawText: string): unknown {
  const normalizedText = trimBomAndWhitespace(rawText);
  return JSON.parse(normalizedText);
}

function stripImportPromptPrefix(reason: string): string {
  return reason.startsWith(CONNECTION_IMPORT_DECRYPT_PROMPT_PREFIX)
    ? reason.slice(CONNECTION_IMPORT_DECRYPT_PROMPT_PREFIX.length).trim()
    : reason;
}
