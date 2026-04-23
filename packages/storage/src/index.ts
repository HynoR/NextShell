import path from "node:path";
import { randomUUID } from "node:crypto";
import { createRequire } from "node:module";
import type Database from "better-sqlite3";

export { CachedConnectionRepository, CachedSshKeyRepository, CachedProxyRepository } from "./cached-repository";
import type {
  AppPreferences,
  CloudSyncResourceStateV2,
  CloudSyncPendingOp,
  CloudSyncWorkspaceProfile,
  RecycleBinEntry,
  AuditLogRecord,
  CommandHistoryEntry,
  CommandTemplateParam,
  ConnectionListQuery,
  ConnectionProfile,
  MasterKeyMeta,
  MigrationRecord,
  OriginKind,
  ProxyProfile,
  SavedCommand,
  SshKeyProfile,
  WorkspaceCommandItem,
  WorkspaceRepoCommitMeta,
  WorkspaceRepoConflict,
  WorkspaceRepoLocalState,
  WorkspaceRepoSnapshot
} from "../../core/src/index";
import {
  DEFAULT_APP_PREFERENCES as DEFAULT_APP_PREFERENCES_VALUE,
  LOCAL_DEFAULT_SCOPE_KEY,
  buildResourceId,
  normalizeBatchMaxConcurrency,
  normalizeBatchRetryCount
} from "../../core/src/index";
import type { SecretStoreDB } from "../../security/src/index";

const require = createRequire(import.meta.url);

interface BetterSqlite3OpenOptions {
  readonly?: boolean;
  fileMustExist?: boolean;
}

interface BetterSqlite3Module {
  new (filename: string, options?: BetterSqlite3OpenOptions): Database.Database;
}

interface ConnectionRow {
  id: string;
  name: string;
  host: string;
  port: number;
  username: string;
  auth_type: ConnectionProfile["authType"];
  credential_ref: string | null;
  ssh_key_id: string | null;
  host_fingerprint: string | null;
  strict_host_key_checking: number;
  proxy_id: string | null;
  keepalive_enabled: number | null;
  keepalive_interval_sec: number | null;
  terminal_encoding: "utf-8" | "gb18030" | "gbk" | "big5" | null;
  backspace_mode: "ascii-backspace" | "ascii-delete" | null;
  delete_mode: "vt220-delete" | "ascii-delete" | "ascii-backspace" | null;
  group_path: string;
  tags: string;
  notes: string | null;
  favorite: number;
  monitor_session: number;
  created_at: string;
  updated_at: string;
  last_connected_at: string | null;
  // v2 origin fields (nullable for old data)
  resource_id: string | null;
  uuid_in_scope: string | null;
  origin_kind: string | null;
  origin_scope_key: string | null;
  origin_workspace_id: string | null;
  ssh_key_resource_id: string | null;
  copied_from_resource_id: string | null;
}

interface SshKeyRow {
  id: string;
  name: string;
  key_content_ref: string;
  passphrase_ref: string | null;
  created_at: string;
  updated_at: string;
  // v2 origin fields (nullable for old data)
  resource_id: string | null;
  uuid_in_scope: string | null;
  origin_kind: string | null;
  origin_scope_key: string | null;
  origin_workspace_id: string | null;
  copied_from_resource_id: string | null;
}

interface CloudSyncWorkspaceRow {
  id: string;
  api_base_url: string;
  workspace_name: string;
  display_name: string;
  pull_interval_sec: number;
  ignore_tls_errors: number;
  enabled: number;
  created_at: string;
  updated_at: string;
  last_sync_at: string | null;
  last_error: string | null;
}

interface RecycleBinRow {
  id: string;
  resource_type: "server" | "sshKey";
  display_name: string;
  original_resource_id: string;
  original_scope_key: string;
  reason: string;
  snapshot_json: string;
  created_at: string;
}

interface PendingOpRow {
  id: number;
  workspace_id: string;
  resource_type: "server" | "sshKey";
  resource_id: string;
  action: "upsert" | "delete";
  base_revision: number | null;
  force: number;
  payload_json: string | null;
  queued_at: string;
  last_attempt_at: string | null;
  last_error: string | null;
}

interface CloudSyncResourceStateV2Row {
  workspace_id: string;
  resource_type: "server" | "sshKey";
  resource_id: string;
  server_revision: number | null;
  conflict_remote_revision: number | null;
  conflict_remote_payload_json: string | null;
  conflict_remote_updated_at: string | null;
  conflict_remote_deleted: number;
  conflict_detected_at: string | null;
}

interface ProxyRow {
  id: string;
  name: string;
  proxy_type: "socks4" | "socks5";
  host: string;
  port: number;
  username: string | null;
  credential_ref: string | null;
  created_at: string;
  updated_at: string;
  resource_id: string | null;
  uuid_in_scope: string | null;
  origin_kind: string | null;
  origin_scope_key: string | null;
  origin_workspace_id: string | null;
  copied_from_resource_id: string | null;
}

interface WorkspaceRepoCommitRow {
  workspace_id: string;
  commit_id: string;
  parent_commit_id: string | null;
  snapshot_id: string;
  author_name: string;
  author_kind: "system" | "user" | "reconcile";
  message: string;
  created_at: string;
}

interface WorkspaceRepoSnapshotRow {
  workspace_id: string;
  snapshot_id: string;
  snapshot_json: string;
  created_at: string;
}

interface WorkspaceRepoLocalStateRow {
  workspace_id: string;
  local_head_commit_id: string | null;
  remote_head_commit_id: string | null;
  remote_commands_version: string | null;
  last_sync_at: string | null;
  last_error: string | null;
  sync_state: string;
}

interface WorkspaceRepoConflictRow {
  workspace_id: string;
  resource_type: "connection" | "sshKey" | "proxy";
  resource_id: string;
  display_name: string;
  local_snapshot_json: string | null;
  remote_snapshot_json: string | null;
  remote_deleted: number;
  detected_at: string;
}

interface WorkspaceCommandRow {
  id: string;
  workspace_id: string;
  name: string;
  description: string | null;
  group_name: string;
  command: string;
  is_template: number;
  created_at: string;
  updated_at: string;
}

interface WorkspaceCommandSyncStateRow {
  workspace_id: string;
  commands_version: string | null;
  updated_at: string | null;
}

interface MigrationRow {
  version: number;
  name: string;
  applied_at: string;
}

interface AuditLogRow {
  id: string;
  action: string;
  level: "info" | "warn" | "error";
  connection_id: string | null;
  message: string;
  metadata_json: string | null;
  created_at: string;
}

interface CommandHistoryRow {
  command: string;
  use_count: number;
  last_used_at: string;
}

interface SavedCommandRow {
  id: string;
  name: string;
  description: string | null;
  group_name: string;
  command: string;
  is_template: number;
  created_at: string;
  updated_at: string;
}

interface AppSettingRow {
  key: string;
  value_json: string;
  updated_at: string;
}

interface MigrationDefinition {
  version: number;
  name: string;
  apply: (db: Database.Database) => void;
}

export interface AppendAuditLogInput {
  action: string;
  level: "info" | "warn" | "error";
  connectionId?: string;
  message: string;
  metadata?: Record<string, unknown>;
}

export type CommandHistoryMutationInput =
  | { type: "push"; command: string }
  | { type: "remove"; command: string }
  | { type: "clear" };

const loadDatabaseDriver = (): BetterSqlite3Module => {
  const moduleName = `better-sqlite${3}`;
  return require(moduleName) as BetterSqlite3Module;
};

const toJSON = (value: string[]): string => JSON.stringify(value);

const fromJSON = (value: string): string[] => {
  try {
    const parsed = JSON.parse(value);
    return Array.isArray(parsed) ? parsed.filter((item): item is string => typeof item === "string") : [];
  } catch {
    return [];
  }
};

const fromMetadataJSON = (value: string | null): Record<string, unknown> | undefined => {
  if (!value) {
    return undefined;
  }

  try {
    const parsed = JSON.parse(value);
    return parsed && typeof parsed === "object" ? (parsed as Record<string, unknown>) : undefined;
  } catch {
    return undefined;
  }
};

const toMetadataJSON = (value: Record<string, unknown> | undefined): string | null => {
  if (!value) {
    return null;
  }

  return JSON.stringify(value);
};

const parseGroupPath = (raw: string | null | undefined): string => {
  if (!raw) return "/server";
  const trimmed = raw.trim();
  let path: string;
  if (trimmed.startsWith("/")) {
    path = trimmed;
  } else if (trimmed.startsWith("[")) {
    try {
      const arr = JSON.parse(trimmed) as string[];
      if (Array.isArray(arr) && arr.length > 0) {
        path = "/" + arr.join("/");
      } else {
        path = "/server";
      }
    } catch {
      path = "/" + trimmed;
    }
  } else {
    path = "/" + trimmed;
  }
  // Enforce zone prefix: if path's first segment is not a valid zone, prepend /server
  const segments = path.split("/").filter((s) => s.length > 0);
  const zone = segments[0] ?? "server";
  if (zone !== "server" && zone !== "workspace" && zone !== "import") {
    return "/server" + path;
  }
  return path;
};

const rowToConnection = (row: ConnectionRow): ConnectionProfile => {
  const rawKeepAliveEnabled = (row as ConnectionRow & { keepalive_enabled?: number | null }).keepalive_enabled;
  const keepAliveEnabled =
    rawKeepAliveEnabled === 1 ? true : rawKeepAliveEnabled === 0 ? false : undefined;
  const rawKeepAliveInterval = (row as ConnectionRow & { keepalive_interval_sec?: number | null }).keepalive_interval_sec;
  const keepAliveIntervalSec =
    typeof rawKeepAliveInterval === "number" &&
    Number.isInteger(rawKeepAliveInterval) &&
    rawKeepAliveInterval > 0
      ? rawKeepAliveInterval
      : undefined;
  // Derive origin fields: old data without origin columns → local-default
  const originKind = (row.origin_kind === "cloud" ? "cloud" : "local") as OriginKind;
  const originScopeKey = row.origin_scope_key ?? LOCAL_DEFAULT_SCOPE_KEY;
  const uuidInScope = row.uuid_in_scope ?? row.id;
  const resourceId = row.resource_id ?? buildResourceId(originScopeKey, uuidInScope);
  return {
    id: row.id,
    name: row.name,
    host: row.host,
    port: row.port,
    username: row.username,
    authType: row.auth_type,
    credentialRef: row.credential_ref ?? undefined,
    sshKeyId: row.ssh_key_id ?? undefined,
    hostFingerprint: row.host_fingerprint ?? undefined,
    strictHostKeyChecking: row.strict_host_key_checking === 1,
    proxyId: row.proxy_id ?? undefined,
    keepAliveEnabled,
    keepAliveIntervalSec,
    terminalEncoding:
      row.terminal_encoding === "gb18030" ||
      row.terminal_encoding === "gbk" ||
      row.terminal_encoding === "big5"
        ? row.terminal_encoding
        : "utf-8",
    backspaceMode: row.backspace_mode === "ascii-delete" ? "ascii-delete" : "ascii-backspace",
    deleteMode:
      row.delete_mode === "ascii-delete" || row.delete_mode === "ascii-backspace"
        ? row.delete_mode
        : "vt220-delete",
    groupPath: parseGroupPath(row.group_path),
    tags: fromJSON(row.tags),
    notes: row.notes ?? undefined,
    favorite: row.favorite === 1,
    monitorSession: (row as ConnectionRow & { monitor_session?: number }).monitor_session === 1,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
    lastConnectedAt: row.last_connected_at ?? undefined,
    resourceId,
    uuidInScope,
    originKind,
    originScopeKey,
    originWorkspaceId: row.origin_workspace_id ?? undefined,
    sshKeyResourceId: row.ssh_key_resource_id ?? undefined,
    copiedFromResourceId: row.copied_from_resource_id ?? undefined
  };
};

const rowToSshKey = (row: SshKeyRow): SshKeyProfile => {
  const originKind = (row.origin_kind === "cloud" ? "cloud" : "local") as OriginKind;
  const originScopeKey = row.origin_scope_key ?? LOCAL_DEFAULT_SCOPE_KEY;
  const uuidInScope = row.uuid_in_scope ?? row.id;
  const resourceId = row.resource_id ?? buildResourceId(originScopeKey, uuidInScope);
  return {
    id: row.id,
    name: row.name,
    keyContentRef: row.key_content_ref,
    passphraseRef: row.passphrase_ref ?? undefined,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
    resourceId,
    uuidInScope,
    originKind,
    originScopeKey,
    originWorkspaceId: row.origin_workspace_id ?? undefined,
    copiedFromResourceId: row.copied_from_resource_id ?? undefined
  };
};

const rowToCloudSyncWorkspace = (row: CloudSyncWorkspaceRow): CloudSyncWorkspaceProfile => ({
  id: row.id,
  apiBaseUrl: row.api_base_url,
  workspaceName: row.workspace_name,
  displayName: row.display_name,
  pullIntervalSec: row.pull_interval_sec,
  ignoreTlsErrors: row.ignore_tls_errors === 1,
  enabled: row.enabled === 1,
  createdAt: row.created_at,
  updatedAt: row.updated_at,
  lastSyncAt: row.last_sync_at,
  lastError: row.last_error
});

const rowToRecycleBinEntry = (row: RecycleBinRow): RecycleBinEntry => ({
  id: row.id,
  resourceType: row.resource_type,
  displayName: row.display_name,
  originalResourceId: row.original_resource_id,
  originalScopeKey: row.original_scope_key,
  reason: row.reason as RecycleBinEntry["reason"],
  snapshotJson: row.snapshot_json,
  createdAt: row.created_at
});

const rowToPendingOp = (row: PendingOpRow): CloudSyncPendingOp => ({
  id: row.id,
  workspaceId: row.workspace_id,
  resourceType: row.resource_type,
  resourceId: row.resource_id,
  action: row.action,
  baseRevision: row.base_revision,
  force: row.force === 1,
  payloadJson: row.payload_json ?? undefined,
  queuedAt: row.queued_at,
  lastAttemptAt: row.last_attempt_at ?? undefined,
  lastError: row.last_error ?? undefined
});

const rowToCloudSyncResourceStateV2 = (row: CloudSyncResourceStateV2Row): CloudSyncResourceStateV2 => ({
  workspaceId: row.workspace_id,
  resourceType: row.resource_type,
  resourceId: row.resource_id,
  serverRevision: typeof row.server_revision === "number" ? row.server_revision : undefined,
  conflictRemoteRevision: typeof row.conflict_remote_revision === "number" ? row.conflict_remote_revision : undefined,
  conflictRemotePayloadJson: row.conflict_remote_payload_json ?? undefined,
  conflictRemoteUpdatedAt: row.conflict_remote_updated_at ?? undefined,
  conflictRemoteDeleted: row.conflict_remote_deleted === 1,
  conflictDetectedAt: row.conflict_detected_at ?? undefined
});

