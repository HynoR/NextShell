
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { randomUUID } from "node:crypto";
import { spawn, execFileSync, type ChildProcess } from "node:child_process";
import * as iconv from "iconv-lite";
import { BrowserWindow, dialog, shell, session as electronSession } from "electron";
import type { OpenDialogOptions, WebContents } from "electron";
import { spawn as spawnPty, type IPty } from "node-pty";
import type {
  AppPreferences,
  AuditLogRecord,
  BackupArchiveMeta,
  BackupConflictPolicy,
  BackspaceMode,
  BatchCommandExecutionResult,
  BatchCommandResultItem,
  CommandExecutionResult,
  CommandHistoryEntry,
  CommandTemplateParam,
  ConnectionExportFile,
  ConnectionImportEntry,
  ConnectionImportResult,
  ConnectionListQuery,
  ConnectionProfile,
  DeleteMode,
  ExportedConnection,
  MigrationRecord,
  MonitorSnapshot,
  NetworkConnection,
  NetworkSnapshot,
  ProcessDetailSnapshot,
  ProcessSnapshot,
  ProxyProfile,
  RemoteFileEntry,
  RestoreConflictPolicy,
  SavedCommand,
  SessionDescriptor,
  SessionStatus,
  SystemInfoSnapshot,
  SshKeyProfile,
  TerminalEncoding
} from "../../../../../packages/core/src/index";
import {
  SshConnection,
  type RemotePathType,
  type SshConnectOptions,
  type SshDirectoryEntry,
  type SshShellChannel
} from "../../../../../packages/ssh/src/index";
import type {
  DebugLogEntry,
  DialogOpenDirectoryInput,
  DialogOpenFilesInput,
  DialogOpenPathInput,
  CloudSyncConflictItem,
  CloudSyncConfigureInput,
  CloudSyncResolveConflictInput,
  CloudSyncStatus,
  CommandBatchExecInput,
  ConnectionExportInput,
  ConnectionExportBatchInput,
  ConnectionExportBatchResult,
  ConnectionImportFinalShellPreviewInput,
  ConnectionImportPreviewInput,
  ConnectionImportExecuteInput,
  ConnectionUpsertInput,
  MonitorProcessKillInput,
  SessionAuthOverrideInput,
  SessionOpenInput,
  SftpTransferStatusEvent,
  SavedCommandListInput,
  SavedCommandRemoveInput,
  SavedCommandUpsertInput,
  SettingsUpdateInput,
  SessionStatusEvent,
  TemplateParamsListInput,
  TemplateParamsClearInput,
  TemplateParamsUpsertInput,
  SftpEditSessionInfo,
  SshKeyUpsertInput,
  SshKeyRemoveInput,
  ProxyUpsertInput,
  ProxyRemoveInput
} from "../../../../../packages/shared/src/index";
import {
  IPCChannel,
  AUTH_REQUIRED_PREFIX,
  CONNECTION_IMPORT_DECRYPT_PROMPT_PREFIX
} from "../../../../../packages/shared/src/index";
import type {
  PingResult,
  SessionDataEvent,
  StreamDeliveryAckInput,
  StreamDeliveryEnvelope,
  TracerouteEvent,
  UpdateCheckResult
} from "../../../../../packages/shared/src/index";
import {
  EncryptedSecretVault,
  KeytarPasswordCache,
  generateDeviceKey,
  createMasterKeyMeta,
  clearDerivedKeyCache,
  verifyMasterPassword
} from "../../../../../packages/security/src/index";
import {
  SQLiteConnectionRepository,
  CachedConnectionRepository,
  SQLiteSshKeyRepository,
  CachedSshKeyRepository,
  SQLiteProxyRepository,
  CachedProxyRepository
} from "../../../../../packages/storage/src/index";
import { RemoteEditManager } from "./remote-edit-manager";
import { mergePreferences } from "./preferences";
import { BackupService, applyPendingRestore } from "./backup-service";
import {
  CloudSyncService,
  CLOUD_SYNC_PENDING_QUEUE_SETTING_KEY,
  type CloudSyncApplyConnectionInput,
  type CloudSyncApplyProxyInput,
  type CloudSyncApplySshKeyInput
} from "./cloud-sync-service";
import { changeMasterPassword } from "./master-password-change";
import { resolveAuditRuntime } from "./audit-runtime";
import {
  isFinalShellFormat,
  isNextShellFormat,
  parseFinalShellImport,
  parseNextShellImport,
} from "./import-export";
import {
  decryptConnectionExportPayload,
  encryptConnectionExportPayload,
  obfuscatePassword
} from "./connection-export-crypto";
import { exportConnectionsBatchToDirectory } from "./connection-export-batch";
import {
  assertLocalTarAvailable,
  buildRemoteRemoveFileCommand,
  buildRemoteTarCheckCommand,
  buildRemoteTarCreateCommand,
  buildRemoteTarExtractCommand,
  createLocalTarGzArchive,
  normalizeArchiveName,
  normalizeRemoteEntryNames
} from "./sftp-archive-utils";
import { logger } from "../logger";
import { applyAppearanceToAllWindows } from "../window-theme";
import {
  parseCpuInfo,
  parseFilesystemEntries,
  parseMeminfoTotals,
  parseNetworkInterfaceTotals,
  parseOsReleaseName
} from "./system-info-parser";
import {
  SystemMonitorController,
  type MonitorSelectionState,
  type ProbeExecutionLog,
} from "./monitor/system-monitor-controller";
import {
  ProcessMonitorController,
  type ProcessProbeExecutionLog,
} from "./monitor/process-monitor-controller";
import {
  firstNonEmptyLine,
  parseProcessDetailPrimary,
} from "./monitor/process-probe-parser";
import {
  buildRemoteHomeDirCommand,
  parseRemoteHomeDir
} from "./remote-home-dir";
import {
  NetworkMonitorController,
  type NetworkProbeExecutionLog,
  type NetworkTool,
} from "./monitor/network-monitor-controller";
import {
  createRemoteOsc7BootstrapPlan,
  resolveOsc7ShellFamily,
} from "./terminal-osc7-bootstrap";
import {
  createLatestOnlyDispatcher,
  createOrderedBytesDispatcher
} from "./ipc-stream-dispatcher";
import { resolveLocalShellLaunch } from "./local-shell";

interface BootstrapSuppression {
  startMarker: string;
  endMarker: string;
  buffer: string;
  timeout: ReturnType<typeof setTimeout>;
}

interface ActiveRemoteSession {
  kind: "remote";
  descriptor: SessionDescriptor;
  channel: SshShellChannel;
  sender: WebContents;
  connectionId: string;
  terminalEncoding: TerminalEncoding;
  backspaceMode: BackspaceMode;
  deleteMode: DeleteMode;
  bootstrapSuppression?: BootstrapSuppression;
}

interface ActiveLocalSession {
  kind: "local";
  descriptor: SessionDescriptor;
  pty: IPty;
  sender: WebContents;
  terminalEncoding: TerminalEncoding;
}

type ActiveSession = ActiveRemoteSession | ActiveLocalSession;

interface CreateServiceContainerOptions {
  dataDir: string;
  keytarServiceName?: string;
}

interface MonitorState {
  selectedNetworkInterface?: string;
  networkInterfaceOptions?: string[];
}

// ─── Hidden Session ① System Monitor (primary, long-lived) ───────────────
interface SystemMonitorRuntime {
  controller: SystemMonitorController;
  sender?: WebContents;
  disposed: boolean;
}

// ─── Hidden Session ② Process Monitor (on-demand, ps-based polling) ─────
interface ProcessMonitorRuntime {
  controller: ProcessMonitorController;
  sender?: WebContents;
  disposed: boolean;
}

// ─── Hidden Session ③ Network Monitor (on-demand) ────────────────────────
interface NetworkMonitorRuntime {
  controller: NetworkMonitorController;
  sender?: WebContents;
  disposed: boolean;
}

// ─── Hidden Session ④ Ad-hoc (on-demand, idle auto-destroy) ─────────────
interface AdhocSessionRuntime {
  connection: SshConnection;
  idleTimer?: ReturnType<typeof setTimeout>;
  lastUsedAt: number;
  disposed: boolean;
}

export interface ServiceContainer {
  listConnections: (query: ConnectionListQuery) => ConnectionProfile[];
  upsertConnection: (input: ConnectionUpsertInput) => Promise<ConnectionProfile>;
  removeConnection: (id: string) => Promise<{ ok: true }>;
  exportConnections: (
    sender: WebContents,
    input: ConnectionExportInput
  ) => Promise<{ ok: true; filePath: string } | { ok: false; canceled: true }>;
  exportConnectionsBatch: (input: ConnectionExportBatchInput) => Promise<ConnectionExportBatchResult>;
  revealConnectionPassword: (connectionId: string, masterPassword?: string) => Promise<{ password: string }>;
  importConnectionsPreview: (input: ConnectionImportPreviewInput) => Promise<ConnectionImportEntry[]>;
  importFinalShellConnectionsPreview: (input: ConnectionImportFinalShellPreviewInput) => Promise<ConnectionImportEntry[]>;
  importConnectionsExecute: (input: ConnectionImportExecuteInput) => Promise<ConnectionImportResult>;
  listSshKeys: () => SshKeyProfile[];
  upsertSshKey: (input: SshKeyUpsertInput) => Promise<SshKeyProfile>;
  removeSshKey: (input: SshKeyRemoveInput) => Promise<{ ok: true }>;
  listProxies: () => ProxyProfile[];
  upsertProxy: (input: ProxyUpsertInput) => Promise<ProxyProfile>;
  removeProxy: (input: ProxyRemoveInput) => Promise<{ ok: true }>;
  checkForUpdate: () => Promise<UpdateCheckResult>;
  pingHost: (host: string) => Promise<PingResult>;
  tracerouteRun: (host: string, sender: WebContents) => Promise<{ ok: true }>;
  tracerouteStop: () => { ok: true };
  getAppPreferences: () => AppPreferences;
  updateAppPreferences: (patch: SettingsUpdateInput) => AppPreferences;
  cloudSyncConfigure: (input: CloudSyncConfigureInput) => Promise<CloudSyncStatus>;
  cloudSyncDisable: () => Promise<{ ok: true }>;
  cloudSyncStatus: () => Promise<CloudSyncStatus>;
  cloudSyncSyncNow: () => Promise<{ ok: true }>;
  cloudSyncListConflicts: () => Promise<CloudSyncConflictItem[]>;
  cloudSyncResolveConflict: (input: CloudSyncResolveConflictInput) => Promise<{ ok: true }>;
  openFilesDialog: (
    sender: WebContents,
    input: DialogOpenFilesInput
  ) => Promise<{ canceled: boolean; filePaths: string[] }>;
  openDirectoryDialog: (
    sender: WebContents,
    input: DialogOpenDirectoryInput
  ) => Promise<{ canceled: boolean; filePath?: string }>;
  openLocalPath: (
    sender: WebContents,
    input: DialogOpenPathInput
  ) => Promise<{ ok: boolean; error?: string }>;
  openSession: (
    input: SessionOpenInput,
    sender: WebContents
  ) => Promise<SessionDescriptor>;
  ackStreamDelivery: (input: StreamDeliveryAckInput) => { ok: true };
  writeSession: (sessionId: string, data: string) => { ok: true };
  resizeSession: (sessionId: string, cols: number, rows: number) => { ok: true };
  closeSession: (sessionId: string) => Promise<{ ok: true }>;
  getSystemInfoSnapshot: (connectionId: string) => Promise<SystemInfoSnapshot>;
  startSystemMonitor: (connectionId: string, sender: WebContents) => Promise<{ ok: true }>;
  stopSystemMonitor: (connectionId: string) => { ok: true };
  selectSystemNetworkInterface: (connectionId: string, networkInterface: string) => Promise<{ ok: true }>;
  execCommand: (connectionId: string, command: string) => Promise<CommandExecutionResult>;
  getSessionHomeDir: (connectionId: string) => Promise<{ path: string } | null>;
  execBatchCommand: (input: CommandBatchExecInput) => Promise<BatchCommandExecutionResult>;
  listAuditLogs: (limit: number) => AuditLogRecord[];
  clearAuditLogs: () => { ok: true; deleted: number };
  listMigrations: () => MigrationRecord[];
  listRemoteFiles: (connectionId: string, path: string) => Promise<RemoteFileEntry[]>;
  listLocalFiles: (path: string) => Promise<RemoteFileEntry[]>;
  uploadRemoteFile: (
    connectionId: string,
    localPath: string,
    remotePath: string,
    sender?: WebContents,
    taskId?: string
  ) => Promise<{ ok: true }>;
  downloadRemoteFile: (
    connectionId: string,
    remotePath: string,
    localPath: string,
    sender?: WebContents,
    taskId?: string
  ) => Promise<{ ok: true }>;
  uploadRemotePacked: (
    connectionId: string,
    localPaths: string[],
    remoteDir: string,
    archiveName?: string,
    sender?: WebContents,
    taskId?: string
  ) => Promise<{ ok: true }>;
  downloadRemotePacked: (
    connectionId: string,
    remoteDir: string,
    entryNames: string[],
    localDir: string,
    archiveName?: string,
    sender?: WebContents,
    taskId?: string
  ) => Promise<{ ok: true; localArchivePath: string }>;
  transferRemotePacked: (
    sourceConnectionId: string,
    sourceDir: string,
    entryNames: string[],
    targetConnectionId: string,
    targetDir: string,
    archiveName?: string,
    sender?: WebContents,
    taskId?: string
  ) => Promise<{ ok: true }>;
  createRemoteDirectory: (connectionId: string, pathName: string) => Promise<{ ok: true }>;
  renameRemoteFile: (connectionId: string, fromPath: string, toPath: string) => Promise<{ ok: true }>;
  deleteRemoteFile: (
    connectionId: string,
    targetPath: string,
    type: RemoteFileEntry["type"]
  ) => Promise<{ ok: true }>;
  listCommandHistory: () => CommandHistoryEntry[];
  pushCommandHistory: (command: string) => CommandHistoryEntry;
  removeCommandHistory: (command: string) => { ok: true };
  clearCommandHistory: () => { ok: true };
  listSavedCommands: (query?: SavedCommandListInput) => SavedCommand[];
  upsertSavedCommand: (input: SavedCommandUpsertInput) => SavedCommand;
  removeSavedCommand: (input: SavedCommandRemoveInput) => { ok: true };
  openRemoteEdit: (
    connectionId: string,
    remotePath: string,
    editorCommand: string,
    sender: WebContents
  ) => Promise<{ editId: string; localPath: string }>;
  stopRemoteEdit: (editId: string) => Promise<{ ok: true }>;
  stopAllRemoteEdits: () => Promise<{ ok: true }>;
  listRemoteEdits: () => SftpEditSessionInfo[];
  openBuiltinEdit: (connectionId: string, remotePath: string, sender: WebContents) => Promise<{ editId: string; content: string }>;
  saveBuiltinEdit: (editId: string, connectionId: string, remotePath: string, content: string) => Promise<{ ok: true }>;
  startProcessMonitor: (connectionId: string, sender: WebContents) => Promise<{ ok: true }>;
  stopProcessMonitor: (connectionId: string) => { ok: true };
  getProcessDetail: (connectionId: string, pid: number) => Promise<ProcessDetailSnapshot>;
  killRemoteProcess: (connectionId: string, pid: number, signal: "SIGTERM" | "SIGKILL") => Promise<{ ok: true }>;
  startNetworkMonitor: (connectionId: string, sender: WebContents) => Promise<{ ok: true }>;
  stopNetworkMonitor: (connectionId: string) => { ok: true };
  getNetworkConnections: (connectionId: string, port: number) => Promise<NetworkConnection[]>;
  backupList: () => Promise<BackupArchiveMeta[]>;
  backupRun: (conflictPolicy: BackupConflictPolicy) => Promise<{ ok: true; fileName?: string }>;
  backupRestore: (archiveId: string, conflictPolicy: RestoreConflictPolicy) => Promise<{ ok: true }>;
  masterPasswordSet: (password: string) => Promise<{ ok: true }>;
  masterPasswordUnlock: (password: string) => Promise<{ ok: true }>;
  masterPasswordChange: (oldPassword: string, newPassword: string) => Promise<{ ok: true }>;
  masterPasswordClearRemembered: () => Promise<{ ok: true }>;
  masterPasswordStatus: () => Promise<{ isSet: boolean; isUnlocked: boolean; keytarAvailable: boolean }>;
  masterPasswordGetCached: () => Promise<{ password?: string }>;
  backupSetPassword: (password: string) => Promise<{ ok: true }>;
  backupUnlockPassword: (password: string) => Promise<{ ok: true }>;
  backupClearRemembered: () => Promise<{ ok: true }>;
  backupPasswordStatus: () => Promise<{ isSet: boolean; isUnlocked: boolean; keytarAvailable: boolean }>;
  listTemplateParams: (input?: TemplateParamsListInput) => CommandTemplateParam[];
  upsertTemplateParams: (input: TemplateParamsUpsertInput) => { ok: true };
  clearTemplateParams: (input: TemplateParamsClearInput) => { ok: true };
  enableDebugLog: (sender: WebContents) => { ok: true };
  disableDebugLog: (sender: WebContents) => { ok: true };
  dispose: () => Promise<void>;
}

const MONITOR_UPTIME_COMMAND = "cat /proc/uptime 2>/dev/null | awk '{print $1}'";
const MONITOR_SYSTEM_INFO_OS_RELEASE_COMMAND = "cat /etc/os-release 2>/dev/null";
const MONITOR_SYSTEM_INFO_HOSTNAME_COMMAND = "hostname 2>/dev/null";
const MONITOR_SYSTEM_INFO_KERNEL_NAME_COMMAND = "uname -s 2>/dev/null";
const MONITOR_SYSTEM_INFO_KERNEL_VERSION_COMMAND = "uname -r 2>/dev/null";
const MONITOR_SYSTEM_INFO_ARCH_COMMAND = "uname -m 2>/dev/null";
const MONITOR_SYSTEM_INFO_CPUINFO_COMMAND = "cat /proc/cpuinfo 2>/dev/null";
const MONITOR_SYSTEM_INFO_MEMINFO_COMMAND = "cat /proc/meminfo 2>/dev/null";
const MONITOR_SYSTEM_INFO_NET_DEV_COMMAND = "cat /proc/net/dev 2>/dev/null";
const MONITOR_SYSTEM_INFO_FILESYSTEMS_COMMAND = "export LANG=C LC_ALL=C; (df -kP || df -k || df) 2>/dev/null";
const MONITOR_NETWORK_INTERVAL_MS = 5000;
const MONITOR_PROCESS_INTERVAL_MS = 5000;
const ADHOC_IDLE_TIMEOUT_MS = 30_000;
const MONITOR_MAX_CONSECUTIVE_FAILURES = 3;
const MONITOR_COMMAND_TIMEOUT_MS = 20000;
const SFTP_WARMUP_TIMEOUT_MS = 5000;

const mapEntryType = (permissions: string): RemoteFileEntry["type"] => {
  if (permissions.startsWith("d")) {
    return "directory";
  }

  if (permissions.startsWith("l")) {
    return "link";
  }

  return "file";
};

const parseLongname = (longname: string): { permissions: string; owner: string; group: string } => {
  const parts = longname.trim().split(/\s+/);

  return {
    permissions: parts[0] ?? "----------",
    owner: parts[2] ?? "unknown",
    group: parts[3] ?? "unknown"
  };
};

const joinRemotePath = (parent: string, name: string): string => {
  const base = parent === "/" ? "" : parent.replace(/\/$/, "");
  return `${base}/${name}` || "/";
};

const normalizeError = (error: unknown): string => {
  if (error instanceof Error) {
    return error.message;
  }

  return "Unknown SSH error";
};

const toAuthRequiredReason = (message: string): string | undefined => {
  const lower = message.toLowerCase();

  if (lower.includes("proxy authentication failed")) {
    return undefined;
  }

  if (lower.includes("ssh username is required")) {
    return "缺少用户名，请输入用户名和认证信息后重试。";
  }

  if (lower.includes("password credential is missing")) {
    return "缺少密码，请输入密码后重试。";
  }

  if (lower.includes("password auth requires password")) {
    return "缺少密码，请输入密码后重试。";
  }

  if (lower.includes("interactive auth requires password")) {
    return "缺少密码，请输入密码后重试。";
  }

  if (lower.includes("private key auth requires")) {
    return "缺少私钥信息，请输入私钥路径或私钥内容后重试。";
  }

  if (
    lower.includes("cannot parse privatekey") ||
    lower.includes("bad decrypt") ||
    lower.includes("passphrase")
  ) {
    return "私钥或口令无效，请重新输入后重试。";
  }

  if (lower.includes("ssh agent auth requires ssh_auth_sock")) {
    return "SSH Agent 不可用，请改用密码或私钥认证。";
  }

  if (
    lower.includes("all configured authentication methods failed") ||
    lower.includes("permission denied") ||
    lower.includes("authentication failed") ||
    lower.includes("unable to authenticate") ||
    lower.includes("userauth failure") ||
    lower.includes("no supported authentication methods available")
  ) {
    return "认证失败，请检查用户名和凭据后重试。";
  }

  return undefined;
};

