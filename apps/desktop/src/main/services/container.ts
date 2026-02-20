import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { randomUUID } from "node:crypto";
import * as iconv from "iconv-lite";
import { BrowserWindow, dialog, shell } from "electron";
import type { OpenDialogOptions, WebContents } from "electron";
import type {
  AppPreferences,
  AppPreferencesPatch,
  AuditLogRecord,
  BackupArchiveMeta,
  BackupConflictPolicy,
  BackspaceMode,
  BatchCommandExecutionResult,
  BatchCommandResultItem,
  CommandExecutionResult,
  CommandHistoryEntry,
  CommandTemplateParam,
  ConnectionListQuery,
  ConnectionProfile,
  DeleteMode,
  MigrationRecord,
  MonitorProcess,
  MonitorSnapshot,
  NetworkConnection,
  NetworkListener,
  NetworkSnapshot,
  ProcessDetailSnapshot,
  ProcessSnapshot,
  ProxyProfile,
  RemoteFileEntry,
  RestoreConflictPolicy,
  SavedCommand,
  SessionDescriptor,
  SessionStatus,
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
  DialogOpenDirectoryInput,
  DialogOpenFilesInput,
  DialogOpenPathInput,
  CommandBatchExecInput,
  ConnectionUpsertInput,
  MonitorProcessKillInput,
  SessionAuthOverrideInput,
  SftpTransferStatusEvent,
  SavedCommandListInput,
  SavedCommandRemoveInput,
  SavedCommandUpsertInput,
  SettingsUpdateInput,
  SessionDataEvent,
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
import { IPCChannel, AUTH_REQUIRED_PREFIX } from "../../../../../packages/shared/src/index";
import {
  EncryptedSecretVault,
  KeytarPasswordCache,
  generateDeviceKey,
  createMasterKeyMeta,
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
import { BackupService, applyPendingRestore } from "./backup-service";
import { logger } from "../logger";

interface ActiveSession {
  descriptor: SessionDescriptor;
  channel: SshShellChannel;
  sender: WebContents;
  connectionId: string;
  terminalEncoding: TerminalEncoding;
  backspaceMode: BackspaceMode;
  deleteMode: DeleteMode;
}

interface CreateServiceContainerOptions {
  dataDir: string;
  keytarServiceName?: string;
}

interface MonitorState {
  cpuTotal?: number;
  cpuIdle?: number;
  netRxBytes?: number;
  netTxBytes?: number;
  selectedNetworkInterface?: string;
  networkInterfaceOptions?: string[];
  sampledAt?: number;
}

interface MonitorRuntimeTimers {
  systemTimer?: ReturnType<typeof setInterval>;
  networkTimer?: ReturnType<typeof setTimeout>;
  networkTimerActive?: boolean;
}

interface ProcessTopStreamRuntime {
  channel: SshShellChannel;
  sender: WebContents;
  buffer: string;
  disposed: boolean;
  dataListener: (chunk: Buffer | string) => void;
  errorListener: (error: unknown) => void;
  closeListener: () => void;
}

interface MonitorRuntime {
  monitorConnection: SshConnection;
  monitorShellChannel: SshShellChannel;
  processTopStream?: ProcessTopStreamRuntime;
  commandQueue: Promise<void>;
  timers: MonitorRuntimeTimers;
  disposed: boolean;
}

interface MonitorProbeResult {
  uptimeSeconds: number;
  loadAverage: [number, number, number];
  cpuTotal?: number;
  cpuIdle?: number;
  cpuPercentHint?: number;
  memTotalKb: number;
  memAvailableKb: number;
  swapTotalKb: number;
  swapFreeKb: number;
  diskTotalKb: number;
  diskUsedKb: number;
  networkInterface: string;
  networkInterfaceOptions: string[];
  netRxBytes: number;
  netTxBytes: number;
  processes: MonitorProcess[];
}

export interface ServiceContainer {
  listConnections: (query: ConnectionListQuery) => ConnectionProfile[];
  upsertConnection: (input: ConnectionUpsertInput) => Promise<ConnectionProfile>;
  removeConnection: (id: string) => Promise<{ ok: true }>;
  listSshKeys: () => SshKeyProfile[];
  upsertSshKey: (input: SshKeyUpsertInput) => Promise<SshKeyProfile>;
  removeSshKey: (input: SshKeyRemoveInput) => Promise<{ ok: true }>;
  listProxies: () => ProxyProfile[];
  upsertProxy: (input: ProxyUpsertInput) => Promise<ProxyProfile>;
  removeProxy: (input: ProxyRemoveInput) => Promise<{ ok: true }>;
  getAppPreferences: () => AppPreferences;
  updateAppPreferences: (patch: SettingsUpdateInput) => AppPreferences;
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
    connectionId: string,
    sender: WebContents,
    sessionId?: string,
    authOverride?: SessionAuthOverrideInput
  ) => Promise<SessionDescriptor>;
  writeSession: (sessionId: string, data: string) => { ok: true };
  resizeSession: (sessionId: string, cols: number, rows: number) => { ok: true };
  closeSession: (sessionId: string) => Promise<{ ok: true }>;
  getMonitorSnapshot: (connectionId: string) => Promise<MonitorSnapshot>;
  startSystemMonitor: (connectionId: string, sender: WebContents) => Promise<{ ok: true }>;
  stopSystemMonitor: (connectionId: string) => { ok: true };
  selectSystemNetworkInterface: (connectionId: string, networkInterface: string) => Promise<{ ok: true }>;
  execCommand: (connectionId: string, command: string) => Promise<CommandExecutionResult>;
  execBatchCommand: (input: CommandBatchExecInput) => Promise<BatchCommandExecutionResult>;
  listAuditLogs: (limit: number) => AuditLogRecord[];
  listMigrations: () => MigrationRecord[];
  listRemoteFiles: (connectionId: string, path: string) => Promise<RemoteFileEntry[]>;
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
  backupSetPassword: (password: string) => Promise<{ ok: true }>;
  backupUnlockPassword: (password: string) => Promise<{ ok: true }>;
  backupClearRemembered: () => Promise<{ ok: true }>;
  backupPasswordStatus: () => Promise<{ isSet: boolean; isUnlocked: boolean; keytarAvailable: boolean }>;
  listTemplateParams: (input?: TemplateParamsListInput) => CommandTemplateParam[];
  upsertTemplateParams: (input: TemplateParamsUpsertInput) => { ok: true };
  clearTemplateParams: (input: TemplateParamsClearInput) => { ok: true };
  dispose: () => Promise<void>;
}

const MONITOR_UPTIME_COMMAND = "cat /proc/uptime 2>/dev/null | awk '{print $1}'";
const MONITOR_LOADAVG_COMMAND = "cat /proc/loadavg 2>/dev/null | awk '{print $1\" \"$2\" \"$3}' || uptime 2>/dev/null";
const MONITOR_CPU_STAT_COMMAND = "grep '^cpu ' /proc/stat 2>/dev/null";
const MONITOR_CPU_TOP_COMMAND =
  "top -bn1 2>/dev/null | head -n 20 || top -l 1 -n 0 2>/dev/null | head -n 20";
const MONITOR_MEMINFO_COMMAND = "cat /proc/meminfo 2>/dev/null";
const MONITOR_FREE_COMMAND = "free -k 2>/dev/null";
const MONITOR_DISK_COMMAND = "df -kP / 2>/dev/null | tail -n 1";
const MONITOR_NET_INTERFACES_COMMAND = "ls -1 /sys/class/net 2>/dev/null | grep -v '^lo$'";
const MONITOR_NET_DEFAULT_INTERFACE_COMMAND =
  "ip route show default 2>/dev/null | awk 'NR==1 {for (i=1;i<=NF;i++) if ($i==\"dev\") {print $(i+1); exit}}'";
const MONITOR_SYSTEM_PROCESS_COMMAND =
  "ps -eo pid=,comm=,%cpu=,rss= --sort=-%cpu 2>/dev/null | head -n 5";
const MONITOR_SYSTEM_INTERVAL_MS = 5000;
const MONITOR_NETWORK_INTERVAL_MS = 5000;
const MONITOR_SHELL_READY_MARKER = "__NS_MONITOR_SHELL_READY__";
const MONITOR_COMMAND_TIMEOUT_MS = 20000;
const MONITOR_SHELL_READY_TIMEOUT_MS = 8000;
const MONITOR_MAX_BUFFER_BYTES = 2 * 1024 * 1024;
const MONITOR_PROCESS_TOP_STREAM_COMMAND = "TERM=dumb LANG=C COLUMNS=512 top -b -d 5";
const MONITOR_PROCESS_TOP_MAX_ROWS = 200;
const MONITOR_LINUX_CHECK_COMMAND = "uname -s 2>/dev/null";
const MONITOR_TOP_CHECK_COMMAND = "command -v top >/dev/null 2>&1";
const SFTP_WARMUP_TIMEOUT_MS = 5000;
const NETWORK_PROBE_SS =
  "export LANG=en_US.UTF-8 LANGUAGE=en_US LC_ALL=en_US.UTF-8; " +
  "ss -ltnup 2>/dev/null";
const NETWORK_PROBE_NETSTAT =
  "export LANG=en_US.UTF-8 LANGUAGE=en_US LC_ALL=en_US.UTF-8; " +
  "netstat -ltnup 2>/dev/null";

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

const mergePreferences = (
  current: AppPreferences,
  patch: AppPreferencesPatch
): AppPreferences => {
  const normalizeTerminalColor = (value: string | undefined, fallback: string): string => {
    const trimmed = value?.trim();
    if (!trimmed || !/^#[0-9a-fA-F]{6}$/.test(trimmed)) {
      return fallback;
    }
    return trimmed;
  };

  const normalizeTerminalFontSize = (value: number | undefined, fallback: number): number => {
    if (!Number.isInteger(value) || (value ?? 0) < 10 || (value ?? 0) > 24) {
      return fallback;
    }
    return value as number;
  };

  const normalizeTerminalLineHeight = (value: number | undefined, fallback: number): number => {
    if (!Number.isFinite(value) || (value ?? 0) < 1 || (value ?? 0) > 2) {
      return fallback;
    }
    return value as number;
  };

  return {
    transfer: {
      uploadDefaultDir:
        patch.transfer?.uploadDefaultDir?.trim() || current.transfer.uploadDefaultDir,
      downloadDefaultDir:
        patch.transfer?.downloadDefaultDir?.trim() || current.transfer.downloadDefaultDir
    },
    remoteEdit: {
      defaultEditorCommand:
        patch.remoteEdit?.defaultEditorCommand?.trim() || current.remoteEdit.defaultEditorCommand
    },
    commandCenter: {
      rememberTemplateParams:
        patch.commandCenter?.rememberTemplateParams ?? current.commandCenter.rememberTemplateParams
    },
    terminal: {
      backgroundColor: normalizeTerminalColor(
        patch.terminal?.backgroundColor,
        current.terminal.backgroundColor
      ),
      foregroundColor: normalizeTerminalColor(
        patch.terminal?.foregroundColor,
        current.terminal.foregroundColor
      ),
      fontSize: normalizeTerminalFontSize(
        patch.terminal?.fontSize,
        current.terminal.fontSize
      ),
      lineHeight: normalizeTerminalLineHeight(
        patch.terminal?.lineHeight,
        current.terminal.lineHeight
      )
    },
    backup: {
      remotePath: patch.backup?.remotePath !== undefined
        ? patch.backup.remotePath
        : current.backup.remotePath,
      rclonePath: patch.backup?.rclonePath !== undefined
        ? patch.backup.rclonePath
        : current.backup.rclonePath,
      defaultBackupConflictPolicy:
        patch.backup?.defaultBackupConflictPolicy ?? current.backup.defaultBackupConflictPolicy,
      defaultRestoreConflictPolicy:
        patch.backup?.defaultRestoreConflictPolicy ?? current.backup.defaultRestoreConflictPolicy,
      rememberPassword:
        patch.backup?.rememberPassword ?? current.backup.rememberPassword,
      lastBackupAt:
        patch.backup?.lastBackupAt !== undefined
          ? patch.backup.lastBackupAt
          : current.backup.lastBackupAt
    }
  };
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

const parseLoadAverage = (raw: string): [number, number, number] => {
  if (!raw.trim()) {
    return [0, 0, 0];
  }

  const lower = raw.toLowerCase();
  const loadSegment = lower.includes("load average")
    ? raw.slice(lower.indexOf("load average"))
    : raw;

  const numbers = Array.from(loadSegment.matchAll(/-?\d+(?:\.\d+)?/g))
    .map((match) => parseFloatSafe(match[0]))
    .filter((value) => Number.isFinite(value));

  if (numbers.length < 3) {
    return [0, 0, 0];
  }

  return [numbers[0] ?? 0, numbers[1] ?? 0, numbers[2] ?? 0];
};

const parseCpuTotals = (raw: string): { cpuTotal?: number; cpuIdle?: number } => {
  const cpuLine = raw
    .split(/\r?\n/)
    .map((line) => line.trim())
    .find((line) => line.startsWith("cpu "));

  if (!cpuLine) {
    return {};
  }

  const fields = cpuLine
    .replace(/\s+/g, " ")
    .trim()
    .split(" ")
    .slice(1)
    .map((value) => parseFloatSafe(value));

  if (fields.length < 4) {
    return {};
  }

  const cpuTotal = fields.reduce((sum, value) => sum + value, 0);
  const cpuIdle = (fields[3] ?? 0) + (fields[4] ?? 0);

  if (cpuTotal <= 0) {
    return {};
  }

  return { cpuTotal, cpuIdle };
};

const parseCpuPercentFromTop = (raw: string): number | undefined => {
  const lines = raw.split(/\r?\n/).map((line) => line.trim()).filter(Boolean);
  for (const line of lines) {
    const lower = line.toLowerCase();
    if (!lower.includes("cpu")) {
      continue;
    }

    const idleMatch = line.match(/([0-9]+(?:\.[0-9]+)?)\s*id\b/) ??
      line.match(/([0-9]+(?:\.[0-9]+)?)%\s*idle\b/);
    if (idleMatch?.[1]) {
      const idle = parseFloatSafe(idleMatch[1]);
      return Math.max(0, Math.min(100, 100 - idle));
    }

    const userMatch = line.match(/([0-9]+(?:\.[0-9]+)?)%?\s*(?:us|user)\b/i);
    const systemMatch = line.match(/([0-9]+(?:\.[0-9]+)?)%?\s*(?:sy|sys)\b/i);
    const combined = parseFloatSafe(userMatch?.[1]) + parseFloatSafe(systemMatch?.[1]);
    if (combined > 0) {
      return Math.max(0, Math.min(100, combined));
    }
  }

  return undefined;
};

const parseMemoryFromMeminfo = (
  raw: string
): { memTotalKb: number; memAvailableKb: number; swapTotalKb: number; swapFreeKb: number } | undefined => {
  const values = new Map<string, number>();
  for (const line of raw.split(/\r?\n/)) {
    const match = line.match(/^([A-Za-z_()]+):\s+(\d+)/);
    if (!match?.[1] || !match[2]) {
      continue;
    }
    values.set(match[1], parseIntSafe(match[2]));
  }

  const memTotalKb = values.get("MemTotal") ?? 0;
  if (memTotalKb <= 0) {
    return undefined;
  }

  const memAvailableKb = values.get("MemAvailable") ??
    ((values.get("MemFree") ?? 0) + (values.get("Buffers") ?? 0) + (values.get("Cached") ?? 0));

  return {
    memTotalKb,
    memAvailableKb,
    swapTotalKb: values.get("SwapTotal") ?? 0,
    swapFreeKb: values.get("SwapFree") ?? 0
  };
};

const parseMemoryFromFree = (
  raw: string
): { memTotalKb: number; memAvailableKb: number; swapTotalKb: number; swapFreeKb: number } | undefined => {
  const lines = raw.split(/\r?\n/).map((line) => line.trim()).filter(Boolean);
  const memLine = lines.find((line) => /^mem:/i.test(line));
  if (!memLine) {
    return undefined;
  }

  const memParts = memLine.split(/\s+/);
  const memTotalKb = parseIntSafe(memParts[1]);
  if (memTotalKb <= 0) {
    return undefined;
  }

  const memAvailableKb = parseIntSafe(memParts[6]) || parseIntSafe(memParts[3]);
  const swapLine = lines.find((line) => /^swap:/i.test(line));
  const swapParts = swapLine?.split(/\s+/) ?? [];
  const swapTotalKb = parseIntSafe(swapParts[1]);
  const swapFreeKb = parseIntSafe(swapParts[3]) || Math.max(0, swapTotalKb - parseIntSafe(swapParts[2]));

  return {
    memTotalKb,
    memAvailableKb,
    swapTotalKb,
    swapFreeKb
  };
};

const parseDiskUsage = (raw: string): { diskTotalKb: number; diskUsedKb: number } => {
  const line = raw
    .split(/\r?\n/)
    .map((value) => value.trim())
    .filter(Boolean)
    .at(-1) ?? "";
  const parts = line.replace(/\s+/g, " ").trim().split(" ");
  return {
    diskTotalKb: parseIntSafe(parts[1]),
    diskUsedKb: parseIntSafe(parts[2])
  };
};

const normalizeNetworkInterfaceName = (value: string): string | undefined => {
  const trimmed = value.trim();
  if (!trimmed) {
    return undefined;
  }

  if (!/^[a-zA-Z0-9_.:-]+$/.test(trimmed)) {
    return undefined;
  }

  return trimmed;
};

const parseNetworkInterfaceList = (raw: string): string[] => {
  const interfaces = raw
    .split(/\r?\n/)
    .map((line) => normalizeNetworkInterfaceName(line))
    .filter((line): line is string => Boolean(line) && line !== "lo");

  if (interfaces.length > 0) {
    return Array.from(new Set(interfaces)).sort((a, b) => a.localeCompare(b));
  }

  return ["eth0"];
};

const parseNetworkCounters = (raw: string): { rxBytes: number; txBytes: number } => {
  const values = raw
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => Number.parseFloat(line))
    .filter((value) => Number.isFinite(value));

  return {
    rxBytes: values[0] ?? 0,
    txBytes: values[1] ?? 0
  };
};

const parseMonitorProcesses = (raw: string): MonitorProcess[] => {
  const processes = raw
    .split(/\r?\n/)
    .map((line) => line.replace(/\s+/g, " ").trim())
    .filter(Boolean)
    .map((line) => {
      const parts = line.split(" ");
      const pid = parseIntSafe(parts[0]);
      const command = parts[1] ?? "unknown";
      const cpuPercent = parseFloatSafe(parts[2]);
      const rssKb = parseFloatSafe(parts[3]);

      return {
        pid,
        command,
        cpuPercent,
        memoryMb: Number((rssKb / 1024).toFixed(2))
      };
    })
    .filter((item) => item.pid > 0)
    .slice(0, 5);

  return processes.length > 0
    ? processes
    : [{ pid: 0, command: "n/a", cpuPercent: 0, memoryMb: 0 }];
};

const parseMonitorProbe = (samples: {
  uptime: string;
  loadAverage: string;
  cpuStat: string;
  cpuTop: string;
  meminfo: string;
  free: string;
  disk: string;
  networkInterface: string;
  networkInterfaceOptions: string[];
  networkCounters: string;
  process: string;
}): MonitorProbeResult => {
  const memory = parseMemoryFromMeminfo(samples.meminfo) ??
    parseMemoryFromFree(samples.free) ?? {
      memTotalKb: 0,
      memAvailableKb: 0,
      swapTotalKb: 0,
      swapFreeKb: 0
    };

  const disk = parseDiskUsage(samples.disk);
  const counters = parseNetworkCounters(samples.networkCounters);
  const cpuTotals = parseCpuTotals(samples.cpuStat);

  return {
    uptimeSeconds: parseUptimeSeconds(samples.uptime),
    loadAverage: parseLoadAverage(samples.loadAverage),
    cpuTotal: cpuTotals.cpuTotal,
    cpuIdle: cpuTotals.cpuIdle,
    cpuPercentHint: parseCpuPercentFromTop(samples.cpuTop),
    memTotalKb: memory.memTotalKb,
    memAvailableKb: memory.memAvailableKb,
    swapTotalKb: memory.swapTotalKb,
    swapFreeKb: memory.swapFreeKb,
    diskTotalKb: disk.diskTotalKb,
    diskUsedKb: disk.diskUsedKb,
    networkInterface: samples.networkInterface,
    networkInterfaceOptions: samples.networkInterfaceOptions,
    netRxBytes: counters.rxBytes,
    netTxBytes: counters.txBytes,
    processes: parseMonitorProcesses(samples.process)
  };
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

  // ─── Backup Password (optional, for cloud backup only) ────────────────────
  const keytarCache = new KeytarPasswordCache();
  let backupPassword: string | undefined;

  // Try to recall backup password from keytar on startup
  const tryRecallBackupPassword = async (): Promise<void> => {
    const meta = connections.getMasterKeyMeta();
    if (!meta) return;
    const cached = await keytarCache.recall();
    if (!cached) return;
    if (verifyMasterPassword(cached, meta)) {
      backupPassword = cached;
      logger.info("[Security] recalled backup password from keytar");
    }
  };

  void tryRecallBackupPassword();

  const backupService = new BackupService({
    dataDir: options.dataDir,
    repo: connections,
    getMasterPassword: () => backupPassword
  });

  const activeSessions = new Map<string, ActiveSession>();
  const activeConnections = new Map<string, SshConnection>();
  const connectionPromises = new Map<string, Promise<SshConnection>>();
  const monitorConnections = new Map<string, SshConnection>();
  const monitorConnectionPromises = new Map<string, Promise<SshConnection>>();
  const monitorRuntimePromises = new Map<string, Promise<MonitorRuntime>>();
  const monitorRuntimes = new Map<string, MonitorRuntime>();
  const monitorStates = new Map<string, MonitorState>();
  const networkToolCache = new Map<string, "ss" | "netstat">();

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

    const base: Omit<SshConnectOptions, "authType"> = {
      host: profile.host,
      port: profile.port,
      username,
      hostFingerprint: profile.hostFingerprint,
      strictHostKeyChecking: profile.strictHostKeyChecking,
      proxy
    };

    const secret = profile.credentialRef
      ? await vault.readCredential(profile.credentialRef)
      : undefined;
    const effectiveAuthType = authOverride?.authType ?? profile.authType;

    if (effectiveAuthType === "password") {
      const password =
        authOverride?.authType === "password"
          ? authOverride.password
          : profile.authType === "password"
            ? secret
            : undefined;

      if (!password) {
        throw new Error("Password credential is missing. Please provide password.");
      }

      return {
        ...base,
        authType: "password",
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

  const sendSessionData = (sender: WebContents, payload: SessionDataEvent): void => {
    if (!sender.isDestroyed()) {
      sender.send(IPCChannel.SessionData, payload);
    }
  };

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

  const updateSessionStatus = (sessionId: string, status: SessionStatus, reason?: string): void => {
    const active = activeSessions.get(sessionId);
    if (!active) {
      return;
    }

    active.descriptor.status = status;
    sendSessionStatus(active.sender, { sessionId, status, reason });
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

  const stopProcessTopStream = (
    connectionId: string,
    runtime: MonitorRuntime,
    reason: "manual" | "runtime_dispose" | "channel_closed" | "channel_error" | "sender_destroyed"
  ): void => {
    const stream = runtime.processTopStream;
    if (!stream) {
      return;
    }

    runtime.processTopStream = undefined;
    stream.disposed = true;

    stream.channel.off("data", stream.dataListener);
    stream.channel.off("error", stream.errorListener);
    stream.channel.off("close", stream.closeListener);

    try {
      stream.channel.end();
    } catch {
      // ignore close errors from stale channels
    }

    logger.info("[ProcessMonitor] stopped", { connectionId, reason });
  };

  const clearMonitorRuntimeTimers = (connectionId: string, runtime: MonitorRuntime): void => {
    if (runtime.timers.systemTimer) {
      clearInterval(runtime.timers.systemTimer);
      runtime.timers.systemTimer = undefined;
    }
    stopProcessTopStream(connectionId, runtime, "runtime_dispose");
    if (runtime.timers.networkTimer) {
      clearTimeout(runtime.timers.networkTimer);
      runtime.timers.networkTimer = undefined;
      runtime.timers.networkTimerActive = false;
      logger.info("[NetworkMonitor] stopped", { connectionId });
    }
  };

  const disposeMonitorRuntime = async (connectionId: string): Promise<void> => {
    const runtime = monitorRuntimes.get(connectionId);
    if (runtime) {
      runtime.disposed = true;
      clearMonitorRuntimeTimers(connectionId, runtime);
      monitorRuntimes.delete(connectionId);
    }

    monitorRuntimePromises.delete(connectionId);
    monitorStates.delete(connectionId);
    networkToolCache.delete(connectionId);

    if (runtime?.monitorShellChannel) {
      try {
        runtime.monitorShellChannel.end();
      } catch {
        // ignore close errors from stale channels
      }
    }

    const monitorConnection = runtime?.monitorConnection ?? monitorConnections.get(connectionId);
    monitorConnections.delete(connectionId);

    if (monitorConnection) {
      try {
        await monitorConnection.close();
      } catch (error) {
        logger.warn("[MonitorSession] failed to close monitor connection", {
          connectionId,
          reason: normalizeError(error)
        });
      }
    }
  };

  const establishMonitorConnection = async (
    connectionId: string,
    profile: ConnectionProfile
  ): Promise<SshConnection> => {
    logger.info("[MonitorSession] connecting hidden SSH", {
      connectionId,
      host: profile.host,
      port: profile.port
    });

    const ssh = await SshConnection.connect(await resolveConnectOptions(profile));
    ssh.onClose(() => {
      monitorConnections.delete(connectionId);
      monitorRuntimePromises.delete(connectionId);
      monitorStates.delete(connectionId);
      networkToolCache.delete(connectionId);

      const runtime = monitorRuntimes.get(connectionId);
      if (runtime) {
        runtime.disposed = true;
        clearMonitorRuntimeTimers(connectionId, runtime);
        monitorRuntimes.delete(connectionId);
      }

      logger.info("[MonitorSession] hidden SSH disconnected", { connectionId });
    });

    monitorConnections.set(connectionId, ssh);
    logger.info("[MonitorSession] hidden SSH connected", { connectionId });
    return ssh;
  };

  const ensureMonitorConnection = async (connectionId: string): Promise<SshConnection> => {
    const existing = monitorConnections.get(connectionId);
    if (existing) {
      return existing;
    }

    const pending = monitorConnectionPromises.get(connectionId);
    if (pending) {
      return pending;
    }

    const profile = assertMonitorEnabled(connectionId);
    const promise = establishMonitorConnection(connectionId, profile);
    monitorConnectionPromises.set(connectionId, promise);

    try {
      return await promise;
    } finally {
      monitorConnectionPromises.delete(connectionId);
    }
  };

  const initializeMonitorShell = async (
    connectionId: string,
    shellChannel: SshShellChannel
  ): Promise<void> => {
    await new Promise<void>((resolve, reject) => {
      let buffer = "";
      let settled = false;

      const cleanup = () => {
        shellChannel.off("data", onData);
        shellChannel.off("error", onError);
        clearTimeout(timeout);
      };

      const onError = (error: unknown) => {
        if (settled) {
          return;
        }
        settled = true;
        cleanup();
        reject(new Error(`初始化 Monitor Session shell 失败：${normalizeError(error)}`));
      };

      const onData = (chunk: Buffer | string) => {
        if (settled) {
          return;
        }
        buffer += chunk.toString();
        if (buffer.includes(MONITOR_SHELL_READY_MARKER)) {
          settled = true;
          cleanup();
          resolve();
        } else if (buffer.length > MONITOR_MAX_BUFFER_BYTES) {
          buffer = buffer.slice(-MONITOR_MAX_BUFFER_BYTES);
        }
      };

      const timeout = setTimeout(() => {
        if (settled) {
          return;
        }
        settled = true;
        cleanup();
        reject(new Error("初始化 Monitor Session shell 超时。"));
      }, MONITOR_SHELL_READY_TIMEOUT_MS);

      shellChannel.on("data", onData);
      shellChannel.on("error", onError);
      shellChannel.write(
        "unset HISTFILE 2>/dev/null || true; " +
        "set +o history 2>/dev/null || true; " +
        `stty -echo 2>/dev/null || true; export PS1=''; export PROMPT_COMMAND=''; echo ${MONITOR_SHELL_READY_MARKER}\n`
      );
    });

    logger.info("[MonitorSession] hidden shell initialized", { connectionId });
  };

  const ensureMonitorRuntime = async (connectionId: string): Promise<MonitorRuntime> => {
    const existing = monitorRuntimes.get(connectionId);
    if (existing && !existing.disposed) {
      return existing;
    }

    const pending = monitorRuntimePromises.get(connectionId);
    if (pending) {
      return pending;
    }

    const promise = (async () => {
      const monitorConnection = await ensureMonitorConnection(connectionId);
      const monitorShellChannel = await monitorConnection.openShell({
        cols: 120,
        rows: 40,
        term: "xterm-256color"
      });

      await initializeMonitorShell(connectionId, monitorShellChannel);

      const runtime: MonitorRuntime = {
        monitorConnection,
        monitorShellChannel,
        commandQueue: Promise.resolve(),
        timers: {},
        disposed: false
      };

      monitorShellChannel.on("close", () => {
        if (runtime.disposed) {
          return;
        }
        runtime.disposed = true;
        clearMonitorRuntimeTimers(connectionId, runtime);
        monitorRuntimes.delete(connectionId);
        monitorStates.delete(connectionId);
        networkToolCache.delete(connectionId);
        logger.warn("[MonitorSession] hidden shell closed unexpectedly", { connectionId });
      });

      monitorRuntimes.set(connectionId, runtime);

      if (!hasVisibleTerminalAlive(connectionId)) {
        await disposeMonitorRuntime(connectionId);
        throw new Error("可见 SSH 会话已关闭，Monitor Session 启动取消。");
      }

      logger.info("[MonitorSession] runtime ready", { connectionId });
      return runtime;
    })();

    monitorRuntimePromises.set(connectionId, promise);

    try {
      return await promise;
    } catch (error) {
      await disposeMonitorRuntime(connectionId);
      throw error;
    } finally {
      monitorRuntimePromises.delete(connectionId);
    }
  };

  const escapeForRegex = (value: string): string => {
    return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  };

  const execInMonitorShell = async (
    connectionId: string,
    command: string,
    timeoutMs = MONITOR_COMMAND_TIMEOUT_MS
  ): Promise<{ stdout: string; exitCode: number }> => {
    const runtime = await ensureMonitorRuntime(connectionId);
    if (runtime.disposed) {
      throw new Error("Monitor Session 后台 shell 不可用。");
    }

    const run = async (): Promise<{ stdout: string; exitCode: number }> => {
      if (runtime.disposed) {
        throw new Error("Monitor Session 后台 shell 已销毁。");
      }

      return new Promise<{ stdout: string; exitCode: number }>((resolve, reject) => {
        const marker = randomUUID().replace(/-/g, "");
        const beginMarker = `__NS_CMD_BEGIN_${marker}__`;
        const exitMarkerPrefix = `__NS_CMD_EXIT_${marker}__:`;
        const endMarker = `__NS_CMD_END_${marker}__`;
        const exitLineRegex = new RegExp(`${escapeForRegex(exitMarkerPrefix)}(-?\\d+)`);

        let buffer = "";
        let started = false;
        let settled = false;

        const cleanup = () => {
          clearTimeout(timeout);
          runtime.monitorShellChannel.off("data", onChunk);
          runtime.monitorShellChannel.stderr.off("data", onChunk);
        };

        const fail = (error: Error) => {
          if (settled) {
            return;
          }
          settled = true;
          cleanup();
          reject(error);
        };

        const tryResolve = () => {
          if (!started) {
            const beginIndex = buffer.indexOf(beginMarker);
            if (beginIndex === -1) {
              if (buffer.length > MONITOR_MAX_BUFFER_BYTES) {
                buffer = buffer.slice(-MONITOR_MAX_BUFFER_BYTES);
              }
              return;
            }
            started = true;
            buffer = buffer.slice(beginIndex + beginMarker.length);
          }

          const endIndex = buffer.indexOf(endMarker);
          if (endIndex === -1) {
            if (buffer.length > MONITOR_MAX_BUFFER_BYTES) {
              buffer = buffer.slice(-MONITOR_MAX_BUFFER_BYTES);
            }
            return;
          }

          const payload = buffer.slice(0, endIndex);
          const exitMatch = payload.match(exitLineRegex);
          const exitCode = exitMatch?.[1] ? Number.parseInt(exitMatch[1], 10) : 1;
          const output = payload
            .replace(new RegExp(`${escapeForRegex(exitMarkerPrefix)}-?\\d+\\r?\\n?`, "g"), "")
            .replace(/^\r?\n/, "")
            .replace(/\r?\n$/, "");

          if (settled) {
            return;
          }
          settled = true;
          cleanup();
          resolve({ stdout: output, exitCode });
        };

        const onChunk = (chunk: Buffer | string) => {
          if (settled) {
            return;
          }
          buffer += chunk.toString();
          tryResolve();
        };

        const timeout = setTimeout(() => {
          fail(new Error("Monitor Session 后台命令执行超时。"));
        }, timeoutMs);

        runtime.monitorShellChannel.on("data", onChunk);
        runtime.monitorShellChannel.stderr.on("data", onChunk);

        const wrappedCommand =
          `printf '%s\\n' '${beginMarker}'; ` +
          `( ${command} ); ` +
          "__NS_CMD_EXIT_CODE=$?; " +
          `printf '%s%s\\n' '${exitMarkerPrefix}' \"$__NS_CMD_EXIT_CODE\"; ` +
          `printf '%s\\n' '${endMarker}'`;

        runtime.monitorShellChannel.write(`${wrappedCommand}\n`);
      });
    };

    const queued = runtime.commandQueue.then(run, run);
    runtime.commandQueue = queued.then(
      () => undefined,
      () => undefined
    );

    try {
      return await queued;
    } catch (error) {
      const reason = normalizeError(error);
      logger.warn("[MonitorSession] hidden shell command failed", { connectionId, reason });
      if (reason.includes("超时") || reason.includes("closed")) {
        await disposeMonitorRuntime(connectionId);
      }
      throw error;
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

      connections.appendAuditLog({
        action: "sftp.init_ready",
        level: "info",
        connectionId,
        message: "SFTP warmup completed after SSH session open"
      });
      return undefined;
    } catch (error) {
      const reason = normalizeError(error);
      logger.warn("[SFTP] warmup failed", { connectionId, reason });
      connections.appendAuditLog({
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

  const getAppPreferences = (): AppPreferences => {
    return connections.getAppPreferences();
  };

  const updateAppPreferences = (patch: SettingsUpdateInput): AppPreferences => {
    const current = connections.getAppPreferences();
    const merged = mergePreferences(current, patch);
    return connections.saveAppPreferences(merged);
  };

  const openFilesDialog = async (
    sender: WebContents,
    input: DialogOpenFilesInput
  ): Promise<{ canceled: boolean; filePaths: string[] }> => {
    const owner = BrowserWindow.fromWebContents(sender);
    const dialogOptions: OpenDialogOptions = {
      title: input.title ?? "选择文件",
      defaultPath: input.defaultPath ? resolveLocalPath(input.defaultPath) : undefined,
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

    let credentialRef = current?.credentialRef;

    if (shouldDropPreviousCredential && current?.credentialRef) {
      await vault.deleteCredential(current.credentialRef);
      credentialRef = undefined;
    }

    if (input.authType === "password") {
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
      credentialRef: input.authType === "password" ? credentialRef : undefined,
      sshKeyId: input.authType === "privateKey" ? input.sshKeyId : undefined,
      hostFingerprint: input.hostFingerprint,
      strictHostKeyChecking: input.strictHostKeyChecking,
      proxyId: input.proxyId,
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
      await disposeMonitorRuntime(profile.id);
    }

    connections.appendAuditLog({
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
    return profile;
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
      password: authOverride.authType === "password" ? authOverride.password : undefined,
      sshKeyId: authOverride.authType === "privateKey" ? effectiveSshKeyId : undefined,
      hostFingerprint: latest.hostFingerprint,
      strictHostKeyChecking: latest.strictHostKeyChecking,
      proxyId: latest.proxyId,
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
      connections.appendAuditLog({
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
      (session) => session.connectionId === connectionId
    );

    if (stillUsed) {
      return;
    }

    await disposeMonitorRuntime(connectionId);

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
    return profile;
  };

  const removeSshKey = async (input: SshKeyRemoveInput): Promise<{ ok: true }> => {
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
    return profile;
  };

  const removeProxy = async (input: ProxyRemoveInput): Promise<{ ok: true }> => {
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

  const removeConnection = async (id: string): Promise<{ ok: true }> => {
    const sessions = Array.from(activeSessions.values()).filter(
      (session) => session.connectionId === id
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
    connections.appendAuditLog({
      action: "connection.remove",
      level: "warn",
      connectionId: id,
      message: "Connection profile deleted"
    });
    return { ok: true };
  };

  const openSession = async (
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
      const shell = await connection.openShell({
        cols: 140,
        rows: 40,
        term: "xterm-256color"
      });

      const now = new Date().toISOString();
      connections.save({
        ...profile,
        lastConnectedAt: now,
        updatedAt: now
      });

      descriptor.status = "connected";

      activeSessions.set(descriptor.id, {
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
        const encoding = active?.terminalEncoding ?? profile.terminalEncoding;
        sendSessionData(sender, {
          sessionId: descriptor.id,
          data: decodeTerminalData(chunk, encoding)
        });
      });

      shell.stderr.on("data", (chunk: Buffer | string) => {
        const active = activeSessions.get(descriptor.id);
        const encoding = active?.terminalEncoding ?? profile.terminalEncoding;
        sendSessionData(sender, {
          sessionId: descriptor.id,
          data: decodeTerminalData(chunk, encoding)
        });
      });

      shell.on("close", () => {
        const active = activeSessions.get(descriptor.id);
        if (active) {
          activeSessions.delete(descriptor.id);
          sendSessionStatus(active.sender, {
            sessionId: descriptor.id,
            status: "disconnected"
          });
          void closeConnectionIfIdle(connectionId);
        }
      });

      shell.on("error", (error: unknown) => {
        updateSessionStatus(descriptor.id, "failed", normalizeError(error));
      });

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
          await ensureMonitorRuntime(connectionId);
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

      connections.appendAuditLog({
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
      sendSessionStatus(sender, {
        sessionId: descriptor.id,
        status: "failed",
        reason
      });
      connections.appendAuditLog({
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

  const writeSession = (sessionId: string, data: string): { ok: true } => {
    const active = activeSessions.get(sessionId);
    if (!active) {
      throw new Error("Session not found");
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
      throw new Error("Session not found");
    }

    active.channel.setWindow(rows, cols, 0, 0);
    return { ok: true };
  };

  const closeSession = async (sessionId: string): Promise<{ ok: true }> => {
    const active = activeSessions.get(sessionId);
    if (!active) {
      return { ok: true };
    }

    logger.info("[Session] closing", { sessionId, connectionId: active.connectionId });
    active.channel.end();
    activeSessions.delete(sessionId);
    sendSessionStatus(active.sender, {
      sessionId,
      status: "disconnected"
    });

    connections.appendAuditLog({
      action: "session.close",
      level: "info",
      connectionId: active.connectionId,
      message: "SSH session closed",
      metadata: { sessionId }
    });

    await closeConnectionIfIdle(active.connectionId);
    return { ok: true };
  };

  const buildMonitorSnapshot = (
    connectionId: string,
    probe: MonitorProbeResult
  ): MonitorSnapshot => {
    const now = Date.now();
    const previous = monitorStates.get(connectionId);

    let cpuPercent = probe.cpuPercentHint ?? 0;
    if (
      previous?.cpuTotal !== undefined &&
      previous.cpuIdle !== undefined &&
      probe.cpuTotal !== undefined &&
      probe.cpuIdle !== undefined
    ) {
      const deltaTotal = probe.cpuTotal - previous.cpuTotal;
      const deltaIdle = probe.cpuIdle - previous.cpuIdle;
      if (deltaTotal > 0) {
        cpuPercent = ((deltaTotal - deltaIdle) / deltaTotal) * 100;
      }
    }

    const elapsedSeconds =
      previous?.sampledAt !== undefined
        ? (now - previous.sampledAt) / 1000
        : undefined;
    let networkInMbps = 0;
    let networkOutMbps = 0;
    if (
      elapsedSeconds &&
      elapsedSeconds > 0 &&
      previous?.netRxBytes !== undefined &&
      previous.netTxBytes !== undefined
    ) {
      networkInMbps = ((probe.netRxBytes - previous.netRxBytes) * 8) / (elapsedSeconds * 1000 * 1000);
      networkOutMbps = ((probe.netTxBytes - previous.netTxBytes) * 8) / (elapsedSeconds * 1000 * 1000);
    }

    monitorStates.set(connectionId, {
      cpuTotal: probe.cpuTotal ?? previous?.cpuTotal,
      cpuIdle: probe.cpuIdle ?? previous?.cpuIdle,
      netRxBytes: probe.netRxBytes,
      netTxBytes: probe.netTxBytes,
      selectedNetworkInterface: probe.networkInterface,
      networkInterfaceOptions: probe.networkInterfaceOptions,
      sampledAt: now
    });

    const memoryUsedKb = Math.max(0, probe.memTotalKb - probe.memAvailableKb);
    const swapUsedKb = Math.max(0, probe.swapTotalKb - probe.swapFreeKb);

    const memoryPercent = probe.memTotalKb > 0 ? (memoryUsedKb / probe.memTotalKb) * 100 : 0;
    const swapPercent = probe.swapTotalKb > 0 ? (swapUsedKb / probe.swapTotalKb) * 100 : 0;
    const diskPercent = probe.diskTotalKb > 0 ? (probe.diskUsedKb / probe.diskTotalKb) * 100 : 0;

    return {
      connectionId,
      uptimeHours: Math.max(0, Math.floor(probe.uptimeSeconds / 3600)),
      loadAverage: probe.loadAverage,
      cpuPercent: Number(cpuPercent.toFixed(2)),
      memoryPercent: Number(memoryPercent.toFixed(2)),
      memoryUsedMb: Number((memoryUsedKb / 1024).toFixed(2)),
      memoryTotalMb: Number((probe.memTotalKb / 1024).toFixed(2)),
      swapPercent: Number(swapPercent.toFixed(2)),
      swapUsedMb: Number((swapUsedKb / 1024).toFixed(2)),
      swapTotalMb: Number((probe.swapTotalKb / 1024).toFixed(2)),
      diskPercent: Number(diskPercent.toFixed(2)),
      diskUsedGb: Number((probe.diskUsedKb / (1024 * 1024)).toFixed(2)),
      diskTotalGb: Number((probe.diskTotalKb / (1024 * 1024)).toFixed(2)),
      networkInMbps: Number(Math.max(0, networkInMbps).toFixed(2)),
      networkOutMbps: Number(Math.max(0, networkOutMbps).toFixed(2)),
      networkInterface: probe.networkInterface,
      networkInterfaceOptions: probe.networkInterfaceOptions,
      processes: probe.processes,
      capturedAt: new Date(now).toISOString()
    };
  };

  const runMonitorCommand = async (
    connectionId: string,
    command: string
  ): Promise<string> => {
    try {
      const { stdout, exitCode } = await execInMonitorShell(connectionId, command);
      if (exitCode !== 0) {
        logger.debug("[SystemMonitor] command non-zero exit", {
          connectionId,
          command,
          exitCode,
          output: stdout.slice(0, 200)
        });
      }
      return stdout;
    } catch (error) {
      logger.debug("[SystemMonitor] command execution failed", {
        connectionId,
        command,
        reason: normalizeError(error)
      });
      return "";
    }
  };

  const buildNetCountersCommand = (networkInterface: string): string => {
    const normalized = normalizeNetworkInterfaceName(networkInterface);
    if (!normalized) {
      throw new Error("无效网卡名称");
    }

    return (
      `cat /sys/class/net/${normalized}/statistics/rx_bytes 2>/dev/null; ` +
      `cat /sys/class/net/${normalized}/statistics/tx_bytes 2>/dev/null`
    );
  };

  const resolveSystemNetworkInterface = async (
    connectionId: string,
    options: string[]
  ): Promise<string> => {
    const state = monitorStates.get(connectionId);
    const selected = state?.selectedNetworkInterface;
    if (selected && options.includes(selected)) {
      return selected;
    }

    const defaultIfaceRaw = await runMonitorCommand(connectionId, MONITOR_NET_DEFAULT_INTERFACE_COMMAND);
    const defaultIface = normalizeNetworkInterfaceName(defaultIfaceRaw.split(/\r?\n/)[0] ?? "");
    if (defaultIface && options.includes(defaultIface)) {
      return defaultIface;
    }

    return options[0] ?? "eth0";
  };

  const probeMonitorSnapshot = async (connectionId: string): Promise<MonitorSnapshot> => {
    const uptime = await runMonitorCommand(connectionId, MONITOR_UPTIME_COMMAND);
    const loadAverage = await runMonitorCommand(connectionId, MONITOR_LOADAVG_COMMAND);
    const cpuStat = await runMonitorCommand(connectionId, MONITOR_CPU_STAT_COMMAND);
    const cpuTop = await runMonitorCommand(connectionId, MONITOR_CPU_TOP_COMMAND);
    const meminfo = await runMonitorCommand(connectionId, MONITOR_MEMINFO_COMMAND);
    const free = await runMonitorCommand(connectionId, MONITOR_FREE_COMMAND);
    const disk = await runMonitorCommand(connectionId, MONITOR_DISK_COMMAND);
    const netIfacesRaw = await runMonitorCommand(connectionId, MONITOR_NET_INTERFACES_COMMAND);
    const networkInterfaceOptions = parseNetworkInterfaceList(netIfacesRaw);
    const networkInterface = await resolveSystemNetworkInterface(connectionId, networkInterfaceOptions);
    const networkCounters = await runMonitorCommand(connectionId, buildNetCountersCommand(networkInterface));
    const process = await runMonitorCommand(connectionId, MONITOR_SYSTEM_PROCESS_COMMAND);

    const probe = parseMonitorProbe({
      uptime,
      loadAverage,
      cpuStat,
      cpuTop,
      meminfo,
      free,
      disk,
      networkInterface,
      networkInterfaceOptions,
      networkCounters,
      process
    });

    return buildMonitorSnapshot(connectionId, probe);
  };

  const startSystemMonitor = async (
    connectionId: string,
    sender: WebContents
  ): Promise<{ ok: true }> => {
    assertMonitorEnabled(connectionId);
    assertVisibleTerminalAlive(connectionId);
    const runtime = await ensureMonitorRuntime(connectionId);
    if (runtime.timers.systemTimer) {
      return { ok: true };
    }

    const poll = async () => {
      try {
        const snapshot = await probeMonitorSnapshot(connectionId);
        if (!sender.isDestroyed()) {
          sender.send(IPCChannel.MonitorSystemData, snapshot);
        }
      } catch (error) {
        logger.warn("[SystemMonitor] poll failed", {
          connectionId,
          reason: normalizeError(error)
        });
      }
    };

    void poll();
    runtime.timers.systemTimer = setInterval(() => void poll(), MONITOR_SYSTEM_INTERVAL_MS);
    logger.info("[SystemMonitor] started", { connectionId });
    return { ok: true };
  };

  const stopSystemMonitor = (connectionId: string): { ok: true } => {
    const runtime = monitorRuntimes.get(connectionId);
    if (runtime?.timers.systemTimer) {
      clearInterval(runtime.timers.systemTimer);
      runtime.timers.systemTimer = undefined;
      logger.info("[SystemMonitor] stopped", { connectionId });
    }
    return { ok: true };
  };

  const selectSystemNetworkInterface = async (
    connectionId: string,
    networkInterface: string
  ): Promise<{ ok: true }> => {
    assertMonitorEnabled(connectionId);
    assertVisibleTerminalAlive(connectionId);
    await ensureMonitorRuntime(connectionId);

    const normalized = normalizeNetworkInterfaceName(networkInterface);
    if (!normalized) {
      throw new Error("无效网卡名称");
    }

    const optionsRaw = await runMonitorCommand(connectionId, MONITOR_NET_INTERFACES_COMMAND);
    const options = parseNetworkInterfaceList(optionsRaw);
    if (!options.includes(normalized)) {
      throw new Error(`网卡不存在或不可用: ${normalized}`);
    }

    const previous = monitorStates.get(connectionId);
    monitorStates.set(connectionId, {
      ...previous,
      selectedNetworkInterface: normalized,
      networkInterfaceOptions: options,
      // 切换网卡后重置速率基线，避免出现跨网卡跳变
      netRxBytes: undefined,
      netTxBytes: undefined,
      sampledAt: undefined
    });

    return { ok: true };
  };

  const getMonitorSnapshot = async (connectionId: string): Promise<MonitorSnapshot> => {
    assertMonitorEnabled(connectionId);
    assertVisibleTerminalAlive(connectionId);
    await ensureMonitorRuntime(connectionId);
    return probeMonitorSnapshot(connectionId);
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

    connections.appendAuditLog({
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

    connections.appendAuditLog({
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
      connections.appendAuditLog({
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
      connections.appendAuditLog({
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

  const createRemoteDirectory = async (
    connectionId: string,
    pathName: string
  ): Promise<{ ok: true }> => {
    getConnectionOrThrow(connectionId);
    const connection = await ensureConnection(connectionId);
    await connection.mkdir(pathName, true);
    connections.appendAuditLog({
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
    connections.appendAuditLog({
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
    connections.appendAuditLog({
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
    const result = await remoteEditManager.open(connectionId, remotePath, editorCommand, sender);
    connections.appendAuditLog({
      action: "sftp.edit_open",
      level: "info",
      connectionId,
      message: "Opened remote file for live editing",
      metadata: { remotePath, editId: result.editId }
    });
    return result;
  };

  const stopRemoteEdit = async (editId: string): Promise<{ ok: true }> => {
    await remoteEditManager.stop(editId);
    connections.appendAuditLog({
      action: "sftp.edit_stop",
      level: "info",
      message: "Stopped remote file live editing",
      metadata: { editId }
    });
    return { ok: true };
  };

  const stopAllRemoteEdits = async (): Promise<{ ok: true }> => {
    await remoteEditManager.stopAll();
    connections.appendAuditLog({
      action: "sftp.edit_stop_all",
      level: "info",
      message: "Stopped all remote file live editing sessions"
    });
    return { ok: true };
  };

  const listRemoteEdits = (): SftpEditSessionInfo[] => {
    return remoteEditManager.listSessions();
  };

  // ─── Process Monitor ──────────────────────────────────────────────────────

  const assertLinuxHost = async (connectionId: string): Promise<void> => {
    const { stdout, exitCode } = await execInMonitorShell(connectionId, MONITOR_LINUX_CHECK_COMMAND);
    const platform = stdout.trim().split(/\s+/)[0] ?? "";
    if (exitCode !== 0 || platform !== "Linux") {
      throw new Error("当前模式仅支持 Linux");
    }
  };

  const assertTopAvailable = async (connectionId: string): Promise<void> => {
    const { exitCode } = await execInMonitorShell(connectionId, MONITOR_TOP_CHECK_COMMAND);
    if (exitCode !== 0) {
      throw new Error("当前主机缺少 top 命令，无法启动任务管理器。");
    }
  };

  const parseTopResidentMb = (raw: string): number => {
    const normalized = raw.trim().toLowerCase().replace(/,/g, "");
    if (!normalized) {
      return 0;
    }

    const match = normalized.match(/^([0-9]*\.?[0-9]+)([kmgt]?)$/);
    if (!match?.[1]) {
      return 0;
    }

    const value = Number.parseFloat(match[1]);
    if (!Number.isFinite(value)) {
      return 0;
    }

    const unit = match[2] ?? "";
    const multiplierMb =
      unit === "t"
        ? 1024 * 1024
        : unit === "g"
          ? 1024
          : unit === "m"
            ? 1
            : unit === "k" || unit === ""
              ? 1 / 1024
              : 0;

    return Number((value * multiplierMb).toFixed(1));
  };

  const extractTopFrames = (buffer: string): { frames: string[]; remaining: string } => {
    const normalized = buffer.replace(/\r/g, "");
    const starts: number[] = [];
    const startRegex = /^top - /gm;
    let match: RegExpExecArray | null = startRegex.exec(normalized);

    while (match) {
      starts.push(match.index);
      match = startRegex.exec(normalized);
    }

    if (starts.length === 0) {
      return {
        frames: [],
        remaining: normalized.slice(-MONITOR_MAX_BUFFER_BYTES)
      };
    }

    if (starts.length === 1) {
      const remaining = normalized.slice(starts[0]);
      return {
        frames: [],
        remaining: remaining.slice(-MONITOR_MAX_BUFFER_BYTES)
      };
    }

    const frames: string[] = [];
    for (let index = 0; index < starts.length - 1; index += 1) {
      const begin = starts[index];
      const end = starts[index + 1];
      if (begin === undefined || end === undefined) {
        continue;
      }
      const frame = normalized.slice(begin, end).trim();
      if (frame) {
        frames.push(frame);
      }
    }

    const lastStart = starts[starts.length - 1];
    const remaining = lastStart === undefined
      ? normalized.slice(-MONITOR_MAX_BUFFER_BYTES)
      : normalized.slice(lastStart).slice(-MONITOR_MAX_BUFFER_BYTES);

    return { frames, remaining };
  };

  const parseTopFrame = (connectionId: string, frame: string): ProcessSnapshot => {
    const lines = frame.split("\n");
    const headerIndex = lines.findIndex((line) => /^PID\s+USER\b/.test(line.trim()));
    if (headerIndex < 0) {
      throw new Error("top 帧缺少进程表头");
    }

    const processes: MonitorProcess[] = [];

    for (let index = headerIndex + 1; index < lines.length; index += 1) {
      const line = (lines[index] ?? "").trim();
      if (!line || /^top - /.test(line) || !/^\d+\s+/.test(line)) {
        continue;
      }

      const columns = line.split(/\s+/);
      if (columns.length < 12) {
        continue;
      }

      const pid = parseIntSafe(columns[0]);
      if (pid <= 0) {
        continue;
      }

      const user = columns[1] ?? "unknown";
      const residentRaw = columns[5] ?? "0";
      const cpuRaw = columns[8] ?? "0";
      const command = columns.slice(11).join(" ") || columns[11] || "unknown";

      processes.push({
        pid,
        user,
        command,
        commandLine: command,
        cpuPercent: parseFloatSafe(cpuRaw),
        memoryMb: parseTopResidentMb(residentRaw)
      });

      if (processes.length >= MONITOR_PROCESS_TOP_MAX_ROWS) {
        break;
      }
    }

    return {
      connectionId,
      processes,
      capturedAt: new Date().toISOString()
    };
  };

  const firstNonEmptyLine = (value: string): string | undefined => {
    return value
      .split(/\r?\n/)
      .map((line) => line.trim())
      .find((line) => line.length > 0);
  };

  const parseProcessDetailPrimary = (
    connectionId: string,
    stdout: string
  ): Omit<ProcessDetailSnapshot, "commandLine" | "capturedAt"> | undefined => {
    const line = firstNonEmptyLine(stdout);
    if (!line) {
      return undefined;
    }

    const parts = line.replace(/\s+/g, " ").trim().split(" ");
    if (parts.length < 9) {
      return undefined;
    }

    const pid = parseIntSafe(parts[0]);
    const ppid = parseIntSafe(parts[1]);
    if (pid <= 0) {
      return undefined;
    }

    const user = parts[2] ?? "unknown";
    const state = parts[3] ?? "-";
    const cpuPercent = parseFloatSafe(parts[4]);
    const memoryPercent = parseFloatSafe(parts[5]);
    const rssKb = parseFloatSafe(parts[6]);
    const elapsed = parts[7] ?? "-";
    const command = parts.slice(8).join(" ") || "unknown";

    return {
      connectionId,
      pid,
      ppid,
      user,
      state,
      cpuPercent: Number(cpuPercent.toFixed(2)),
      memoryPercent: Number(memoryPercent.toFixed(2)),
      rssMb: Number((rssKb / 1024).toFixed(2)),
      elapsed,
      command
    };
  };

  const startProcessMonitor = async (
    connectionId: string,
    sender: WebContents
  ): Promise<{ ok: true }> => {
    assertMonitorEnabled(connectionId);
    assertVisibleTerminalAlive(connectionId);
    const runtime = await ensureMonitorRuntime(connectionId);
    if (runtime.processTopStream && !runtime.processTopStream.disposed) {
      runtime.processTopStream.sender = sender;
      return { ok: true };
    }

    await assertLinuxHost(connectionId);
    await assertTopAvailable(connectionId);

    const channel = await runtime.monitorConnection.openShell({
      cols: 200,
      rows: 60,
      term: "dumb"
    });

    const stream: ProcessTopStreamRuntime = {
      channel,
      sender,
      buffer: "",
      disposed: false,
      dataListener: () => {},
      errorListener: () => {},
      closeListener: () => {}
    };

    stream.dataListener = (chunk: Buffer | string) => {
      if (stream.disposed) {
        return;
      }

      stream.buffer += chunk.toString();
      const { frames, remaining } = extractTopFrames(stream.buffer);
      stream.buffer = remaining;

      for (const frame of frames) {
        try {
          const snapshot = parseTopFrame(connectionId, frame);
          if (!stream.sender.isDestroyed()) {
            stream.sender.send(IPCChannel.MonitorProcessData, snapshot);
          } else {
            stopProcessTopStream(connectionId, runtime, "sender_destroyed");
            return;
          }
        } catch (error) {
          logger.debug("[ProcessMonitor] top frame parse failed", {
            connectionId,
            reason: normalizeError(error)
          });
        }
      }
    };

    stream.errorListener = (error: unknown) => {
      if (stream.disposed) {
        return;
      }

      logger.warn("[ProcessMonitor] top stream error", {
        connectionId,
        reason: normalizeError(error)
      });
      stopProcessTopStream(connectionId, runtime, "channel_error");
    };

    stream.closeListener = () => {
      if (stream.disposed) {
        return;
      }

      logger.warn("[ProcessMonitor] top stream closed unexpectedly", { connectionId });
      stopProcessTopStream(connectionId, runtime, "channel_closed");
    };

    channel.on("data", stream.dataListener);
    channel.on("error", stream.errorListener);
    channel.on("close", stream.closeListener);

    runtime.processTopStream = stream;
    channel.write(
      "unset HISTFILE 2>/dev/null || true; " +
      "set +o history 2>/dev/null || true; " +
      `stty -echo 2>/dev/null || true; export PS1=''; export PROMPT_COMMAND=''; ${MONITOR_PROCESS_TOP_STREAM_COMMAND}\n`
    );

    logger.info("[ProcessMonitor] started", { connectionId });
    return { ok: true };
  };

  const stopProcessMonitor = (connectionId: string): { ok: true } => {
    const runtime = monitorRuntimes.get(connectionId);
    if (runtime) {
      stopProcessTopStream(connectionId, runtime, "manual");
    }
    return { ok: true };
  };

  const getProcessDetail = async (
    connectionId: string,
    pid: number
  ): Promise<ProcessDetailSnapshot> => {
    assertMonitorEnabled(connectionId);
    assertVisibleTerminalAlive(connectionId);
    await ensureMonitorRuntime(connectionId);
    await assertLinuxHost(connectionId);
    await assertTopAvailable(connectionId);

    const normalizedPid = Math.trunc(pid);
    if (normalizedPid < 1) {
      throw new Error("无效进程 PID");
    }

    const primaryCommand =
      `ps -p ${normalizedPid} -o pid=,ppid=,user=,state=,%cpu=,%mem=,rss=,etime=,comm=`;
    const argsCommand = `ps -p ${normalizedPid} -o args=`;

    const primary = await execInMonitorShell(connectionId, primaryCommand);
    if (primary.exitCode !== 0) {
      throw new Error("进程不存在或已结束");
    }

    const parsed = parseProcessDetailPrimary(connectionId, primary.stdout);
    if (!parsed) {
      throw new Error("进程不存在或已结束");
    }

    const args = await execInMonitorShell(connectionId, argsCommand);
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
    await ensureMonitorRuntime(connectionId);
    if (signal !== "SIGTERM" && signal !== "SIGKILL") {
      throw new Error("Invalid signal");
    }
    const { stdout, exitCode } = await execInMonitorShell(connectionId, `kill -${signal} ${pid} 2>&1`);
    if (exitCode !== 0) {
      throw new Error(`kill 失败 (exit ${exitCode}): ${stdout.trim() || "unknown error"}`);
    }
    connections.appendAuditLog({
      action: "monitor.process_kill",
      level: "warn",
      connectionId,
      message: `Sent ${signal} to PID ${pid}`,
      metadata: { pid, signal }
    });
    return { ok: true };
  };

  // ─── Network Monitor ─────────────────────────────────────────────────────

  const detectNetworkTool = async (connectionId: string): Promise<"ss" | "netstat"> => {
    const cached = networkToolCache.get(connectionId);
    if (cached) {
      return cached;
    }

    const ssProbe = await execInMonitorShell(connectionId, "command -v ss >/dev/null 2>&1");
    if (ssProbe.exitCode === 0) {
      networkToolCache.set(connectionId, "ss");
      return "ss";
    }

    const netstatProbe = await execInMonitorShell(connectionId, "command -v netstat >/dev/null 2>&1");
    if (netstatProbe.exitCode === 0) {
      networkToolCache.set(connectionId, "netstat");
      return "netstat";
    }

    throw new Error("未找到 ss 或 netstat 命令，无法启动网络监控。");
  };

  const parseSsOutput = (stdout: string): { listeners: NetworkListener[]; connections: NetworkConnection[] } => {
    const lines = stdout.split(/\r?\n/).map((l) => l.trim()).filter(Boolean);
    const listeners: NetworkListener[] = [];
    const connections: NetworkConnection[] = [];
    const listenerMap = new Map<string, NetworkListener>();
    const connectionStates = new Set([
      "ESTAB",
      "ESTABLISHED",
      "TIME-WAIT",
      "CLOSE-WAIT",
      "SYN-SENT",
      "SYN-RECV",
      "FIN-WAIT-1",
      "FIN-WAIT-2",
      "LAST-ACK"
    ]);

    const parseAddress = (address: string): { ip: string; port: number } => {
      const cut = address.lastIndexOf(":");
      if (cut <= 0) {
        return { ip: "*", port: 0 };
      }
      const host = address.slice(0, cut).replace(/^\[/, "").replace(/\]$/, "");
      const port = parseInt(address.slice(cut + 1) || "0", 10);
      return {
        ip: host || "*",
        port
      };
    };

    const knownState = (value: string): boolean => {
      return value === "LISTEN" || value === "UNCONN" || connectionStates.has(value);
    };

    for (let i = 0; i < lines.length; i++) {
      const line = lines[i] ?? "";
      const parts = line.replace(/\s+/g, " ").split(" ");
      if (parts.length < 5 || (parts[0] ?? "").toLowerCase() === "netid") {
        continue;
      }

      let state = "";
      let localAddr = "";
      let peerAddr = "";
      let extra = "";

      // ss default layout: Netid State Recv-Q Send-Q Local Address:Port Peer Address:Port Process
      if (parts.length >= 6 && knownState(parts[1] ?? "")) {
        state = parts[1] ?? "";
        localAddr = parts[4] ?? "";
        peerAddr = parts[5] ?? "";
        extra = parts.slice(6).join(" ");
      } else if (knownState(parts[0] ?? "")) {
        // fallback layout without Netid
        state = parts[0] ?? "";
        localAddr = parts[3] ?? "";
        peerAddr = parts[4] ?? "";
        extra = parts.slice(5).join(" ");
      } else {
        continue;
      }

      const pidMatch = extra.match(/pid=(\d+)/);
      const nameMatch = extra.match(/\("([^"]+)"/);
      const pid = pidMatch ? parseInt(pidMatch[1] ?? "0", 10) : 0;
      const processName = nameMatch ? (nameMatch[1] ?? "unknown") : "unknown";

      const local = parseAddress(localAddr);
      const peer = parseAddress(peerAddr);
      const localIp = local.ip;
      const localPort = local.port;

      if (localPort <= 0) {
        continue;
      }

      if (state === "LISTEN" || state === "UNCONN") {
        const key = `${pid}:${localPort}`;
        const existing = listenerMap.get(key);
        if (existing) {
          existing.connectionCount += 1;
        } else {
          const listener: NetworkListener = {
            pid,
            name: processName,
            listenIp: localIp,
            port: localPort,
            ipCount: 0,
            connectionCount: 0,
            uploadBytes: 0,
            downloadBytes: 0
          };
          listenerMap.set(key, listener);
        }
      } else if (connectionStates.has(state)) {
        const remoteIp = peer.ip !== "*" ? peer.ip : "0.0.0.0";
        const remotePort = peer.port;
        connections.push({
          localPort,
          remoteIp,
          remotePort,
          state,
          pid,
          processName
        });

        // update listener counts
        for (const listener of listenerMap.values()) {
          if (listener.port === localPort) {
            listener.connectionCount += 1;
          }
        }
      }
    }

    // compute ipCount per listener
    for (const listener of listenerMap.values()) {
      const uniqueIps = new Set(
        connections
          .filter((c) => c.localPort === listener.port)
          .map((c) => c.remoteIp)
      );
      listener.ipCount = uniqueIps.size;
    }

    listeners.push(...listenerMap.values());
    return { listeners, connections };
  };

  const parseNetstatOutput = (stdout: string): { listeners: NetworkListener[]; connections: NetworkConnection[] } => {
    const lines = stdout.split(/\r?\n/).map((l) => l.trim()).filter(Boolean);
    const listeners: NetworkListener[] = [];
    const connections: NetworkConnection[] = [];
    const listenerMap = new Map<string, NetworkListener>();

    for (let i = 2; i < lines.length; i++) {
      const line = lines[i] ?? "";
      const parts = line.replace(/\s+/g, " ").split(" ");
      // netstat -tunap: Proto RecvQ SendQ LocalAddr ForeignAddr State PID/Program
      const localAddr = parts[3] ?? "";
      const peerAddr = parts[4] ?? "";
      const state = parts[5] ?? "";
      const pidProg = parts[6] ?? "";

      const pidMatch = pidProg.match(/^(\d+)\//);
      const pid = pidMatch ? parseInt(pidMatch[1] ?? "0", 10) : 0;
      const processName = pidProg.includes("/") ? pidProg.split("/").slice(1).join("/") : "unknown";

      const lastColon = localAddr.lastIndexOf(":");
      const localIp = lastColon > 0 ? localAddr.slice(0, lastColon) : "*";
      const localPort = parseInt(localAddr.slice(lastColon + 1) || "0", 10);

      if (state === "LISTEN") {
        const key = `${pid}:${localPort}`;
        const existing = listenerMap.get(key);
        if (!existing) {
          listenerMap.set(key, {
            pid,
            name: processName,
            listenIp: localIp,
            port: localPort,
            ipCount: 0,
            connectionCount: 0,
            uploadBytes: 0,
            downloadBytes: 0
          });
        }
      } else if (state === "ESTABLISHED" || state === "TIME_WAIT" || state === "CLOSE_WAIT" || state === "SYN_SENT") {
        const peerColon = peerAddr.lastIndexOf(":");
        const remoteIp = peerColon > 0 ? peerAddr.slice(0, peerColon) : "0.0.0.0";
        const remotePort = parseInt(peerAddr.slice(peerColon + 1) || "0", 10);
        connections.push({
          localPort,
          remoteIp,
          remotePort,
          state,
          pid,
          processName
        });
      }
    }

    for (const listener of listenerMap.values()) {
      const conns = connections.filter((c) => c.localPort === listener.port);
      listener.connectionCount = conns.length;
      listener.ipCount = new Set(conns.map((c) => c.remoteIp)).size;
    }

    listeners.push(...listenerMap.values());
    return { listeners, connections };
  };

  const startNetworkMonitor = async (
    connectionId: string,
    sender: WebContents
  ): Promise<{ ok: true }> => {
    assertMonitorEnabled(connectionId);
    assertVisibleTerminalAlive(connectionId);
    const runtime = await ensureMonitorRuntime(connectionId);
    if (runtime.timers.networkTimerActive) {
      return { ok: true };
    }

    const tool = await detectNetworkTool(connectionId);
    runtime.timers.networkTimerActive = true;

    const scheduleNextPoll = () => {
      if (!runtime.timers.networkTimerActive) {
        return;
      }
      runtime.timers.networkTimer = setTimeout(() => void poll(), MONITOR_NETWORK_INTERVAL_MS);
    };

    const poll = async () => {
      if (!runtime.timers.networkTimerActive) {
        return;
      }
      try {
        const command = tool === "ss" ? NETWORK_PROBE_SS : NETWORK_PROBE_NETSTAT;
        const { stdout, exitCode } = await execInMonitorShell(connectionId, command);
        if (exitCode !== 0) {
          throw new Error(`网络监控采样失败 (exit ${exitCode})`);
        }
        const parsed = tool === "ss" ? parseSsOutput(stdout) : parseNetstatOutput(stdout);
        const snapshot: NetworkSnapshot = {
          connectionId,
          listeners: parsed.listeners,
          connections: [],
          capturedAt: new Date().toISOString()
        };
        if (!sender.isDestroyed()) {
          sender.send(IPCChannel.MonitorNetworkData, snapshot);
        }
      } catch (error) {
        logger.warn("[NetworkMonitor] poll failed", {
          connectionId,
          reason: normalizeError(error)
        });
      } finally {
        scheduleNextPoll();
      }
    };

    void poll();

    logger.info("[NetworkMonitor] started", { connectionId });
    return { ok: true };
  };

  const stopNetworkMonitor = (connectionId: string): { ok: true } => {
    const runtime = monitorRuntimes.get(connectionId);
    if (runtime?.timers.networkTimerActive) {
      runtime.timers.networkTimerActive = false;
      if (runtime.timers.networkTimer) {
        clearTimeout(runtime.timers.networkTimer);
        runtime.timers.networkTimer = undefined;
      }
      logger.info("[NetworkMonitor] stopped", { connectionId });
    }
    return { ok: true };
  };

  const getNetworkConnections = async (
    connectionId: string,
    port: number
  ): Promise<NetworkConnection[]> => {
    assertMonitorEnabled(connectionId);
    assertVisibleTerminalAlive(connectionId);
    await ensureMonitorRuntime(connectionId);
    const tool = await detectNetworkTool(connectionId);
    const command = tool === "ss"
      ? `export LANG=en_US.UTF-8; ss -tnap '( sport = :${port} )' 2>/dev/null`
      : `export LANG=en_US.UTF-8; netstat -tnp 2>/dev/null | awk 'NR>2 {split($4,a,\":\"); p=a[length(a)]; if (p==\"${port}\") print $0}'`;
    const { stdout, exitCode } = await execInMonitorShell(connectionId, command);
    if (exitCode !== 0) {
      throw new Error(`网络连接查询失败 (exit ${exitCode})`);
    }
    const parsed = tool === "ss" ? parseSsOutput(stdout) : parseNetstatOutput(stdout);
    return parsed.connections;
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
    phase: "set" | "unlock"
  ): Promise<void> => {
    const prefs = connections.getAppPreferences();
    if (!prefs.backup.rememberPassword) {
      return;
    }

    try {
      await keytarCache.remember(password);
    } catch (error) {
      const reason = normalizeError(error);
      logger.warn("[Security] failed to cache backup password in keytar", { phase, reason });
      connections.appendAuditLog({
        action: "backup.password_cache_failed",
        level: "warn",
        message: "Failed to cache backup password in keytar",
        metadata: { phase, reason }
      });
    }
  };

  const backupSetPassword = async (password: string): Promise<{ ok: true }> => {
    const meta = createMasterKeyMeta(password);
    connections.saveMasterKeyMeta(meta);
    backupPassword = password;
    await rememberPasswordBestEffort(password, "set");
    connections.appendAuditLog({
      action: "backup.password_set",
      level: "info",
      message: "Cloud backup password configured"
    });
    return { ok: true };
  };

  const backupUnlockPassword = async (password: string): Promise<{ ok: true }> => {
    const meta = connections.getMasterKeyMeta();
    if (!meta) {
      throw new Error("尚未设置备份密码。请先设置密码。");
    }
    if (!verifyMasterPassword(password, meta)) {
      throw new Error("备份密码错误。");
    }
    backupPassword = password;
    await rememberPasswordBestEffort(password, "unlock");
    return { ok: true };
  };

  const backupClearRemembered = async (): Promise<{ ok: true }> => {
    await keytarCache.clear();
    return { ok: true };
  };

  const backupPasswordStatus = async (): Promise<{ isSet: boolean; isUnlocked: boolean; keytarAvailable: boolean }> => {
    const meta = connections.getMasterKeyMeta();
    return {
      isSet: meta !== undefined,
      isUnlocked: backupPassword !== undefined,
      keytarAvailable: keytarCache.isAvailable()
    };
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

    const runtimeConnectionIds = new Set([
      ...monitorRuntimes.keys(),
      ...monitorConnections.keys()
    ]);

    for (const connectionId of runtimeConnectionIds) {
      await disposeMonitorRuntime(connectionId);
    }

    await remoteEditManager.dispose();

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
    listSshKeys,
    upsertSshKey,
    removeSshKey,
    listProxies,
    upsertProxy,
    removeProxy,
    getAppPreferences,
    updateAppPreferences,
    openFilesDialog,
    openDirectoryDialog,
    openLocalPath,
    openSession,
    writeSession,
    resizeSession,
    closeSession,
    getMonitorSnapshot,
    startSystemMonitor,
    stopSystemMonitor,
    selectSystemNetworkInterface,
    execCommand,
    execBatchCommand,
    listAuditLogs,
    listMigrations,
    listRemoteFiles,
    uploadRemoteFile,
    downloadRemoteFile,
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
    backupSetPassword,
    backupUnlockPassword,
    backupClearRemembered,
    backupPasswordStatus,
    listTemplateParams,
    upsertTemplateParams,
    clearTemplateParams,
    dispose
  };
};