const rowToProxy = (row: ProxyRow): ProxyProfile => ({
  id: row.id,
  name: row.name,
  proxyType: row.proxy_type,
  host: row.host,
  port: row.port,
  username: row.username ?? undefined,
  credentialRef: row.credential_ref ?? undefined,
  createdAt: row.created_at,
  updatedAt: row.updated_at,
  resourceId: row.resource_id ?? undefined,
  uuidInScope: row.uuid_in_scope ?? undefined,
  originKind: row.origin_kind === "cloud" ? "cloud" : "local",
  originScopeKey: row.origin_scope_key ?? LOCAL_DEFAULT_SCOPE_KEY,
  originWorkspaceId: row.origin_workspace_id ?? undefined,
  copiedFromResourceId: row.copied_from_resource_id ?? undefined,
});

const rowToWorkspaceRepoCommit = (row: WorkspaceRepoCommitRow): WorkspaceRepoCommitMeta => ({
  workspaceId: row.workspace_id,
  commitId: row.commit_id,
  parentCommitId: row.parent_commit_id ?? undefined,
  snapshotId: row.snapshot_id,
  authorName: row.author_name,
  authorKind: row.author_kind,
  message: row.message,
  createdAt: row.created_at,
});

const rowToWorkspaceRepoSnapshot = (row: WorkspaceRepoSnapshotRow): WorkspaceRepoSnapshot => {
  const parsed = JSON.parse(row.snapshot_json) as Omit<WorkspaceRepoSnapshot, "workspaceId" | "snapshotId" | "createdAt">;
  return {
    workspaceId: row.workspace_id,
    snapshotId: row.snapshot_id,
    createdAt: row.created_at,
    connections: parsed.connections ?? [],
    sshKeys: parsed.sshKeys ?? [],
    proxies: parsed.proxies ?? [],
  };
};

const rowToWorkspaceRepoLocalState = (row: WorkspaceRepoLocalStateRow): WorkspaceRepoLocalState => ({
  workspaceId: row.workspace_id,
  localHeadCommitId: row.local_head_commit_id ?? undefined,
  remoteHeadCommitId: row.remote_head_commit_id ?? undefined,
  remoteCommandsVersion: row.remote_commands_version ?? undefined,
  lastSyncAt: row.last_sync_at ?? undefined,
  lastError: row.last_error ?? undefined,
  syncState: row.sync_state as WorkspaceRepoLocalState["syncState"],
});

const rowToWorkspaceRepoConflict = (row: WorkspaceRepoConflictRow): WorkspaceRepoConflict => ({
  workspaceId: row.workspace_id,
  resourceType: row.resource_type,
  resourceId: row.resource_id,
  displayName: row.display_name,
  localSnapshotJson: row.local_snapshot_json ?? undefined,
  remoteSnapshotJson: row.remote_snapshot_json ?? undefined,
  remoteDeleted: row.remote_deleted === 1,
  detectedAt: row.detected_at,
});

const rowToWorkspaceCommand = (row: WorkspaceCommandRow): WorkspaceCommandItem => ({
  id: row.id,
  workspaceId: row.workspace_id,
  name: row.name,
  description: row.description ?? undefined,
  group: row.group_name,
  command: row.command,
  isTemplate: row.is_template === 1,
  createdAt: row.created_at,
  updatedAt: row.updated_at,
});

const rowToMigration = (row: MigrationRow): MigrationRecord => {
  return {
    version: row.version,
    name: row.name,
    appliedAt: row.applied_at
  };
};

const rowToCommandHistory = (row: CommandHistoryRow): CommandHistoryEntry => ({
  command: row.command,
  useCount: row.use_count,
  lastUsedAt: row.last_used_at
});

const rowToSavedCommand = (row: SavedCommandRow): SavedCommand => ({
  id: row.id,
  name: row.name,
  description: row.description ?? undefined,
  group: row.group_name,
  command: row.command,
  isTemplate: row.is_template === 1,
  createdAt: row.created_at,
  updatedAt: row.updated_at
});

const rowToAuditLog = (row: AuditLogRow): AuditLogRecord => {
  return {
    id: row.id,
    action: row.action,
    level: row.level,
    connectionId: row.connection_id ?? undefined,
    message: row.message,
    metadata: fromMetadataJSON(row.metadata_json),
    createdAt: row.created_at
  };
};

const cloneDefaultPreferences = (): AppPreferences => {
  return {
    transfer: { ...DEFAULT_APP_PREFERENCES_VALUE.transfer },
    remoteEdit: { ...DEFAULT_APP_PREFERENCES_VALUE.remoteEdit },
    commandCenter: { ...DEFAULT_APP_PREFERENCES_VALUE.commandCenter },
    terminal: { ...DEFAULT_APP_PREFERENCES_VALUE.terminal },
    ssh: { ...DEFAULT_APP_PREFERENCES_VALUE.ssh },
    backup: { ...DEFAULT_APP_PREFERENCES_VALUE.backup },
    window: { ...DEFAULT_APP_PREFERENCES_VALUE.window },
    traceroute: { ...DEFAULT_APP_PREFERENCES_VALUE.traceroute },
    audit: { ...DEFAULT_APP_PREFERENCES_VALUE.audit },
    ai: {
      ...DEFAULT_APP_PREFERENCES_VALUE.ai,
      providers: DEFAULT_APP_PREFERENCES_VALUE.ai.providers.map((p) => ({ ...p }))
    }
  };
};

const parseAppPreferences = (value: string | null): AppPreferences => {
  const fallback = cloneDefaultPreferences();
  if (!value) {
    return fallback;
  }

  try {
    const parsed = JSON.parse(value) as Partial<AppPreferences> | null;
    if (!parsed || typeof parsed !== "object") {
      return fallback;
    }

    const legacyBackgroundImagePath = ((): string | undefined => {
      const legacyTerminal = (parsed as { terminal?: { backgroundImagePath?: unknown } }).terminal;
      if (typeof legacyTerminal?.backgroundImagePath === "string") {
        return legacyTerminal.backgroundImagePath;
      }
      return undefined;
    })();

    return {
      transfer: {
        uploadDefaultDir:
          typeof parsed.transfer?.uploadDefaultDir === "string" &&
          parsed.transfer.uploadDefaultDir.trim().length > 0
            ? parsed.transfer.uploadDefaultDir.trim()
            : fallback.transfer.uploadDefaultDir,
        downloadDefaultDir:
          typeof parsed.transfer?.downloadDefaultDir === "string" &&
          parsed.transfer.downloadDefaultDir.trim().length > 0
            ? parsed.transfer.downloadDefaultDir.trim()
            : fallback.transfer.downloadDefaultDir
      },
      remoteEdit: {
        defaultEditorCommand:
          typeof parsed.remoteEdit?.defaultEditorCommand === "string"
            ? parsed.remoteEdit.defaultEditorCommand.trim()
            : fallback.remoteEdit.defaultEditorCommand,
        editorMode:
          parsed.remoteEdit?.editorMode === "builtin" ||
          parsed.remoteEdit?.editorMode === "external"
            ? parsed.remoteEdit.editorMode
            : fallback.remoteEdit.editorMode
      },
      commandCenter: {
        rememberTemplateParams:
          typeof parsed.commandCenter?.rememberTemplateParams === "boolean"
            ? parsed.commandCenter.rememberTemplateParams
            : fallback.commandCenter.rememberTemplateParams,
        batchMaxConcurrency: normalizeBatchMaxConcurrency(
          typeof parsed.commandCenter?.batchMaxConcurrency === "number"
            ? parsed.commandCenter.batchMaxConcurrency
            : undefined,
          fallback.commandCenter.batchMaxConcurrency
        ),
        batchRetryCount: normalizeBatchRetryCount(
          typeof parsed.commandCenter?.batchRetryCount === "number"
            ? parsed.commandCenter.batchRetryCount
            : undefined,
          fallback.commandCenter.batchRetryCount
        )
      },
      terminal: {
        backgroundColor:
          typeof parsed.terminal?.backgroundColor === "string" &&
          /^#[0-9a-fA-F]{6}$/.test(parsed.terminal.backgroundColor.trim())
            ? parsed.terminal.backgroundColor.trim()
            : fallback.terminal.backgroundColor,
        foregroundColor:
          typeof parsed.terminal?.foregroundColor === "string" &&
          /^#[0-9a-fA-F]{6}$/.test(parsed.terminal.foregroundColor.trim())
            ? parsed.terminal.foregroundColor.trim()
            : fallback.terminal.foregroundColor,
        fontSize:
          typeof parsed.terminal?.fontSize === "number" &&
          Number.isInteger(parsed.terminal.fontSize) &&
          parsed.terminal.fontSize >= 10 &&
          parsed.terminal.fontSize <= 24
            ? parsed.terminal.fontSize
            : fallback.terminal.fontSize,
        lineHeight:
          typeof parsed.terminal?.lineHeight === "number" &&
          Number.isFinite(parsed.terminal.lineHeight) &&
          parsed.terminal.lineHeight >= 1 &&
          parsed.terminal.lineHeight <= 2
            ? parsed.terminal.lineHeight
            : fallback.terminal.lineHeight,
        fontFamily:
          typeof parsed.terminal?.fontFamily === "string" &&
          parsed.terminal.fontFamily.trim().length > 0
            ? parsed.terminal.fontFamily.trim()
            : fallback.terminal.fontFamily,
        localShell: {
          mode:
            parsed.terminal?.localShell?.mode === "preset" ||
            parsed.terminal?.localShell?.mode === "custom"
              ? parsed.terminal.localShell.mode
              : fallback.terminal.localShell.mode,
          preset:
            parsed.terminal?.localShell?.preset === "system" ||
            parsed.terminal?.localShell?.preset === "powershell" ||
            parsed.terminal?.localShell?.preset === "cmd" ||
            parsed.terminal?.localShell?.preset === "zsh" ||
            parsed.terminal?.localShell?.preset === "sh" ||
            parsed.terminal?.localShell?.preset === "bash"
              ? parsed.terminal.localShell.preset
              : fallback.terminal.localShell.preset,
          customPath:
            typeof parsed.terminal?.localShell?.customPath === "string"
              ? parsed.terminal.localShell.customPath.trim()
              : fallback.terminal.localShell.customPath
        }
      },
      ssh: {
        keepAliveEnabled:
          typeof parsed.ssh?.keepAliveEnabled === "boolean"
            ? parsed.ssh.keepAliveEnabled
            : fallback.ssh.keepAliveEnabled,
        keepAliveIntervalSec:
          typeof parsed.ssh?.keepAliveIntervalSec === "number" &&
          Number.isInteger(parsed.ssh.keepAliveIntervalSec) &&
          parsed.ssh.keepAliveIntervalSec >= 5 &&
          parsed.ssh.keepAliveIntervalSec <= 600
            ? parsed.ssh.keepAliveIntervalSec
            : fallback.ssh.keepAliveIntervalSec
      },
      backup: {
        remotePath:
          typeof parsed.backup?.remotePath === "string"
            ? parsed.backup.remotePath
            : fallback.backup.remotePath,
        rclonePath:
          typeof parsed.backup?.rclonePath === "string"
            ? parsed.backup.rclonePath
            : fallback.backup.rclonePath,
        defaultBackupConflictPolicy:
          parsed.backup?.defaultBackupConflictPolicy === "skip" ||
          parsed.backup?.defaultBackupConflictPolicy === "force"
            ? parsed.backup.defaultBackupConflictPolicy
            : fallback.backup.defaultBackupConflictPolicy,
        defaultRestoreConflictPolicy:
          parsed.backup?.defaultRestoreConflictPolicy === "skip_older" ||
          parsed.backup?.defaultRestoreConflictPolicy === "force"
            ? parsed.backup.defaultRestoreConflictPolicy
            : fallback.backup.defaultRestoreConflictPolicy,
        rememberPassword:
          typeof parsed.backup?.rememberPassword === "boolean"
            ? parsed.backup.rememberPassword
            : fallback.backup.rememberPassword,
        lastBackupAt:
          typeof parsed.backup?.lastBackupAt === "string"
            ? parsed.backup.lastBackupAt
            : fallback.backup.lastBackupAt
      },
      window: {
        appearance:
          parsed.window?.appearance === "system" ||
          parsed.window?.appearance === "light" ||
          parsed.window?.appearance === "dark"
            ? parsed.window.appearance
            : fallback.window.appearance,
        minimizeToTray:
          typeof parsed.window?.minimizeToTray === "boolean"
            ? parsed.window.minimizeToTray
            : fallback.window.minimizeToTray,
        confirmBeforeClose:
          typeof parsed.window?.confirmBeforeClose === "boolean"
            ? parsed.window.confirmBeforeClose
            : fallback.window.confirmBeforeClose,
        backgroundImagePath:
          typeof parsed.window?.backgroundImagePath === "string"
            ? parsed.window.backgroundImagePath
            : (legacyBackgroundImagePath ?? fallback.window.backgroundImagePath),
        backgroundOpacity:
          typeof parsed.window?.backgroundOpacity === "number" &&
          Number.isFinite(parsed.window.backgroundOpacity) &&
          Math.round(parsed.window.backgroundOpacity) >= 30 &&
          Math.round(parsed.window.backgroundOpacity) <= 80
            ? Math.round(parsed.window.backgroundOpacity)
            : fallback.window.backgroundOpacity,
        leftSidebarDefaultCollapsed:
          typeof parsed.window?.leftSidebarDefaultCollapsed === "boolean"
            ? parsed.window.leftSidebarDefaultCollapsed
            : fallback.window.leftSidebarDefaultCollapsed,
        bottomWorkbenchDefaultCollapsed:
          typeof parsed.window?.bottomWorkbenchDefaultCollapsed === "boolean"
            ? parsed.window.bottomWorkbenchDefaultCollapsed
            : fallback.window.bottomWorkbenchDefaultCollapsed
      },
      traceroute: {
        nexttracePath:
          typeof parsed.traceroute?.nexttracePath === "string"
            ? parsed.traceroute.nexttracePath
            : fallback.traceroute.nexttracePath,
        protocol:
          parsed.traceroute?.protocol === "icmp" ||
          parsed.traceroute?.protocol === "tcp" ||
          parsed.traceroute?.protocol === "udp"
            ? parsed.traceroute.protocol
            : fallback.traceroute.protocol,
        port:
          typeof parsed.traceroute?.port === "number" &&
          Number.isInteger(parsed.traceroute.port) &&
          parsed.traceroute.port >= 0 &&
          parsed.traceroute.port <= 65535
            ? parsed.traceroute.port
            : fallback.traceroute.port,
        queries:
          typeof parsed.traceroute?.queries === "number" &&
          Number.isInteger(parsed.traceroute.queries) &&
          parsed.traceroute.queries >= 1 &&
          parsed.traceroute.queries <= 10
            ? parsed.traceroute.queries
            : fallback.traceroute.queries,
        maxHops:
          typeof parsed.traceroute?.maxHops === "number" &&
          Number.isInteger(parsed.traceroute.maxHops) &&
          parsed.traceroute.maxHops >= 1 &&
          parsed.traceroute.maxHops <= 64
            ? parsed.traceroute.maxHops
            : fallback.traceroute.maxHops,
        ipVersion:
          parsed.traceroute?.ipVersion === "auto" ||
          parsed.traceroute?.ipVersion === "ipv4" ||
          parsed.traceroute?.ipVersion === "ipv6"
            ? parsed.traceroute.ipVersion
            : fallback.traceroute.ipVersion,
        dataProvider:
          parsed.traceroute?.dataProvider === "LeoMoeAPI" ||
          parsed.traceroute?.dataProvider === "ip-api.com" ||
          parsed.traceroute?.dataProvider === "IPInfo" ||
          parsed.traceroute?.dataProvider === "IPInsight" ||
          parsed.traceroute?.dataProvider === "IP.SB" ||
          parsed.traceroute?.dataProvider === "disable-geoip"
            ? parsed.traceroute.dataProvider
            : fallback.traceroute.dataProvider,
        noRdns:
          typeof parsed.traceroute?.noRdns === "boolean"
            ? parsed.traceroute.noRdns
            : fallback.traceroute.noRdns,
        language:
          parsed.traceroute?.language === "cn" ||
          parsed.traceroute?.language === "en"
            ? parsed.traceroute.language
            : fallback.traceroute.language,
        powProvider:
          parsed.traceroute?.powProvider === "api.nxtrace.org" ||
          parsed.traceroute?.powProvider === "sakura"
            ? parsed.traceroute.powProvider
            : fallback.traceroute.powProvider,
        showTracerouteTab:
          typeof parsed.traceroute?.showTracerouteTab === "boolean"
            ? parsed.traceroute.showTracerouteTab
            : fallback.traceroute.showTracerouteTab
      },
      audit: {
        enabled:
          typeof parsed.audit?.enabled === "boolean"
            ? parsed.audit.enabled
            : fallback.audit.enabled,
        retentionDays:
          typeof parsed.audit?.retentionDays === "number" &&
          Number.isInteger(parsed.audit.retentionDays) &&
          parsed.audit.retentionDays >= 0 &&
          parsed.audit.retentionDays <= 365
            ? parsed.audit.retentionDays
            : fallback.audit.retentionDays
      },
      ai: {
        enabled:
          typeof parsed.ai?.enabled === "boolean"
            ? parsed.ai.enabled
            : fallback.ai.enabled,
        activeProviderId:
          typeof parsed.ai?.activeProviderId === "string"
            ? parsed.ai.activeProviderId
            : fallback.ai.activeProviderId,
        providers: Array.isArray(parsed.ai?.providers)
          ? parsed.ai.providers
          : fallback.ai.providers,
        systemPromptOverride:
          typeof parsed.ai?.systemPromptOverride === "string"
            ? parsed.ai.systemPromptOverride
            : fallback.ai.systemPromptOverride,
        executionTimeoutSec:
          typeof parsed.ai?.executionTimeoutSec === "number" &&
          Number.isInteger(parsed.ai.executionTimeoutSec) &&
          parsed.ai.executionTimeoutSec >= 5 &&
          parsed.ai.executionTimeoutSec <= 300
            ? parsed.ai.executionTimeoutSec
            : fallback.ai.executionTimeoutSec,
        providerRequestTimeoutSec:
          typeof parsed.ai?.providerRequestTimeoutSec === "number" &&
          Number.isInteger(parsed.ai.providerRequestTimeoutSec) &&
          parsed.ai.providerRequestTimeoutSec >= 5 &&
          parsed.ai.providerRequestTimeoutSec <= 120
            ? parsed.ai.providerRequestTimeoutSec
            : fallback.ai.providerRequestTimeoutSec,
        providerMaxRetries:
          typeof parsed.ai?.providerMaxRetries === "number" &&
          Number.isInteger(parsed.ai.providerMaxRetries) &&
          parsed.ai.providerMaxRetries >= 0 &&
          parsed.ai.providerMaxRetries <= 3
            ? parsed.ai.providerMaxRetries
            : fallback.ai.providerMaxRetries
      }
    };
  } catch {
    return fallback;
  }
};