const resolveIconvEncoding = (encoding: TerminalEncoding): string => {
  if (encoding === "gb18030") {
    return "gb18030";
  }

  if (encoding === "gbk") {
    return "gbk";
  }

  if (encoding === "big5") {
    return "big5";
  }

  return "utf8";
};

const decodeTerminalData = (chunk: Buffer | string, encoding: TerminalEncoding): string => {
  if (typeof chunk === "string") {
    return chunk;
  }

  const codec = resolveIconvEncoding(encoding);
  try {
    return iconv.decode(chunk, codec);
  } catch (error) {
    logger.debug("[TerminalEncoding] decode failed, fallback to utf-8", error);
    return chunk.toString("utf8");
  }
};

const encodeTerminalData = (data: string, encoding: TerminalEncoding): Buffer => {
  const codec = resolveIconvEncoding(encoding);
  try {
    return iconv.encode(data, codec);
  } catch (error) {
    logger.debug("[TerminalEncoding] encode failed, fallback to utf-8", error);
    return Buffer.from(data, "utf8");
  }
};

const parseFloatSafe = (value: string | undefined): number => {
  if (!value) {
    return 0;
  }

  const parsed = Number.parseFloat(value);
  return Number.isFinite(parsed) ? parsed : 0;
};

const parseIntSafe = (value: string | undefined): number => {
  if (!value) {
    return 0;
  }

  const parsed = Number.parseInt(value, 10);
  return Number.isFinite(parsed) ? parsed : 0;
};

type ParsedPrereleaseIdentifier = number | string;

interface ParsedComparableVersion {
  core: number[];
  prerelease: ParsedPrereleaseIdentifier[] | null;
}

const parseExternalUrl = (rawPath: string): URL | undefined => {
  const trimmed = rawPath.trim();
  if (!trimmed) {
    return undefined;
  }

  try {
    const url = new URL(trimmed);
    const protocol = url.protocol.toLowerCase();
    if (protocol === "http:" || protocol === "https:") {
      return url;
    }
  } catch {
    // ignore invalid URL payloads and continue with local path logic
  }

  return undefined;
};

const normalizeGithubRepo = (rawRepo: string): string | undefined => {
  const trimmed = rawRepo.trim();
  if (!trimmed) {
    return undefined;
  }

  const withoutPrefix = trimmed
    .replace(/^https?:\/\/github\.com\//i, "")
    .replace(/\.git$/i, "")
    .replace(/^\/+/, "")
    .replace(/\/+$/, "");

  if (/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(withoutPrefix)) {
    return withoutPrefix;
  }
  return undefined;
};

const parseComparableVersion = (rawVersion: string): ParsedComparableVersion | null => {
  const normalized = rawVersion.trim().replace(/^v/i, "");
  const match = normalized.match(
    /^(\d+(?:\.\d+)*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z.-]+)?$/
  );
  if (!match?.[1]) {
    return null;
  }

  const core = match[1].split(".").map((segment) => Number.parseInt(segment, 10));
  if (core.length === 0 || core.some((segment) => !Number.isSafeInteger(segment) || segment < 0)) {
    return null;
  }

  const prereleasePart = match[2];
  if (!prereleasePart) {
    return { core, prerelease: null };
  }

  const prerelease = prereleasePart.split(".").map((identifier): ParsedPrereleaseIdentifier => {
    if (/^\d+$/.test(identifier)) {
      return Number.parseInt(identifier, 10);
    }
    return identifier.toLowerCase();
  });

  return { core, prerelease };
};

const compareCoreSegments = (a: number[], b: number[]): number => {
  const maxLength = Math.max(a.length, b.length);
  for (let index = 0; index < maxLength; index += 1) {
    const left = a[index] ?? 0;
    const right = b[index] ?? 0;
    if (left !== right) {
      return left - right;
    }
  }
  return 0;
};

const comparePrerelease = (
  a: ParsedPrereleaseIdentifier[] | null,
  b: ParsedPrereleaseIdentifier[] | null
): number => {
  if (!a && !b) {
    return 0;
  }
  if (!a) {
    return 1;
  }
  if (!b) {
    return -1;
  }

  const maxLength = Math.max(a.length, b.length);
  for (let index = 0; index < maxLength; index += 1) {
    const left = a[index];
    const right = b[index];

    if (left === undefined) {
      return -1;
    }
    if (right === undefined) {
      return 1;
    }
    if (left === right) {
      continue;
    }

    const leftIsNumber = typeof left === "number";
    const rightIsNumber = typeof right === "number";

    if (leftIsNumber && rightIsNumber) {
      return left - right;
    }
    if (leftIsNumber) {
      return -1;
    }
    if (rightIsNumber) {
      return 1;
    }

    const textCompare = left.localeCompare(right, "en", { sensitivity: "base" });
    if (textCompare !== 0) {
      return textCompare;
    }
  }

  return 0;
};

const resolveLocalPath = (rawPath: string): string => {
  const trimmed = rawPath.trim();
  if (!trimmed) {
    return trimmed;
  }

  if (trimmed === "~") {
    return os.homedir();
  }

  if (trimmed.startsWith("~/")) {
    return path.join(os.homedir(), trimmed.slice(2));
  }

  return path.resolve(trimmed);
};

const parseUptimeSeconds = (raw: string): number => {
  const firstLine = raw.split(/\r?\n/).find((line) => line.trim().length > 0)?.trim() ?? "";
  if (!firstLine) {
    return 0;
  }

  const direct = parseFloatSafe(firstLine.split(/\s+/)[0]);
  if (direct > 0) {
    return direct;
  }

  const lower = firstLine.toLowerCase();
  let seconds = 0;

  const dayMatch = lower.match(/(\d+)\s+day/);
  if (dayMatch?.[1]) {
    seconds += parseIntSafe(dayMatch[1]) * 24 * 3600;
  }

  const hourMinuteMatch = lower.match(/(\d+):(\d+)/);
  if (hourMinuteMatch?.[1] && hourMinuteMatch[2]) {
    seconds += parseIntSafe(hourMinuteMatch[1]) * 3600 + parseIntSafe(hourMinuteMatch[2]) * 60;
  } else {
    const hourMatch = lower.match(/(\d+)\s+hr/);
    if (hourMatch?.[1]) {
      seconds += parseIntSafe(hourMatch[1]) * 3600;
    }
    const minuteMatch = lower.match(/(\d+)\s+min/);
    if (minuteMatch?.[1]) {
      seconds += parseIntSafe(minuteMatch[1]) * 60;
    }
  }

  return seconds;
};

/** Parse a compound probe output back into named sections. */
const parseCompoundOutput = (stdout: string): Map<string, string> => {
  const sections = new Map<string, string>();
  const lines = stdout.split("\n");
  let currentSection = "";
  let currentContent: string[] = [];

  for (const line of lines) {
    const match = line.match(/^---NS_(\w+)---\r?$/);
    if (match?.[1]) {
      if (currentSection) {
        sections.set(currentSection, currentContent.join("\n"));
      }
      currentSection = match[1];
      currentContent = [];
    } else {
      currentContent.push(line);
    }
  }
  if (currentSection) {
    sections.set(currentSection, currentContent.join("\n"));
  }

  return sections;
};

/**
 * Build a single compound shell command to collect all system info
 * (hostname, kernel, CPU, memory, filesystems, etc.) in one exec.
 */
const buildSystemInfoCommand = (): string => {
  return [
    "echo '---NS_UPTIME---'",
    MONITOR_UPTIME_COMMAND,
    "echo '---NS_OSRELEASE---'",
    MONITOR_SYSTEM_INFO_OS_RELEASE_COMMAND,
    "echo '---NS_HOSTNAME---'",
    MONITOR_SYSTEM_INFO_HOSTNAME_COMMAND,
    "echo '---NS_KERNELNAME---'",
    MONITOR_SYSTEM_INFO_KERNEL_NAME_COMMAND,
    "echo '---NS_KERNELVER---'",
    MONITOR_SYSTEM_INFO_KERNEL_VERSION_COMMAND,
    "echo '---NS_ARCH---'",
    MONITOR_SYSTEM_INFO_ARCH_COMMAND,
    "echo '---NS_CPUINFO---'",
    MONITOR_SYSTEM_INFO_CPUINFO_COMMAND,
    "echo '---NS_MEMINFO---'",
    MONITOR_SYSTEM_INFO_MEMINFO_COMMAND,
    "echo '---NS_NETDEV---'",
    MONITOR_SYSTEM_INFO_NET_DEV_COMMAND,
    "echo '---NS_FILESYSTEMS---'",
    MONITOR_SYSTEM_INFO_FILESYSTEMS_COMMAND,
    "echo '---NS_SYSINFO_END---'"
  ].join("; ");
};

