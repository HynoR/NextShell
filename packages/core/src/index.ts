export type AuthType = "password" | "privateKey" | "agent";
export type ProxyType = "none" | "socks4" | "socks5";
export type TerminalEncoding = "utf-8" | "gb18030" | "gbk" | "big5";
export type BackspaceMode = "ascii-backspace" | "ascii-delete";
export type DeleteMode = "vt220-delete" | "ascii-delete" | "ascii-backspace";

export interface ConnectionProfile {
  id: string;
  name: string;
  host: string;
  port: number;
  username: string;
  authType: AuthType;
  credentialRef?: string;
  privateKeyPath?: string;
  privateKeyRef?: string;
  hostFingerprint?: string;
  strictHostKeyChecking: boolean;
  proxyType: ProxyType;
  proxyHost?: string;
  proxyPort?: number;
  proxyUsername?: string;
  proxyCredentialRef?: string;
  terminalEncoding: TerminalEncoding;
  backspaceMode: BackspaceMode;
  deleteMode: DeleteMode;
  groupPath: string[];
  tags: string[];
  notes?: string;
  favorite: boolean;
  monitorSession: boolean;
  createdAt: string;
  updatedAt: string;
  lastConnectedAt?: string;
}

export interface ConnectionListQuery {
  keyword?: string;
  group?: string;
  favoriteOnly?: boolean;
}

export type SessionStatus = "connecting" | "connected" | "disconnected" | "failed";
export type SessionType = "terminal" | "processManager" | "networkMonitor";

export interface SessionDescriptor {
  id: string;
  connectionId: string;
  title: string;
  status: SessionStatus;
  type: SessionType;
  createdAt: string;
  reconnectable: boolean;
}

export interface RemoteFileEntry {
  name: string;
  path: string;
  type: "file" | "directory" | "link";
  size: number;
  permissions: string;
  owner: string;
  group: string;
  modifiedAt: string;
}

export interface MonitorProcess {
  pid: number;
  command: string;
  cpuPercent: number;
  memoryMb: number;
  user?: string;
  commandLine?: string;
}

export interface ProcessSnapshot {
  connectionId: string;
  processes: MonitorProcess[];
  capturedAt: string;
}

export interface ProcessDetailSnapshot {
  connectionId: string;
  pid: number;
  ppid: number;
  user: string;
  state: string;
  cpuPercent: number;
  memoryPercent: number;
  rssMb: number;
  elapsed: string;
  command: string;
  commandLine: string;
  capturedAt: string;
}

export interface NetworkListener {
  pid: number;
  name: string;
  listenIp: string;
  port: number;
  ipCount: number;
  connectionCount: number;
  uploadBytes: number;
  downloadBytes: number;
}

export interface NetworkConnection {
  localPort: number;
  remoteIp: string;
  remotePort: number;
  state: string;
  pid: number;
  processName: string;
}

export interface NetworkSnapshot {
  connectionId: string;
  listeners: NetworkListener[];
  connections: NetworkConnection[];
  capturedAt: string;
}

export interface MonitorSnapshot {
  connectionId: string;
  uptimeHours: number;
  loadAverage: [number, number, number];
  cpuPercent: number;
  memoryPercent: number;
  memoryUsedMb: number;
  memoryTotalMb: number;
  swapPercent: number;
  swapUsedMb: number;
  swapTotalMb: number;
  diskPercent: number;
  diskUsedGb: number;
  diskTotalGb: number;
  networkInMbps: number;
  networkOutMbps: number;
  networkInterface: string;
  networkInterfaceOptions: string[];
  processes: MonitorProcess[];
  capturedAt: string;
}

export interface BatchCommandTask {
  id: string;
  command: string;
  connectionIds: string[];
  createdAt: string;
}

export interface BatchCommandResultItem extends CommandExecutionResult {
  success: boolean;
  attempts: number;
  durationMs: number;
  error?: string;
}