const hasColumn = (db: Database.Database, table: string, column: string): boolean => {
  const rows = db.prepare(`PRAGMA table_info(${table})`).all() as Array<{ name: string }>;
  return rows.some((row) => row.name === column);
};

const hasTable = (db: Database.Database, table: string): boolean => {
  const row = db.prepare(
    "SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?"
  ).get(table) as { name?: string } | undefined;
  return Boolean(row?.name);
};

const ensureColumn = (
  db: Database.Database,
  table: string,
  column: string,
  definition: string
): void => {
  if (hasColumn(db, table, column)) {
    return;
  }

  db.exec(`ALTER TABLE ${table} ADD COLUMN ${definition}`);
};

const migrations: MigrationDefinition[] = [
  {
    version: 1,
    name: "create_connections_table",
    apply: (db) => {
      db.exec(`
        CREATE TABLE IF NOT EXISTS connections (
          id TEXT PRIMARY KEY,
          name TEXT NOT NULL,
          host TEXT NOT NULL,
          port INTEGER NOT NULL,
          username TEXT NOT NULL,
          auth_type TEXT NOT NULL,
          credential_ref TEXT,
          private_key_path TEXT,
          private_key_ref TEXT,
          host_fingerprint TEXT,
          strict_host_key_checking INTEGER NOT NULL DEFAULT 0,
          proxy_type TEXT NOT NULL DEFAULT 'none',
          proxy_host TEXT,
          proxy_port INTEGER,
          proxy_username TEXT,
          proxy_credential_ref TEXT,
          terminal_encoding TEXT NOT NULL DEFAULT 'utf-8',
          backspace_mode TEXT NOT NULL DEFAULT 'ascii-backspace',
          delete_mode TEXT NOT NULL DEFAULT 'vt220-delete',
          group_path TEXT NOT NULL,
          tags TEXT NOT NULL,
          notes TEXT,
          favorite INTEGER NOT NULL DEFAULT 0,
          created_at TEXT NOT NULL,
          updated_at TEXT NOT NULL,
          last_connected_at TEXT
        );

        CREATE INDEX IF NOT EXISTS idx_connections_name ON connections(name);
        CREATE INDEX IF NOT EXISTS idx_connections_host ON connections(host);
        CREATE INDEX IF NOT EXISTS idx_connections_updated_at ON connections(updated_at DESC);
      `);
    }
  },
  {
    version: 2,
    name: "add_connection_security_columns",
    apply: (db) => {
      ensureColumn(db, "connections", "private_key_ref", "private_key_ref TEXT");
      ensureColumn(db, "connections", "host_fingerprint", "host_fingerprint TEXT");
      ensureColumn(
        db,
        "connections",
        "strict_host_key_checking",
        "strict_host_key_checking INTEGER NOT NULL DEFAULT 0"
      );
    }
  },
  {
    version: 3,
    name: "create_audit_logs_table",
    apply: (db) => {
      db.exec(`
        CREATE TABLE IF NOT EXISTS audit_logs (
          id TEXT PRIMARY KEY,
          action TEXT NOT NULL,
          level TEXT NOT NULL,
          connection_id TEXT,
          message TEXT NOT NULL,
          metadata_json TEXT,
          created_at TEXT NOT NULL
        );

        CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at DESC);
        CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON audit_logs(action);
      `);
    }
  },
  {
    version: 4,
    name: "create_command_history_table",
    apply: (db) => {
      db.exec(`
        CREATE TABLE IF NOT EXISTS command_history (
          command TEXT PRIMARY KEY,
          use_count INTEGER NOT NULL DEFAULT 1,
          last_used_at TEXT NOT NULL
        );

        CREATE INDEX IF NOT EXISTS idx_command_history_last_used_at ON command_history(last_used_at DESC);
        CREATE INDEX IF NOT EXISTS idx_command_history_use_count ON command_history(use_count ASC);
      `);
    }
  },
  {
    version: 5,
    name: "add_connection_proxy_columns",
    apply: (db) => {
      ensureColumn(db, "connections", "proxy_type", "proxy_type TEXT NOT NULL DEFAULT 'none'");
      ensureColumn(db, "connections", "proxy_host", "proxy_host TEXT");
      ensureColumn(db, "connections", "proxy_port", "proxy_port INTEGER");
      ensureColumn(db, "connections", "proxy_username", "proxy_username TEXT");
      ensureColumn(db, "connections", "proxy_credential_ref", "proxy_credential_ref TEXT");
    }
  },
  {
    version: 6,
    name: "add_connection_terminal_columns",
    apply: (db) => {
      ensureColumn(
        db,
        "connections",
        "terminal_encoding",
        "terminal_encoding TEXT NOT NULL DEFAULT 'utf-8'"
      );
      ensureColumn(
        db,
        "connections",
        "backspace_mode",
        "backspace_mode TEXT NOT NULL DEFAULT 'ascii-backspace'"
      );
      ensureColumn(
        db,
        "connections",
        "delete_mode",
        "delete_mode TEXT NOT NULL DEFAULT 'vt220-delete'"
      );
    }
  },
  {
    version: 7,
    name: "create_saved_commands_table",
    apply: (db) => {
      db.exec(`
        CREATE TABLE IF NOT EXISTS saved_commands (
          id TEXT PRIMARY KEY,
          name TEXT NOT NULL,
          description TEXT,
          group_name TEXT NOT NULL DEFAULT '默认',
          command TEXT NOT NULL,
          is_template INTEGER NOT NULL DEFAULT 0,
          created_at TEXT NOT NULL,
          updated_at TEXT NOT NULL
        );
        CREATE INDEX IF NOT EXISTS idx_saved_commands_group ON saved_commands(group_name);
        CREATE INDEX IF NOT EXISTS idx_saved_commands_updated_at ON saved_commands(updated_at DESC);
      `);
    }
  },
  {
    version: 8,
    name: "create_app_settings_table",
    apply: (db) => {
      db.exec(`
        CREATE TABLE IF NOT EXISTS app_settings (
          key TEXT PRIMARY KEY,
          value_json TEXT NOT NULL,
          updated_at TEXT NOT NULL
        );
        CREATE INDEX IF NOT EXISTS idx_app_settings_updated_at ON app_settings(updated_at DESC);
      `);
    }
  },
  {
    version: 9,
    name: "add_connection_monitor_session_column",
    apply: (db) => {
      ensureColumn(
        db,
        "connections",
        "monitor_session",
        "monitor_session INTEGER NOT NULL DEFAULT 0"
      );
    }
  },
  {
    version: 10,
    name: "create_secret_store_table",
    apply: (db) => {
      db.exec(`
        CREATE TABLE IF NOT EXISTS secret_store (
          id TEXT PRIMARY KEY,
          purpose TEXT NOT NULL,
          ciphertext_b64 TEXT NOT NULL,
          iv_b64 TEXT NOT NULL,
          tag_b64 TEXT NOT NULL,
          aad TEXT NOT NULL,
          created_at TEXT NOT NULL,
          updated_at TEXT NOT NULL
        );
        CREATE INDEX IF NOT EXISTS idx_secret_store_purpose ON secret_store(purpose);
      `);
    }
  },
  {
    version: 11,
    name: "create_command_template_params_table",
    apply: (db) => {
      db.exec(`
        CREATE TABLE IF NOT EXISTS command_template_params (
          id TEXT PRIMARY KEY,
          command_id TEXT NOT NULL,
          param_name TEXT NOT NULL,
          param_value TEXT NOT NULL,
          updated_at TEXT NOT NULL
        );
        CREATE INDEX IF NOT EXISTS idx_template_params_command_id ON command_template_params(command_id);
        CREATE UNIQUE INDEX IF NOT EXISTS idx_template_params_command_param ON command_template_params(command_id, param_name);
      `);
    }
  },
  {
    version: 12,
    name: "add_device_key_setting",
    apply: (_db) => {
      // Device key is stored as an app_setting row.
      // The actual generation + insertion happens at runtime in container.ts on first launch.
      // This migration is a no-op placeholder for version tracking.
    }
  },
  {
    version: 13,
    name: "restructure_keys_and_proxies",
    apply: (db) => {
      db.exec(`
        CREATE TABLE IF NOT EXISTS ssh_keys (
          id TEXT PRIMARY KEY,
          name TEXT NOT NULL UNIQUE,
          key_content_ref TEXT NOT NULL,
          passphrase_ref TEXT,
          created_at TEXT NOT NULL,
          updated_at TEXT NOT NULL
        );
        CREATE INDEX IF NOT EXISTS idx_ssh_keys_name ON ssh_keys(name);
      `);

      db.exec(`
        CREATE TABLE IF NOT EXISTS proxies (
          id TEXT PRIMARY KEY,
          name TEXT NOT NULL UNIQUE,
          proxy_type TEXT NOT NULL,
          host TEXT NOT NULL,
          port INTEGER NOT NULL,
          username TEXT,
          credential_ref TEXT,
          created_at TEXT NOT NULL,
          updated_at TEXT NOT NULL
        );
        CREATE INDEX IF NOT EXISTS idx_proxies_name ON proxies(name);
      `);

      ensureColumn(db, "connections", "ssh_key_id", "ssh_key_id TEXT");
      ensureColumn(db, "connections", "proxy_id", "proxy_id TEXT");
    }
  },
  {
    version: 16,
    name: "add_connection_keepalive_columns",
    apply: (db) => {
      ensureColumn(db, "connections", "keepalive_enabled", "keepalive_enabled INTEGER");
      ensureColumn(db, "connections", "keepalive_interval_sec", "keepalive_interval_sec INTEGER");
    }
  },
  {
    version: 17,
    name: "create_cloud_sync_resource_state_table",
    apply: (db) => {
      db.exec(`
        CREATE TABLE IF NOT EXISTS cloud_sync_resource_state (
          resource_type TEXT NOT NULL,
          resource_id TEXT NOT NULL,
          server_revision INTEGER,
          conflict_remote_revision INTEGER,
          conflict_remote_payload_json TEXT,
          conflict_remote_updated_at TEXT,
          conflict_remote_deleted INTEGER NOT NULL DEFAULT 0,
          conflict_detected_at TEXT,
          PRIMARY KEY (resource_type, resource_id)
        );
        CREATE INDEX IF NOT EXISTS idx_cloud_sync_resource_state_conflict
          ON cloud_sync_resource_state(conflict_remote_revision DESC, conflict_detected_at DESC);
      `);
    }
  },
  {
    version: 18,
    name: "enforce_connection_zones",
    apply: (db) => {
      // Migrate all connections whose groupPath doesn't start with one of the
      // three allowed zone prefixes (/server, /workspace, /import) by prepending
      // /server.  This normalizes legacy paths like /导入/xxx → /server/导入/xxx.
      db.exec(`
        UPDATE connections
        SET group_path = '/server' || group_path
        WHERE group_path NOT LIKE '/server%'
          AND group_path NOT LIKE '/workspace%'
          AND group_path NOT LIKE '/import%';
      `);
      // Ensure bare root "/" gets a zone
      db.exec(`
        UPDATE connections
        SET group_path = '/server'
        WHERE group_path = '/';
      `);
    }
  },
  {
    version: 19,
    name: "cloud_sync_v2_multi_workspace",
    apply: (db) => {
      // ── 1. Add origin columns to connections ──
      ensureColumn(db, "connections", "resource_id", "resource_id TEXT");
      ensureColumn(db, "connections", "uuid_in_scope", "uuid_in_scope TEXT");
      ensureColumn(db, "connections", "origin_kind", "origin_kind TEXT DEFAULT 'local'");
      ensureColumn(db, "connections", "origin_scope_key", "origin_scope_key TEXT DEFAULT 'local-default'");
      ensureColumn(db, "connections", "origin_workspace_id", "origin_workspace_id TEXT");
      ensureColumn(db, "connections", "ssh_key_resource_id", "ssh_key_resource_id TEXT");
      ensureColumn(db, "connections", "copied_from_resource_id", "copied_from_resource_id TEXT");

      // ── 2. Add origin columns to ssh_keys ──
      ensureColumn(db, "ssh_keys", "resource_id", "resource_id TEXT");
      ensureColumn(db, "ssh_keys", "uuid_in_scope", "uuid_in_scope TEXT");
      ensureColumn(db, "ssh_keys", "origin_kind", "origin_kind TEXT DEFAULT 'local'");
      ensureColumn(db, "ssh_keys", "origin_scope_key", "origin_scope_key TEXT DEFAULT 'local-default'");
      ensureColumn(db, "ssh_keys", "origin_workspace_id", "origin_workspace_id TEXT");
      ensureColumn(db, "ssh_keys", "copied_from_resource_id", "copied_from_resource_id TEXT");

      // ── 3. Cloud sync workspaces table ──
      db.exec(`
        CREATE TABLE IF NOT EXISTS cloud_sync_workspaces (
          id TEXT PRIMARY KEY,
          api_base_url TEXT NOT NULL,
          workspace_name TEXT NOT NULL,
          display_name TEXT NOT NULL,
          pull_interval_sec INTEGER NOT NULL DEFAULT 60,
          ignore_tls_errors INTEGER NOT NULL DEFAULT 0,
          enabled INTEGER NOT NULL DEFAULT 1,
          created_at TEXT NOT NULL DEFAULT '',
          updated_at TEXT NOT NULL DEFAULT '',
          last_sync_at TEXT,
          last_error TEXT
        );
      `);

      // ── 4. Recycle bin table ──
      db.exec(`
        CREATE TABLE IF NOT EXISTS recycle_bin_entries (
          id TEXT PRIMARY KEY,
          resource_type TEXT NOT NULL,
          display_name TEXT NOT NULL,
          original_resource_id TEXT NOT NULL,
          original_scope_key TEXT NOT NULL,
          reason TEXT NOT NULL,
          snapshot_json TEXT NOT NULL,
          created_at TEXT NOT NULL
        );
        CREATE INDEX IF NOT EXISTS idx_recycle_bin_created_at
          ON recycle_bin_entries(created_at DESC);
      `);

      // ── 5. Workspace-scoped pending ops table ──
      db.exec(`
        CREATE TABLE IF NOT EXISTS cloud_sync_pending_ops (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          workspace_id TEXT NOT NULL,
          resource_type TEXT NOT NULL,
          resource_id TEXT NOT NULL,
          action TEXT NOT NULL,
          base_revision INTEGER,
          force INTEGER NOT NULL DEFAULT 0,
          payload_json TEXT,
          queued_at TEXT NOT NULL,
          last_attempt_at TEXT,
          last_error TEXT
        );
        CREATE INDEX IF NOT EXISTS idx_pending_ops_workspace
          ON cloud_sync_pending_ops(workspace_id, queued_at ASC);
      `);

      // ── 6. Rebuild cloud_sync_resource_state with workspace_id ──
      // Old table uses (resource_type, resource_id) PK; new model needs workspace_id.
      // Cloud sync is not yet publicly released → safe to drop & recreate.
      db.exec(`DROP TABLE IF EXISTS cloud_sync_resource_state`);
      db.exec(`
        CREATE TABLE cloud_sync_resource_state (
          workspace_id TEXT NOT NULL,
          resource_type TEXT NOT NULL,
          resource_id TEXT NOT NULL,
          server_revision INTEGER,
          conflict_remote_revision INTEGER,
          conflict_remote_payload_json TEXT,
          conflict_remote_updated_at TEXT,
          conflict_remote_deleted INTEGER NOT NULL DEFAULT 0,
          conflict_detected_at TEXT,
          PRIMARY KEY (workspace_id, resource_type, resource_id)
        );
        CREATE INDEX IF NOT EXISTS idx_cloud_sync_v2_conflict
          ON cloud_sync_resource_state(workspace_id, conflict_remote_revision DESC);
      `);

      // ── 7. Clean up old pending queue JSON setting ──
      db.exec(`DELETE FROM app_settings WHERE key = 'cloud_sync_pending_queue'`);
    }
  },
  {
    version: 20,
    name: "cloud_sync_v2_indexes_and_runtime_state",
    apply: (db) => {
      // Unique partial indexes to prevent duplicate resource_id
      db.exec(`
        CREATE UNIQUE INDEX IF NOT EXISTS idx_conn_resource_id
          ON connections(resource_id) WHERE resource_id IS NOT NULL;
      `);
      db.exec(`
        CREATE UNIQUE INDEX IF NOT EXISTS idx_sshkey_resource_id
          ON ssh_keys(resource_id) WHERE resource_id IS NOT NULL;
      `);
      // Runtime state table for persisting currentVersion across restarts
      db.exec(`
        CREATE TABLE IF NOT EXISTS cloud_sync_runtime_state (
          workspace_id TEXT PRIMARY KEY,
          current_version INTEGER NOT NULL DEFAULT 0
        );
      `);
    }
  },
  {
    version: 21,
    name: "pending_ops_dedup_unique_index",
    apply: (db) => {
      // Deduplicate any existing pending ops before adding unique constraint:
      // keep only the row with the highest id for each (workspace_id, resource_type, resource_id).
      db.exec(`
        DELETE FROM cloud_sync_pending_ops
        WHERE id NOT IN (
          SELECT MAX(id) FROM cloud_sync_pending_ops
          GROUP BY workspace_id, resource_type, resource_id
        );
      `);
      db.exec(`
        CREATE UNIQUE INDEX IF NOT EXISTS idx_pending_ops_resource
          ON cloud_sync_pending_ops(workspace_id, resource_type, resource_id);
      `);
    }
  },
  {
    version: 22,
    name: "workspace_repo_and_commands",
    apply: (db) => {
      ensureColumn(db, "proxies", "resource_id", "resource_id TEXT");
      ensureColumn(db, "proxies", "uuid_in_scope", "uuid_in_scope TEXT");
      ensureColumn(db, "proxies", "origin_kind", "origin_kind TEXT");
      ensureColumn(db, "proxies", "origin_scope_key", "origin_scope_key TEXT");
      ensureColumn(db, "proxies", "origin_workspace_id", "origin_workspace_id TEXT");
      ensureColumn(db, "proxies", "copied_from_resource_id", "copied_from_resource_id TEXT");

      db.exec(`
        CREATE UNIQUE INDEX IF NOT EXISTS idx_proxy_resource_id
          ON proxies(resource_id) WHERE resource_id IS NOT NULL;

        CREATE TABLE IF NOT EXISTS workspace_repo_commits (
          workspace_id TEXT NOT NULL,
          commit_id TEXT NOT NULL,
          parent_commit_id TEXT,
          snapshot_id TEXT NOT NULL,
          author_name TEXT NOT NULL,
          author_kind TEXT NOT NULL,
          message TEXT NOT NULL,
          created_at TEXT NOT NULL,
          PRIMARY KEY (workspace_id, commit_id)
        );
        CREATE INDEX IF NOT EXISTS idx_workspace_repo_commits_created_at
          ON workspace_repo_commits(workspace_id, created_at DESC);

        CREATE TABLE IF NOT EXISTS workspace_repo_snapshots (
          workspace_id TEXT NOT NULL,
          snapshot_id TEXT NOT NULL,
          snapshot_json TEXT NOT NULL,
          created_at TEXT NOT NULL,
          PRIMARY KEY (workspace_id, snapshot_id)
        );

        CREATE TABLE IF NOT EXISTS workspace_repo_local_state (
          workspace_id TEXT PRIMARY KEY,
          local_head_commit_id TEXT,
          remote_head_commit_id TEXT,
          remote_commands_version TEXT,
          last_sync_at TEXT,
          last_error TEXT,
          sync_state TEXT NOT NULL DEFAULT 'idle'
        );

        CREATE TABLE IF NOT EXISTS workspace_repo_conflicts (
          workspace_id TEXT NOT NULL,
          resource_type TEXT NOT NULL,
          resource_id TEXT NOT NULL,
          display_name TEXT NOT NULL,
          local_snapshot_json TEXT,
          remote_snapshot_json TEXT,
          remote_deleted INTEGER NOT NULL DEFAULT 0,
          detected_at TEXT NOT NULL,
          PRIMARY KEY (workspace_id, resource_type, resource_id)
        );
        CREATE INDEX IF NOT EXISTS idx_workspace_repo_conflicts_detected_at
          ON workspace_repo_conflicts(workspace_id, detected_at DESC);

        CREATE TABLE IF NOT EXISTS workspace_commands (
          id TEXT NOT NULL,
          workspace_id TEXT NOT NULL,
          name TEXT NOT NULL,
          description TEXT,
          group_name TEXT NOT NULL DEFAULT '默认',
          command TEXT NOT NULL,
          is_template INTEGER NOT NULL DEFAULT 0,
          created_at TEXT NOT NULL,
          updated_at TEXT NOT NULL,
          PRIMARY KEY (workspace_id, id)
        );
        CREATE INDEX IF NOT EXISTS idx_workspace_commands_group
          ON workspace_commands(workspace_id, group_name, updated_at DESC);

        CREATE TABLE IF NOT EXISTS workspace_command_sync_state (
          workspace_id TEXT PRIMARY KEY,
          commands_version TEXT,
          updated_at TEXT
        );
      `);
    }
  }
];