export const createServiceContainer = (
  options: CreateServiceContainerOptions
): ServiceContainer => {
  fs.mkdirSync(options.dataDir, { recursive: true });
  const dbPath = path.join(options.dataDir, "nextshell.db");

  // Apply pending restore before opening database
  applyPendingRestore(options.dataDir, dbPath);

  const rawRepo = new SQLiteConnectionRepository(dbPath);
  const connections = new CachedConnectionRepository(rawRepo);
  connections.seedIfEmpty([]);

  // ─── Sibling repositories (share same SQLite DB) ──────────────────────────
  const sshKeyRepo = new CachedSshKeyRepository(new SQLiteSshKeyRepository(rawRepo.getDb()));
  const proxyRepo = new CachedProxyRepository(new SQLiteProxyRepository(rawRepo.getDb()));

  // ─── Device Key (always-on local credential encryption) ────────────────────
  let deviceKeyHex = connections.getDeviceKey();
  if (!deviceKeyHex) {
    deviceKeyHex = generateDeviceKey();
    connections.saveDeviceKey(deviceKeyHex);
    logger.info("[Security] generated new device key");
  }
  const vault = new EncryptedSecretVault(connections.getSecretStore(), Buffer.from(deviceKeyHex, "hex"));

  // ─── Master Password (backup/export/reveal authorization) ─────────────────
  const keytarServiceName = options.keytarServiceName ?? "NextShell";
  const keytarCache = new KeytarPasswordCache(keytarServiceName);
  let masterPassword: string | undefined;

  const tryRecallMasterPassword = async (): Promise<void> => {
    if (masterPassword) {
      return;
    }
    const meta = connections.getMasterKeyMeta();
    if (!meta) return;
    const cached = await keytarCache.recall();
    if (!cached) return;
    if (await verifyMasterPassword(cached, meta)) {
      masterPassword = cached;
      logger.info("[Security] recalled master password from keytar");
    }
  };

  void tryRecallMasterPassword();

  const backupService = new BackupService({
    dataDir: options.dataDir,
    repo: connections,
    getMasterPassword: () => masterPassword
  });

  const auditEnabledForSession = connections.getAppPreferences().audit.enabled;
  const auditRuntime = resolveAuditRuntime(connections.getAppPreferences().audit);
  const appendAuditLogDirect = connections.appendAuditLog.bind(connections);

  const appendAuditLogIfEnabled = (payload: {
    action: string;
    level: "info" | "warn" | "error";
    connectionId?: string;
    message: string;
    metadata?: Record<string, unknown>;
  }): void => {
    if (!auditEnabledForSession) {
      return;
    }
    appendAuditLogDirect(payload);
  };

  const broadcastToAllWindows = (channel: string, payload: unknown): void => {
    for (const window of BrowserWindow.getAllWindows()) {
      if (window.isDestroyed()) {
        continue;
      }
      window.webContents.send(channel, payload);
    }
  };

  // ─── Audit log auto-purge ──────────────────────────────────────────────
  const purgeExpiredAuditLogs = (allowWhenDisabled = false): void => {
    try {
      if (!auditEnabledForSession && !allowWhenDisabled) {
        return;
      }
      const prefs = connections.getAppPreferences();
      const days = prefs.audit.retentionDays;
      if (days > 0) {
        const deleted = connections.purgeExpiredAuditLogs(days);
        if (deleted > 0) {
          logger.info(`[Audit] purged ${deleted} expired audit log(s) (retention=${days}d)`);
        }
      }
    } catch (error) {
      logger.warn("[Audit] failed to purge expired logs", error);
    }
  };

  if (auditRuntime.runStartupPurge) {
    const prefs = connections.getAppPreferences();
    const days = prefs.audit.retentionDays;
    if (days > 0) {
      purgeExpiredAuditLogs(true);
    }
  }
  const auditPurgeTimer = auditRuntime.runPeriodicPurge
    ? setInterval(purgeExpiredAuditLogs, 6 * 3600_000)
    : undefined;

  const activeSessions = new Map<string, ActiveSession>();
  const activeConnections = new Map<string, SshConnection>();
  const connectionPromises = new Map<string, Promise<SshConnection>>();
  // ─── Hidden Session Maps ──────────────────────────────────────────────
  const systemMonitorRuntimes = new Map<string, SystemMonitorRuntime>();
  const systemMonitorConnections = new Map<string, SshConnection>();
  const systemMonitorConnectionPromises = new Map<string, Promise<SshConnection>>();
  const cancelledSystemMonitorConnections = new Set<string>();
  const processMonitorRuntimes = new Map<string, ProcessMonitorRuntime>();
  const processMonitorPromises = new Map<string, Promise<ProcessMonitorRuntime>>();
  const processMonitorConnections = new Map<string, SshConnection>();
  const processMonitorConnectionPromises = new Map<string, Promise<SshConnection>>();
  const cancelledProcessMonitorConnections = new Set<string>();
  const networkMonitorRuntimes = new Map<string, NetworkMonitorRuntime>();
  const networkMonitorPromises = new Map<string, Promise<NetworkMonitorRuntime>>();
  const networkMonitorConnections = new Map<string, SshConnection>();
  const networkMonitorConnectionPromises = new Map<string, Promise<SshConnection>>();
  const cancelledNetworkMonitorConnections = new Set<string>();
  const adhocSessionRuntimes = new Map<string, AdhocSessionRuntime>();
  const adhocSessionPromises = new Map<string, Promise<AdhocSessionRuntime>>();
  const monitorStates = new Map<string, MonitorState>();
  const networkToolCache = new Map<string, NetworkTool>();

  const getConnectionOrThrow = (id: string): ConnectionProfile => {
    const connection = connections.getById(id);
    if (!connection) {
      throw new Error("Connection not found");
    }
    return connection;
  };

  const resolveConnectOptions = async (
    profile: ConnectionProfile,
    authOverride?: SessionAuthOverrideInput
  ): Promise<SshConnectOptions> => {
    // ── Proxy resolution ──────────────────────────────────────────────────
    let proxy: SshConnectOptions["proxy"];
    if (profile.proxyId) {
      const proxyProfile = proxyRepo.getById(profile.proxyId);
      if (!proxyProfile) {
        throw new Error("Referenced proxy profile not found. Please update the connection.");
      }

      const proxySecret = proxyProfile.credentialRef
        ? await vault.readCredential(proxyProfile.credentialRef)
        : undefined;

      proxy = {
        type: proxyProfile.proxyType,
        host: proxyProfile.host,
        port: proxyProfile.port,
        username: proxyProfile.username,
        password:
          proxyProfile.proxyType === "socks5" && proxyProfile.username
            ? proxySecret
            : undefined
      };

      if (!proxy.host || proxy.port <= 0) {
        throw new Error("Proxy host and port are required when proxy is enabled.");
      }
    }

    const username = authOverride?.username?.trim() || profile.username.trim();
    if (!username) {
      throw new Error("SSH username is required.");
    }

    const prefs = connections.getAppPreferences();
    const keepAliveEnabled = profile.keepAliveEnabled ?? prefs.ssh.keepAliveEnabled;
    const intervalCandidate = profile.keepAliveIntervalSec ?? prefs.ssh.keepAliveIntervalSec;
    const keepAliveIntervalSec =
      Number.isInteger(intervalCandidate) &&
      intervalCandidate >= 5 &&
      intervalCandidate <= 600
        ? intervalCandidate
        : prefs.ssh.keepAliveIntervalSec;
    const keepaliveInterval = keepAliveEnabled ? keepAliveIntervalSec * 1000 : 0;

    const base: Omit<SshConnectOptions, "authType"> = {
      host: profile.host,
      port: profile.port,
      username,
      hostFingerprint: profile.hostFingerprint,
      strictHostKeyChecking: profile.strictHostKeyChecking,
      proxy,
      keepaliveInterval
    };

    const secret = profile.credentialRef
      ? await vault.readCredential(profile.credentialRef)
      : undefined;
    const effectiveAuthType = authOverride?.authType ?? profile.authType;
    const isPasswordStyleAuth = effectiveAuthType === "password" || effectiveAuthType === "interactive";

    if (isPasswordStyleAuth) {
      const password =
        authOverride?.authType === "password" || authOverride?.authType === "interactive"
          ? authOverride.password
          : profile.authType === "password" || profile.authType === "interactive"
            ? secret
            : undefined;

      if (!password) {
        if (effectiveAuthType === "interactive") {
          throw new Error("Interactive auth requires password");
        }
        throw new Error("Password credential is missing. Please provide password.");
      }

      return {
        ...base,
        authType: effectiveAuthType,
        password
      };
    }

    if (effectiveAuthType === "privateKey") {
      // Resolve SSH key entity
      const effectiveKeyId = authOverride?.sshKeyId ?? profile.sshKeyId;
      let privateKey: string | undefined;
      let passphrase: string | undefined;

      if (authOverride?.privateKeyContent) {
        // Temporary key content for retry (not persisted as entity)
        privateKey = authOverride.privateKeyContent;
        passphrase = authOverride.passphrase;
      } else if (effectiveKeyId) {
        const keyProfile = sshKeyRepo.getById(effectiveKeyId);
        if (!keyProfile) {
          throw new Error("Referenced SSH key not found. Please update the connection.");
        }
        privateKey = await vault.readCredential(keyProfile.keyContentRef);
        if (keyProfile.passphraseRef) {
          passphrase = await vault.readCredential(keyProfile.passphraseRef);
        }
        // Allow override passphrase (e.g. retry with different passphrase)
        if (authOverride?.passphrase) {
          passphrase = authOverride.passphrase;
        }
      }

      if (!privateKey) {
        throw new Error("Private key auth requires an SSH key. Please select a key.");
      }

      return {
        ...base,
        authType: "privateKey",
        privateKey,
        passphrase
      };
    }

    return {
      ...base,
      authType: "agent"
    };
  };

  const sessionDataDispatcher = createOrderedBytesDispatcher<SessionDataEvent>({
    channel: IPCChannel.SessionData,
    flushIntervalMs: 16,
    targetChunkBytes: 64 * 1024,
    highWaterBytes: 512 * 1024,
    lowWaterBytes: 256 * 1024,
    buildPayload: ({ streamId, deliveryId, chunk }) => ({
      sessionId: streamId,
      data: chunk,
      deliveryId,
      byteLength: Buffer.byteLength(chunk, "utf8")
    })
  });

  const createMonitorDispatcher = <TSnapshot>(channel: string) =>
    createLatestOnlyDispatcher<StreamDeliveryEnvelope<TSnapshot>, TSnapshot>({
      channel,
      buildPayload: ({ deliveryId, payload }) => ({
        deliveryId,
        payload
      })
    });

  const systemMonitorDispatcher = createMonitorDispatcher<MonitorSnapshot>(
    IPCChannel.MonitorSystemData
  );
  const processMonitorDispatcher = createMonitorDispatcher<ProcessSnapshot>(
    IPCChannel.MonitorProcessData
  );
  const networkMonitorDispatcher = createMonitorDispatcher<NetworkSnapshot>(
    IPCChannel.MonitorNetworkData
  );

  const sendSessionStatus = (sender: WebContents, payload: SessionStatusEvent): void => {
    if (!sender.isDestroyed()) {
      sender.send(IPCChannel.SessionStatus, payload);
    }
  };

  const sendTransferStatus = (
    sender: WebContents | undefined,
    payload: SftpTransferStatusEvent
  ): void => {
    if (!sender || sender.isDestroyed()) {
      return;
    }

    sender.send(IPCChannel.SftpTransferStatus, payload);
  };

  const ackStreamDelivery = (input: StreamDeliveryAckInput): { ok: true } => {
    switch (input.streamKind) {
      case "session":
        sessionDataDispatcher.ack({
          streamId: input.streamId,
          deliveryId: input.deliveryId,
          consumedBytes: input.consumedBytes
        });
        break;
      case "monitor-system":
        systemMonitorDispatcher.ack({
          streamId: input.streamId,
          deliveryId: input.deliveryId
        });
        break;
      case "monitor-process":
        processMonitorDispatcher.ack({
          streamId: input.streamId,
          deliveryId: input.deliveryId
        });
        break;
      case "monitor-network":
        networkMonitorDispatcher.ack({
          streamId: input.streamId,
          deliveryId: input.deliveryId
        });
        break;
    }

    return { ok: true };
  };

  const finalizeRemoteSession = (
    sessionId: string,
    status: Extract<SessionStatus, "disconnected" | "failed">,
    reason?: string
  ): void => {
    const active = activeSessions.get(sessionId);
    if (!active) {
      return;
    }

    if (active.kind === "remote" && active.bootstrapSuppression) {
      clearTimeout(active.bootstrapSuppression.timeout);
      active.bootstrapSuppression = undefined;
    }

    active.descriptor.status = status;
    sessionDataDispatcher.closeWhenDrained(sessionId, () => {
      const drained = activeSessions.get(sessionId);
      if (!drained || drained.kind !== "remote") {
        return;
      }

      activeSessions.delete(sessionId);
      drained.descriptor.status = status;
      sendSessionStatus(drained.sender, { sessionId, status, reason });
      void closeConnectionIfIdle(drained.connectionId);
    });
  };

  const finalizeLocalSession = (
    sessionId: string,
    status: Extract<SessionStatus, "disconnected" | "failed">,
    reason?: string
  ): void => {
    const active = activeSessions.get(sessionId);
    if (!active || active.kind !== "local") {
      return;
    }

    active.descriptor.status = status;
    sessionDataDispatcher.closeWhenDrained(sessionId, () => {
      const drained = activeSessions.get(sessionId);
      if (!drained || drained.kind !== "local") {
        return;
      }

      activeSessions.delete(sessionId);
      drained.descriptor.status = status;
      sendSessionStatus(drained.sender, { sessionId, status, reason });
      appendAuditLogIfEnabled({
        action: "session.local_close",
        level: status === "failed" ? "error" : "info",
        message: "Local terminal session closed",
        metadata: { sessionId, reason }
      });
    });
  };

  const establishConnection = async (
    connectionId: string,
    profile: ConnectionProfile,
    authOverride?: SessionAuthOverrideInput
  ): Promise<SshConnection> => {
    logger.info("[SSH] connecting", { connectionId, host: profile.host, port: profile.port });
    const ssh = await SshConnection.connect(await resolveConnectOptions(profile, authOverride));
    ssh.onClose(() => {
      activeConnections.delete(connectionId);
      void remoteEditManager.cleanupByConnectionId(connectionId);
      logger.info("[SSH] disconnected", { connectionId });
    });
    activeConnections.set(connectionId, ssh);
    logger.info("[SSH] connected", { connectionId });
    return ssh;
  };

  const ensureConnection = async (
    connectionId: string,
    authOverride?: SessionAuthOverrideInput
  ): Promise<SshConnection> => {
    const existing = activeConnections.get(connectionId);
    if (existing) {
      return existing;
    }

    if (authOverride) {
      const profile = getConnectionOrThrow(connectionId);
      return establishConnection(connectionId, profile, authOverride);
    }

    const pending = connectionPromises.get(connectionId);
    if (pending) {
      return pending;
    }

    const profile = getConnectionOrThrow(connectionId);

    const promise = establishConnection(connectionId, profile);

    connectionPromises.set(connectionId, promise);

    try {
      return await promise;
    } finally {
      connectionPromises.delete(connectionId);
    }
  };

  const hasVisibleTerminalAlive = (connectionId: string): boolean => {
    return Array.from(activeSessions.values()).some((session) => {
      return (
        session.kind === "remote" &&
        session.connectionId === connectionId &&
        session.descriptor.type === "terminal" &&
        session.descriptor.status === "connected"
      );
    });
  };

  const assertMonitorEnabled = (connectionId: string): ConnectionProfile => {
    const profile = getConnectionOrThrow(connectionId);
    if (!profile.monitorSession) {
      throw new Error("当前连接未启用 Monitor Session，请在连接配置中开启后重试。");
    }
    return profile;
  };

  const assertVisibleTerminalAlive = (connectionId: string): void => {
    if (!hasVisibleTerminalAlive(connectionId)) {
      throw new Error("请先连接 SSH 终端以启动 Monitor Session。");
    }
  };

  // ─── Session ① System Monitor: dispose ────────────────────────────────────

  const disposeSystemMonitorRuntime = async (connectionId: string): Promise<void> => {
    const runtime = systemMonitorRuntimes.get(connectionId);
    if (runtime) {
      runtime.disposed = true;
      systemMonitorDispatcher.clear(connectionId);
      await runtime.controller.stop();
      systemMonitorRuntimes.delete(connectionId);
    }
    await closeSystemMonitorConnection(connectionId);
  };

  // ─── Session ② Process Monitor: dispose ────────────────────────────────────

  const disposeProcessMonitorRuntime = async (connectionId: string): Promise<void> => {
    const runtime = processMonitorRuntimes.get(connectionId);
    if (runtime) {
      runtime.disposed = true;
      processMonitorDispatcher.clear(connectionId);
      await runtime.controller.stop();
      processMonitorRuntimes.delete(connectionId);
    }
    await closeProcessMonitorConnection(connectionId);
    processMonitorPromises.delete(connectionId);
  };

  // ─── Session ③ Network Monitor: dispose ────────────────────────────────────

  const disposeNetworkMonitorRuntime = async (connectionId: string): Promise<void> => {
    const runtime = networkMonitorRuntimes.get(connectionId);
    if (runtime) {
      runtime.disposed = true;
      networkMonitorDispatcher.clear(connectionId);
      await runtime.controller.stop();
      networkMonitorRuntimes.delete(connectionId);
    }
    await closeNetworkMonitorConnection(connectionId);
    networkMonitorPromises.delete(connectionId);
  };

  // ─── Session ④ Ad-hoc: dispose ─────────────────────────────────────────────

  const disposeAdhocSession = async (connectionId: string): Promise<void> => {
    const runtime = adhocSessionRuntimes.get(connectionId);
    if (runtime) {
      runtime.disposed = true;
      if (runtime.idleTimer) {
        clearTimeout(runtime.idleTimer);
        runtime.idleTimer = undefined;
      }
      adhocSessionRuntimes.delete(connectionId);

      try { await runtime.connection.close(); } catch (error) {
        logger.warn("[AdhocSession] failed to close connection", { connectionId, reason: normalizeError(error) });
      }
    }
    adhocSessionPromises.delete(connectionId);
  };

  // ─── Dispose all hidden sessions for a connection ──────────────────────────

  const disposeAllMonitorSessions = async (connectionId: string): Promise<void> => {
    await Promise.all([
      disposeSystemMonitorRuntime(connectionId),
      disposeProcessMonitorRuntime(connectionId),
      disposeNetworkMonitorRuntime(connectionId),
      disposeAdhocSession(connectionId)
    ]);
    monitorStates.delete(connectionId);
    networkToolCache.delete(connectionId);
  };

  // ─── Generic hidden SSH connection factory ────────────────────────────────

  const establishHiddenConnection = async (
    connectionId: string,
    tag: string
  ): Promise<SshConnection> => {
    const profile = assertMonitorEnabled(connectionId);
    logger.info(`[${tag}] connecting hidden SSH`, { connectionId, host: profile.host, port: profile.port });
    const ssh = await SshConnection.connect(await resolveConnectOptions(profile));
    logger.info(`[${tag}] hidden SSH connected`, { connectionId });
    return ssh;
  };

  const closeSystemMonitorConnection = async (connectionId: string): Promise<void> => {
    cancelledSystemMonitorConnections.add(connectionId);
    const existing = systemMonitorConnections.get(connectionId);
    systemMonitorConnections.delete(connectionId);
    systemMonitorConnectionPromises.delete(connectionId);
    if (!existing) {
      return;
    }

    try {
      await existing.close();
    } catch (error) {
      logger.warn("[SystemMonitor] failed to close connection", {
        connectionId,
        reason: normalizeError(error),
      });
    }
  };

  const ensureSystemMonitorConnection = async (connectionId: string): Promise<SshConnection> => {
    cancelledSystemMonitorConnections.delete(connectionId);
    const existing = systemMonitorConnections.get(connectionId);
    if (existing) {
      return existing;
    }

    const pending = systemMonitorConnectionPromises.get(connectionId);
    if (pending) {
      return pending;
    }

    const promise = (async () => {
      const connection = await establishHiddenConnection(connectionId, "SystemMonitor");
      if (cancelledSystemMonitorConnections.has(connectionId)) {
        cancelledSystemMonitorConnections.delete(connectionId);
        try { await connection.close(); } catch { /* ignore */ }
        throw new Error("SystemMonitor connection discarded");
      }

      systemMonitorConnections.set(connectionId, connection);
      connection.onClose(() => {
        const wasActive = systemMonitorConnections.get(connectionId) === connection;
        if (wasActive) {
          systemMonitorConnections.delete(connectionId);
          logger.warn("[SystemMonitor] hidden SSH disconnected unexpectedly", { connectionId });
        }
      });
      return connection;
    })();

    systemMonitorConnectionPromises.set(connectionId, promise);
    try {
      return await promise;
    } finally {
      if (systemMonitorConnectionPromises.get(connectionId) === promise) {
        systemMonitorConnectionPromises.delete(connectionId);
      }
    }
  };

  const closeProcessMonitorConnection = async (connectionId: string): Promise<void> => {
    cancelledProcessMonitorConnections.add(connectionId);
    const existing = processMonitorConnections.get(connectionId);
    processMonitorConnections.delete(connectionId);
    processMonitorConnectionPromises.delete(connectionId);
    if (!existing) {
      return;
    }

    try {
      await existing.close();
    } catch (error) {
      logger.warn("[ProcessMonitor] failed to close connection", {
        connectionId,
        reason: normalizeError(error),
      });
    }
  };

  const ensureProcessMonitorConnection = async (connectionId: string): Promise<SshConnection> => {
    cancelledProcessMonitorConnections.delete(connectionId);
    const existing = processMonitorConnections.get(connectionId);
    if (existing) {
      return existing;
    }

    const pending = processMonitorConnectionPromises.get(connectionId);
    if (pending) {
      return pending;
    }

    const promise = (async () => {
      const connection = await establishHiddenConnection(connectionId, "ProcessMonitor");
      if (cancelledProcessMonitorConnections.has(connectionId)) {
        cancelledProcessMonitorConnections.delete(connectionId);
        try { await connection.close(); } catch { /* ignore */ }
        throw new Error("ProcessMonitor connection discarded");
      }

      processMonitorConnections.set(connectionId, connection);
      connection.onClose(() => {
        const wasActive = processMonitorConnections.get(connectionId) === connection;
        if (wasActive) {
          processMonitorConnections.delete(connectionId);
          logger.warn("[ProcessMonitor] hidden SSH disconnected unexpectedly", { connectionId });
        }
      });
      return connection;
    })();

    processMonitorConnectionPromises.set(connectionId, promise);
    try {
      return await promise;
    } finally {
      if (processMonitorConnectionPromises.get(connectionId) === promise) {
        processMonitorConnectionPromises.delete(connectionId);
      }
    }
  };

  const closeNetworkMonitorConnection = async (connectionId: string): Promise<void> => {
    cancelledNetworkMonitorConnections.add(connectionId);
    const existing = networkMonitorConnections.get(connectionId);
    networkMonitorConnections.delete(connectionId);
    networkMonitorConnectionPromises.delete(connectionId);
    if (!existing) {
      return;
    }

    try {
      await existing.close();
    } catch (error) {
      logger.warn("[NetworkMonitor] failed to close connection", {
        connectionId,
        reason: normalizeError(error),
      });
    }
  };

  const ensureNetworkMonitorConnection = async (connectionId: string): Promise<SshConnection> => {
    cancelledNetworkMonitorConnections.delete(connectionId);
    const existing = networkMonitorConnections.get(connectionId);
    if (existing) {
      return existing;
    }

    const pending = networkMonitorConnectionPromises.get(connectionId);
    if (pending) {
      return pending;
    }

    const promise = (async () => {
      const connection = await establishHiddenConnection(connectionId, "NetworkMonitor");
      if (cancelledNetworkMonitorConnections.has(connectionId)) {
        cancelledNetworkMonitorConnections.delete(connectionId);
        try { await connection.close(); } catch { /* ignore */ }
        throw new Error("NetworkMonitor connection discarded");
      }

      networkMonitorConnections.set(connectionId, connection);
      connection.onClose(() => {
        const wasActive = networkMonitorConnections.get(connectionId) === connection;
        if (wasActive) {
          networkMonitorConnections.delete(connectionId);
          logger.warn("[NetworkMonitor] hidden SSH disconnected unexpectedly", { connectionId });
        }
      });
      return connection;
    })();

    networkMonitorConnectionPromises.set(connectionId, promise);
    try {
      return await promise;
    } finally {
      if (networkMonitorConnectionPromises.get(connectionId) === promise) {
        networkMonitorConnectionPromises.delete(connectionId);
      }
    }
  };

  // ─── Session ① System Monitor: ensure ─────────────────────────────────────

  const ensureSystemMonitorRuntime = async (connectionId: string): Promise<SystemMonitorRuntime> => {
    const existing = systemMonitorRuntimes.get(connectionId);
    if (existing && !existing.disposed) {
      return existing;
    }

    let runtime: SystemMonitorRuntime;

    const onProbeExecution = (entry: ProbeExecutionLog) => {
      if (debugSenders.size > 0) {
        emitDebugLog({
          id: randomUUID(),
          timestamp: Date.now(),
          connectionId,
          command: entry.command,
          stdout: entry.stdout.slice(0, 4096),
          exitCode: entry.exitCode,
          durationMs: entry.durationMs,
          ok: entry.ok,
          error: entry.error,
        });
      }

      if (!entry.ok && entry.exitCode >= 0) {
        logger.debug("[SystemMonitor] command non-zero exit", {
          connectionId,
          command: entry.command,
          exitCode: entry.exitCode,
          output: entry.stdout.slice(0, 200),
        });
      }
    };

    const controller = new SystemMonitorController({
      connectionId,
      getConnection: () => ensureSystemMonitorConnection(connectionId),
      closeConnection: () => closeSystemMonitorConnection(connectionId),
      isVisibleTerminalAlive: () => hasVisibleTerminalAlive(connectionId),
      isReceiverAlive: () => Boolean(runtime.sender && !runtime.sender.isDestroyed()),
      emitSnapshot: (snapshot) => {
        if (runtime.sender && !runtime.sender.isDestroyed()) {
          systemMonitorDispatcher.publish({
            streamId: connectionId,
            sender: runtime.sender,
            payload: snapshot
          });
        }
      },
      readSelection: () => monitorStates.get(connectionId),
      writeSelection: (state: MonitorSelectionState) => {
        const previous = monitorStates.get(connectionId);
        monitorStates.set(connectionId, { ...previous, ...state });
      },
      logger,
      onProbeExecution,
    });

    runtime = {
      disposed: false,
      controller,
      sender: undefined,
    };

    systemMonitorRuntimes.set(connectionId, runtime);
    logger.info("[SystemMonitor] runtime ready", { connectionId });
    return runtime;
  };

  // ─── Session ② Process Monitor: ensure ────────────────────────────────────

  const ensureProcessMonitorRuntime = async (connectionId: string): Promise<ProcessMonitorRuntime> => {
    const existing = processMonitorRuntimes.get(connectionId);
    if (existing && !existing.disposed) {
      return existing;
    }

    const pending = processMonitorPromises.get(connectionId);
    if (pending) {
      return pending;
    }

    const promise = (async () => {
      let runtime: ProcessMonitorRuntime;
      const onProbeExecution = (entry: ProcessProbeExecutionLog) => {
        if (debugSenders.size > 0) {
          emitDebugLog({
            id: randomUUID(),
            timestamp: Date.now(),
            connectionId,
            command: entry.command,
            stdout: entry.stdout.slice(0, 4096),
            exitCode: entry.exitCode,
            durationMs: entry.durationMs,
            ok: entry.ok,
            error: entry.error,
          });
        }
      };

      const controller = new ProcessMonitorController({
        connectionId,
        getConnection: () => ensureProcessMonitorConnection(connectionId),
        closeConnection: () => closeProcessMonitorConnection(connectionId),
        isVisibleTerminalAlive: () => hasVisibleTerminalAlive(connectionId),
        isReceiverAlive: () => Boolean(runtime.sender && !runtime.sender.isDestroyed()),
        emitSnapshot: (snapshot) => {
          if (runtime.sender && !runtime.sender.isDestroyed()) {
            processMonitorDispatcher.publish({
              streamId: connectionId,
              sender: runtime.sender,
              payload: snapshot
            });
          }
        },
        logger,
        onProbeExecution,
        timing: {
          pollIntervalMs: MONITOR_PROCESS_INTERVAL_MS,
          execTimeoutMs: MONITOR_COMMAND_TIMEOUT_MS,
          maxConsecutiveFailures: MONITOR_MAX_CONSECUTIVE_FAILURES,
        },
      });

      runtime = {
        controller,
        sender: undefined,
        disposed: false,
      };

      processMonitorRuntimes.set(connectionId, runtime);

      if (!hasVisibleTerminalAlive(connectionId)) {
        await disposeProcessMonitorRuntime(connectionId);
        throw new Error("可见 SSH 会话已关闭，Process Monitor 启动取消。");
      }

      logger.info("[ProcessMonitor] runtime ready", { connectionId });
      return runtime;
    })();

    processMonitorPromises.set(connectionId, promise);
    try {
      return await promise;
    } catch (error) {
      await disposeProcessMonitorRuntime(connectionId);
      throw error;
    } finally {
      processMonitorPromises.delete(connectionId);
    }
  };

  // ─── Session ③ Network Monitor: ensure ────────────────────────────────────

  const ensureNetworkMonitorRuntime = async (connectionId: string): Promise<NetworkMonitorRuntime> => {
    const existing = networkMonitorRuntimes.get(connectionId);
    if (existing && !existing.disposed) {
      return existing;
    }

    const pending = networkMonitorPromises.get(connectionId);
    if (pending) {
      return pending;
    }

    const promise = (async () => {
      let runtime: NetworkMonitorRuntime;
      const onProbeExecution = (entry: NetworkProbeExecutionLog) => {
        if (debugSenders.size > 0) {
          emitDebugLog({
            id: randomUUID(),
            timestamp: Date.now(),
            connectionId,
            command: entry.command,
            stdout: entry.stdout.slice(0, 4096),
            exitCode: entry.exitCode,
            durationMs: entry.durationMs,
            ok: entry.ok,
            error: entry.error,
          });
        }
      };

      const controller = new NetworkMonitorController({
        connectionId,
        getConnection: () => ensureNetworkMonitorConnection(connectionId),
        closeConnection: () => closeNetworkMonitorConnection(connectionId),
        isVisibleTerminalAlive: () => hasVisibleTerminalAlive(connectionId),
        isReceiverAlive: () => Boolean(runtime.sender && !runtime.sender.isDestroyed()),
        emitSnapshot: (snapshot) => {
          if (runtime.sender && !runtime.sender.isDestroyed()) {
            networkMonitorDispatcher.publish({
              streamId: connectionId,
              sender: runtime.sender,
              payload: snapshot
            });
          }
        },
        readToolCache: () => networkToolCache.get(connectionId),
        writeToolCache: (tool) => {
          if (tool) {
            networkToolCache.set(connectionId, tool);
          } else {
            networkToolCache.delete(connectionId);
          }
        },
        logger,
        onProbeExecution,
        timing: {
          pollIntervalMs: MONITOR_NETWORK_INTERVAL_MS,
          execTimeoutMs: MONITOR_COMMAND_TIMEOUT_MS,
          maxConsecutiveFailures: MONITOR_MAX_CONSECUTIVE_FAILURES,
        },
      });

      runtime = {
        controller,
        sender: undefined,
        disposed: false,
      };

      networkMonitorRuntimes.set(connectionId, runtime);

      if (!hasVisibleTerminalAlive(connectionId)) {
        await disposeNetworkMonitorRuntime(connectionId);
        throw new Error("可见 SSH 会话已关闭，Network Monitor 启动取消。");
      }

      logger.info("[NetworkMonitor] runtime ready", { connectionId });
      return runtime;
    })();

    networkMonitorPromises.set(connectionId, promise);
    try {
      return await promise;
    } catch (error) {
      await disposeNetworkMonitorRuntime(connectionId);
      throw error;
    } finally {
      networkMonitorPromises.delete(connectionId);
    }
  };

  // ─── Session ④ Ad-hoc: ensure ─────────────────────────────────────────────

  const resetAdhocIdleTimer = (connectionId: string, runtime: AdhocSessionRuntime): void => {
    if (runtime.idleTimer) {
      clearTimeout(runtime.idleTimer);
    }
    runtime.lastUsedAt = Date.now();
    runtime.idleTimer = setTimeout(() => {
      logger.info("[AdhocSession] idle timeout, disposing", { connectionId });
      void disposeAdhocSession(connectionId);
    }, ADHOC_IDLE_TIMEOUT_MS);
  };

  const ensureAdhocSession = async (connectionId: string): Promise<AdhocSessionRuntime> => {
    const existing = adhocSessionRuntimes.get(connectionId);
    if (existing && !existing.disposed) {
      resetAdhocIdleTimer(connectionId, existing);
      return existing;
    }

    const pending = adhocSessionPromises.get(connectionId);
    if (pending) {
      return pending;
    }

    const promise = (async () => {
      const connection = await establishHiddenConnection(connectionId, "AdhocSession");

      const runtime: AdhocSessionRuntime = {
        connection,
        lastUsedAt: Date.now(),
        disposed: false
      };

      connection.onClose(() => {
        if (runtime.disposed) return;
        runtime.disposed = true;
        if (runtime.idleTimer) { clearTimeout(runtime.idleTimer); runtime.idleTimer = undefined; }
        adhocSessionRuntimes.delete(connectionId);
        adhocSessionPromises.delete(connectionId);
        logger.info("[AdhocSession] hidden SSH disconnected", { connectionId });
      });

      adhocSessionRuntimes.set(connectionId, runtime);
      resetAdhocIdleTimer(connectionId, runtime);

      logger.info("[AdhocSession] runtime ready", { connectionId });
      return runtime;
    })();

    adhocSessionPromises.set(connectionId, promise);
    try {
      return await promise;
    } catch (error) {
      await disposeAdhocSession(connectionId);
      throw error;
    } finally {
      adhocSessionPromises.delete(connectionId);
    }
  };

  const warmupSftp = async (
    connectionId: string,
    connection: SshConnection
  ): Promise<string | undefined> => {
    let timeout: ReturnType<typeof setTimeout> | undefined;
    try {
      await Promise.race([
        connection.list("."),
        new Promise<never>((_resolve, reject) => {
          timeout = setTimeout(() => {
            reject(new Error(`SFTP warmup timed out after ${SFTP_WARMUP_TIMEOUT_MS}ms`));
          }, SFTP_WARMUP_TIMEOUT_MS);
        })
      ]);

      appendAuditLogIfEnabled({
        action: "sftp.init_ready",
        level: "info",
        connectionId,
        message: "SFTP warmup completed after SSH session open"
      });
      return undefined;
    } catch (error) {
      const reason = normalizeError(error);
      logger.warn("[SFTP] warmup failed", { connectionId, reason });
      appendAuditLogIfEnabled({
        action: "sftp.init_failed",
        level: "warn",
        connectionId,
        message: "SFTP warmup failed after SSH session open",
        metadata: { reason }
      });
      return `SSH 已连接，但 SFTP 初始化失败：${reason}`;
    } finally {
      if (timeout) {
        clearTimeout(timeout);
      }
    }
  };

  const remoteEditManager = new RemoteEditManager({
    getConnection: ensureConnection
  });

  let cloudSyncService: CloudSyncService | undefined;
  const cloudSyncNetworkSession = electronSession.fromPartition("persist:nextshell-cloud-sync");

  const configureCloudSyncTlsVerification = (apiBaseUrl: string, ignoreTlsErrors: boolean): void => {
    if (!ignoreTlsErrors) {
      cloudSyncNetworkSession.setCertificateVerifyProc(null);
      return;
    }

    void apiBaseUrl;
    cloudSyncNetworkSession.setCertificateVerifyProc((_request, callback) => {
      callback(0);
    });
  };

  const cloudSyncRequestJson = async <T>(
    request: {
      apiBaseUrl: string;
      workspaceName: string;
      workspacePassword: string;
      pathname: string;
      payload: unknown;
      ignoreTlsErrors: boolean;
    },
    schema: { parse: (value: unknown) => T }
  ): Promise<T> => {
    configureCloudSyncTlsVerification(request.apiBaseUrl, request.ignoreTlsErrors);

    const response = await cloudSyncNetworkSession.fetch(`${request.apiBaseUrl}${request.pathname}`, {
      method: "POST",
      headers: {
        Accept: "application/json",
        Authorization: `Basic ${Buffer.from(`${request.workspaceName}:${request.workspacePassword}`, "utf8").toString("base64")}`,
        "Content-Type": "application/json"
      },
      body: JSON.stringify(request.payload)
    });

    if (!response.ok) {
      const bodyText = await response.text();
      if (response.status === 409 && bodyText.trim()) {
        throw new Error(bodyText);
      }
      let message: string | undefined;
      if (bodyText.trim()) {
        try {
          const parsed = JSON.parse(bodyText) as { error?: unknown };
          if (typeof parsed.error === "string" && parsed.error.trim()) {
            message = parsed.error;
          }
        } catch {
          message = bodyText.trim();
        }
        message ??= bodyText.trim();
      }
      throw new Error(message ?? `HTTP ${response.status}`);
    }

    return schema.parse(await response.json());
  };

  const getAppPreferences = (): AppPreferences => {
    return connections.getAppPreferences();
  };

  const saveAppPreferencesPatch = (
    patch: SettingsUpdateInput,
    options?: { reconfigureCloudSync?: boolean }
  ): AppPreferences => {
    const current = connections.getAppPreferences();
    const merged = mergePreferences(current, patch);
    const saved = connections.saveAppPreferences(merged);

    if (patch.window?.appearance !== undefined) {
      applyAppearanceToAllWindows(saved.window.appearance);
    }

    if (patch.audit?.retentionDays !== undefined && auditEnabledForSession) {
      purgeExpiredAuditLogs();
    }

    if (options?.reconfigureCloudSync !== false) {
      void cloudSyncService?.refreshFromPreferences({ triggerPull: false });
    }

    return saved;
  };

  const updateAppPreferences = (patch: SettingsUpdateInput): AppPreferences => {
    return saveAppPreferencesPatch(patch);
  };

  const openFilesDialog = async (
    sender: WebContents,
    input: DialogOpenFilesInput
  ): Promise<{ canceled: boolean; filePaths: string[] }> => {
    const owner = BrowserWindow.fromWebContents(sender);
    const dialogOptions: OpenDialogOptions = {
      title: input.title ?? "选择文件",
      defaultPath: input.defaultPath ? resolveLocalPath(input.defaultPath) : undefined,
      filters: input.filters,
      properties: input.multi
        ? ["openFile", "multiSelections"]
        : ["openFile"],
      buttonLabel: "选择"
    };
    const result = owner
      ? await dialog.showOpenDialog(owner, dialogOptions)
      : await dialog.showOpenDialog(dialogOptions);

    return {
      canceled: result.canceled,
      filePaths: result.filePaths
    };
  };

  const openDirectoryDialog = async (
    sender: WebContents,
    input: DialogOpenDirectoryInput
  ): Promise<{ canceled: boolean; filePath?: string }> => {
    const owner = BrowserWindow.fromWebContents(sender);
    const dialogOptions: OpenDialogOptions = {
      title: input.title ?? "选择目录",
      defaultPath: input.defaultPath ? resolveLocalPath(input.defaultPath) : undefined,
      properties: ["openDirectory", "createDirectory"],
      buttonLabel: "选择"
    };
    const result = owner
      ? await dialog.showOpenDialog(owner, dialogOptions)
      : await dialog.showOpenDialog(dialogOptions);

    return {
      canceled: result.canceled,
      filePath: result.filePaths[0]
    };
  };

  const openLocalPath = async (
    sender: WebContents,
    input: DialogOpenPathInput
  ): Promise<{ ok: boolean; error?: string }> => {
    const owner = BrowserWindow.fromWebContents(sender);
    const externalUrl = parseExternalUrl(input.path);

    if (externalUrl) {
      if (input.revealInFolder) {
        return { ok: false, error: "URL 不支持在文件夹中显示。" };
      }

      try {
        await shell.openExternal(externalUrl.toString());
        return { ok: true };
      } catch (error) {
        const message = error instanceof Error ? error.message : "打开链接失败";
        if (owner) {
          void dialog.showMessageBox(owner, {
            type: "error",
            title: "打开链接失败",
            message
          });
        }
        return { ok: false, error: message };
      }
    }

    const targetPath = resolveLocalPath(input.path);

    if (!targetPath || !fs.existsSync(targetPath)) {
      if (owner) {
        void dialog.showMessageBox(owner, {
          type: "error",
          title: "打开本地文件失败",
          message: "文件不存在或路径无效。"
        });
      }
      return { ok: false, error: "文件不存在或路径无效。" };
    }

    if (input.revealInFolder) {
      shell.showItemInFolder(targetPath);
      return { ok: true };
    }

    const error = await shell.openPath(targetPath);
    return error ? { ok: false, error } : { ok: true };
  };

  const listConnections = (query: ConnectionListQuery): ConnectionProfile[] => {
    return connections.list(query);
  };

  const upsertConnection = async (input: ConnectionUpsertInput): Promise<ConnectionProfile> => {
    const now = new Date().toISOString();
    const id = input.id ?? randomUUID();
    const current = connections.getById(id);
    const isNew = !current;
    const authTypeChanged = Boolean(current && current.authType !== input.authType);
    const needsPasswordCredential = input.authType === "password" || input.authType === "interactive";
    const shouldDropPreviousCredential = input.authType === "agent" || authTypeChanged;

    if (input.authType === "privateKey" && !input.sshKeyId) {
      throw new Error("Private key auth requires selecting an SSH key.");
    }

    // Validate referenced entities exist
    if (input.sshKeyId) {
      const keyProfile = sshKeyRepo.getById(input.sshKeyId);
      if (!keyProfile) {
        throw new Error("Referenced SSH key not found.");
      }
    }
    if (input.proxyId) {
      const proxyProfile = proxyRepo.getById(input.proxyId);
      if (!proxyProfile) {
        throw new Error("Referenced proxy not found.");
      }
    }

    const normalizedUsername = input.username.trim();
    const keepAliveEnabled = input.keepAliveEnabled;
    const keepAliveIntervalSec =
      Number.isInteger(input.keepAliveIntervalSec) &&
      (input.keepAliveIntervalSec as number) >= 5 &&
      (input.keepAliveIntervalSec as number) <= 600
        ? input.keepAliveIntervalSec
        : undefined;

    let credentialRef = current?.credentialRef;

    if (shouldDropPreviousCredential && current?.credentialRef) {
      await vault.deleteCredential(current.credentialRef);
      credentialRef = undefined;
    }

    if (needsPasswordCredential) {
      if (input.password) {
        credentialRef = await vault.storeCredential(`conn-${id}`, input.password);
      }
    } else {
      // privateKey / agent: no password credential on the connection
      if (credentialRef && (isNew || authTypeChanged)) {
        credentialRef = undefined;
      }
    }

    const profile: ConnectionProfile = {
      id,
      name: input.name,
      host: input.host,
      port: input.port,
      username: normalizedUsername,
      authType: input.authType,
      credentialRef: needsPasswordCredential ? credentialRef : undefined,
      sshKeyId: input.authType === "privateKey" ? input.sshKeyId : undefined,
      hostFingerprint: input.hostFingerprint,
      strictHostKeyChecking: input.strictHostKeyChecking,
      proxyId: input.proxyId,
      keepAliveEnabled,
      keepAliveIntervalSec,
      terminalEncoding: input.terminalEncoding,
      backspaceMode: input.backspaceMode,
      deleteMode: input.deleteMode,
      groupPath: input.groupPath,
      tags: input.tags,
      notes: input.notes,
      favorite: input.favorite,
      monitorSession: input.monitorSession,
      createdAt: current?.createdAt ?? now,
      updatedAt: now,
      lastConnectedAt: current?.lastConnectedAt
    };

    connections.save(profile);

    if (!profile.monitorSession) {
      await disposeAllMonitorSessions(profile.id);
    }

    appendAuditLogIfEnabled({
      action: "connection.upsert",
      level: "info",
      connectionId: profile.id,
      message: current ? "Updated connection profile" : "Created connection profile",
      metadata: {
        authType: profile.authType,
        strictHostKeyChecking: profile.strictHostKeyChecking,
        hasSshKey: Boolean(profile.sshKeyId),
        hasProxy: Boolean(profile.proxyId),
        terminalEncoding: profile.terminalEncoding,
        backspaceMode: profile.backspaceMode,
        deleteMode: profile.deleteMode
      }
    });

    void cloudSyncService?.pushConnectionUpsert(profile);
    return profile;
  };

  const applyConnectionFromCloudSync = async (input: CloudSyncApplyConnectionInput): Promise<void> => {
    const current = connections.getById(input.id);
    const needsPasswordCredential = input.authType === "password" || input.authType === "interactive";

    if (input.authType === "privateKey" && !input.sshKeyId) {
      throw new Error("Cloud sync connection is missing sshKeyId for private key auth.");
    }
    if (input.sshKeyId && !sshKeyRepo.getById(input.sshKeyId)) {
      throw new Error(`Cloud sync referenced SSH key not found: ${input.sshKeyId}`);
    }
    if (input.proxyId && !proxyRepo.getById(input.proxyId)) {
      throw new Error(`Cloud sync referenced proxy not found: ${input.proxyId}`);
    }

    let credentialRef = current?.credentialRef;
    if (!needsPasswordCredential && credentialRef) {
      await vault.deleteCredential(credentialRef);
      credentialRef = undefined;
    }
    if (needsPasswordCredential) {
      if (!input.password) {
        throw new Error(`Cloud sync connection ${input.name} is missing password content.`);
      }
      credentialRef = await vault.storeCredential(`conn-${input.id}`, input.password);
    }

    const now = new Date().toISOString();
    const profile: ConnectionProfile = {
      id: input.id,
      name: input.name,
      host: input.host,
      port: input.port,
      username: input.username.trim(),
      authType: input.authType,
      credentialRef: needsPasswordCredential ? credentialRef : undefined,
      sshKeyId: input.authType === "privateKey" ? input.sshKeyId : undefined,
      hostFingerprint: input.hostFingerprint,
      strictHostKeyChecking: input.strictHostKeyChecking,
      proxyId: input.proxyId,
      keepAliveEnabled: input.keepAliveEnabled,
      keepAliveIntervalSec: input.keepAliveIntervalSec,
      terminalEncoding: current?.terminalEncoding ?? "utf-8",
      backspaceMode: current?.backspaceMode ?? "ascii-backspace",
      deleteMode: current?.deleteMode ?? "vt220-delete",
      groupPath: input.groupPath,
      tags: input.tags,
      notes: input.notes,
      favorite: input.favorite,
      monitorSession: current?.monitorSession ?? false,
      createdAt: current?.createdAt ?? now,
      updatedAt: input.updatedAt,
      lastConnectedAt: current?.lastConnectedAt
    };

    connections.save(profile);
    if (!profile.monitorSession) {
      await disposeAllMonitorSessions(profile.id);
    }
  };

  const persistSuccessfulAuthOverride = async (
    connectionId: string,
    authOverride: SessionAuthOverrideInput
  ): Promise<string | undefined> => {
    const latest = getConnectionOrThrow(connectionId);

    // If the override supplies a raw private key, import it as a new SshKeyProfile first
    let effectiveSshKeyId = authOverride.sshKeyId ?? latest.sshKeyId;
    if (authOverride.authType === "privateKey" && authOverride.privateKeyContent) {
      const keyId = randomUUID();
      const keyContentRef = await vault.storeCredential(`sshkey-${keyId}`, authOverride.privateKeyContent);
      let passphraseRef: string | undefined;
      if (authOverride.passphrase) {
        passphraseRef = await vault.storeCredential(`sshkey-${keyId}-pass`, authOverride.passphrase);
      }
      const now = new Date().toISOString();
      sshKeyRepo.save({
        id: keyId,
        name: `${latest.name}-retried-${now}`,
        keyContentRef,
        passphraseRef,
        createdAt: now,
        updatedAt: now,
      });
      effectiveSshKeyId = keyId;
    }

    const payload: ConnectionUpsertInput = {
      id: latest.id,
      name: latest.name,
      host: latest.host,
      port: latest.port,
      username: authOverride.username?.trim() || latest.username,
      authType: authOverride.authType,
      password:
        authOverride.authType === "password" || authOverride.authType === "interactive"
          ? authOverride.password
          : undefined,
      sshKeyId: authOverride.authType === "privateKey" ? effectiveSshKeyId : undefined,
      hostFingerprint: latest.hostFingerprint,
      strictHostKeyChecking: latest.strictHostKeyChecking,
      proxyId: latest.proxyId,
      keepAliveEnabled: latest.keepAliveEnabled,
      keepAliveIntervalSec: latest.keepAliveIntervalSec,
      terminalEncoding: latest.terminalEncoding,
      backspaceMode: latest.backspaceMode,
      deleteMode: latest.deleteMode,
      groupPath: latest.groupPath,
      tags: latest.tags,
      notes: latest.notes,
      favorite: latest.favorite,
      monitorSession: latest.monitorSession
    };

    try {
      await upsertConnection(payload);
      return undefined;
    } catch (error) {
      const reason = normalizeError(error);
      logger.error("[Session] failed to persist auth override", {
        connectionId,
        reason
      });
      appendAuditLogIfEnabled({
        action: "connection.auth_override_persist_failed",
        level: "warn",
        connectionId,
        message: "SSH auth override could not be persisted",
        metadata: {
          reason
        }
      });
      return "认证成功，但自动保存凭据失败，请在连接管理器中手动保存。";
    }
  };

  const closeConnectionIfIdle = async (connectionId: string): Promise<void> => {
    const stillUsed = Array.from(activeSessions.values()).some(
      (session) => session.kind === "remote" && session.connectionId === connectionId
    );

    if (stillUsed) {
      return;
    }

    await disposeAllMonitorSessions(connectionId);

    const client = activeConnections.get(connectionId);
    if (!client) {
      return;
    }

    await remoteEditManager.cleanupByConnectionId(connectionId);
    activeConnections.delete(connectionId);
    await client.close();
  };

  // ── SSH Key CRUD ────────────────────────────────────────────────
  const listSshKeys = (): SshKeyProfile[] => sshKeyRepo.list();

  const upsertSshKey = async (input: SshKeyUpsertInput): Promise<SshKeyProfile> => {
    const now = new Date().toISOString();
    const id = input.id ?? randomUUID();
    const current = sshKeyRepo.getById(id);

    // Store key content in vault
    let keyContentRef = current?.keyContentRef;
    if (input.keyContent) {
      if (current?.keyContentRef) {
        await vault.deleteCredential(current.keyContentRef);
      }
      keyContentRef = await vault.storeCredential(`sshkey-${id}`, input.keyContent);
    }
    if (!keyContentRef) {
      throw new Error("SSH key content is required.");
    }

    // Store passphrase in vault (optional)
    let passphraseRef = current?.passphraseRef;
    if (input.passphrase !== undefined) {
      if (current?.passphraseRef) {
        await vault.deleteCredential(current.passphraseRef);
        passphraseRef = undefined;
      }
      if (input.passphrase) {
        passphraseRef = await vault.storeCredential(`sshkey-${id}-pass`, input.passphrase);
      }
    }

    const profile: SshKeyProfile = {
      id,
      name: input.name,
      keyContentRef,
      passphraseRef,
      createdAt: current?.createdAt ?? now,
      updatedAt: now,
    };

    sshKeyRepo.save(profile);
    void cloudSyncService?.pushSshKeyUpsert(profile);
    return profile;
  };

  const applySshKeyFromCloudSync = async (input: CloudSyncApplySshKeyInput): Promise<void> => {
    const current = sshKeyRepo.getById(input.id);
    const keyContentRef = await vault.storeCredential(`sshkey-${input.id}`, input.keyContent);
    let passphraseRef = current?.passphraseRef;

    if (input.passphrase) {
      passphraseRef = await vault.storeCredential(`sshkey-${input.id}-pass`, input.passphrase);
    } else if (current?.passphraseRef) {
      await vault.deleteCredential(current.passphraseRef);
      passphraseRef = undefined;
    }

    sshKeyRepo.save({
      id: input.id,
      name: input.name,
      keyContentRef,
      passphraseRef,
      createdAt: current?.createdAt ?? input.updatedAt,
      updatedAt: input.updatedAt
    });
  };

  const removeSshKeyRecord = async (
    input: SshKeyRemoveInput
  ): Promise<{ ok: true }> => {
    const profile = sshKeyRepo.getById(input.id);
    if (!profile) {
      throw new Error("SSH key not found.");
    }

    const refs = sshKeyRepo.getReferencingConnectionIds(input.id);
    if (refs.length > 0) {
      throw new Error(`该密钥仍被 ${refs.length} 个连接引用，无法删除。`);
    }

    if (profile.keyContentRef) {
      await vault.deleteCredential(profile.keyContentRef);
    }
    if (profile.passphraseRef) {
      await vault.deleteCredential(profile.passphraseRef);
    }

    sshKeyRepo.remove(input.id);
    return { ok: true };
  };

  const removeSshKey = async (input: SshKeyRemoveInput): Promise<{ ok: true }> => {
    const result = await removeSshKeyRecord(input);
    void cloudSyncService?.pushSshKeyDelete(input.id);
    return result;
  };

  // ── Proxy CRUD ──────────────────────────────────────────────────
  const listProxies = (): ProxyProfile[] => proxyRepo.list();

  const upsertProxy = async (input: ProxyUpsertInput): Promise<ProxyProfile> => {
    const now = new Date().toISOString();
    const id = input.id ?? randomUUID();
    const current = proxyRepo.getById(id);

    // Store proxy credential in vault (optional)
    let credentialRef = current?.credentialRef;
    if (input.password !== undefined) {
      if (current?.credentialRef) {
        await vault.deleteCredential(current.credentialRef);
        credentialRef = undefined;
      }
      if (input.password) {
        credentialRef = await vault.storeCredential(`proxy-${id}`, input.password);
      }
    }

    const profile: ProxyProfile = {
      id,
      name: input.name,
      proxyType: input.proxyType,
      host: input.host,
      port: input.port,
      username: input.username,
      credentialRef,
      createdAt: current?.createdAt ?? now,
      updatedAt: now,
    };

    proxyRepo.save(profile);
    void cloudSyncService?.pushProxyUpsert(profile);
    return profile;
  };

  const applyProxyFromCloudSync = async (input: CloudSyncApplyProxyInput): Promise<void> => {
    const current = proxyRepo.getById(input.id);
    let credentialRef = current?.credentialRef;

    if (input.password) {
      credentialRef = await vault.storeCredential(`proxy-${input.id}`, input.password);
    } else if (current?.credentialRef) {
      await vault.deleteCredential(current.credentialRef);
      credentialRef = undefined;
    }

    proxyRepo.save({
      id: input.id,
      name: input.name,
      proxyType: input.proxyType,
      host: input.host,
      port: input.port,
      username: input.username,
      credentialRef,
      createdAt: current?.createdAt ?? input.updatedAt,
      updatedAt: input.updatedAt
    });
  };

  const removeProxyRecord = async (
    input: ProxyRemoveInput
  ): Promise<{ ok: true }> => {
    const profile = proxyRepo.getById(input.id);
    if (!profile) {
      throw new Error("Proxy not found.");
    }

    const refs = proxyRepo.getReferencingConnectionIds(input.id);
    if (refs.length > 0) {
      throw new Error(`该代理仍被 ${refs.length} 个连接引用，无法删除。`);
    }

    if (profile.credentialRef) {
      await vault.deleteCredential(profile.credentialRef);
    }

    proxyRepo.remove(input.id);
    return { ok: true };
  };

  const removeProxy = async (input: ProxyRemoveInput): Promise<{ ok: true }> => {
    const result = await removeProxyRecord(input);
    void cloudSyncService?.pushProxyDelete(input.id);
    return result;
  };

  cloudSyncService = new CloudSyncService({
    keytarServiceName,
    getPreferences: getAppPreferences,
    savePreferencesPatch: saveAppPreferencesPatch,
    vault,
    listConnections: () => connections.list({}),
    listSshKeys,
    listProxies,
    applyConnectionFromCloudSync,
    applySshKeyFromCloudSync,
    applyProxyFromCloudSync,
    removeConnectionFromCloudSync: async (id) => {
      await removeConnectionRecord(id, { skipAudit: true });
    },
    removeSshKeyFromCloudSync: async (id) => {
      await removeSshKeyRecord({ id, force: true });
    },
    removeProxyFromCloudSync: async (id) => {
      await removeProxyRecord({ id, force: true });
    },
    emitStatus: (status) => {
      broadcastToAllWindows(IPCChannel.CloudSyncStatusEvent, status);
    },
    emitApplied: (event) => {
      broadcastToAllWindows(IPCChannel.CloudSyncAppliedEvent, event);
    },
    loadPendingQueueState: () => connections.getJsonSetting(CLOUD_SYNC_PENDING_QUEUE_SETTING_KEY),
    savePendingQueueState: (state) => {
      if (state === undefined) {
        connections.removeSetting(CLOUD_SYNC_PENDING_QUEUE_SETTING_KEY);
        return;
      }
      connections.saveJsonSetting(CLOUD_SYNC_PENDING_QUEUE_SETTING_KEY, state);
    },
    listResourceStates: () => connections.listCloudSyncResourceStates(),
    getResourceState: (resourceType, resourceId) => connections.getCloudSyncResourceState(resourceType, resourceId),
    saveResourceState: (state) => {
      connections.saveCloudSyncResourceState(state);
    },
    removeResourceState: (resourceType, resourceId) => {
      connections.removeCloudSyncResourceState(resourceType, resourceId);
    },
    requestJson: cloudSyncRequestJson
  });
  cloudSyncService.initialize();

  const cloudSyncConfigure = async (input: CloudSyncConfigureInput): Promise<CloudSyncStatus> => {
    return cloudSyncService.configure(input);
  };

  const cloudSyncDisable = async (): Promise<{ ok: true }> => {
    return cloudSyncService!.disable();
  };

  const cloudSyncStatus = async (): Promise<CloudSyncStatus> => {
    return cloudSyncService!.status();
  };

  const cloudSyncSyncNow = async (): Promise<{ ok: true }> => {
    return cloudSyncService!.syncNow();
  };

  const cloudSyncListConflicts = async (): Promise<CloudSyncConflictItem[]> => {
    return cloudSyncService!.listConflicts();
  };

  const cloudSyncResolveConflict = async (input: CloudSyncResolveConflictInput): Promise<{ ok: true }> => {
    return cloudSyncService!.resolveConflict(input);
  };

  // ─── Update Check ───────────────────────────────────────────────────────

  const compareVersions = (a: string, b: string): number => {
    const parsedA = parseComparableVersion(a);
    const parsedB = parseComparableVersion(b);

    if (parsedA && parsedB) {
      const coreCompare = compareCoreSegments(parsedA.core, parsedB.core);
      if (coreCompare !== 0) {
        return coreCompare;
      }
      return comparePrerelease(parsedA.prerelease, parsedB.prerelease);
    }

    return a.localeCompare(b, "en", { numeric: true, sensitivity: "base" });
  };

  const checkForUpdate = async (): Promise<UpdateCheckResult> => {
    const githubRepo = normalizeGithubRepo(process.env["VITE_GITHUB_REPO"] ?? "");
    const currentVersion = process.env["VITE_APP_VERSION"] ?? "0.0.0";

    if (!githubRepo) {
      return {
        currentVersion,
        latestVersion: null,
        hasUpdate: false,
        releaseUrl: null,
        error: "未配置或配置了无效的 GitHub 仓库"
      };
    }

    try {
      const response = await fetch(
        `https://api.github.com/repos/${githubRepo}/releases/latest`,
        {
          headers: {
            Accept: "application/vnd.github.v3+json",
            "User-Agent": "NextShell-UpdateChecker"
          }
        }
      );

      if (!response.ok) {
        if (response.status === 404) {
          return {
            currentVersion,
            latestVersion: null,
            hasUpdate: false,
            releaseUrl: `https://github.com/${githubRepo}/releases`,
            error: "未找到任何 Release"
          };
        }
        throw new Error(`GitHub API 返回 ${response.status}`);
      }

      const data = (await response.json()) as { tag_name?: string; html_url?: string };
      const latestVersion = data.tag_name ?? null;

      if (!latestVersion) {
        return {
          currentVersion,
          latestVersion: null,
          hasUpdate: false,
          releaseUrl: `https://github.com/${githubRepo}/releases`,
          error: "Release 缺少 tag"
        };
      }

      return {
        currentVersion,
        latestVersion,
        hasUpdate: compareVersions(latestVersion, currentVersion) > 0,
        releaseUrl: data.html_url ?? `https://github.com/${githubRepo}/releases`,
        error: null
      };
    } catch (error) {
      logger.error("[UpdateCheck]", error);
      return {
        currentVersion,
        latestVersion: null,
        hasUpdate: false,
        releaseUrl: null,
        error: error instanceof Error ? error.message : "检查更新失败"
      };
    }
  };

  const pingHost = async (host: string): Promise<PingResult> => {
    try {
      const ping = await import("ping");
      const res = await ping.promise.probe(host, { timeout: 3 });
      if (res.alive && typeof res.time === "number") {
        return { ok: true, avgMs: res.time };
      }
      return { ok: false, error: (res as { output?: string }).output ?? "不可达" };
    } catch (err) {
      const message = err instanceof Error ? err.message : "Ping 失败";
      return { ok: false, error: message };
    }
  };

  // ─── Traceroute ──────────────────────────────────────────────────────────

  let activeTracerouteProcess: ChildProcess | null = null;

  const resolveNexttrace = (): string => {
    const prefs = connections.getAppPreferences();
    const configured = prefs.traceroute.nexttracePath.trim();
    if (configured) {
      return configured;
    }
    // Fallback: try to find nexttrace in PATH
    const cmd = process.platform === "win32" ? "where" : "which";
    try {
      return execFileSync(cmd, ["nexttrace"], { encoding: "utf-8" }).trim().split(/\r?\n/)[0]!;
    } catch {
      throw new Error("未找到 nexttrace，请在设置 > 网络工具中配置路径，或确保 nexttrace 已安装到 PATH。");
    }
  };

  const tracerouteRun = async (host: string, sender: WebContents): Promise<{ ok: true }> => {
    // Kill any existing traceroute process
    if (activeTracerouteProcess) {
      activeTracerouteProcess.kill();
      activeTracerouteProcess = null;
    }

    const bin = resolveNexttrace();
    const prefs = connections.getAppPreferences().traceroute;

    // Build CLI args from preferences
    const args: string[] = [];
    if (prefs.protocol === "tcp") {
      args.push("--tcp");
    } else if (prefs.protocol === "udp") {
      args.push("--udp");
    }
    if ((prefs.protocol === "tcp" || prefs.protocol === "udp") && prefs.port > 0) {
      args.push("--port", String(prefs.port));
    }
    if (prefs.ipVersion === "ipv4") {
      args.push("--ipv4");
    } else if (prefs.ipVersion === "ipv6") {
      args.push("--ipv6");
    }
    if (prefs.queries !== 3) {
      args.push("--queries", String(prefs.queries));
    }
    if (prefs.maxHops !== 30) {
      args.push("--max-hops", String(prefs.maxHops));
    }
    if (prefs.dataProvider !== "LeoMoeAPI") {
      args.push("--data-provider", prefs.dataProvider);
    }
    if (prefs.noRdns) {
      args.push("--no-rdns");
    }
    if (prefs.language !== "cn") {
      args.push("--language", prefs.language);
    }
    if (prefs.powProvider !== "api.nxtrace.org") {
      args.push("--pow-provider", prefs.powProvider);
    }
    args.push(host);

    const child = spawn(bin, args, {
      stdio: ["ignore", "pipe", "pipe"]
    });
    activeTracerouteProcess = child;

    const pendingTracerouteEvents: TracerouteEvent[] = [];
    let tracerouteFlushTimer: ReturnType<typeof setTimeout> | undefined;

    const flushTracerouteEvents = (): void => {
      tracerouteFlushTimer = undefined;
      if (pendingTracerouteEvents.length === 0 || sender.isDestroyed()) return;
      const batch = pendingTracerouteEvents.splice(0);
      for (const event of batch) {
        sender.send(IPCChannel.TracerouteData, event);
      }
    };

    const sendEvent = (event: TracerouteEvent): void => {
      if (sender.isDestroyed()) return;
      if (event.type === "done" || event.type === "error") {
        if (tracerouteFlushTimer) { clearTimeout(tracerouteFlushTimer); tracerouteFlushTimer = undefined; }
        const batch = pendingTracerouteEvents.splice(0);
        for (const e of batch) { sender.send(IPCChannel.TracerouteData, e); }
        sender.send(IPCChannel.TracerouteData, event);
        return;
      }
      pendingTracerouteEvents.push(event);
      if (!tracerouteFlushTimer) {
        tracerouteFlushTimer = setTimeout(flushTracerouteEvents, 50);
      }
    };

    let stdoutBuffer = "";
    child.stdout?.on("data", (chunk: Buffer) => {
      stdoutBuffer += chunk.toString("utf-8");
      const lines = stdoutBuffer.split(/\r?\n/);
      stdoutBuffer = lines.pop() ?? "";
      for (const line of lines) {
        sendEvent({ type: "data", line });
      }
    });

    let stderrBuffer = "";
    child.stderr?.on("data", (chunk: Buffer) => {
      stderrBuffer += chunk.toString("utf-8");
      const lines = stderrBuffer.split(/\r?\n/);
      stderrBuffer = lines.pop() ?? "";
      for (const line of lines) {
        sendEvent({ type: "data", line });
      }
    });

    child.on("error", (err) => {
      sendEvent({ type: "error", message: err.message });
      child.removeAllListeners();
      if (activeTracerouteProcess === child) {
        activeTracerouteProcess = null;
      }
    });

    child.on("close", (code) => {
      // Flush remaining buffer
      if (stdoutBuffer) {
        sendEvent({ type: "data", line: stdoutBuffer });
      }
      if (stderrBuffer) {
        sendEvent({ type: "data", line: stderrBuffer });
      }
      sendEvent({ type: "done", exitCode: code });
      child.removeAllListeners();
      if (activeTracerouteProcess === child) {
        activeTracerouteProcess = null;
      }
    });

    return { ok: true };
  };

  const tracerouteStop = (): { ok: true } => {
    if (activeTracerouteProcess) {
      activeTracerouteProcess.removeAllListeners();
      activeTracerouteProcess.kill();
      activeTracerouteProcess = null;
    }
    return { ok: true };
  };

  const removeConnectionRecord = async (
    id: string,
    options?: { skipAudit?: boolean }
  ): Promise<{ ok: true }> => {
    const sessions = Array.from(activeSessions.values()).filter(
      (session): session is ActiveRemoteSession =>
        session.kind === "remote" && session.connectionId === id
    );

    for (const session of sessions) {
      session.channel.end();
      activeSessions.delete(session.descriptor.id);
      sendSessionStatus(session.sender, {
        sessionId: session.descriptor.id,
        status: "disconnected",
        reason: "Connection deleted"
      });
    }

    await remoteEditManager.cleanupByConnectionId(id);

    const connection = connections.getById(id);
    if (connection?.credentialRef) {
      await vault.deleteCredential(connection.credentialRef);
    }

    await closeConnectionIfIdle(id);
    connections.remove(id);
    monitorStates.delete(id);
    if (!options?.skipAudit) {
      appendAuditLogIfEnabled({
        action: "connection.remove",
        level: "warn",
        connectionId: id,
        message: "Connection profile deleted"
      });
    }
    return { ok: true };
  };

  const removeConnection = async (id: string): Promise<{ ok: true }> => {
    const result = await removeConnectionRecord(id);
    void cloudSyncService?.pushConnectionDelete(id);
    return { ok: true };
  };

  // ── Connection Import/Export ────────────────────────────────────────────

  const ENCRYPTED_EXPORT_PREFIX = "b64##";
  const trimBomAndWhitespace = (value: string): string => value.replace(/^\uFEFF/, "").trim();

  const parseImportPayloadText = async (
    rawText: string,
    decryptionPassword?: string
  ): Promise<unknown> => {
    const normalizedText = trimBomAndWhitespace(rawText);
    const encryptedPrefix = ENCRYPTED_EXPORT_PREFIX;

    if (normalizedText.startsWith(encryptedPrefix)) {
      if (!decryptionPassword) {
        throw new Error(
          `${CONNECTION_IMPORT_DECRYPT_PROMPT_PREFIX}该导入文件已加密，请输入密码`
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
          `${CONNECTION_IMPORT_DECRYPT_PROMPT_PREFIX}密码错误或文件损坏，请重试`
        );
      }

      try {
        return JSON.parse(decryptedText);
      } catch {
        throw new Error("解密成功，但文件内容不是合法 JSON");
      }
    }

    return JSON.parse(normalizedText);
  };

  const parseJsonPayloadText = (rawText: string): unknown => {
    const normalizedText = trimBomAndWhitespace(rawText);
    return JSON.parse(normalizedText);
  };

  const buildExportedConnection = async (conn: ConnectionProfile): Promise<ExportedConnection> => {
    let password: string | undefined;
    if ((conn.authType === "password" || conn.authType === "interactive") && conn.credentialRef) {
      try {
        password = await vault.readCredential(conn.credentialRef);
      } catch {
        // If we can't read the credential, export without password
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
      monitorSession: conn.monitorSession
    };
  };

  const exportConnections = async (
    sender: WebContents,
    input: ConnectionExportInput
  ): Promise<{ ok: true; filePath: string } | { ok: false; canceled: true }> => {
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

    const allConnections = connections.list({});
    const idSet = new Set(input.connectionIds);
    const filtered = allConnections.filter((c) => idSet.has(c.id));

    const exportedConnections: ExportedConnection[] = [];
    for (const conn of filtered) {
      exportedConnections.push(await buildExportedConnection(conn));
    }

    const encryptionPassword = input.encryptionPassword;
    const encrypted = typeof encryptionPassword === "string";

    // When not encrypted, XOR-obfuscate passwords so they aren't stored as plaintext.
    const exportedConnectionsFinal = encrypted
      ? exportedConnections
      : exportedConnections.map((c) => ({
          ...c,
          password: c.password !== undefined ? obfuscatePassword(c.password, c.name, c.host, c.port) : undefined
        }));

    const exportFile: ConnectionExportFile = {
      format: "nextshell-connections",
      version: 1,
      exportedAt: new Date().toISOString(),
      ...(encrypted ? {} : { passwordsObfuscated: true }),
      connections: exportedConnectionsFinal
    };
    const plainJson = JSON.stringify(exportFile, null, 2);
    const fileContent = encrypted
      ? `${ENCRYPTED_EXPORT_PREFIX}${await encryptConnectionExportPayload(
          plainJson,
          encryptionPassword
        )}`
      : plainJson;

    fs.writeFileSync(result.filePath, fileContent, "utf-8");

    appendAuditLogIfEnabled({
      action: "connection.export",
      level: "info",
      message: `Exported ${exportedConnections.length} connections`,
      metadata: { filePath: result.filePath, count: exportedConnections.length, encrypted }
    });

    return { ok: true, filePath: result.filePath };
  };

  const exportConnectionsBatch = async (
    input: ConnectionExportBatchInput
  ): Promise<ConnectionExportBatchResult> => {
    const allConnections = connections.list({});
    const idSet = new Set(input.connectionIds);
    const filtered = allConnections.filter((conn) => idSet.has(conn.id));
    const result = await exportConnectionsBatchToDirectory({
      connections: filtered,
      directoryPath: input.directoryPath,
      encryptionPassword: input.encryptionPassword,
      buildExportedConnection
    });

    appendAuditLogIfEnabled({
      action: "connection.export.batch",
      level: "info",
      message: `Batch exported ${result.exported}/${result.total} connections`,
      metadata: { ...result }
    });

    return result;
  };

  const importConnectionsPreview = async (
    input: ConnectionImportPreviewInput
  ): Promise<ConnectionImportEntry[]> => {
    const raw = fs.readFileSync(input.filePath, "utf-8");
    const data = await parseImportPayloadText(raw, input.decryptionPassword);

    if (isNextShellFormat(data)) {
      return parseNextShellImport(data);
    }

    throw new Error("该文件不是 NextShell 导出格式，请使用“导入 FinalShell 文件”按钮导入 FinalShell 配置");
  };

  const importFinalShellConnectionsPreview = async (
    input: ConnectionImportFinalShellPreviewInput
  ): Promise<ConnectionImportEntry[]> => {
    const raw = fs.readFileSync(input.filePath, "utf-8");
    const data = parseJsonPayloadText(raw);

    if (!isFinalShellFormat(data)) {
      throw new Error("该文件不是 FinalShell 配置格式");
    }

    return parseFinalShellImport(data);
  };

  const importConnectionsExecute = async (
    input: ConnectionImportExecuteInput
  ): Promise<ConnectionImportResult> => {
    const result: ConnectionImportResult = {
      created: 0,
      skipped: 0,
      overwritten: 0,
      failed: 0,
      passwordsUnavailable: 0,
      errors: []
    };

    const allConnections = connections.list({});

    for (const entry of input.entries) {
      try {
        const existing = allConnections.find(
          (c) => c.host === entry.host && c.port === entry.port && c.username === entry.username
        );

        if (existing) {
          if (input.conflictPolicy === "skip") {
            result.skipped++;
            continue;
          }

          if (input.conflictPolicy === "overwrite") {
            await upsertConnection({
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
              monitorSession: entry.monitorSession
            });
            result.overwritten++;
            if (!entry.password && (entry.authType === "password" || entry.authType === "interactive")) {
              result.passwordsUnavailable++;
            }
            continue;
          }
        }

        // "duplicate" policy or no existing match — create new
        await upsertConnection({
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
          monitorSession: entry.monitorSession
        });
        result.created++;
        if (!entry.password && (entry.authType === "password" || entry.authType === "interactive")) {
          result.passwordsUnavailable++;
        }
      } catch (error) {
        result.failed++;
        const reason = error instanceof Error ? error.message : "未知错误";
        result.errors.push(`${entry.name} (${entry.host}:${entry.port}): ${reason}`);
      }
    }

    appendAuditLogIfEnabled({
      action: "connection.import",
      level: "info",
      message: `Imported connections: ${result.created} created, ${result.overwritten} overwritten, ${result.skipped} skipped, ${result.failed} failed`,
      metadata: { ...result }
    });

    return result;
  };

  const openRemoteSession = async (
    connectionId: string,
    sender: WebContents,
    sessionId?: string,
    authOverride?: SessionAuthOverrideInput
  ): Promise<SessionDescriptor> => {
    const profile = getConnectionOrThrow(connectionId);
    const descriptorId = sessionId ?? randomUUID();
    if (activeSessions.has(descriptorId)) {
      throw new Error("Session id already exists");
    }
    const descriptor: SessionDescriptor = {
      id: descriptorId,
      target: "remote",
      connectionId,
      title: `${profile.name}@${profile.host}`,
      status: "connecting",
      type: "terminal",
      createdAt: new Date().toISOString(),
      reconnectable: true
    };

    sendSessionStatus(sender, {
      sessionId: descriptor.id,
      status: "connecting"
    });

    try {
      const connection = await ensureConnection(connectionId, authOverride);
      let osc7ShellFamily: ReturnType<typeof resolveOsc7ShellFamily> = undefined;
      if (profile.monitorSession) {
        try {
          const shellProbe = await connection.exec('printf \'%s\' "${SHELL:-}"');
          osc7ShellFamily = resolveOsc7ShellFamily(shellProbe.stdout);
        } catch {
          osc7ShellFamily = undefined;
        }
      }
      const osc7Bootstrap = createRemoteOsc7BootstrapPlan(
        Boolean(profile.monitorSession),
        profile.host,
        osc7ShellFamily
      );
      const shell = await connection.openShell({
        cols: 140,
        rows: 40,
        term: "xterm-256color",
        env: osc7Bootstrap.enabled ? osc7Bootstrap.env : undefined
      });

      const now = new Date().toISOString();
      connections.save({
        ...profile,
        lastConnectedAt: now,
        updatedAt: now
      });

      descriptor.status = "connected";

      activeSessions.set(descriptor.id, {
        kind: "remote",
        descriptor,
        channel: shell,
        sender,
        connectionId,
        terminalEncoding: profile.terminalEncoding,
        backspaceMode: profile.backspaceMode,
        deleteMode: profile.deleteMode
      });

      shell.on("data", (chunk: Buffer | string) => {
        const active = activeSessions.get(descriptor.id);
        if (!active) {
          return;
        }

        const decoded = decodeTerminalData(chunk, active.terminalEncoding);

        if (active.kind === "remote" && active.bootstrapSuppression) {
          const suppression = active.bootstrapSuppression;
          suppression.buffer += decoded;

          const endIdx = suppression.buffer.indexOf(suppression.endMarker);
          if (endIdx < 0) {
            return; // continue buffering
          }

          clearTimeout(suppression.timeout);
          const startIdx = suppression.buffer.indexOf(suppression.startMarker);
          const before = startIdx >= 0 ? suppression.buffer.slice(0, startIdx) : "";
          const after = suppression.buffer.slice(endIdx + suppression.endMarker.length);
          active.bootstrapSuppression = undefined;

          const remaining = before + after;
          if (remaining) {
            sessionDataDispatcher.push({
              streamId: descriptor.id,
              sender: active.sender,
              chunk: remaining,
              onPause: () => shell.pause(),
              onResume: () => shell.resume()
            });
          }
          return;
        }

        sessionDataDispatcher.push({
          streamId: descriptor.id,
          sender: active.sender,
          chunk: decoded,
          onPause: () => shell.pause(),
          onResume: () => shell.resume()
        });
      });

      shell.stderr.on("data", (chunk: Buffer | string) => {
        const active = activeSessions.get(descriptor.id);
        if (!active) {
          return;
        }
        sessionDataDispatcher.push({
          streamId: descriptor.id,
          sender: active.sender,
          chunk: decodeTerminalData(chunk, active.terminalEncoding),
          onPause: () => shell.pause(),
          onResume: () => shell.resume()
        });
      });

      shell.on("close", () => {
        shell.removeAllListeners();
        shell.stderr.removeAllListeners();
        finalizeRemoteSession(descriptor.id, "disconnected");
      });

      shell.on("error", (error: unknown) => {
        shell.removeAllListeners();
        shell.stderr.removeAllListeners();
        finalizeRemoteSession(descriptor.id, "failed", normalizeError(error));
      });

      if (osc7Bootstrap.enabled && osc7Bootstrap.shellBootstrap) {
        const active = activeSessions.get(descriptor.id);
        if (active?.kind === "remote" && osc7Bootstrap.startMarker && osc7Bootstrap.endMarker) {
          active.bootstrapSuppression = {
            startMarker: osc7Bootstrap.startMarker,
            endMarker: osc7Bootstrap.endMarker,
            buffer: "",
            timeout: setTimeout(() => {
              if (active.bootstrapSuppression) {
                const buffered = active.bootstrapSuppression.buffer;
                active.bootstrapSuppression = undefined;
                if (buffered) {
                  sessionDataDispatcher.push({
                    streamId: descriptor.id,
                    sender: active.sender,
                    chunk: buffered,
                    onPause: () => shell.pause(),
                    onResume: () => shell.resume()
                  });
                }
              }
            }, 3000)
          };
        }
        shell.write(`${osc7Bootstrap.shellBootstrap}\r`);
      }

      let connectedReason = await warmupSftp(connectionId, connection);
      if (authOverride) {
        const persistWarning = await persistSuccessfulAuthOverride(connectionId, authOverride);
        if (persistWarning) {
          connectedReason = connectedReason
            ? `${connectedReason}；${persistWarning}`
            : persistWarning;
        }
      }

      if (profile.monitorSession) {
        try {
          await ensureSystemMonitorRuntime(connectionId);
        } catch (error) {
          const monitorReason = `Monitor Session 后台连接初始化失败：${normalizeError(error)}`;
          connectedReason = connectedReason
            ? `${connectedReason}；${monitorReason}`
            : monitorReason;
          logger.warn("[MonitorSession] failed to bootstrap runtime after terminal open", {
            connectionId,
            reason: normalizeError(error)
          });
        }
      }

      sendSessionStatus(sender, {
        sessionId: descriptor.id,
        status: "connected",
        reason: connectedReason
      });

      appendAuditLogIfEnabled({
        action: "session.open",
        level: "info",
        connectionId,
        message: "SSH session opened",
        metadata: {
          sessionId: descriptor.id
        }
      });

      return descriptor;
    } catch (error) {
      const rawReason = normalizeError(error);
      const authReason = toAuthRequiredReason(rawReason);
      const reason = authReason ? `${AUTH_REQUIRED_PREFIX}${authReason}` : rawReason;
      logger.error("[Session] failed to open", {
        connectionId,
        reason
      });
      if (!authReason) {
        sendSessionStatus(sender, {
          sessionId: descriptor.id,
          status: "failed",
          reason
        });
      }
      appendAuditLogIfEnabled({
        action: "session.open_failed",
        level: "error",
        connectionId,
        message: "SSH session failed to open",
        metadata: {
          reason,
          authRequired: Boolean(authReason)
        }
      });
      throw new Error(reason);
    }
  };

  const openLocalSession = async (
    sender: WebContents,
    sessionId?: string
  ): Promise<SessionDescriptor> => {
    const descriptorId = sessionId ?? randomUUID();
    if (activeSessions.has(descriptorId)) {
      throw new Error("Session id already exists");
    }

    const prefs = connections.getAppPreferences();
    const shellLaunch = resolveLocalShellLaunch(prefs.terminal.localShell, process.platform);
    const descriptor: SessionDescriptor = {
      id: descriptorId,
      target: "local",
      title: `本地终端 · ${shellLaunch.label}`,
      status: "connecting",
      type: "terminal",
      createdAt: new Date().toISOString(),
      reconnectable: true
    };

    sendSessionStatus(sender, {
      sessionId: descriptor.id,
      status: "connecting"
    });

    try {
      const localShellEnv = Object.fromEntries(
        Object.entries(process.env).filter((entry): entry is [string, string] => typeof entry[1] === "string")
      );
      const pty = spawnPty(shellLaunch.command, shellLaunch.args, {
        name: "xterm-256color",
        cols: 140,
        rows: 40,
        cwd: os.homedir(),
        env: localShellEnv
      });

      descriptor.status = "connected";
      activeSessions.set(descriptor.id, {
        kind: "local",
        descriptor,
        pty,
        sender,
        terminalEncoding: "utf-8"
      });

      pty.onData((chunk) => {
        const active = activeSessions.get(descriptor.id);
        if (!active || active.kind !== "local") {
          return;
        }

        sessionDataDispatcher.push({
          streamId: descriptor.id,
          sender: active.sender,
          chunk,
          onPause: () => pty.pause(),
          onResume: () => pty.resume()
        });
      });

      pty.onExit(({ exitCode, signal }) => {
        const reasonParts: string[] = [];
        if (typeof exitCode === "number") {
          reasonParts.push(`exit ${exitCode}`);
        }
        if (typeof signal === "number") {
          reasonParts.push(`signal ${signal}`);
        }
        finalizeLocalSession(
          descriptor.id,
          "disconnected",
          reasonParts.length > 0 ? reasonParts.join(", ") : undefined
        );
      });

      sendSessionStatus(sender, {
        sessionId: descriptor.id,
        status: "connected"
      });

      appendAuditLogIfEnabled({
        action: "session.local_open",
        level: "info",
        message: "Local terminal session opened",
        metadata: {
          sessionId: descriptor.id,
          shell: shellLaunch.command
        }
      });

      return descriptor;
    } catch (error) {
      const reason = error instanceof Error ? error.message : "Failed to open local shell";
      logger.error("[Session] failed to open local terminal", {
        sessionId: descriptor.id,
        reason
      });
      sendSessionStatus(sender, {
        sessionId: descriptor.id,
        status: "failed",
        reason
      });
      appendAuditLogIfEnabled({
        action: "session.local_open_failed",
        level: "error",
        message: "Local terminal session failed to open",
        metadata: {
          sessionId: descriptor.id,
          reason
        }
      });
      throw new Error(reason);
    }
  };

  const openSession = async (
    input: SessionOpenInput,
    sender: WebContents
  ): Promise<SessionDescriptor> => {
    if (input.target === "local") {
      return openLocalSession(sender, input.sessionId);
    }

    return openRemoteSession(input.connectionId, sender, input.sessionId, input.authOverride);
  };

  const writeSession = (sessionId: string, data: string): { ok: true } => {
    const active = activeSessions.get(sessionId);
    if (!active) {
      throw new Error("Session not found");
    }

    if (active.kind === "local") {
      active.pty.write(data);
      return { ok: true };
    }

    const buffer = encodeTerminalData(data, active.terminalEncoding);
    active.channel.write(buffer);
    return { ok: true };
  };

  const resizeSession = (
    sessionId: string,
    cols: number,
    rows: number
  ): { ok: true } => {
    const active = activeSessions.get(sessionId);
    if (!active) {
      // Session may have already disconnected; silently ignore resize requests
      return { ok: true };
    }

    if (active.kind === "local") {
      active.pty.resize(cols, rows);
      return { ok: true };
    }

    active.channel.setWindow(rows, cols, 0, 0);
    return { ok: true };
  };

  const closeSession = async (sessionId: string): Promise<{ ok: true }> => {
    const active = activeSessions.get(sessionId);
    if (!active) {
      return { ok: true };
    }

    logger.info("[Session] closing", {
      sessionId,
      connectionId: active.kind === "remote" ? active.connectionId : undefined,
      target: active.descriptor.target
    });
    sessionDataDispatcher.clear(sessionId);
    if (active.kind === "local") {
      active.pty.kill();
      activeSessions.delete(sessionId);
      sendSessionStatus(active.sender, {
        sessionId,
        status: "disconnected"
      });

      appendAuditLogIfEnabled({
        action: "session.local_close",
        level: "info",
        message: "Local terminal session closed",
        metadata: { sessionId }
      });
      return { ok: true };
    }

    active.channel.removeAllListeners();
    if (active.channel.stderr) {
      active.channel.stderr.removeAllListeners();
    }
    active.channel.end();
    activeSessions.delete(sessionId);
    sendSessionStatus(active.sender, {
      sessionId,
      status: "disconnected"
    });

    appendAuditLogIfEnabled({
      action: "session.close",
      level: "info",
      connectionId: active.connectionId,
      message: "SSH session closed",
      metadata: { sessionId }
    });

    await closeConnectionIfIdle(active.connectionId);
    return { ok: true };
  };

  const debugSenders = new Set<WebContents>();

  const enableDebugLog = (sender: WebContents): { ok: true } => {
    debugSenders.add(sender);
    sender.once("destroyed", () => {
      debugSenders.delete(sender);
    });
    return { ok: true };
  };

  const disableDebugLog = (sender: WebContents): { ok: true } => {
    debugSenders.delete(sender);
    return { ok: true };
  };

  const DEBUG_FLUSH_INTERVAL_MS = 200;
  const DEBUG_MAX_PENDING = 50;
  let debugPending: DebugLogEntry[] = [];
  let debugFlushTimer: ReturnType<typeof setTimeout> | undefined;

  const flushDebugLog = (): void => {
    debugFlushTimer = undefined;
    if (debugPending.length === 0 || debugSenders.size === 0) return;
    const batch = debugPending.splice(0);
    for (const sender of debugSenders) {
      if (sender.isDestroyed()) {
        debugSenders.delete(sender);
      } else {
        for (const entry of batch) {
          sender.send(IPCChannel.DebugLogEvent, entry);
        }
      }
    }
  };

  const emitDebugLog = (entry: DebugLogEntry): void => {
    if (debugSenders.size === 0) return;
    if (debugPending.length >= DEBUG_MAX_PENDING) {
      debugPending.shift();
    }
    debugPending.push(entry);
    if (!debugFlushTimer) {
      debugFlushTimer = setTimeout(flushDebugLog, DEBUG_FLUSH_INTERVAL_MS);
    }
  };

  const startSystemMonitor = async (
    connectionId: string,
    sender: WebContents
  ): Promise<{ ok: true }> => {
    assertMonitorEnabled(connectionId);
    assertVisibleTerminalAlive(connectionId);
    const runtime = await ensureSystemMonitorRuntime(connectionId);
    runtime.sender = sender;
    systemMonitorDispatcher.clear(connectionId);
    return runtime.controller.start();
  };

  const stopSystemMonitor = (connectionId: string): { ok: true } => {
    const runtime = systemMonitorRuntimes.get(connectionId);
    if (runtime) {
      runtime.sender = undefined;
      systemMonitorDispatcher.clear(connectionId);
      void runtime.controller.stop();
    }
    return { ok: true };
  };

  const selectSystemNetworkInterface = async (
    connectionId: string,
    networkInterface: string
  ): Promise<{ ok: true }> => {
    assertMonitorEnabled(connectionId);
    assertVisibleTerminalAlive(connectionId);
    const runtime = await ensureSystemMonitorRuntime(connectionId);
    return runtime.controller.selectNetworkInterface(networkInterface);
  };

  const assertSystemInfoLinuxHost = async (connectionId: string): Promise<void> => {
    // Use ad-hoc session for one-off checks
    const adhoc = await ensureAdhocSession(connectionId);
    const result = await adhoc.connection.exec(MONITOR_SYSTEM_INFO_KERNEL_NAME_COMMAND);
    const platform = result.stdout.trim().split(/\s+/)[0] ?? "";
    if (platform !== "Linux") {
      throw new Error("系统信息标签页当前仅支持 Linux 主机");
    }
  };

  const getSystemInfoSnapshot = async (connectionId: string): Promise<SystemInfoSnapshot> => {
    assertMonitorEnabled(connectionId);
    assertVisibleTerminalAlive(connectionId);
    await assertSystemInfoLinuxHost(connectionId);

    // Use ad-hoc session with compound command (9 commands → 1 exec)
    const adhoc = await ensureAdhocSession(connectionId);
    const compoundCmd = buildSystemInfoCommand();
    const result = await adhoc.connection.exec(compoundCmd);
    const sections = parseCompoundOutput(result.stdout);

    const memInfoRaw = sections.get("MEMINFO") ?? "";
    const totals = parseMeminfoTotals(memInfoRaw);
    return {
      connectionId,
      hostname: (sections.get("HOSTNAME") ?? "").trim() || "unknown",
      osName: parseOsReleaseName(sections.get("OSRELEASE") ?? ""),
      kernelName: (sections.get("KERNELNAME") ?? "").trim() || "Linux",
      kernelVersion: (sections.get("KERNELVER") ?? "").trim() || "unknown",
      architecture: (sections.get("ARCH") ?? "").trim() || "unknown",
      cpu: parseCpuInfo(sections.get("CPUINFO") ?? ""),
      memoryTotalKb: totals.memoryTotalKb,
      swapTotalKb: totals.swapTotalKb,
      networkInterfaces: parseNetworkInterfaceTotals(sections.get("NETDEV") ?? ""),
      filesystems: parseFilesystemEntries(sections.get("FILESYSTEMS") ?? ""),
      uptimeSeconds: parseUptimeSeconds(sections.get("UPTIME") ?? ""),
      capturedAt: new Date().toISOString()
    };
  };

  const execCommand = async (
    connectionId: string,
    command: string
  ): Promise<CommandExecutionResult> => {
    getConnectionOrThrow(connectionId);
    const connection = await ensureConnection(connectionId);
    const result = await connection.exec(command);

    const execution: CommandExecutionResult = {
      connectionId,
      command,
      stdout: result.stdout,
      stderr: result.stderr,
      exitCode: result.exitCode,
      executedAt: new Date().toISOString()
    };

    appendAuditLogIfEnabled({
      action: "command.exec",
      level: result.exitCode === 0 ? "info" : "warn",
      connectionId,
      message: "Executed command on remote host",
      metadata: {
        command,
        exitCode: result.exitCode
      }
    });

    return execution;
  };

  const getSessionHomeDir = async (
    connectionId: string
  ): Promise<{ path: string } | null> => {
    getConnectionOrThrow(connectionId);
    const connection = await ensureConnection(connectionId);
    try {
      const result = await connection.exec(buildRemoteHomeDirCommand());
      const homeDir = parseRemoteHomeDir(result.stdout);
      return homeDir ? { path: homeDir } : null;
    } catch {
      return null;
    }
  };

  const executeCommandWithRetry = async (
    connectionId: string,
    command: string,
    retryCount: number
  ): Promise<BatchCommandResultItem> => {
    const maxAttempts = Math.max(1, retryCount + 1);
    let attempts = 0;
    const startedAt = Date.now();
    let lastExecution: CommandExecutionResult | undefined;
    let lastError: string | undefined;

    while (attempts < maxAttempts) {
      attempts += 1;
      try {
        const execution = await execCommand(connectionId, command);
        lastExecution = execution;
        if (execution.exitCode === 0) {
          return {
            ...execution,
            success: true,
            attempts,
            durationMs: Date.now() - startedAt
          };
        }

        lastError = execution.stderr || `Exit code ${execution.exitCode}`;
      } catch (error) {
        lastError = normalizeError(error);
      }
    }

    const failedAt = new Date().toISOString();
    return {
      connectionId,
      command,
      stdout: lastExecution?.stdout ?? "",
      stderr: lastExecution?.stderr ?? "",
      exitCode: lastExecution?.exitCode ?? -1,
      executedAt: lastExecution?.executedAt ?? failedAt,
      success: false,
      attempts,
      durationMs: Date.now() - startedAt,
      error: lastError
    };
  };

  const execBatchCommand = async (
    input: CommandBatchExecInput
  ): Promise<BatchCommandExecutionResult> => {
    const startedAt = new Date();
    const uniqueConnectionIds = Array.from(new Set(input.connectionIds));
    const queue = [...uniqueConnectionIds];
    const results: BatchCommandResultItem[] = [];
    const workerCount = Math.max(1, Math.min(input.maxConcurrency, queue.length));

    await Promise.all(
      Array.from({ length: workerCount }, async () => {
        while (queue.length > 0) {
          const connectionId = queue.shift();
          if (!connectionId) {
            return;
          }

          if (!connections.getById(connectionId)) {
            results.push({
              connectionId,
              command: input.command,
              stdout: "",
              stderr: "",
              exitCode: -1,
              executedAt: new Date().toISOString(),
              success: false,
              attempts: 0,
              durationMs: 0,
              error: "Connection not found"
            });
            continue;
          }

          const result = await executeCommandWithRetry(
            connectionId,
            input.command,
            input.retryCount
          );
          results.push(result);
        }
      })
    );

    const finishedAt = new Date();
    const successCount = results.filter((item) => item.success).length;
    const failedCount = results.length - successCount;
    const summary: BatchCommandExecutionResult = {
      command: input.command,
      startedAt: startedAt.toISOString(),
      finishedAt: finishedAt.toISOString(),
      durationMs: finishedAt.getTime() - startedAt.getTime(),
      total: results.length,
      successCount,
      failedCount,
      results: results.sort((a, b) => a.connectionId.localeCompare(b.connectionId))
    };

    appendAuditLogIfEnabled({
      action: "command.exec_batch",
      level: failedCount > 0 ? "warn" : "info",
      message: "Executed batch command",
      metadata: {
        command: input.command,
        total: summary.total,
        successCount,
        failedCount,
        retryCount: input.retryCount,
        maxConcurrency: input.maxConcurrency
      }
    });

    return summary;
  };

  const listRemoteFiles = async (
    connectionId: string,
    pathName: string
  ): Promise<RemoteFileEntry[]> => {
    getConnectionOrThrow(connectionId);

    const connection = await ensureConnection(connectionId);
    const rows = await connection.list(pathName);

    return rows
      .filter((entry) => entry.name !== "." && entry.name !== "..")
      .map((entry: SshDirectoryEntry) => {
        const parsed = parseLongname(entry.longname);
        const modifiedAt = entry.mtime
          ? new Date(entry.mtime * 1000).toISOString()
          : new Date().toISOString();

        return {
          name: entry.name,
          path: joinRemotePath(pathName, entry.name),
          type: mapEntryType(parsed.permissions),
          size: entry.size,
          permissions: parsed.permissions,
          owner: parsed.owner,
          group: parsed.group,
          modifiedAt
        };
      });
  };

  const listLocalFiles = async (pathName: string): Promise<RemoteFileEntry[]> => {
    const resolvedPath = resolveLocalPath(pathName);
    let rows: fs.Dirent[];
    try {
      rows = await fs.promises.readdir(resolvedPath, { withFileTypes: true });
    } catch (error) {
      throw new Error(`读取本机目录失败：${normalizeError(error)}`);
    }

    const entries = await Promise.all(
      rows
        .filter((entry) => entry.name !== "." && entry.name !== "..")
        .map(async (entry) => {
          const fullPath = path.join(resolvedPath, entry.name);
          const stats = await fs.promises.lstat(fullPath);
          const type: RemoteFileEntry["type"] = entry.isDirectory()
            ? "directory"
            : entry.isSymbolicLink()
              ? "link"
              : "file";

          return {
            name: entry.name,
            path: fullPath,
            type,
            size: stats.size,
            permissions: (stats.mode & 0o777).toString(8).padStart(3, "0"),
            owner: typeof stats.uid === "number" ? String(stats.uid) : "-",
            group: typeof stats.gid === "number" ? String(stats.gid) : "-",
            modifiedAt: stats.mtime.toISOString()
          } satisfies RemoteFileEntry;
        })
    );

    return entries.sort((a, b) => {
      if (a.type === "directory" && b.type !== "directory") return -1;
      if (a.type !== "directory" && b.type === "directory") return 1;
      return a.name.localeCompare(b.name);
    });
  };

  const uploadRemoteFile = async (
    connectionId: string,
    localPath: string,
    remotePath: string,
    sender?: WebContents,
    taskId?: string
  ): Promise<{ ok: true }> => {
    getConnectionOrThrow(connectionId);
    sendTransferStatus(sender, {
      taskId,
      direction: "upload",
      connectionId,
      localPath,
      remotePath,
      status: "running",
      progress: 5
    });
    const connection = await ensureConnection(connectionId);
    try {
      await connection.upload(localPath, remotePath);
      sendTransferStatus(sender, {
        taskId,
        direction: "upload",
        connectionId,
        localPath,
        remotePath,
        status: "success",
        progress: 100
      });
      appendAuditLogIfEnabled({
        action: "sftp.upload",
        level: "info",
        connectionId,
        message: "Uploaded file to remote host",
        metadata: { localPath, remotePath }
      });
      return { ok: true };
    } catch (error) {
      sendTransferStatus(sender, {
        taskId,
        direction: "upload",
        connectionId,
        localPath,
        remotePath,
        status: "failed",
        progress: 100,
        error: normalizeError(error)
      });
      throw error;
    }
  };

  const downloadRemoteFile = async (
    connectionId: string,
    remotePath: string,
    localPath: string,
    sender?: WebContents,
    taskId?: string
  ): Promise<{ ok: true }> => {
    getConnectionOrThrow(connectionId);
    sendTransferStatus(sender, {
      taskId,
      direction: "download",
      connectionId,
      localPath,
      remotePath,
      status: "running",
      progress: 5
    });
    const connection = await ensureConnection(connectionId);
    try {
      await connection.download(remotePath, localPath);
      sendTransferStatus(sender, {
        taskId,
        direction: "download",
        connectionId,
        localPath,
        remotePath,
        status: "success",
        progress: 100
      });
      appendAuditLogIfEnabled({
        action: "sftp.download",
        level: "info",
        connectionId,
        message: "Downloaded file from remote host",
        metadata: { remotePath, localPath }
      });
      return { ok: true };
    } catch (error) {
      sendTransferStatus(sender, {
        taskId,
        direction: "download",
        connectionId,
        localPath,
        remotePath,
        status: "failed",
        progress: 100,
        error: normalizeError(error)
      });
      throw error;
    }
  };

  const ensureRemoteTarAvailable = async (
    connection: SshConnection,
    actionLabel: string
  ): Promise<void> => {
    const result = await connection.exec(buildRemoteTarCheckCommand());
    if (result.exitCode !== 0) {
      throw new Error(`${actionLabel}失败：远端缺少 tar/gzip 命令`);
    }
  };

  const pickRemoteCommandError = (stdout: string, stderr: string, exitCode: number): string => {
    return stderr.trim() || stdout.trim() || `exit ${exitCode}`;
  };

  const uploadRemotePacked = async (
    connectionId: string,
    localPaths: string[],
    remoteDir: string,
    archiveName?: string,
    sender?: WebContents,
    taskId?: string
  ): Promise<{ ok: true }> => {
    getConnectionOrThrow(connectionId);
    const resolvedLocalPaths = localPaths.map((localPath) => resolveLocalPath(localPath));
    const defaultArchiveBase = resolvedLocalPaths.length === 1
      ? path.basename(resolvedLocalPaths[0]!)
      : `upload-bundle-${Date.now()}`;
    const finalArchiveName = normalizeArchiveName(archiveName, defaultArchiveBase);
    const normalizedRemoteDir = remoteDir.trim() || "/";
    const remoteDisplayPath = joinRemotePath(normalizedRemoteDir, finalArchiveName);
    const localDisplayPath = resolvedLocalPaths.length === 1
      ? resolvedLocalPaths[0]!
      : `${resolvedLocalPaths[0] ?? ""} (+${resolvedLocalPaths.length - 1} files)`;
    const localArchivePath = path.join(os.tmpdir(), `nextshell-upload-${randomUUID()}-${finalArchiveName}`);
    const remoteArchivePath = `/tmp/nextshell-upload-${randomUUID()}.tar.gz`;
    let localArchiveCreated = false;
    let remoteArchiveCleaned = false;
    let connection: SshConnection | undefined;

    const cleanupRemoteArchive = async (): Promise<void> => {
      if (!connection || remoteArchiveCleaned) {
        return;
      }

      try {
        await connection.exec(buildRemoteRemoveFileCommand(remoteArchivePath));
        remoteArchiveCleaned = true;
      } catch (error) {
        logger.warn("[SFTP Packed Upload] failed to cleanup remote archive", {
          connectionId,
          remoteArchivePath,
          reason: normalizeError(error)
        });
      }
    };

    sendTransferStatus(sender, {
      taskId,
      direction: "upload",
      connectionId,
      localPath: localDisplayPath,
      remotePath: remoteDisplayPath,
      status: "running",
      progress: 5,
      message: "开始打包上传"
    });

    try {
      await assertLocalTarAvailable();
      connection = await ensureConnection(connectionId);
      await ensureRemoteTarAvailable(connection, "打包上传");
      sendTransferStatus(sender, {
        taskId,
        direction: "upload",
        connectionId,
        localPath: localDisplayPath,
        remotePath: remoteDisplayPath,
        status: "running",
        progress: 20,
        message: "tar 环境检查通过"
      });

      await createLocalTarGzArchive(resolvedLocalPaths, localArchivePath);
      localArchiveCreated = true;
      sendTransferStatus(sender, {
        taskId,
        direction: "upload",
        connectionId,
        localPath: localDisplayPath,
        remotePath: remoteDisplayPath,
        status: "running",
        progress: 45,
        message: "本地打包完成"
      });

      await connection.upload(localArchivePath, remoteArchivePath);
      sendTransferStatus(sender, {
        taskId,
        direction: "upload",
        connectionId,
        localPath: localDisplayPath,
        remotePath: remoteDisplayPath,
        status: "running",
        progress: 75,
        message: "压缩包上传完成"
      });

      const extractResult = await connection.exec(
        buildRemoteTarExtractCommand(remoteArchivePath, normalizedRemoteDir)
      );
      if (extractResult.exitCode !== 0) {
        throw new Error(`远端解包失败：${pickRemoteCommandError(
          extractResult.stdout,
          extractResult.stderr,
          extractResult.exitCode
        )}`);
      }

      await cleanupRemoteArchive();
      sendTransferStatus(sender, {
        taskId,
        direction: "upload",
        connectionId,
        localPath: localDisplayPath,
        remotePath: remoteDisplayPath,
        status: "running",
        progress: 90,
        message: "远端解包完成"
      });

      sendTransferStatus(sender, {
        taskId,
        direction: "upload",
        connectionId,
        localPath: localDisplayPath,
        remotePath: remoteDisplayPath,
        status: "success",
        progress: 100
      });
      appendAuditLogIfEnabled({
        action: "sftp.upload.packed",
        level: "info",
        connectionId,
        message: "Uploaded packed files to remote host",
        metadata: {
          localPaths: resolvedLocalPaths,
          remoteDir: normalizedRemoteDir,
          archiveName: finalArchiveName
        }
      });
      return { ok: true };
    } catch (error) {
      sendTransferStatus(sender, {
        taskId,
        direction: "upload",
        connectionId,
        localPath: localDisplayPath,
        remotePath: remoteDisplayPath,
        status: "failed",
        progress: 100,
        error: normalizeError(error)
      });
      throw error;
    } finally {
      await cleanupRemoteArchive();
      if (localArchiveCreated || fs.existsSync(localArchivePath)) {
        try {
          fs.rmSync(localArchivePath, { force: true });
        } catch (error) {
          logger.warn("[SFTP Packed Upload] failed to cleanup local archive", {
            localArchivePath,
            reason: normalizeError(error)
          });
        }
      }
    }
  };

  const downloadRemotePacked = async (
    connectionId: string,
    remoteDir: string,
    entryNames: string[],
    localDir: string,
    archiveName?: string,
    sender?: WebContents,
    taskId?: string
  ): Promise<{ ok: true; localArchivePath: string }> => {
    getConnectionOrThrow(connectionId);
    const normalizedRemoteDir = remoteDir.trim() || "/";
    const normalizedEntryNames = normalizeRemoteEntryNames(entryNames);
    const defaultArchiveBase = normalizedEntryNames.length === 1
      ? normalizedEntryNames[0]!
      : `download-bundle-${Date.now()}`;
    const finalArchiveName = normalizeArchiveName(archiveName, defaultArchiveBase);
    const resolvedLocalDir = resolveLocalPath(localDir);
    const localArchivePath = path.join(resolvedLocalDir, finalArchiveName);
    const remoteArchivePath = `/tmp/nextshell-download-${randomUUID()}.tar.gz`;
    const remoteDisplayPath = joinRemotePath(normalizedRemoteDir, finalArchiveName);
    let connection: SshConnection | undefined;
    let remoteArchiveCleaned = false;

    const cleanupRemoteArchive = async (): Promise<void> => {
      if (!connection || remoteArchiveCleaned) {
        return;
      }

      try {
        await connection.exec(buildRemoteRemoveFileCommand(remoteArchivePath));
        remoteArchiveCleaned = true;
      } catch (error) {
        logger.warn("[SFTP Packed Download] failed to cleanup remote archive", {
          connectionId,
          remoteArchivePath,
          reason: normalizeError(error)
        });
      }
    };

    sendTransferStatus(sender, {
      taskId,
      direction: "download",
      connectionId,
      localPath: localArchivePath,
      remotePath: remoteDisplayPath,
      status: "running",
      progress: 5,
      message: "开始打包下载"
    });

    try {
      connection = await ensureConnection(connectionId);
      await ensureRemoteTarAvailable(connection, "打包下载");
      sendTransferStatus(sender, {
        taskId,
        direction: "download",
        connectionId,
        localPath: localArchivePath,
        remotePath: remoteDisplayPath,
        status: "running",
        progress: 20,
        message: "远端 tar 环境检查通过"
      });

      const packResult = await connection.exec(
        buildRemoteTarCreateCommand(normalizedRemoteDir, remoteArchivePath, normalizedEntryNames)
      );
      if (packResult.exitCode !== 0) {
        throw new Error(`远端打包失败：${pickRemoteCommandError(
          packResult.stdout,
          packResult.stderr,
          packResult.exitCode
        )}`);
      }
      sendTransferStatus(sender, {
        taskId,
        direction: "download",
        connectionId,
        localPath: localArchivePath,
        remotePath: remoteDisplayPath,
        status: "running",
        progress: 45,
        message: "远端打包完成"
      });

      fs.mkdirSync(path.dirname(localArchivePath), { recursive: true });
      await connection.download(remoteArchivePath, localArchivePath);
      sendTransferStatus(sender, {
        taskId,
        direction: "download",
        connectionId,
        localPath: localArchivePath,
        remotePath: remoteDisplayPath,
        status: "running",
        progress: 75,
        message: "压缩包下载完成"
      });

      await cleanupRemoteArchive();
      sendTransferStatus(sender, {
        taskId,
        direction: "download",
        connectionId,
        localPath: localArchivePath,
        remotePath: remoteDisplayPath,
        status: "running",
        progress: 90,
        message: "远端临时文件已清理"
      });

      sendTransferStatus(sender, {
        taskId,
        direction: "download",
        connectionId,
        localPath: localArchivePath,
        remotePath: remoteDisplayPath,
        status: "success",
        progress: 100
      });
      appendAuditLogIfEnabled({
        action: "sftp.download.packed",
        level: "info",
        connectionId,
        message: "Downloaded packed files from remote host",
        metadata: {
          remoteDir: normalizedRemoteDir,
          entryNames: normalizedEntryNames,
          localArchivePath
        }
      });
      return { ok: true, localArchivePath };
    } catch (error) {
      sendTransferStatus(sender, {
        taskId,
        direction: "download",
        connectionId,
        localPath: localArchivePath,
        remotePath: remoteDisplayPath,
        status: "failed",
        progress: 100,
        error: normalizeError(error)
      });
      throw error;
    } finally {
      await cleanupRemoteArchive();
    }
  };

  const transferRemotePacked = async (
    sourceConnectionId: string,
    sourceDir: string,
    entryNames: string[],
    targetConnectionId: string,
    targetDir: string,
    archiveName?: string,
    sender?: WebContents,
    taskId?: string
  ): Promise<{ ok: true }> => {
    getConnectionOrThrow(sourceConnectionId);
    getConnectionOrThrow(targetConnectionId);

    const normalizedSourceDir = sourceDir.trim() || "/";
    const normalizedTargetDir = targetDir.trim() || "/";
    const normalizedEntryNames = normalizeRemoteEntryNames(entryNames);
    const defaultArchiveBase = normalizedEntryNames.length === 1
      ? normalizedEntryNames[0]!
      : `transfer-bundle-${Date.now()}`;
    const finalArchiveName = normalizeArchiveName(archiveName, defaultArchiveBase);
    const sourceRemoteArchivePath = `/tmp/nextshell-transfer-src-${randomUUID()}.tar.gz`;
    const targetRemoteArchivePath = `/tmp/nextshell-transfer-target-${randomUUID()}.tar.gz`;
    const localArchivePath = path.join(
      os.tmpdir(),
      `nextshell-transfer-${randomUUID()}-${finalArchiveName}`
    );
    const remoteDisplayPath = `${targetConnectionId}:${joinRemotePath(normalizedTargetDir, finalArchiveName)}`;
    const localDisplayPath = `${sourceConnectionId}:${joinRemotePath(normalizedSourceDir, finalArchiveName)}`;
    let sourceConnection: SshConnection | undefined;
    let targetConnection: SshConnection | undefined;
    let sourceRemoteCleaned = false;
    let targetRemoteCleaned = false;
    let localArchiveCreated = false;

    const cleanupSourceRemoteArchive = async (): Promise<void> => {
      if (!sourceConnection || sourceRemoteCleaned) {
        return;
      }

      try {
        await sourceConnection.exec(buildRemoteRemoveFileCommand(sourceRemoteArchivePath));
        sourceRemoteCleaned = true;
      } catch (error) {
        logger.warn("[SFTP Packed Transfer] failed to cleanup source remote archive", {
          sourceConnectionId,
          sourceRemoteArchivePath,
          reason: normalizeError(error)
        });
      }
    };

    const cleanupTargetRemoteArchive = async (): Promise<void> => {
      if (!targetConnection || targetRemoteCleaned) {
        return;
      }

      try {
        await targetConnection.exec(buildRemoteRemoveFileCommand(targetRemoteArchivePath));
        targetRemoteCleaned = true;
      } catch (error) {
        logger.warn("[SFTP Packed Transfer] failed to cleanup target remote archive", {
          targetConnectionId,
          targetRemoteArchivePath,
          reason: normalizeError(error)
        });
      }
    };

    sendTransferStatus(sender, {
      taskId,
      direction: "download",
      connectionId: sourceConnectionId,
      localPath: localDisplayPath,
      remotePath: remoteDisplayPath,
      status: "running",
      progress: 5,
      message: "开始跨服务器快传"
    });

    try {
      sourceConnection = await ensureConnection(sourceConnectionId);
      targetConnection = await ensureConnection(targetConnectionId);
      await ensureRemoteTarAvailable(sourceConnection, "跨服务器快传");
      await ensureRemoteTarAvailable(targetConnection, "跨服务器快传");
      sendTransferStatus(sender, {
        taskId,
        direction: "download",
        connectionId: sourceConnectionId,
        localPath: localDisplayPath,
        remotePath: remoteDisplayPath,
        status: "running",
        progress: 20,
        message: "两端 tar 环境检查通过"
      });

      const packResult = await sourceConnection.exec(
        buildRemoteTarCreateCommand(
          normalizedSourceDir,
          sourceRemoteArchivePath,
          normalizedEntryNames
        )
      );
      if (packResult.exitCode !== 0) {
        throw new Error(`源服务器打包失败：${pickRemoteCommandError(
          packResult.stdout,
          packResult.stderr,
          packResult.exitCode
        )}`);
      }
      sendTransferStatus(sender, {
        taskId,
        direction: "download",
        connectionId: sourceConnectionId,
        localPath: localDisplayPath,
        remotePath: remoteDisplayPath,
        status: "running",
        progress: 45,
        message: "源服务器打包完成"
      });

      fs.mkdirSync(path.dirname(localArchivePath), { recursive: true });
      await sourceConnection.download(sourceRemoteArchivePath, localArchivePath);
      localArchiveCreated = true;
      sendTransferStatus(sender, {
        taskId,
        direction: "download",
        connectionId: sourceConnectionId,
        localPath: localDisplayPath,
        remotePath: remoteDisplayPath,
        status: "running",
        progress: 65,
        message: "中转包已下载到本机"
      });

      await targetConnection.upload(localArchivePath, targetRemoteArchivePath);
      sendTransferStatus(sender, {
        taskId,
        direction: "download",
        connectionId: sourceConnectionId,
        localPath: localDisplayPath,
        remotePath: remoteDisplayPath,
        status: "running",
        progress: 80,
        message: "中转包已上传到目标服务器"
      });

      const extractResult = await targetConnection.exec(
        buildRemoteTarExtractCommand(targetRemoteArchivePath, normalizedTargetDir)
      );
      if (extractResult.exitCode !== 0) {
        throw new Error(`目标服务器解包失败：${pickRemoteCommandError(
          extractResult.stdout,
          extractResult.stderr,
          extractResult.exitCode
        )}`);
      }

      await cleanupSourceRemoteArchive();
      await cleanupTargetRemoteArchive();
      sendTransferStatus(sender, {
        taskId,
        direction: "download",
        connectionId: sourceConnectionId,
        localPath: localDisplayPath,
        remotePath: remoteDisplayPath,
        status: "running",
        progress: 90,
        message: "目标服务器解包完成"
      });

      sendTransferStatus(sender, {
        taskId,
        direction: "download",
        connectionId: sourceConnectionId,
        localPath: localDisplayPath,
        remotePath: remoteDisplayPath,
        status: "success",
        progress: 100
      });
      appendAuditLogIfEnabled({
        action: "sftp.transfer.packed",
        level: "info",
        connectionId: sourceConnectionId,
        message: "Transferred packed files between remote hosts",
        metadata: {
          sourceConnectionId,
          sourceDir: normalizedSourceDir,
          targetConnectionId,
          targetDir: normalizedTargetDir,
          entryNames: normalizedEntryNames
        }
      });
      return { ok: true };
    } catch (error) {
      sendTransferStatus(sender, {
        taskId,
        direction: "download",
        connectionId: sourceConnectionId,
        localPath: localDisplayPath,
        remotePath: remoteDisplayPath,
        status: "failed",
        progress: 100,
        error: normalizeError(error)
      });
      throw error;
    } finally {
      await cleanupSourceRemoteArchive();
      await cleanupTargetRemoteArchive();
      if (localArchiveCreated || fs.existsSync(localArchivePath)) {
        try {
          fs.rmSync(localArchivePath, { force: true });
        } catch (error) {
          logger.warn("[SFTP Packed Transfer] failed to cleanup local archive", {
            localArchivePath,
            reason: normalizeError(error)
          });
        }
      }
    }
  };

  const createRemoteDirectory = async (
    connectionId: string,
    pathName: string
  ): Promise<{ ok: true }> => {
    getConnectionOrThrow(connectionId);
    const connection = await ensureConnection(connectionId);
    await connection.mkdir(pathName, true);
    appendAuditLogIfEnabled({
      action: "sftp.mkdir",
      level: "info",
      connectionId,
      message: "Created remote directory",
      metadata: { pathName }
    });
    return { ok: true };
  };

  const renameRemoteFile = async (
    connectionId: string,
    fromPath: string,
    toPath: string
  ): Promise<{ ok: true }> => {
    getConnectionOrThrow(connectionId);
    const connection = await ensureConnection(connectionId);
    await connection.rename(fromPath, toPath);
    appendAuditLogIfEnabled({
      action: "sftp.rename",
      level: "warn",
      connectionId,
      message: "Renamed remote path",
      metadata: { fromPath, toPath }
    });
    return { ok: true };
  };

  const deleteRemoteFile = async (
    connectionId: string,
    targetPath: string,
    type: RemoteFileEntry["type"]
  ): Promise<{ ok: true }> => {
    getConnectionOrThrow(connectionId);
    const connection = await ensureConnection(connectionId);

    const normalizedType: RemotePathType =
      type === "directory" ? "directory" : type === "link" ? "link" : "file";

    await connection.remove(targetPath, normalizedType);
    appendAuditLogIfEnabled({
      action: "sftp.delete",
      level: "warn",
      connectionId,
      message: "Deleted remote path",
      metadata: { targetPath, type: normalizedType }
    });
    return { ok: true };
  };

  const listAuditLogs = (limit: number): AuditLogRecord[] => {
    return connections.listAuditLogs(limit);
  };

  const clearAuditLogs = (): { ok: true; deleted: number } => {
    return { ok: true, deleted: connections.clearAuditLogs() };
  };

  const listMigrations = (): MigrationRecord[] => {
    return connections.listMigrations();
  };

  const listCommandHistory = (): CommandHistoryEntry[] => {
    return connections.listCommandHistory();
  };

  const pushCommandHistory = (command: string): CommandHistoryEntry => {
    return connections.pushCommandHistory(command);
  };

  const removeCommandHistory = (command: string): { ok: true } => {
    connections.removeCommandHistory(command);
    return { ok: true };
  };

  const clearCommandHistory = (): { ok: true } => {
    connections.clearCommandHistory();
    return { ok: true };
  };

  const listSavedCommands = (query?: SavedCommandListInput): SavedCommand[] => {
    return connections.listSavedCommands(query ?? {});
  };

  const upsertSavedCommand = (input: SavedCommandUpsertInput): SavedCommand => {
    return connections.upsertSavedCommand({
      id: input.id,
      name: input.name,
      description: input.description,
      group: input.group,
      command: input.command,
      isTemplate: input.isTemplate
    });
  };

  const removeSavedCommand = (input: SavedCommandRemoveInput): { ok: true } => {
    connections.clearTemplateParams(input.id);
    connections.removeSavedCommand(input.id);
    return { ok: true };
  };

  const openRemoteEdit = async (
    connectionId: string,
    remotePath: string,
    editorCommand: string,
    sender: WebContents
  ): Promise<{ editId: string; localPath: string }> => {
    getConnectionOrThrow(connectionId);
    try {
      const result = await remoteEditManager.open(connectionId, remotePath, editorCommand, sender);
      appendAuditLogIfEnabled({
        action: "sftp.edit_open",
        level: "info",
        connectionId,
        message: "Opened remote file for live editing",
        metadata: { remotePath, editId: result.editId }
      });
      return result;
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      const enriched = error as {
        source?: string;
        code?: string;
        requestedCommand?: string;
        resolvedCommand?: string;
      };
      appendAuditLogIfEnabled({
        action: "sftp.edit_open_failed",
        level: "error",
        connectionId,
        message: "Failed to open remote file for live editing",
        metadata: {
          remotePath,
          editorCommand,
          reason,
          commandSource: enriched.source,
          code: enriched.code,
          requestedCommand: enriched.requestedCommand,
          resolvedCommand: enriched.resolvedCommand
        }
      });
      throw error;
    }
  };

  const stopRemoteEdit = async (editId: string): Promise<{ ok: true }> => {
    await remoteEditManager.stop(editId);
    appendAuditLogIfEnabled({
      action: "sftp.edit_stop",
      level: "info",
      message: "Stopped remote file live editing",
      metadata: { editId }
    });
    return { ok: true };
  };

  const stopAllRemoteEdits = async (): Promise<{ ok: true }> => {
    await remoteEditManager.stopAll();
    appendAuditLogIfEnabled({
      action: "sftp.edit_stop_all",
      level: "info",
      message: "Stopped all remote file live editing sessions"
    });
    return { ok: true };
  };

  const listRemoteEdits = (): SftpEditSessionInfo[] => {
    return remoteEditManager.listSessions();
  };

  const openBuiltinEdit = async (
    connectionId: string,
    remotePath: string,
    sender: WebContents
  ): Promise<{ editId: string; content: string }> => {
    return remoteEditManager.openBuiltin(connectionId, remotePath, sender);
  };

  const saveBuiltinEdit = async (
    editId: string,
    connectionId: string,
    remotePath: string,
    content: string
  ): Promise<{ ok: true }> => {
    await remoteEditManager.saveBuiltin(editId, connectionId, remotePath, content);
    return { ok: true };
  };

  // ─── Process Monitor ──────────────────────────────────────────────────────

  const startProcessMonitor = async (
    connectionId: string,
    sender: WebContents
  ): Promise<{ ok: true }> => {
    assertMonitorEnabled(connectionId);
    assertVisibleTerminalAlive(connectionId);

    const runtime = await ensureProcessMonitorRuntime(connectionId);
    runtime.sender = sender;
    processMonitorDispatcher.clear(connectionId);
    return runtime.controller.start();
  };

  const stopProcessMonitor = (connectionId: string): { ok: true } => {
    const runtime = processMonitorRuntimes.get(connectionId);
    if (runtime) {
      runtime.sender = undefined;
      processMonitorDispatcher.clear(connectionId);
      void runtime.controller.stop();
    }
    return { ok: true };
  };

  const getProcessDetail = async (
    connectionId: string,
    pid: number
  ): Promise<ProcessDetailSnapshot> => {
    assertMonitorEnabled(connectionId);
    assertVisibleTerminalAlive(connectionId);

    // Use ad-hoc session for on-demand detail queries
    const adhoc = await ensureAdhocSession(connectionId);

    const normalizedPid = Math.trunc(pid);
    if (normalizedPid < 1) {
      throw new Error("无效进程 PID");
    }

    const primaryCommand =
      `ps -p ${normalizedPid} -o pid=,ppid=,user=,state=,%cpu=,%mem=,rss=,etime=,comm=`;
    const argsCommand = `ps -p ${normalizedPid} -o args=`;

    const primary = await adhoc.connection.exec(primaryCommand);
    if (primary.exitCode !== 0) {
      throw new Error("进程不存在或已结束");
    }

    const parsed = parseProcessDetailPrimary(connectionId, primary.stdout);
    if (!parsed) {
      throw new Error("进程不存在或已结束");
    }

    const args = await adhoc.connection.exec(argsCommand);
    const commandLine = args.exitCode === 0
      ? firstNonEmptyLine(args.stdout) ?? parsed.command
      : parsed.command;

    return {
      ...parsed,
      commandLine,
      capturedAt: new Date().toISOString()
    };
  };

  const killRemoteProcess = async (
    connectionId: string,
    pid: number,
    signal: "SIGTERM" | "SIGKILL"
  ): Promise<{ ok: true }> => {
    assertMonitorEnabled(connectionId);
    assertVisibleTerminalAlive(connectionId);

    // Use ad-hoc session for kill commands
    const adhoc = await ensureAdhocSession(connectionId);
    if (signal !== "SIGTERM" && signal !== "SIGKILL") {
      throw new Error("Invalid signal");
    }
    const result = await adhoc.connection.exec(`kill -${signal} ${pid} 2>&1`);
    if (result.exitCode !== 0) {
      throw new Error(`kill 失败 (exit ${result.exitCode}): ${result.stdout.trim() || "unknown error"}`);
    }
    appendAuditLogIfEnabled({
      action: "monitor.process_kill",
      level: "warn",
      connectionId,
      message: `Sent ${signal} to PID ${pid}`,
      metadata: { pid, signal }
    });
    return { ok: true };
  };

  // ─── Network Monitor ─────────────────────────────────────────────────────

  const startNetworkMonitor = async (
    connectionId: string,
    sender: WebContents
  ): Promise<{ ok: true }> => {
    assertMonitorEnabled(connectionId);
    assertVisibleTerminalAlive(connectionId);

    const runtime = await ensureNetworkMonitorRuntime(connectionId);
    runtime.sender = sender;
    networkMonitorDispatcher.clear(connectionId);
    return runtime.controller.start();
  };

  const stopNetworkMonitor = (connectionId: string): { ok: true } => {
    const runtime = networkMonitorRuntimes.get(connectionId);
    if (runtime) {
      runtime.sender = undefined;
      networkMonitorDispatcher.clear(connectionId);
      void runtime.controller.stop();
    }
    return { ok: true };
  };

  const getNetworkConnections = async (
    connectionId: string,
    port: number
  ): Promise<NetworkConnection[]> => {
    assertMonitorEnabled(connectionId);
    assertVisibleTerminalAlive(connectionId);

    const runtime = await ensureNetworkMonitorRuntime(connectionId);
    return runtime.controller.getConnectionsByPort(port);
  };

  // ─── Backup & Password Management ─────────────────────────────────────────

  const backupList = async (): Promise<BackupArchiveMeta[]> => {
    return backupService.list();
  };

  const backupRun = async (conflictPolicy: BackupConflictPolicy): Promise<{ ok: true; fileName?: string }> => {
    return backupService.run(conflictPolicy);
  };

  const backupRestore = async (archiveId: string, conflictPolicy: RestoreConflictPolicy): Promise<{ ok: true }> => {
    return backupService.restore(archiveId, conflictPolicy);
  };

  const rememberPasswordBestEffort = async (
    password: string,
    phase: "set" | "unlock" | "change"
  ): Promise<void> => {
    const prefs = connections.getAppPreferences();
    if (!prefs.backup.rememberPassword) {
      return;
    }

    try {
      await keytarCache.remember(password);
    } catch (error) {
      const reason = normalizeError(error);
      logger.warn("[Security] failed to cache master password in keytar", { phase, reason });
      appendAuditLogIfEnabled({
        action: "master_password.cache_failed",
        level: "warn",
        message: "Failed to cache master password in keytar",
        metadata: { phase, reason }
      });
    }
  };

  const getMasterKeyMetaOrThrow = () => {
    const meta = connections.getMasterKeyMeta();
    if (!meta) {
      throw new Error("尚未设置主密码。请先设置主密码。");
    }
    return meta;
  };

  const masterPasswordSet = async (password: string): Promise<{ ok: true }> => {
    const meta = await createMasterKeyMeta(password);
    connections.saveMasterKeyMeta(meta);
    masterPassword = password;
    await rememberPasswordBestEffort(password, "set");
    appendAuditLogIfEnabled({
      action: "master_password.set",
      level: "info",
      message: "Master password configured"
    });
    return { ok: true };
  };

  const masterPasswordUnlock = async (password: string): Promise<{ ok: true }> => {
    const meta = getMasterKeyMetaOrThrow();
    if (!(await verifyMasterPassword(password, meta))) {
      throw new Error("主密码错误。");
    }
    masterPassword = password;
    await rememberPasswordBestEffort(password, "unlock");
    return { ok: true };
  };

  const masterPasswordChange = async (oldPassword: string, newPassword: string): Promise<{ ok: true }> => {
    return changeMasterPassword({
      oldPassword,
      newPassword,
      getMasterKeyMeta: () => connections.getMasterKeyMeta(),
      saveMasterKeyMeta: (meta) => connections.saveMasterKeyMeta(meta),
      setMasterPassword: (password) => {
        masterPassword = password;
      },
      rememberPasswordBestEffort,
      appendAuditLog: (payload) => {
        appendAuditLogIfEnabled(payload);
      }
    });
  };

  const masterPasswordClearRemembered = async (): Promise<{ ok: true }> => {
    await keytarCache.clear();
    clearDerivedKeyCache();
    return { ok: true };
  };

  const masterPasswordStatus = async (): Promise<{ isSet: boolean; isUnlocked: boolean; keytarAvailable: boolean }> => {
    const meta = connections.getMasterKeyMeta();
    return {
      isSet: meta !== undefined,
      isUnlocked: masterPassword !== undefined,
      keytarAvailable: keytarCache.isAvailable()
    };
  };

  const masterPasswordGetCached = async (): Promise<{ password?: string }> => {
    if (!masterPassword) {
      await tryRecallMasterPassword();
    }
    return { password: masterPassword };
  };

  const resolveMasterPassword = async (candidate?: string): Promise<string> => {
    const input = candidate?.trim();
    if (input) {
      const meta = getMasterKeyMetaOrThrow();
      if (!(await verifyMasterPassword(input, meta))) {
        throw new Error("主密码错误。");
      }
      masterPassword = input;
      await rememberPasswordBestEffort(input, "unlock");
      return input;
    }

    if (masterPassword) {
      return masterPassword;
    }

    await tryRecallMasterPassword();
    if (masterPassword) {
      return masterPassword;
    }

    throw new Error("主密码未解锁，请先输入主密码。");
  };

  const revealConnectionPassword = async (
    connectionId: string,
    providedMasterPassword?: string
  ): Promise<{ password: string }> => {
    const connection = connections.getById(connectionId);
    if (!connection) {
      throw new Error("连接不存在。");
    }
    if (connection.authType !== "password" && connection.authType !== "interactive") {
      throw new Error("该连接未使用密码或交互式认证。");
    }
    if (!connection.credentialRef) {
      throw new Error("该连接未保存登录密码。");
    }

    await resolveMasterPassword(providedMasterPassword);
    const password = await vault.readCredential(connection.credentialRef);
    if (!password) {
      throw new Error("该连接未保存登录密码。");
    }

    appendAuditLogIfEnabled({
      action: "connection.password_reveal",
      level: "warn",
      connectionId,
      message: "Revealed saved connection password",
      metadata: {
        via: providedMasterPassword?.trim() ? "master-password-input" : "master-password-cache"
      }
    });

    return { password };
  };

  // Backward-compatible aliases
  const backupSetPassword = async (password: string): Promise<{ ok: true }> => {
    return masterPasswordSet(password);
  };

  const backupUnlockPassword = async (password: string): Promise<{ ok: true }> => {
    return masterPasswordUnlock(password);
  };

  const backupClearRemembered = async (): Promise<{ ok: true }> => {
    return masterPasswordClearRemembered();
  };

  const backupPasswordStatus = async (): Promise<{ isSet: boolean; isUnlocked: boolean; keytarAvailable: boolean }> => {
    return masterPasswordStatus();
  };

  // ─── Template Params ──────────────────────────────────────────────────────

  const listTemplateParams = (input?: TemplateParamsListInput): CommandTemplateParam[] => {
    return connections.listTemplateParams(input?.commandId);
  };

  const upsertTemplateParams = (input: TemplateParamsUpsertInput): { ok: true } => {
    connections.upsertTemplateParams(input.commandId, input.params);
    return { ok: true };
  };

  const clearTemplateParams = (input: TemplateParamsClearInput): { ok: true } => {
    connections.clearTemplateParams(input.commandId);
    return { ok: true };
  };

  const dispose = async (): Promise<void> => {
    // 先同步 flush 所有缓冲写入，避免后续 await 链未跑完就退出导致丢失
    connections.flush();

    if (auditPurgeTimer) clearInterval(auditPurgeTimer);

    if (debugFlushTimer) { clearTimeout(debugFlushTimer); debugFlushTimer = undefined; }
    debugPending.length = 0;

    // Dispose all hidden sessions for every connection that has any
    const allConnectionIds = new Set([
      ...systemMonitorRuntimes.keys(),
      ...systemMonitorConnections.keys(),
      ...systemMonitorConnectionPromises.keys(),
      ...processMonitorRuntimes.keys(),
      ...processMonitorPromises.keys(),
      ...processMonitorConnections.keys(),
      ...processMonitorConnectionPromises.keys(),
      ...networkMonitorRuntimes.keys(),
      ...networkMonitorPromises.keys(),
      ...networkMonitorConnections.keys(),
      ...networkMonitorConnectionPromises.keys(),
      ...adhocSessionRuntimes.keys(),
      ...adhocSessionPromises.keys()
    ]);

    await Promise.all(
      Array.from(allConnectionIds).map((connectionId) => disposeAllMonitorSessions(connectionId))
    );

    await remoteEditManager.dispose();

    tracerouteStop();
    await cloudSyncService?.dispose();

    const sessionIds = Array.from(activeSessions.keys());
    await Promise.all(sessionIds.map((sessionId) => closeSession(sessionId)));

    const sshConnections = Array.from(activeConnections.values());
    activeConnections.clear();

    await Promise.all(
      sshConnections.map(async (connection) => {
        await connection.close();
      })
    );

    connections.close();
  };

  return {
    listConnections,
    upsertConnection,
    removeConnection,
    exportConnections,
    exportConnectionsBatch,
    revealConnectionPassword,
    importConnectionsPreview,
    importFinalShellConnectionsPreview,
    importConnectionsExecute,
    listSshKeys,
    upsertSshKey,
    removeSshKey,
    listProxies,
    upsertProxy,
    removeProxy,
    checkForUpdate,
    pingHost,
    tracerouteRun,
    tracerouteStop,
    getAppPreferences,
    updateAppPreferences,
    cloudSyncConfigure,
    cloudSyncDisable,
    cloudSyncStatus,
    cloudSyncSyncNow,
    cloudSyncListConflicts,
    cloudSyncResolveConflict,
    openFilesDialog,
    openDirectoryDialog,
    openLocalPath,
    openSession,
    ackStreamDelivery,
    writeSession,
    resizeSession,
    closeSession,
    getSystemInfoSnapshot,
    startSystemMonitor,
    stopSystemMonitor,
    selectSystemNetworkInterface,
    execCommand,
    getSessionHomeDir,
    execBatchCommand,
    listAuditLogs,
    clearAuditLogs,
    listMigrations,
    listRemoteFiles,
    listLocalFiles,
    uploadRemoteFile,
    downloadRemoteFile,
    uploadRemotePacked,
    downloadRemotePacked,
    transferRemotePacked,
    createRemoteDirectory,
    renameRemoteFile,
    deleteRemoteFile,
    listCommandHistory,
    pushCommandHistory,
    removeCommandHistory,
    clearCommandHistory,
    listSavedCommands,
    upsertSavedCommand,
    removeSavedCommand,
    openRemoteEdit,
    stopRemoteEdit,
    stopAllRemoteEdits,
    listRemoteEdits,
    openBuiltinEdit,
    saveBuiltinEdit,
    startProcessMonitor,
    stopProcessMonitor,
    getProcessDetail,
    killRemoteProcess,
    startNetworkMonitor,
    stopNetworkMonitor,
    getNetworkConnections,
    backupList,
    backupRun,
    backupRestore,
    masterPasswordSet,
    masterPasswordUnlock,
    masterPasswordChange,
    masterPasswordClearRemembered,
    masterPasswordStatus,
    masterPasswordGetCached,
    backupSetPassword,
    backupUnlockPassword,
    backupClearRemembered,
    backupPasswordStatus,
    listTemplateParams,
    upsertTemplateParams,
    clearTemplateParams,
    enableDebugLog,
    disableDebugLog,
    dispose
  };
};