export interface BatchCommandExecutionResult {
  command: string;
  startedAt: string;
  finishedAt: string;
  durationMs: number;
  total: number;
  successCount: number;
  failedCount: number;
  results: BatchCommandResultItem[];
}

export interface MigrationRecord {
  version: number;
  name: string;
  appliedAt: string;
}

export interface AuditLogRecord {
  id: string;
  action: string;
  level: "info" | "warn" | "error";
  connectionId?: string;
  message: string;
  metadata?: Record<string, unknown>;
  createdAt: string;
}

export interface CommandExecutionResult {
  connectionId: string;
  command: string;
  stdout: string;
  stderr: string;
  exitCode: number;
  executedAt: string;
}

export interface CommandHistoryEntry {
  command: string;
  useCount: number;
  lastUsedAt: string;
}

export interface SavedCommand {
  id: string;
  name: string;
  description?: string;
  group: string;
  command: string;
  isTemplate: boolean;
  createdAt: string;
  updatedAt: string;
}

export type BackupConflictPolicy = "skip" | "force";
export type RestoreConflictPolicy = "skip_older" | "force";

export interface AppPreferences {
  transfer: {
    uploadDefaultDir: string;
    downloadDefaultDir: string;
  };
  remoteEdit: {
    defaultEditorCommand: string;
  };
  commandCenter: {
    rememberTemplateParams: boolean;
  };
  terminal: {
    backgroundColor: string;
    foregroundColor: string;
    fontSize: number;
    lineHeight: number;
  };
  backup: {
    remotePath: string;
    /** 留空表示直接使用 PATH 中的 rclone（macOS/Linux），Windows 用户可填绝对路径 */
    rclonePath: string;
    defaultBackupConflictPolicy: BackupConflictPolicy;
    defaultRestoreConflictPolicy: RestoreConflictPolicy;
    rememberPassword: boolean;
    lastBackupAt: string | null;
  };
}

export interface AppPreferencesPatch {
  transfer?: {
    uploadDefaultDir?: string;
    downloadDefaultDir?: string;
  };
  remoteEdit?: {
    defaultEditorCommand?: string;
  };
  commandCenter?: {
    rememberTemplateParams?: boolean;
  };
  terminal?: {
    backgroundColor?: string;
    foregroundColor?: string;
    fontSize?: number;
    lineHeight?: number;
  };
  backup?: {
    remotePath?: string;
    rclonePath?: string;
    defaultBackupConflictPolicy?: BackupConflictPolicy;
    defaultRestoreConflictPolicy?: RestoreConflictPolicy;
    rememberPassword?: boolean;
    lastBackupAt?: string | null;
  };
}

export interface BackupArchiveMeta {
  id: string;
  timestamp: string;
  deviceId: string;
  appVersion: string;
  hash: string;
  fileName: string;
  sizeBytes: number;
}

export interface SecretStoreEntry {
  id: string;
  purpose: string;
  ciphertextB64: string;
  ivB64: string;
  tagB64: string;
  aad: string;
  createdAt: string;
  updatedAt: string;
}

export interface MasterKeyMeta {
  salt: string;
  n: number;
  r: number;
  p: number;
  verifier: string;
}

export interface CommandTemplateParam {
  id: string;
  commandId: string;
  paramName: string;
  paramValue: string;
  updatedAt: string;
}

export const DEFAULT_APP_PREFERENCES: AppPreferences = {
  transfer: {
    uploadDefaultDir: "~",
    downloadDefaultDir: "~/Downloads"
  },
  remoteEdit: {
    defaultEditorCommand: "code"
  },
  commandCenter: {
    rememberTemplateParams: true
  },
  terminal: {
    backgroundColor: "#0b2740",
    foregroundColor: "#d8eaff",
    fontSize: 14,
    lineHeight: 1.2
  },
  backup: {
    remotePath: "",
    rclonePath: "",
    defaultBackupConflictPolicy: "skip",
    defaultRestoreConflictPolicy: "skip_older",
    rememberPassword: true,
    lastBackupAt: null
  }
};