export interface SshKeyRepository {
  list: () => SshKeyProfile[];
  getById: (id: string) => SshKeyProfile | undefined;
  save: (key: SshKeyProfile) => void;
  remove: (id: string) => void;
  getReferencingConnectionIds: (keyId: string) => string[];
}

export interface ProxyRepository {
  list: () => ProxyProfile[];
  getById: (id: string) => ProxyProfile | undefined;
  save: (proxy: ProxyProfile) => void;
  remove: (id: string) => void;
  getReferencingConnectionIds: (proxyId: string) => string[];
}

export interface ConnectionRepository {
  list: (query: ConnectionListQuery) => ConnectionProfile[];
  save: (connection: ConnectionProfile) => void;
  remove: (id: string) => void;
  getById: (id: string) => ConnectionProfile | undefined;
  seedIfEmpty: (connections: ConnectionProfile[]) => void;
  appendAuditLog: (payload: AppendAuditLogInput) => AuditLogRecord;
  listAuditLogs: (limit?: number) => AuditLogRecord[];
  clearAuditLogs: () => number;
  purgeExpiredAuditLogs: (retentionDays: number) => number;
  listMigrations: () => MigrationRecord[];
  listCommandHistory: () => CommandHistoryEntry[];
  pushCommandHistory: (command: string) => CommandHistoryEntry;
  removeCommandHistory: (command: string) => void;
  clearCommandHistory: () => void;
  listSavedCommands: (query: { keyword?: string; group?: string }) => SavedCommand[];
  upsertSavedCommand: (input: {
    id?: string;
    name: string;
    description?: string;
    group: string;
    command: string;
    isTemplate: boolean;
  }) => SavedCommand;
  removeSavedCommand: (id: string) => void;
  getAppPreferences: () => AppPreferences;
  saveAppPreferences: (preferences: AppPreferences) => AppPreferences;
  getJsonSetting: <T = unknown>(key: string) => T | undefined;
  saveJsonSetting: (key: string, value: unknown) => void;
  removeSetting: (key: string) => void;
  // ── Cloud Sync v2: multi-workspace ──
  listCloudSyncWorkspaces: () => CloudSyncWorkspaceProfile[];
  getCloudSyncWorkspace: (id: string) => CloudSyncWorkspaceProfile | undefined;
  saveCloudSyncWorkspace: (ws: CloudSyncWorkspaceProfile) => void;
  removeCloudSyncWorkspace: (id: string) => void;
  // ── Cloud Sync v2: workspace-scoped resource state ──
  listResourceStatesV2: (workspaceId: string) => CloudSyncResourceStateV2[];
  getResourceStateV2: (workspaceId: string, resourceType: string, resourceId: string) => CloudSyncResourceStateV2 | undefined;
  saveResourceStateV2: (state: CloudSyncResourceStateV2) => void;
  removeResourceStateV2: (workspaceId: string, resourceType: string, resourceId: string) => void;
  clearResourceStatesV2: (workspaceId: string) => void;
  // ── Cloud Sync v2: pending ops ──
  listPendingOps: (workspaceId: string) => CloudSyncPendingOp[];
  savePendingOp: (op: CloudSyncPendingOp) => number;
  upsertPendingOp: (op: CloudSyncPendingOp) => number;
  updatePendingOp: (op: CloudSyncPendingOp) => void;
  removePendingOp: (id: number) => void;
  clearPendingOps: (workspaceId: string) => void;
  // ── Cloud Sync v2: runtime state persistence ──
  getRuntimeCurrentVersion: (workspaceId: string) => number | null;
  saveRuntimeCurrentVersion: (workspaceId: string, currentVersion: number) => void;
  removeRuntimeCurrentVersion: (workspaceId: string) => void;
  // ── Workspace repo ──
  listWorkspaceRepoCommits: (workspaceId: string, limit?: number, cursorCreatedAt?: string) => WorkspaceRepoCommitMeta[];
  getWorkspaceRepoCommit: (workspaceId: string, commitId: string) => WorkspaceRepoCommitMeta | undefined;
  saveWorkspaceRepoCommit: (commit: WorkspaceRepoCommitMeta) => void;
  getWorkspaceRepoSnapshot: (workspaceId: string, snapshotId: string) => WorkspaceRepoSnapshot | undefined;
  saveWorkspaceRepoSnapshot: (snapshot: WorkspaceRepoSnapshot) => void;
  getWorkspaceRepoLocalState: (workspaceId: string) => WorkspaceRepoLocalState | undefined;
  saveWorkspaceRepoLocalState: (state: WorkspaceRepoLocalState) => void;
  listWorkspaceRepoConflicts: (workspaceId: string) => WorkspaceRepoConflict[];
  saveWorkspaceRepoConflict: (conflict: WorkspaceRepoConflict) => void;
  removeWorkspaceRepoConflict: (workspaceId: string, resourceType: string, resourceId: string) => void;
  clearWorkspaceRepoConflicts: (workspaceId: string) => void;
  // ── Workspace commands ──
  listWorkspaceCommands: (workspaceId: string) => WorkspaceCommandItem[];
  replaceWorkspaceCommands: (workspaceId: string, commands: WorkspaceCommandItem[]) => void;
  upsertWorkspaceCommand: (command: WorkspaceCommandItem) => WorkspaceCommandItem;
  removeWorkspaceCommand: (workspaceId: string, id: string) => void;
  getWorkspaceCommandsVersion: (workspaceId: string) => string | undefined;
  saveWorkspaceCommandsVersion: (workspaceId: string, version: string) => void;
  // ── Recycle bin ──
  listRecycleBinEntries: () => RecycleBinEntry[];
  getRecycleBinEntry: (id: string) => RecycleBinEntry | undefined;
  saveRecycleBinEntry: (entry: RecycleBinEntry) => void;
  removeRecycleBinEntry: (id: string) => void;
  clearRecycleBin: () => number;
  getMasterKeyMeta: () => MasterKeyMeta | undefined;
  saveMasterKeyMeta: (meta: MasterKeyMeta) => void;
  getDeviceKey: () => string | undefined;
  saveDeviceKey: (key: string) => void;
  getSecretStore: () => SecretStoreDB;
  listTemplateParams: (commandId?: string) => CommandTemplateParam[];
  upsertTemplateParams: (commandId: string, params: Record<string, string>) => void;
  clearTemplateParams: (commandId: string) => void;
  backupDatabase: (targetPath: string) => Promise<void>;
  getDbPath: () => string;
  close: () => void;
}

