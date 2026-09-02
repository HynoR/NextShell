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
import { LOCAL_DEFAULT_SCOPE_KEY } from "@nextshell/core";
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
import {
  CONNECTION_IMPORT_DECRYPT_PROMPT_PREFIX,
  LOCAL_GROUP_PATH_ROOT,
  parseGroupPathSegments,
  resolveOriginScopeKey
} from "@nextshell/shared";
import type { EncryptedSecretVault } from "@nextshell/security";
import type {
  CachedConnectionRepository,
  CachedSshKeyRepository,
  ConnectionFolderRepository
} from "@nextshell/storage";
import {
  enrichImportEntry,
  isFinalShellFormat,
  isNextShellFormat,
  materializeFolderChain,
  parseFinalShellImport,
  parseNextShellImport,
  resolveImportedAuth,
  type ImportFolderNode,
  type ImportFolderStore,
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
  connectionFolders: ConnectionFolderRepository;
  vault: EncryptedSecretVault;
  upsertConnection: (input: ConnectionUpsertInput) => Promise<ConnectionProfile>;
}

const ENCRYPTED_EXPORT_PREFIX = "b64##";

const trimBomAndWhitespace = (value: string): string => value.replace(/^\uFEFF/, "").trim();

export class ImportExportService {
  private readonly connections: CachedConnectionRepository;
  private readonly sshKeyRepo: CachedSshKeyRepository;
  private readonly connectionFolders: ConnectionFolderRepository;
  private readonly vault: EncryptedSecretVault;
  private readonly upsertConnection: (input: ConnectionUpsertInput) => Promise<ConnectionProfile>;

  constructor(options: ImportExportServiceOptions) {
    this.connections = options.connections;
    this.sshKeyRepo = options.sshKeyRepo;
    this.connectionFolders = options.connectionFolders;
    this.vault = options.vault;
    this.upsertConnection = options.upsertConnection;
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

    // 导入执行链路只写本地作用域,所以冲突候选也只能在本地里找。跨作用域匹配的话，一条
    // 同 host 的云连接会被当成"已存在"，overwrite 就变成拿本地目录去改写云资源——投影链路
    // 直接抛"目标目录不存在"，整条 entry 失败。云侧撞同 host 应该照常新建一条本地连接。
    const localConnections = this.connections
      .list({})
      .filter((connection) => resolveOriginScopeKey(connection) === LOCAL_DEFAULT_SCOPE_KEY);
    const keyCandidates = this.loadSshKeyMatchCandidates();
    // 目录先于任何一条 entry 校验:targetFolderId 不合法时整批拒绝，不能一半落根一半落目录。
    const folderStore = this.createImportFolderStore(input.targetFolderId);
    // 目录扫描导入的 groupPath 是磁盘相对路径,没有线格式前缀;剥掉首段会把用户叫
    // `server` 的顶层目录整层吞掉。省略时按导出文件的线格式处理(老调用方的行为)。
    const stripWirePrefix = input.groupPathFormat !== "literal";

    for (const entry of input.entries) {
      try {
        const existing = localConnections.find(
          (c) => c.host === entry.host && c.port === entry.port && c.username === entry.username
        );

        if (existing && input.conflictPolicy === "skip") {
          // 目录物化必须排在冲突判定之后:整批 skip 时一个目录都不该建出来。
          result.skipped++;
          continue;
        }

        // V2 的树完全按 folderId 摆放，groupPath 只是投影：不物化目录，导入进来的连接
        // 无论原本在哪一层都会平铺到根，用户组织好的结构在界面上直接消失。
        const folderId = materializeFolderChain(
          parseGroupPathSegments(entry.groupPath, { stripWirePrefix }),
          input.targetFolderId,
          folderStore
        );
        const upsertInput = this.toImportedUpsertInput(
          entry,
          keyCandidates,
          existing?.id,
          folderId
        );

        if (existing && input.conflictPolicy === "overwrite") {
          await this.upsertConnection(upsertInput);
          result.overwritten++;
          this.notePasswordUnavailable(result, upsertInput.authType, entry.password);
          continue;
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

  /**
   * 导入执行链路只写本地作用域，所以目录也只在本地物化。
   *
   * 目录一次性载入内存后靠 cache 增量维护：每条 entry 都回查一次数据库不只是慢，还看不见
   * 本批刚建出来的目录，同一层会被重复创建到撞唯一索引。
   */
  private createImportFolderStore(targetFolderId: string | undefined): ImportFolderStore {
    if (targetFolderId) {
      const target = this.connectionFolders.getById(targetFolderId);
      if (!target || target.scopeKey !== LOCAL_DEFAULT_SCOPE_KEY) {
        throw new Error("目标目录不存在或不属于本地作用域");
      }
    }
    const load = (): ImportFolderNode[] =>
      this.connectionFolders
        .list(LOCAL_DEFAULT_SCOPE_KEY)
        .map((folder) => ({ id: folder.id, name: folder.name, parentId: folder.parentId }));
    const cache: ImportFolderNode[] = load();
    return {
      list: () => cache,
      // 唯一索引撞车说明缓存已经落后于库(并发导入/别处刚建了同名目录),重读一次再复用。
      refresh: () => {
        cache.length = 0;
        cache.push(...load());
        return cache;
      },
      create: (name, parentId) => {
        const created = this.connectionFolders.create({
          scopeKey: LOCAL_DEFAULT_SCOPE_KEY,
          name,
          parentId
        });
        const node: ImportFolderNode = {
          id: created.id,
          name: created.name,
          parentId: created.parentId
        };
        cache.push(node);
        return node;
      }
    };
  }

  private toImportedUpsertInput(
    entry: ConnectionImportExecuteInput["entries"][number],
    keys: readonly SshKeyMatchCandidate[],
    existingId?: string,
    folderId?: string
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
      // 显式 null 才是"移回顶层";省略在 upsert 里是"别动目录"——overwrite 一条已在某个目录
      // 里的连接时那意味着 folderId 留在旧目录、groupPath 却被下面这行改成了根，两边分叉。
      folderId: folderId ?? null,
      // 有 folderId 时 upsert 会按目录链重新投影 groupPath，这里给的值用不上；没有目录
      // (整条链都被剥成了前缀)时必须落回本地根，否则一份云导出的 `/workspace/<slug>`
      // 会原样进本地库，同步会把它当成那个 workspace 的资源。
      groupPath: folderId ? entry.groupPath : LOCAL_GROUP_PATH_ROOT,
      tags: entry.tags,
      notes: entry.notes,
      favorite: entry.favorite,
      terminalEncoding: entry.terminalEncoding,
      backspaceMode: entry.backspaceMode,
      deleteMode: entry.deleteMode,
      monitorSession: entry.monitorSession
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