export class SQLiteConnectionRepository implements ConnectionRepository {
  private readonly db: Database.Database;
  private readonly resolvedDbPath: string;
  private secretStoreInstance: SQLiteSecretStore | undefined;
  private readonly readonly: boolean;

  constructor(dbPath: string, options?: { readonly?: boolean; fileMustExist?: boolean }) {
    const DatabaseCtor = loadDatabaseDriver();
    const resolved = path.resolve(dbPath);
    this.resolvedDbPath = resolved;
    this.readonly = options?.readonly ?? false;
    this.db = new DatabaseCtor(
      resolved,
      this.readonly
        ? {
            readonly: true,
            fileMustExist: options?.fileMustExist ?? true
          }
        : undefined
    );
    if (!this.readonly) {
      this.bootstrap();
    }
  }

  private bootstrap(): void {
    this.db.pragma("journal_mode = WAL");
    this.db.pragma("foreign_keys = ON");

    this.db.exec(`
      CREATE TABLE IF NOT EXISTS schema_migrations (
        version INTEGER PRIMARY KEY,
        name TEXT NOT NULL,
        applied_at TEXT NOT NULL
      );
    `);

    const applied = new Set(
      (
        this.db.prepare("SELECT version FROM schema_migrations ORDER BY version ASC").all() as Array<{
          version: number;
        }>
      ).map((row) => row.version)
    );

    for (const migration of migrations.sort((a, b) => a.version - b.version)) {
      if (applied.has(migration.version)) {
        continue;
      }

      const tx = this.db.transaction(() => {
        migration.apply(this.db);
        this.db.prepare(
          `
            INSERT INTO schema_migrations (version, name, applied_at)
            VALUES (@version, @name, @applied_at)
          `
        ).run({
          version: migration.version,
          name: migration.name,
          applied_at: new Date().toISOString()
        });
      });

      tx();
    }

    if (hasTable(this.db, "connections")) {
      ensureColumn(this.db, "connections", "keepalive_enabled", "keepalive_enabled INTEGER");
      ensureColumn(this.db, "connections", "keepalive_interval_sec", "keepalive_interval_sec INTEGER");
    }
  }

  seedIfEmpty(connections: ConnectionProfile[]): void {
    const row = this.db.prepare("SELECT COUNT(*) AS total FROM connections").get() as {
      total: number;
    };

    if (row.total > 0) {
      return;
    }

    const tx = this.db.transaction((items: ConnectionProfile[]) => {
      for (const item of items) {
        this.save(item);
      }
    });

    tx(connections);
  }

  list(query: ConnectionListQuery): ConnectionProfile[] {
    const keyword = query.keyword?.trim().toLowerCase();
    const keywordLike = keyword ? `%${keyword}%` : null;
    const group = query.group?.trim() || null;
    const favorite = query.favoriteOnly ? 1 : null;

    const rows = this.db
      .prepare(
        `
          SELECT
            id,
            name,
            host,
            port,
            username,
            auth_type,
            credential_ref,
            ssh_key_id,
            host_fingerprint,
            strict_host_key_checking,
            proxy_id,
            keepalive_enabled,
            keepalive_interval_sec,
            terminal_encoding,
            backspace_mode,
            delete_mode,
            group_path,
            tags,
            notes,
            favorite,
            monitor_session,
            created_at,
            updated_at,
            last_connected_at,
            resource_id,
            uuid_in_scope,
            origin_kind,
            origin_scope_key,
            origin_workspace_id,
            ssh_key_resource_id,
            copied_from_resource_id
          FROM connections
          WHERE (@favorite IS NULL OR favorite = @favorite)
            AND (@group IS NULL OR (group_path = @group OR group_path LIKE @group || '/%'))
            AND (
              @keywordLike IS NULL
              OR lower(name || ' ' || host || ' ' || tags || ' ' || group_path || ' ' || ifnull(notes, '')) LIKE @keywordLike
            )
          ORDER BY favorite DESC, name ASC
        `
      )
      .all({
        favorite,
        group,
        keywordLike
      }) as ConnectionRow[];

    return rows.map(rowToConnection);
  }

  save(connection: ConnectionProfile): void {
    this.db
      .prepare(
        `
          INSERT INTO connections (
            id,
            name,
            host,
            port,
            username,
            auth_type,
            credential_ref,
            ssh_key_id,
            host_fingerprint,
            strict_host_key_checking,
            proxy_id,
            keepalive_enabled,
            keepalive_interval_sec,
            terminal_encoding,
            backspace_mode,
            delete_mode,
            group_path,
            tags,
            notes,
            favorite,
            monitor_session,
            created_at,
            updated_at,
            last_connected_at,
            resource_id,
            uuid_in_scope,
            origin_kind,
            origin_scope_key,
            origin_workspace_id,
            ssh_key_resource_id,
            copied_from_resource_id
          ) VALUES (
            @id,
            @name,
            @host,
            @port,
            @username,
            @auth_type,
            @credential_ref,
            @ssh_key_id,
            @host_fingerprint,
            @strict_host_key_checking,
            @proxy_id,
            @keepalive_enabled,
            @keepalive_interval_sec,
            @terminal_encoding,
            @backspace_mode,
            @delete_mode,
            @group_path,
            @tags,
            @notes,
            @favorite,
            @monitor_session,
            @created_at,
            @updated_at,
            @last_connected_at,
            @resource_id,
            @uuid_in_scope,
            @origin_kind,
            @origin_scope_key,
            @origin_workspace_id,
            @ssh_key_resource_id,
            @copied_from_resource_id
          )
          ON CONFLICT(id) DO UPDATE SET
            name = excluded.name,
            host = excluded.host,
            port = excluded.port,
            username = excluded.username,
            auth_type = excluded.auth_type,
            credential_ref = excluded.credential_ref,
            ssh_key_id = excluded.ssh_key_id,
            host_fingerprint = excluded.host_fingerprint,
            strict_host_key_checking = excluded.strict_host_key_checking,
            proxy_id = excluded.proxy_id,
            keepalive_enabled = excluded.keepalive_enabled,
            keepalive_interval_sec = excluded.keepalive_interval_sec,
            terminal_encoding = excluded.terminal_encoding,
            backspace_mode = excluded.backspace_mode,
            delete_mode = excluded.delete_mode,
            group_path = excluded.group_path,
            tags = excluded.tags,
            notes = excluded.notes,
            favorite = excluded.favorite,
            monitor_session = excluded.monitor_session,
            created_at = excluded.created_at,
            updated_at = excluded.updated_at,
            last_connected_at = excluded.last_connected_at,
            resource_id = excluded.resource_id,
            uuid_in_scope = excluded.uuid_in_scope,
            origin_kind = excluded.origin_kind,
            origin_scope_key = excluded.origin_scope_key,
            origin_workspace_id = excluded.origin_workspace_id,
            ssh_key_resource_id = excluded.ssh_key_resource_id,
            copied_from_resource_id = excluded.copied_from_resource_id
        `
      )
      .run({
        id: connection.id,
        name: connection.name,
        host: connection.host,
        port: connection.port,
        username: connection.username,
        auth_type: connection.authType,
        credential_ref: connection.credentialRef ?? null,
        ssh_key_id: connection.sshKeyId ?? null,
        host_fingerprint: connection.hostFingerprint ?? null,
        strict_host_key_checking: connection.strictHostKeyChecking ? 1 : 0,
        proxy_id: connection.proxyId ?? null,
        keepalive_enabled:
          connection.keepAliveEnabled === undefined ? null : connection.keepAliveEnabled ? 1 : 0,
        keepalive_interval_sec: connection.keepAliveIntervalSec ?? null,
        terminal_encoding: connection.terminalEncoding,
        backspace_mode: connection.backspaceMode,
        delete_mode: connection.deleteMode,
        group_path: connection.groupPath,
        tags: toJSON(connection.tags),
        notes: connection.notes ?? null,
        favorite: connection.favorite ? 1 : 0,
        monitor_session: connection.monitorSession ? 1 : 0,
        created_at: connection.createdAt,
        updated_at: connection.updatedAt,
        last_connected_at: connection.lastConnectedAt ?? null,
        resource_id: connection.resourceId ?? null,
        uuid_in_scope: connection.uuidInScope ?? null,
        origin_kind: connection.originKind ?? "local",
        origin_scope_key: connection.originScopeKey ?? LOCAL_DEFAULT_SCOPE_KEY,
        origin_workspace_id: connection.originWorkspaceId ?? null,
        ssh_key_resource_id: connection.sshKeyResourceId ?? null,
        copied_from_resource_id: connection.copiedFromResourceId ?? null
      });
  }

  remove(id: string): void {
    this.db.prepare("DELETE FROM connections WHERE id = ?").run(id);
  }

  getById(id: string): ConnectionProfile | undefined {
    const row = this.db
      .prepare(
        `
          SELECT
            id,
            name,
            host,
            port,
            username,
            auth_type,
            credential_ref,
            ssh_key_id,
            host_fingerprint,
            strict_host_key_checking,
            proxy_id,
            keepalive_enabled,
            keepalive_interval_sec,
            terminal_encoding,
            backspace_mode,
            delete_mode,
            group_path,
            tags,
            notes,
            favorite,
            monitor_session,
            created_at,
            updated_at,
            last_connected_at,
            resource_id,
            uuid_in_scope,
            origin_kind,
            origin_scope_key,
            origin_workspace_id,
            ssh_key_resource_id,
            copied_from_resource_id
          FROM connections
          WHERE id = ?
        `
      )
      .get(id) as ConnectionRow | undefined;

    if (!row) {
      return undefined;
    }

    return rowToConnection(row);
  }

  appendAuditLog(payload: AppendAuditLogInput): AuditLogRecord {
    const record: AuditLogRecord = {
      id: randomUUID(),
      action: payload.action,
      level: payload.level,
      connectionId: payload.connectionId,
      message: payload.message,
      metadata: payload.metadata,
      createdAt: new Date().toISOString()
    };

    this.db.prepare(
      `
        INSERT INTO audit_logs (
          id,
          action,
          level,
          connection_id,
          message,
          metadata_json,
          created_at
        ) VALUES (
          @id,
          @action,
          @level,
          @connection_id,
          @message,
          @metadata_json,
          @created_at
        )
      `
    ).run({
      id: record.id,
      action: record.action,
      level: record.level,
      connection_id: record.connectionId ?? null,
      message: record.message,
      metadata_json: toMetadataJSON(record.metadata),
      created_at: record.createdAt
    });

    return record;
  }

  appendAuditLogs(payloads: AppendAuditLogInput[]): void {
    if (payloads.length === 0) {
      return;
    }

    const insertAuditLog = this.db.prepare(
      `
        INSERT INTO audit_logs (
          id,
          action,
          level,
          connection_id,
          message,
          metadata_json,
          created_at
        ) VALUES (
          @id,
          @action,
          @level,
          @connection_id,
          @message,
          @metadata_json,
          @created_at
        )
      `
    );

    const tx = this.db.transaction((batch: AppendAuditLogInput[]) => {
      for (const payload of batch) {
        insertAuditLog.run({
          id: randomUUID(),
          action: payload.action,
          level: payload.level,
          connection_id: payload.connectionId ?? null,
          message: payload.message,
          metadata_json: toMetadataJSON(payload.metadata),
          created_at: new Date().toISOString()
        });
      }
    });

    tx(payloads);
  }

  listAuditLogs(limit = 100): AuditLogRecord[] {
    const rows = this.db.prepare(
      `
        SELECT
          id,
          action,
          level,
          connection_id,
          message,
          metadata_json,
          created_at
        FROM audit_logs
        ORDER BY created_at DESC
        LIMIT @limit
      `
    ).all({ limit }) as AuditLogRow[];

    return rows.map(rowToAuditLog);
  }

  clearAuditLogs(): number {
    const result = this.db.prepare("DELETE FROM audit_logs").run();
    return result.changes;
  }

  purgeExpiredAuditLogs(retentionDays: number): number {
    if (retentionDays <= 0) return 0;
    const cutoff = new Date(Date.now() - retentionDays * 86_400_000).toISOString();
    const result = this.db.prepare(
      "DELETE FROM audit_logs WHERE created_at < @cutoff"
    ).run({ cutoff });
    return result.changes;
  }

  listMigrations(): MigrationRecord[] {
    const rows = this.db.prepare(
      `
        SELECT version, name, applied_at
        FROM schema_migrations
        ORDER BY version ASC
      `
    ).all() as MigrationRow[];

    return rows.map(rowToMigration);
  }

  private readonly MAX_COMMAND_HISTORY = 500;

  listCommandHistory(): CommandHistoryEntry[] {
    const rows = this.db.prepare(
      `
        SELECT command, use_count, last_used_at
        FROM command_history
        ORDER BY last_used_at DESC
      `
    ).all() as CommandHistoryRow[];

    return rows.map(rowToCommandHistory);
  }

  pushCommandHistory(command: string): CommandHistoryEntry {
    const now = new Date().toISOString();

    this.db.prepare(
      `
        INSERT INTO command_history (command, use_count, last_used_at)
        VALUES (@command, 1, @now)
        ON CONFLICT(command) DO UPDATE SET
          use_count = use_count + 1,
          last_used_at = @now
      `
    ).run({ command, now });

    this.evictCommandHistory();

    const row = this.db.prepare(
      "SELECT command, use_count, last_used_at FROM command_history WHERE command = ?"
    ).get(command) as CommandHistoryRow;

    return rowToCommandHistory(row);
  }

  removeCommandHistory(command: string): void {
    this.db.prepare("DELETE FROM command_history WHERE command = ?").run(command);
  }

  clearCommandHistory(): void {
    this.db.exec("DELETE FROM command_history");
  }

  applyCommandHistoryBatch(mutations: CommandHistoryMutationInput[]): void {
    if (mutations.length === 0) {
      return;
    }

    const upsertCommand = this.db.prepare(
      `
        INSERT INTO command_history (command, use_count, last_used_at)
        VALUES (@command, 1, @now)
        ON CONFLICT(command) DO UPDATE SET
          use_count = use_count + 1,
          last_used_at = @now
      `
    );
    const removeCommand = this.db.prepare("DELETE FROM command_history WHERE command = ?");
    const clearCommands = this.db.prepare("DELETE FROM command_history");

    const tx = this.db.transaction((batch: CommandHistoryMutationInput[]) => {
      for (const mutation of batch) {
        if (mutation.type === "push") {
          upsertCommand.run({ command: mutation.command, now: new Date().toISOString() });
        } else if (mutation.type === "remove") {
          removeCommand.run(mutation.command);
        } else {
          clearCommands.run();
        }
      }

      this.evictCommandHistory();
    });

    tx(mutations);
  }

  private evictCommandHistory(): void {
    const countRow = this.db.prepare(
      "SELECT COUNT(*) AS total FROM command_history"
    ).get() as { total: number };

    if (countRow.total <= this.MAX_COMMAND_HISTORY) {
      return;
    }

    const excess = countRow.total - this.MAX_COMMAND_HISTORY;
    this.db.prepare(
      `
        DELETE FROM command_history
        WHERE command IN (
          SELECT command FROM command_history
          ORDER BY use_count ASC, last_used_at ASC
          LIMIT @excess
        )
      `
    ).run({ excess });
  }

  listSavedCommands(query: { keyword?: string; group?: string }): SavedCommand[] {
    const keyword = query.keyword?.trim().toLowerCase();
    const keywordLike = keyword ? `%${keyword}%` : null;
    const group = query.group?.trim() || null;

    let sql = `
      SELECT id, name, description, group_name, command, is_template, created_at, updated_at
      FROM saved_commands
      WHERE 1=1
    `;
    const params: Record<string, string | number> = {};
    if (keywordLike) {
      sql += " AND (LOWER(name) LIKE @keyword OR LOWER(command) LIKE @keyword OR LOWER(description) LIKE @keyword)";
      params.keyword = keywordLike;
    }
    if (group) {
      sql += " AND group_name = @group";
      params.group = group;
    }
    sql += " ORDER BY group_name ASC, updated_at DESC";

    const rows = this.db.prepare(sql).all(params) as SavedCommandRow[];
    return rows.map(rowToSavedCommand);
  }

  upsertSavedCommand(input: {
    id?: string;
    name: string;
    description?: string;
    group: string;
    command: string;
    isTemplate: boolean;
  }): SavedCommand {
    const now = new Date().toISOString();
    const id = input.id ?? randomUUID();
    const description = input.description?.trim() || null;
    const groupName = input.group.trim() || "默认";

    this.db.prepare(
      `
        INSERT INTO saved_commands (id, name, description, group_name, command, is_template, created_at, updated_at)
        VALUES (@id, @name, @description, @groupName, @command, @isTemplate, @now, @now)
        ON CONFLICT(id) DO UPDATE SET
          name = @name,
          description = @description,
          group_name = @groupName,
          command = @command,
          is_template = @isTemplate,
          updated_at = @now
      `
    ).run({
      id,
      name: input.name.trim(),
      description,
      groupName,
      command: input.command.trim(),
      isTemplate: input.isTemplate ? 1 : 0,
      now
    });

    const row = this.db.prepare(
      "SELECT id, name, description, group_name, command, is_template, created_at, updated_at FROM saved_commands WHERE id = ?"
    ).get(id) as SavedCommandRow;

    return rowToSavedCommand(row);
  }

  removeSavedCommand(id: string): void {
    this.db.prepare("DELETE FROM saved_commands WHERE id = ?").run(id);
  }

  getAppPreferences(): AppPreferences {
    const row = this.db.prepare(
      "SELECT key, value_json, updated_at FROM app_settings WHERE key = ?"
    ).get("app_preferences") as AppSettingRow | undefined;

    return parseAppPreferences(row?.value_json ?? null);
  }

  saveAppPreferences(preferences: AppPreferences): AppPreferences {
    const now = new Date().toISOString();
    const normalized = parseAppPreferences(JSON.stringify(preferences));

    this.db.prepare(
      `
        INSERT INTO app_settings (key, value_json, updated_at)
        VALUES (@key, @value_json, @updated_at)
        ON CONFLICT(key) DO UPDATE SET
          value_json = excluded.value_json,
          updated_at = excluded.updated_at
      `
    ).run({
      key: "app_preferences",
      value_json: JSON.stringify(normalized),
      updated_at: now
    });

    return normalized;
  }

  getJsonSetting<T = unknown>(key: string): T | undefined {
    const row = this.db.prepare(
      "SELECT value_json FROM app_settings WHERE key = ?"
    ).get(key) as { value_json: string } | undefined;

    if (!row?.value_json) {
      return undefined;
    }

    try {
      return JSON.parse(row.value_json) as T;
    } catch {
      return undefined;
    }
  }

  saveJsonSetting(key: string, value: unknown): void {
    const now = new Date().toISOString();
    this.db.prepare(
      `
        INSERT INTO app_settings (key, value_json, updated_at)
        VALUES (@key, @value_json, @updated_at)
        ON CONFLICT(key) DO UPDATE SET
          value_json = excluded.value_json,
          updated_at = excluded.updated_at
      `
    ).run({
      key,
      value_json: JSON.stringify(value),
      updated_at: now
    });
  }

  removeSetting(key: string): void {
    this.db.prepare("DELETE FROM app_settings WHERE key = ?").run(key);
  }

  // ── Cloud Sync v2: workspace-scoped resource state ──
  listResourceStatesV2(workspaceId: string): CloudSyncResourceStateV2[] {
    const rows = this.db.prepare(
      `SELECT workspace_id, resource_type, resource_id, server_revision,
              conflict_remote_revision, conflict_remote_payload_json,
              conflict_remote_updated_at, conflict_remote_deleted, conflict_detected_at
       FROM cloud_sync_resource_state
       WHERE workspace_id = ?
       ORDER BY resource_type ASC, resource_id ASC`
    ).all(workspaceId) as CloudSyncResourceStateV2Row[];
    return rows.map(rowToCloudSyncResourceStateV2);
  }

  getResourceStateV2(workspaceId: string, resourceType: string, resourceId: string): CloudSyncResourceStateV2 | undefined {
    const row = this.db.prepare(
      `SELECT workspace_id, resource_type, resource_id, server_revision,
              conflict_remote_revision, conflict_remote_payload_json,
              conflict_remote_updated_at, conflict_remote_deleted, conflict_detected_at
       FROM cloud_sync_resource_state
       WHERE workspace_id = ? AND resource_type = ? AND resource_id = ?`
    ).get(workspaceId, resourceType, resourceId) as CloudSyncResourceStateV2Row | undefined;
    return row ? rowToCloudSyncResourceStateV2(row) : undefined;
  }

  saveResourceStateV2(state: CloudSyncResourceStateV2): void {
    this.db.prepare(
      `INSERT INTO cloud_sync_resource_state (
         workspace_id, resource_type, resource_id, server_revision,
         conflict_remote_revision, conflict_remote_payload_json,
         conflict_remote_updated_at, conflict_remote_deleted, conflict_detected_at
       ) VALUES (
         @workspace_id, @resource_type, @resource_id, @server_revision,
         @conflict_remote_revision, @conflict_remote_payload_json,
         @conflict_remote_updated_at, @conflict_remote_deleted, @conflict_detected_at
       )
       ON CONFLICT(workspace_id, resource_type, resource_id) DO UPDATE SET
         server_revision = excluded.server_revision,
         conflict_remote_revision = excluded.conflict_remote_revision,
         conflict_remote_payload_json = excluded.conflict_remote_payload_json,
         conflict_remote_updated_at = excluded.conflict_remote_updated_at,
         conflict_remote_deleted = excluded.conflict_remote_deleted,
         conflict_detected_at = excluded.conflict_detected_at`
    ).run({
      workspace_id: state.workspaceId,
      resource_type: state.resourceType,
      resource_id: state.resourceId,
      server_revision: state.serverRevision ?? null,
      conflict_remote_revision: state.conflictRemoteRevision ?? null,
      conflict_remote_payload_json: state.conflictRemotePayloadJson ?? null,
      conflict_remote_updated_at: state.conflictRemoteUpdatedAt ?? null,
      conflict_remote_deleted: state.conflictRemoteDeleted ? 1 : 0,
      conflict_detected_at: state.conflictDetectedAt ?? null
    });
  }

  removeResourceStateV2(workspaceId: string, resourceType: string, resourceId: string): void {
    this.db.prepare(
      "DELETE FROM cloud_sync_resource_state WHERE workspace_id = ? AND resource_type = ? AND resource_id = ?"
    ).run(workspaceId, resourceType, resourceId);
  }

  clearResourceStatesV2(workspaceId: string): void {
    this.db.prepare("DELETE FROM cloud_sync_resource_state WHERE workspace_id = ?").run(workspaceId);
  }

  // ── Cloud Sync v2: workspace management ──
  listCloudSyncWorkspaces(): CloudSyncWorkspaceProfile[] {
    const rows = this.db.prepare(
      "SELECT id, api_base_url, workspace_name, display_name, pull_interval_sec, ignore_tls_errors, enabled, created_at, updated_at, last_sync_at, last_error FROM cloud_sync_workspaces ORDER BY display_name ASC"
    ).all() as CloudSyncWorkspaceRow[];
    return rows.map(rowToCloudSyncWorkspace);
  }

  getCloudSyncWorkspace(id: string): CloudSyncWorkspaceProfile | undefined {
    const row = this.db.prepare(
      "SELECT id, api_base_url, workspace_name, display_name, pull_interval_sec, ignore_tls_errors, enabled, created_at, updated_at, last_sync_at, last_error FROM cloud_sync_workspaces WHERE id = ?"
    ).get(id) as CloudSyncWorkspaceRow | undefined;
    return row ? rowToCloudSyncWorkspace(row) : undefined;
  }

  saveCloudSyncWorkspace(ws: CloudSyncWorkspaceProfile): void {
    this.db.prepare(
      `INSERT INTO cloud_sync_workspaces (id, api_base_url, workspace_name, display_name, pull_interval_sec, ignore_tls_errors, enabled, created_at, updated_at, last_sync_at, last_error)
       VALUES (@id, @api_base_url, @workspace_name, @display_name, @pull_interval_sec, @ignore_tls_errors, @enabled, @created_at, @updated_at, @last_sync_at, @last_error)
       ON CONFLICT(id) DO UPDATE SET
         api_base_url = excluded.api_base_url,
         workspace_name = excluded.workspace_name,
         display_name = excluded.display_name,
         pull_interval_sec = excluded.pull_interval_sec,
         ignore_tls_errors = excluded.ignore_tls_errors,
         enabled = excluded.enabled,
         updated_at = excluded.updated_at,
         last_sync_at = excluded.last_sync_at,
         last_error = excluded.last_error`
    ).run({
      id: ws.id,
      api_base_url: ws.apiBaseUrl,
      workspace_name: ws.workspaceName,
      display_name: ws.displayName,
      pull_interval_sec: ws.pullIntervalSec,
      ignore_tls_errors: ws.ignoreTlsErrors ? 1 : 0,
      enabled: ws.enabled ? 1 : 0,
      created_at: ws.createdAt,
      updated_at: ws.updatedAt,
      last_sync_at: ws.lastSyncAt,
      last_error: ws.lastError
    });
  }

  removeCloudSyncWorkspace(id: string): void {
    // Atomically clean up workspace + associated data
    this.db.transaction(() => {
      this.db.prepare("DELETE FROM cloud_sync_pending_ops WHERE workspace_id = ?").run(id);
      this.db.prepare("DELETE FROM cloud_sync_resource_state WHERE workspace_id = ?").run(id);
      this.db.prepare("DELETE FROM cloud_sync_runtime_state WHERE workspace_id = ?").run(id);
      this.db.prepare("DELETE FROM workspace_repo_commits WHERE workspace_id = ?").run(id);
      this.db.prepare("DELETE FROM workspace_repo_snapshots WHERE workspace_id = ?").run(id);
      this.db.prepare("DELETE FROM workspace_repo_local_state WHERE workspace_id = ?").run(id);
      this.db.prepare("DELETE FROM workspace_repo_conflicts WHERE workspace_id = ?").run(id);
      this.db.prepare("DELETE FROM workspace_commands WHERE workspace_id = ?").run(id);
      this.db.prepare("DELETE FROM workspace_command_sync_state WHERE workspace_id = ?").run(id);
      this.db.prepare("DELETE FROM cloud_sync_workspaces WHERE id = ?").run(id);
    })();
  }

  // ── Cloud Sync v2: pending ops ──
  listPendingOps(workspaceId: string): CloudSyncPendingOp[] {
    const rows = this.db.prepare(
      "SELECT id, workspace_id, resource_type, resource_id, action, base_revision, force, payload_json, queued_at, last_attempt_at, last_error FROM cloud_sync_pending_ops WHERE workspace_id = ? ORDER BY id ASC"
    ).all(workspaceId) as PendingOpRow[];
    return rows.map(rowToPendingOp);
  }

  savePendingOp(op: CloudSyncPendingOp): number {
    const result = this.db.prepare(
      `INSERT INTO cloud_sync_pending_ops (workspace_id, resource_type, resource_id, action, base_revision, force, payload_json, queued_at, last_attempt_at, last_error)
       VALUES (@workspace_id, @resource_type, @resource_id, @action, @base_revision, @force, @payload_json, @queued_at, @last_attempt_at, @last_error)`
    ).run({
      workspace_id: op.workspaceId,
      resource_type: op.resourceType,
      resource_id: op.resourceId,
      action: op.action,
      base_revision: op.baseRevision,
      force: op.force ? 1 : 0,
      payload_json: op.payloadJson ?? null,
      queued_at: op.queuedAt,
      last_attempt_at: op.lastAttemptAt ?? null,
      last_error: op.lastError ?? null
    });
    return Number(result.lastInsertRowid);
  }

  upsertPendingOp(op: CloudSyncPendingOp): number {
    const result = this.db.prepare(
      `INSERT INTO cloud_sync_pending_ops (workspace_id, resource_type, resource_id, action, base_revision, force, payload_json, queued_at, last_attempt_at, last_error)
       VALUES (@workspace_id, @resource_type, @resource_id, @action, @base_revision, @force, @payload_json, @queued_at, @last_attempt_at, @last_error)
       ON CONFLICT(workspace_id, resource_type, resource_id) DO UPDATE SET
         action = CASE
           WHEN excluded.action = 'delete' THEN 'delete'
           ELSE excluded.action
         END,
         force = MAX(cloud_sync_pending_ops.force, excluded.force),
         queued_at = excluded.queued_at,
         last_attempt_at = NULL,
         last_error = NULL`
    ).run({
      workspace_id: op.workspaceId,
      resource_type: op.resourceType,
      resource_id: op.resourceId,
      action: op.action,
      base_revision: op.baseRevision,
      force: op.force ? 1 : 0,
      payload_json: op.payloadJson ?? null,
      queued_at: op.queuedAt,
      last_attempt_at: op.lastAttemptAt ?? null,
      last_error: op.lastError ?? null
    });
    return Number(result.lastInsertRowid);
  }

  updatePendingOp(op: CloudSyncPendingOp): void {
    if (op.id == null) return;
    this.db.prepare(
      `UPDATE cloud_sync_pending_ops SET
         last_attempt_at = @last_attempt_at,
         last_error = @last_error,
         force = @force
       WHERE id = @id`
    ).run({
      id: op.id,
      last_attempt_at: op.lastAttemptAt ?? null,
      last_error: op.lastError ?? null,
      force: op.force ? 1 : 0
    });
  }

  removePendingOp(id: number): void {
    this.db.prepare("DELETE FROM cloud_sync_pending_ops WHERE id = ?").run(id);
  }

  clearPendingOps(workspaceId: string): void {
    this.db.prepare("DELETE FROM cloud_sync_pending_ops WHERE workspace_id = ?").run(workspaceId);
  }

  // ── Cloud Sync v2: runtime state persistence ──
  getRuntimeCurrentVersion(workspaceId: string): number | null {
    const row = this.db.prepare(
      "SELECT current_version FROM cloud_sync_runtime_state WHERE workspace_id = ?"
    ).get(workspaceId) as { current_version: number } | undefined;
    return row?.current_version ?? null;
  }

  saveRuntimeCurrentVersion(workspaceId: string, currentVersion: number): void {
    this.db.prepare(
      `INSERT INTO cloud_sync_runtime_state (workspace_id, current_version) VALUES (?, ?)
       ON CONFLICT(workspace_id) DO UPDATE SET current_version = excluded.current_version`
    ).run(workspaceId, currentVersion);
  }

  removeRuntimeCurrentVersion(workspaceId: string): void {
    this.db.prepare("DELETE FROM cloud_sync_runtime_state WHERE workspace_id = ?").run(workspaceId);
  }

  // ── Workspace repo ──
  listWorkspaceRepoCommits(
    workspaceId: string,
    limit = 50,
    cursorCreatedAt?: string,
  ): WorkspaceRepoCommitMeta[] {
    const safeLimit = Math.max(1, Math.min(limit, 500));
    const rows = this.db.prepare(
      `
        SELECT workspace_id, commit_id, parent_commit_id, snapshot_id, author_name, author_kind, message, created_at
        FROM workspace_repo_commits
        WHERE workspace_id = @workspace_id
          AND (@cursor_created_at IS NULL OR created_at < @cursor_created_at)
        ORDER BY created_at DESC
        LIMIT @limit
      `
    ).all({
      workspace_id: workspaceId,
      cursor_created_at: cursorCreatedAt ?? null,
      limit: safeLimit
    }) as WorkspaceRepoCommitRow[];
    return rows.map(rowToWorkspaceRepoCommit);
  }

  getWorkspaceRepoCommit(workspaceId: string, commitId: string): WorkspaceRepoCommitMeta | undefined {
    const row = this.db.prepare(
      `
        SELECT workspace_id, commit_id, parent_commit_id, snapshot_id, author_name, author_kind, message, created_at
        FROM workspace_repo_commits
        WHERE workspace_id = ? AND commit_id = ?
      `
    ).get(workspaceId, commitId) as WorkspaceRepoCommitRow | undefined;
    return row ? rowToWorkspaceRepoCommit(row) : undefined;
  }

  saveWorkspaceRepoCommit(commit: WorkspaceRepoCommitMeta): void {
    this.db.prepare(
      `
        INSERT INTO workspace_repo_commits (
          workspace_id,
          commit_id,
          parent_commit_id,
          snapshot_id,
          author_name,
          author_kind,
          message,
          created_at
        ) VALUES (
          @workspace_id,
          @commit_id,
          @parent_commit_id,
          @snapshot_id,
          @author_name,
          @author_kind,
          @message,
          @created_at
        )
        ON CONFLICT(workspace_id, commit_id) DO UPDATE SET
          parent_commit_id = excluded.parent_commit_id,
          snapshot_id = excluded.snapshot_id,
          author_name = excluded.author_name,
          author_kind = excluded.author_kind,
          message = excluded.message,
          created_at = excluded.created_at
      `
    ).run({
      workspace_id: commit.workspaceId,
      commit_id: commit.commitId,
      parent_commit_id: commit.parentCommitId ?? null,
      snapshot_id: commit.snapshotId,
      author_name: commit.authorName,
      author_kind: commit.authorKind,
      message: commit.message,
      created_at: commit.createdAt
    });
  }

  getWorkspaceRepoSnapshot(workspaceId: string, snapshotId: string): WorkspaceRepoSnapshot | undefined {
    const row = this.db.prepare(
      `
        SELECT workspace_id, snapshot_id, snapshot_json, created_at
        FROM workspace_repo_snapshots
        WHERE workspace_id = ? AND snapshot_id = ?
      `
    ).get(workspaceId, snapshotId) as WorkspaceRepoSnapshotRow | undefined;
    return row ? rowToWorkspaceRepoSnapshot(row) : undefined;
  }

  saveWorkspaceRepoSnapshot(snapshot: WorkspaceRepoSnapshot): void {
    this.db.prepare(
      `
        INSERT INTO workspace_repo_snapshots (
          workspace_id,
          snapshot_id,
          snapshot_json,
          created_at
        ) VALUES (
          @workspace_id,
          @snapshot_id,
          @snapshot_json,
          @created_at
        )
        ON CONFLICT(workspace_id, snapshot_id) DO UPDATE SET
          snapshot_json = excluded.snapshot_json,
          created_at = excluded.created_at
      `
    ).run({
      workspace_id: snapshot.workspaceId,
      snapshot_id: snapshot.snapshotId,
      snapshot_json: JSON.stringify({
        connections: snapshot.connections,
        sshKeys: snapshot.sshKeys,
        proxies: snapshot.proxies
      }),
      created_at: snapshot.createdAt
    });
  }

  getWorkspaceRepoLocalState(workspaceId: string): WorkspaceRepoLocalState | undefined {
    const row = this.db.prepare(
      `
        SELECT workspace_id, local_head_commit_id, remote_head_commit_id, remote_commands_version, last_sync_at, last_error, sync_state
        FROM workspace_repo_local_state
        WHERE workspace_id = ?
      `
    ).get(workspaceId) as WorkspaceRepoLocalStateRow | undefined;
    return row ? rowToWorkspaceRepoLocalState(row) : undefined;
  }

  saveWorkspaceRepoLocalState(state: WorkspaceRepoLocalState): void {
    this.db.prepare(
      `
        INSERT INTO workspace_repo_local_state (
          workspace_id,
          local_head_commit_id,
          remote_head_commit_id,
          remote_commands_version,
          last_sync_at,
          last_error,
          sync_state
        ) VALUES (
          @workspace_id,
          @local_head_commit_id,
          @remote_head_commit_id,
          @remote_commands_version,
          @last_sync_at,
          @last_error,
          @sync_state
        )
        ON CONFLICT(workspace_id) DO UPDATE SET
          local_head_commit_id = excluded.local_head_commit_id,
          remote_head_commit_id = excluded.remote_head_commit_id,
          remote_commands_version = excluded.remote_commands_version,
          last_sync_at = excluded.last_sync_at,
          last_error = excluded.last_error,
          sync_state = excluded.sync_state
      `
    ).run({
      workspace_id: state.workspaceId,
      local_head_commit_id: state.localHeadCommitId ?? null,
      remote_head_commit_id: state.remoteHeadCommitId ?? null,
      remote_commands_version: state.remoteCommandsVersion ?? null,
      last_sync_at: state.lastSyncAt ?? null,
      last_error: state.lastError ?? null,
      sync_state: state.syncState
    });
  }

  listWorkspaceRepoConflicts(workspaceId: string): WorkspaceRepoConflict[] {
    const rows = this.db.prepare(
      `
        SELECT workspace_id, resource_type, resource_id, display_name, local_snapshot_json, remote_snapshot_json, remote_deleted, detected_at
        FROM workspace_repo_conflicts
        WHERE workspace_id = ?
        ORDER BY detected_at DESC, resource_type ASC, resource_id ASC
      `
    ).all(workspaceId) as WorkspaceRepoConflictRow[];
    return rows.map(rowToWorkspaceRepoConflict);
  }

  saveWorkspaceRepoConflict(conflict: WorkspaceRepoConflict): void {
    this.db.prepare(
      `
        INSERT INTO workspace_repo_conflicts (
          workspace_id,
          resource_type,
          resource_id,
          display_name,
          local_snapshot_json,
          remote_snapshot_json,
          remote_deleted,
          detected_at
        ) VALUES (
          @workspace_id,
          @resource_type,
          @resource_id,
          @display_name,
          @local_snapshot_json,
          @remote_snapshot_json,
          @remote_deleted,
          @detected_at
        )
        ON CONFLICT(workspace_id, resource_type, resource_id) DO UPDATE SET
          display_name = excluded.display_name,
          local_snapshot_json = excluded.local_snapshot_json,
          remote_snapshot_json = excluded.remote_snapshot_json,
          remote_deleted = excluded.remote_deleted,
          detected_at = excluded.detected_at
      `
    ).run({
      workspace_id: conflict.workspaceId,
      resource_type: conflict.resourceType,
      resource_id: conflict.resourceId,
      display_name: conflict.displayName,
      local_snapshot_json: conflict.localSnapshotJson ?? null,
      remote_snapshot_json: conflict.remoteSnapshotJson ?? null,
      remote_deleted: conflict.remoteDeleted ? 1 : 0,
      detected_at: conflict.detectedAt
    });
  }

  removeWorkspaceRepoConflict(workspaceId: string, resourceType: string, resourceId: string): void {
    this.db.prepare(
      "DELETE FROM workspace_repo_conflicts WHERE workspace_id = ? AND resource_type = ? AND resource_id = ?"
    ).run(workspaceId, resourceType, resourceId);
  }

  clearWorkspaceRepoConflicts(workspaceId: string): void {
    this.db.prepare("DELETE FROM workspace_repo_conflicts WHERE workspace_id = ?").run(workspaceId);
  }

  // ── Workspace commands ──
  listWorkspaceCommands(workspaceId: string): WorkspaceCommandItem[] {
    const rows = this.db.prepare(
      `
        SELECT id, workspace_id, name, description, group_name, command, is_template, created_at, updated_at
        FROM workspace_commands
        WHERE workspace_id = ?
        ORDER BY group_name ASC, updated_at DESC, id ASC
      `
    ).all(workspaceId) as WorkspaceCommandRow[];
    return rows.map(rowToWorkspaceCommand);
  }

  replaceWorkspaceCommands(workspaceId: string, commands: WorkspaceCommandItem[]): void {
    const now = new Date().toISOString();
    const deleteStmt = this.db.prepare("DELETE FROM workspace_commands WHERE workspace_id = ?");
    const insertStmt = this.db.prepare(
      `
        INSERT INTO workspace_commands (
          id,
          workspace_id,
          name,
          description,
          group_name,
          command,
          is_template,
          created_at,
          updated_at
        ) VALUES (
          @id,
          @workspace_id,
          @name,
          @description,
          @group_name,
          @command,
          @is_template,
          @created_at,
          @updated_at
        )
      `
    );
    this.db.transaction((items: WorkspaceCommandItem[]) => {
      deleteStmt.run(workspaceId);
      for (const item of items) {
        insertStmt.run({
          id: item.id,
          workspace_id: workspaceId,
          name: item.name,
          description: item.description ?? null,
          group_name: item.group.trim() || "默认",
          command: item.command,
          is_template: item.isTemplate ? 1 : 0,
          created_at: item.createdAt,
          updated_at: item.updatedAt
        });
      }
      this.db.prepare(
        `
          INSERT INTO workspace_command_sync_state (workspace_id, commands_version, updated_at)
          VALUES (@workspace_id, @commands_version, @updated_at)
          ON CONFLICT(workspace_id) DO UPDATE SET
            commands_version = excluded.commands_version,
            updated_at = excluded.updated_at
        `
      ).run({
        workspace_id: workspaceId,
        commands_version: null,
        updated_at: now
      });
    })(commands);
  }

  upsertWorkspaceCommand(command: WorkspaceCommandItem): WorkspaceCommandItem {
    const now = new Date().toISOString();
    const id = command.id || randomUUID();
    this.db.prepare(
      `
        INSERT INTO workspace_commands (
          id,
          workspace_id,
          name,
          description,
          group_name,
          command,
          is_template,
          created_at,
          updated_at
        ) VALUES (
          @id,
          @workspace_id,
          @name,
          @description,
          @group_name,
          @command,
          @is_template,
          @created_at,
          @updated_at
        )
        ON CONFLICT(workspace_id, id) DO UPDATE SET
          name = excluded.name,
          description = excluded.description,
          group_name = excluded.group_name,
          command = excluded.command,
          is_template = excluded.is_template,
          updated_at = excluded.updated_at
      `
    ).run({
      id,
      workspace_id: command.workspaceId,
      name: command.name.trim(),
      description: command.description?.trim() || null,
      group_name: command.group.trim() || "默认",
      command: command.command.trim(),
      is_template: command.isTemplate ? 1 : 0,
      created_at: command.createdAt,
      updated_at: command.updatedAt || now
    });

    this.db.prepare(
      `
        INSERT INTO workspace_command_sync_state (workspace_id, commands_version, updated_at)
        VALUES (@workspace_id, @commands_version, @updated_at)
        ON CONFLICT(workspace_id) DO UPDATE SET
          commands_version = excluded.commands_version,
          updated_at = excluded.updated_at
      `
    ).run({
      workspace_id: command.workspaceId,
      commands_version: null,
      updated_at: now
    });

    const row = this.db.prepare(
      `
        SELECT id, workspace_id, name, description, group_name, command, is_template, created_at, updated_at
        FROM workspace_commands
        WHERE workspace_id = ? AND id = ?
      `
    ).get(command.workspaceId, id) as WorkspaceCommandRow;
    return rowToWorkspaceCommand(row);
  }

  removeWorkspaceCommand(workspaceId: string, id: string): void {
    this.db.prepare("DELETE FROM workspace_commands WHERE workspace_id = ? AND id = ?").run(workspaceId, id);
    this.db.prepare(
      `
        INSERT INTO workspace_command_sync_state (workspace_id, commands_version, updated_at)
        VALUES (@workspace_id, @commands_version, @updated_at)
        ON CONFLICT(workspace_id) DO UPDATE SET
          commands_version = excluded.commands_version,
          updated_at = excluded.updated_at
      `
    ).run({
      workspace_id: workspaceId,
      commands_version: null,
      updated_at: new Date().toISOString()
    });
  }

  getWorkspaceCommandsVersion(workspaceId: string): string | undefined {
    const row = this.db.prepare(
      "SELECT commands_version FROM workspace_command_sync_state WHERE workspace_id = ?"
    ).get(workspaceId) as { commands_version: string | null } | undefined;
    return row?.commands_version ?? undefined;
  }

  saveWorkspaceCommandsVersion(workspaceId: string, version: string): void {
    this.db.prepare(
      `
        INSERT INTO workspace_command_sync_state (workspace_id, commands_version, updated_at)
        VALUES (@workspace_id, @commands_version, @updated_at)
        ON CONFLICT(workspace_id) DO UPDATE SET
          commands_version = excluded.commands_version,
          updated_at = excluded.updated_at
      `
    ).run({
      workspace_id: workspaceId,
      commands_version: version,
      updated_at: new Date().toISOString()
    });
  }

  // ── Recycle bin ──
  listRecycleBinEntries(): RecycleBinEntry[] {
    const rows = this.db.prepare(
      "SELECT id, resource_type, display_name, original_resource_id, original_scope_key, reason, snapshot_json, created_at FROM recycle_bin_entries ORDER BY created_at DESC"
    ).all() as RecycleBinRow[];
    return rows.map(rowToRecycleBinEntry);
  }

  getRecycleBinEntry(id: string): RecycleBinEntry | undefined {
    const row = this.db.prepare(
      "SELECT id, resource_type, display_name, original_resource_id, original_scope_key, reason, snapshot_json, created_at FROM recycle_bin_entries WHERE id = ?"
    ).get(id) as RecycleBinRow | undefined;
    return row ? rowToRecycleBinEntry(row) : undefined;
  }

  saveRecycleBinEntry(entry: RecycleBinEntry): void {
    this.db.prepare(
      `INSERT INTO recycle_bin_entries (id, resource_type, display_name, original_resource_id, original_scope_key, reason, snapshot_json, created_at)
       VALUES (@id, @resource_type, @display_name, @original_resource_id, @original_scope_key, @reason, @snapshot_json, @created_at)
       ON CONFLICT(id) DO UPDATE SET
         resource_type = excluded.resource_type,
         display_name = excluded.display_name,
         original_resource_id = excluded.original_resource_id,
         original_scope_key = excluded.original_scope_key,
         reason = excluded.reason,
         snapshot_json = excluded.snapshot_json`
    ).run({
      id: entry.id,
      resource_type: entry.resourceType,
      display_name: entry.displayName,
      original_resource_id: entry.originalResourceId,
      original_scope_key: entry.originalScopeKey,
      reason: entry.reason,
      snapshot_json: entry.snapshotJson,
      created_at: entry.createdAt
    });
  }

  removeRecycleBinEntry(id: string): void {
    this.db.prepare("DELETE FROM recycle_bin_entries WHERE id = ?").run(id);
  }

  clearRecycleBin(): number {
    const result = this.db.prepare("DELETE FROM recycle_bin_entries").run();
    return result.changes;
  }

  getMasterKeyMeta(): MasterKeyMeta | undefined {
    const row = this.db.prepare(
      "SELECT key, value_json, updated_at FROM app_settings WHERE key = ?"
    ).get("master_key_meta") as AppSettingRow | undefined;

    if (!row?.value_json) {
      return undefined;
    }

    try {
      const parsed = JSON.parse(row.value_json) as Partial<MasterKeyMeta>;
      if (
        typeof parsed.salt === "string" &&
        typeof parsed.n === "number" &&
        typeof parsed.r === "number" &&
        typeof parsed.p === "number" &&
        typeof parsed.verifier === "string"
      ) {
        return parsed as MasterKeyMeta;
      }
      return undefined;
    } catch {
      return undefined;
    }
  }

  saveMasterKeyMeta(meta: MasterKeyMeta): void {
    const now = new Date().toISOString();
    this.db.prepare(
      `
        INSERT INTO app_settings (key, value_json, updated_at)
        VALUES (@key, @value_json, @updated_at)
        ON CONFLICT(key) DO UPDATE SET
          value_json = excluded.value_json,
          updated_at = excluded.updated_at
      `
    ).run({
      key: "master_key_meta",
      value_json: JSON.stringify(meta),
      updated_at: now
    });
  }

  getDeviceKey(): string | undefined {
    const row = this.db.prepare(
      "SELECT value_json FROM app_settings WHERE key = ?"
    ).get("device_key") as { value_json: string } | undefined;
    if (!row?.value_json) return undefined;
    try {
      const parsed = JSON.parse(row.value_json);
      return typeof parsed === "string" ? parsed : undefined;
    } catch {
      return undefined;
    }
  }

  saveDeviceKey(key: string): void {
    const now = new Date().toISOString();
    this.db.prepare(
      `INSERT INTO app_settings (key, value_json, updated_at)
       VALUES (@key, @value_json, @updated_at)
       ON CONFLICT(key) DO UPDATE SET
         value_json = excluded.value_json,
         updated_at = excluded.updated_at`
    ).run({ key: "device_key", value_json: JSON.stringify(key), updated_at: now });
  }

  getSecretStore(): SecretStoreDB {
    if (!this.secretStoreInstance) {
      this.secretStoreInstance = new SQLiteSecretStore(this.db);
    }
    return this.secretStoreInstance;
  }

  listTemplateParams(commandId?: string): CommandTemplateParam[] {
    if (commandId) {
      const rows = this.db.prepare(
        "SELECT id, command_id, param_name, param_value, updated_at FROM command_template_params WHERE command_id = ? ORDER BY param_name ASC"
      ).all(commandId) as Array<{ id: string; command_id: string; param_name: string; param_value: string; updated_at: string }>;
      return rows.map((r) => ({
        id: r.id,
        commandId: r.command_id,
        paramName: r.param_name,
        paramValue: r.param_value,
        updatedAt: r.updated_at
      }));
    }

    const rows = this.db.prepare(
      "SELECT id, command_id, param_name, param_value, updated_at FROM command_template_params ORDER BY command_id ASC, param_name ASC"
    ).all() as Array<{ id: string; command_id: string; param_name: string; param_value: string; updated_at: string }>;
    return rows.map((r) => ({
      id: r.id,
      commandId: r.command_id,
      paramName: r.param_name,
      paramValue: r.param_value,
      updatedAt: r.updated_at
    }));
  }

  upsertTemplateParams(commandId: string, params: Record<string, string>): void {
    const now = new Date().toISOString();
    const tx = this.db.transaction(() => {
      for (const [paramName, paramValue] of Object.entries(params)) {
        const existing = this.db.prepare(
          "SELECT id FROM command_template_params WHERE command_id = ? AND param_name = ?"
        ).get(commandId, paramName) as { id: string } | undefined;

        const id = existing?.id ?? randomUUID();
        this.db.prepare(
          `
            INSERT INTO command_template_params (id, command_id, param_name, param_value, updated_at)
            VALUES (@id, @command_id, @param_name, @param_value, @updated_at)
            ON CONFLICT(command_id, param_name) DO UPDATE SET
              param_value = excluded.param_value,
              updated_at = excluded.updated_at
          `
        ).run({
          id,
          command_id: commandId,
          param_name: paramName,
          param_value: paramValue,
          updated_at: now
        });
      }
    });
    tx();
  }

  clearTemplateParams(commandId: string): void {
    this.db.prepare("DELETE FROM command_template_params WHERE command_id = ?").run(commandId);
  }

  async backupDatabase(targetPath: string): Promise<void> {
    await this.db.backup(targetPath);
  }

  getDbPath(): string {
    return this.resolvedDbPath;
  }

  /** Expose the underlying database for sibling repositories. */
  getDb(): Database.Database {
    return this.db;
  }

  close(): void {
    this.db.close();
  }
}

// ─── SQLiteSshKeyRepository ─────────────────────────────────────────────────

export class SQLiteSshKeyRepository implements SshKeyRepository {
  constructor(private readonly db: Database.Database) {}

  list(): SshKeyProfile[] {
    const rows = this.db.prepare(
      "SELECT id, name, key_content_ref, passphrase_ref, created_at, updated_at, resource_id, uuid_in_scope, origin_kind, origin_scope_key, origin_workspace_id, copied_from_resource_id FROM ssh_keys ORDER BY name ASC"
    ).all() as SshKeyRow[];
    return rows.map(rowToSshKey);
  }

  getById(id: string): SshKeyProfile | undefined {
    const row = this.db.prepare(
      "SELECT id, name, key_content_ref, passphrase_ref, created_at, updated_at, resource_id, uuid_in_scope, origin_kind, origin_scope_key, origin_workspace_id, copied_from_resource_id FROM ssh_keys WHERE id = ?"
    ).get(id) as SshKeyRow | undefined;
    return row ? rowToSshKey(row) : undefined;
  }

  save(key: SshKeyProfile): void {
    this.db.prepare(
      `
        INSERT INTO ssh_keys (id, name, key_content_ref, passphrase_ref, created_at, updated_at, resource_id, uuid_in_scope, origin_kind, origin_scope_key, origin_workspace_id, copied_from_resource_id)
        VALUES (@id, @name, @key_content_ref, @passphrase_ref, @created_at, @updated_at, @resource_id, @uuid_in_scope, @origin_kind, @origin_scope_key, @origin_workspace_id, @copied_from_resource_id)
        ON CONFLICT(id) DO UPDATE SET
          name = excluded.name,
          key_content_ref = excluded.key_content_ref,
          passphrase_ref = excluded.passphrase_ref,
          updated_at = excluded.updated_at,
          resource_id = excluded.resource_id,
          uuid_in_scope = excluded.uuid_in_scope,
          origin_kind = excluded.origin_kind,
          origin_scope_key = excluded.origin_scope_key,
          origin_workspace_id = excluded.origin_workspace_id,
          copied_from_resource_id = excluded.copied_from_resource_id
      `
    ).run({
      id: key.id,
      name: key.name,
      key_content_ref: key.keyContentRef,
      passphrase_ref: key.passphraseRef ?? null,
      created_at: key.createdAt,
      updated_at: key.updatedAt,
      resource_id: key.resourceId ?? null,
      uuid_in_scope: key.uuidInScope ?? null,
      origin_kind: key.originKind ?? "local",
      origin_scope_key: key.originScopeKey ?? LOCAL_DEFAULT_SCOPE_KEY,
      origin_workspace_id: key.originWorkspaceId ?? null,
      copied_from_resource_id: key.copiedFromResourceId ?? null
    });
  }

  remove(id: string): void {
    this.db.prepare("DELETE FROM ssh_keys WHERE id = ?").run(id);
  }

  getReferencingConnectionIds(keyId: string): string[] {
    const rows = this.db.prepare(
      "SELECT id FROM connections WHERE ssh_key_id = ?"
    ).all(keyId) as Array<{ id: string }>;
    return rows.map((r) => r.id);
  }
}

// ─── SQLiteProxyRepository ──────────────────────────────────────────────────

export class SQLiteProxyRepository implements ProxyRepository {
  constructor(private readonly db: Database.Database) {}

  list(): ProxyProfile[] {
    const rows = this.db.prepare(
      `
        SELECT
          id,
          name,
          proxy_type,
          host,
          port,
          username,
          credential_ref,
          created_at,
          updated_at,
          resource_id,
          uuid_in_scope,
          origin_kind,
          origin_scope_key,
          origin_workspace_id,
          copied_from_resource_id
        FROM proxies
        ORDER BY name ASC
      `
    ).all() as ProxyRow[];
    return rows.map(rowToProxy);
  }

  getById(id: string): ProxyProfile | undefined {
    const row = this.db.prepare(
      `
        SELECT
          id,
          name,
          proxy_type,
          host,
          port,
          username,
          credential_ref,
          created_at,
          updated_at,
          resource_id,
          uuid_in_scope,
          origin_kind,
          origin_scope_key,
          origin_workspace_id,
          copied_from_resource_id
        FROM proxies
        WHERE id = ?
      `
    ).get(id) as ProxyRow | undefined;
    return row ? rowToProxy(row) : undefined;
  }

  save(proxy: ProxyProfile): void {
    this.db.prepare(
      `
        INSERT INTO proxies (
          id,
          name,
          proxy_type,
          host,
          port,
          username,
          credential_ref,
          created_at,
          updated_at,
          resource_id,
          uuid_in_scope,
          origin_kind,
          origin_scope_key,
          origin_workspace_id,
          copied_from_resource_id
        )
        VALUES (
          @id,
          @name,
          @proxy_type,
          @host,
          @port,
          @username,
          @credential_ref,
          @created_at,
          @updated_at,
          @resource_id,
          @uuid_in_scope,
          @origin_kind,
          @origin_scope_key,
          @origin_workspace_id,
          @copied_from_resource_id
        )
        ON CONFLICT(id) DO UPDATE SET
          name = excluded.name,
          proxy_type = excluded.proxy_type,
          host = excluded.host,
          port = excluded.port,
          username = excluded.username,
          credential_ref = excluded.credential_ref,
          updated_at = excluded.updated_at,
          resource_id = excluded.resource_id,
          uuid_in_scope = excluded.uuid_in_scope,
          origin_kind = excluded.origin_kind,
          origin_scope_key = excluded.origin_scope_key,
          origin_workspace_id = excluded.origin_workspace_id,
          copied_from_resource_id = excluded.copied_from_resource_id
      `
    ).run({
      id: proxy.id,
      name: proxy.name,
      proxy_type: proxy.proxyType,
      host: proxy.host,
      port: proxy.port,
      username: proxy.username ?? null,
      credential_ref: proxy.credentialRef ?? null,
      created_at: proxy.createdAt,
      updated_at: proxy.updatedAt,
      resource_id: proxy.resourceId ?? null,
      uuid_in_scope: proxy.uuidInScope ?? null,
      origin_kind: proxy.originKind ?? "local",
      origin_scope_key: proxy.originScopeKey ?? LOCAL_DEFAULT_SCOPE_KEY,
      origin_workspace_id: proxy.originWorkspaceId ?? null,
      copied_from_resource_id: proxy.copiedFromResourceId ?? null
    });
  }

  remove(id: string): void {
    this.db.prepare("DELETE FROM proxies WHERE id = ?").run(id);
  }

  getReferencingConnectionIds(proxyId: string): string[] {
    const rows = this.db.prepare(
      "SELECT id FROM connections WHERE proxy_id = ?"
    ).all(proxyId) as Array<{ id: string }>;
    return rows.map((r) => r.id);
  }
}

// ─── SQLiteSecretStore ──────────────────────────────────────────────────────

class SQLiteSecretStore implements SecretStoreDB {
  constructor(private readonly db: Database.Database) {}

  putSecret(id: string, purpose: string, ciphertextB64: string, ivB64: string, tagB64: string, aad: string): void {
    const now = new Date().toISOString();
    this.db.prepare(
      `
        INSERT INTO secret_store (id, purpose, ciphertext_b64, iv_b64, tag_b64, aad, created_at, updated_at)
        VALUES (@id, @purpose, @ciphertext_b64, @iv_b64, @tag_b64, @aad, @created_at, @updated_at)
        ON CONFLICT(id) DO UPDATE SET
          purpose = excluded.purpose,
          ciphertext_b64 = excluded.ciphertext_b64,
          iv_b64 = excluded.iv_b64,
          tag_b64 = excluded.tag_b64,
          aad = excluded.aad,
          updated_at = excluded.updated_at
      `
    ).run({
      id,
      purpose,
      ciphertext_b64: ciphertextB64,
      iv_b64: ivB64,
      tag_b64: tagB64,
      aad,
      created_at: now,
      updated_at: now
    });
  }

  getSecret(id: string): { ciphertext_b64: string; iv_b64: string; tag_b64: string; aad: string } | undefined {
    const row = this.db.prepare(
      "SELECT ciphertext_b64, iv_b64, tag_b64, aad FROM secret_store WHERE id = ?"
    ).get(id) as { ciphertext_b64: string; iv_b64: string; tag_b64: string; aad: string } | undefined;
    return row;
  }

  deleteSecret(id: string): void {
    this.db.prepare("DELETE FROM secret_store WHERE id = ?").run(id);
  }

  listSecrets(): Array<{ id: string; purpose: string; ciphertext_b64: string; iv_b64: string; tag_b64: string; aad: string }> {
    return this.db.prepare(
      "SELECT id, purpose, ciphertext_b64, iv_b64, tag_b64, aad FROM secret_store ORDER BY id ASC"
    ).all() as Array<{ id: string; purpose: string; ciphertext_b64: string; iv_b64: string; tag_b64: string; aad: string }>;
  }
}
