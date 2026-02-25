export type AuthType = "password" | "privateKey" | "agent" | "interactive";
export type ProxyType = "socks4" | "socks5";
export type TerminalEncoding = "utf-8" | "gb18030" | "gbk" | "big5";
export type BackspaceMode = "ascii-backspace" | "ascii-delete";
export type DeleteMode = "vt220-delete" | "ascii-delete" | "ascii-backspace";

/** SSH 密钥实体 — 独立于服务器连接，可被多个连接引用 */
export interface SshKeyProfile {
  id: string;
  name: string;
  /** 加密存储的密钥内容引用 (secret://sshkey-{id}) */
  keyContentRef: string;
  /** 加密存储的 passphrase 引用 (secret://sshkey-{id}-pass)，可选 */
  passphraseRef?: string;
  createdAt: string;
  updatedAt: string;
}

/** 代理实体 — 独立于服务器连接，可被多个连接引用 */
export interface ProxyProfile {
  id: string;
  name: string;
  proxyType: ProxyType;
  host: string;
  port: number;
  username?: string;
  /** 加密存储的代理密码引用 (secret://proxy-{id})，仅 SOCKS5 */
  credentialRef?: string;
  createdAt: string;
  updatedAt: string;
}

export interface ConnectionProfile {
  id: string;
  name: string;
  host: string;
  port: number;
  username: string;
  authType: AuthType;
  /** 密码认证时的密码引用 (secret://conn-{id}) */
  credentialRef?: string;
  /** 私钥认证时引用的密钥实体 ID */
  sshKeyId?: string;
  hostFingerprint?: string;
  strictHostKeyChecking: boolean;
  /** 引用的代理实体 ID */
  proxyId?: string;
  terminalEncoding: TerminalEncoding;
  backspaceMode: BackspaceMode;
  deleteMode: DeleteMode;
  /** 分组路径，如 /server/hk，以 / 分隔层级 */
  groupPath: string;
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
export type SessionType = "terminal" | "processManager" | "networkMonitor" | "editor";

export interface SessionDescriptor {
  id: string;
  connectionId: string;
  title: string;
  status: SessionStatus;
  reason?: string;
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
  ppid: number;
  command: string;
  cpuPercent: number;
  memoryPercent: number;
  memoryMb: number;
  user: string;
  stat: string;
  nice: number;
  priority: number;
  vszMb: number;
  elapsedSeconds: number;
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

export interface SystemCpuInfo {
  modelName: string;
  coreCount: number;
  frequencyMhz?: number;
  cacheSize?: string;
  bogoMips?: number;
}

export interface SystemNetworkInterfaceTotal {
  name: string;
  rxBytes: number;
  txBytes: number;
}

export interface SystemFilesystemEntry {
  filesystem: string;
  totalKb: number;
  usedKb: number;
  availableKb: number;
  mountPoint: string;
}

export interface SystemInfoSnapshot {
  connectionId: string;
  hostname: string;
  osName: string;
  kernelName: string;
  kernelVersion: string;
  architecture: string;
  cpu: SystemCpuInfo;
  memoryTotalKb: number;
  swapTotalKb: number;
  networkInterfaces: SystemNetworkInterfaceTotal[];
  filesystems: SystemFilesystemEntry[];
  uptimeSeconds: number;
  capturedAt: string;
}

export interface MonitorSnapshot {
  connectionId: string;
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
export type WindowAppearance = "system" | "light" | "dark";

export interface AppPreferences {
  transfer: {
    uploadDefaultDir: string;
    downloadDefaultDir: string;
  };
  remoteEdit: {
    defaultEditorCommand: string;
    editorMode: "builtin" | "external";
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
  window: {
    appearance: WindowAppearance;
    minimizeToTray: boolean;
    confirmBeforeClose: boolean;
    /** APP 背景图片绝对路径，空字符串表示不使用图片 */
    backgroundImagePath: string;
    /** APP 背景整体透明度（30-80） */
    backgroundOpacity: number;
  };
  traceroute: {
    /** nexttrace 可执行文件路径，留空表示从 PATH 查找 */
    nexttracePath: string;
    /** 探测协议 */
    protocol: "icmp" | "tcp" | "udp";
    /** 目标端口（仅 TCP/UDP 有效，0 = 使用协议默认值） */
    port: number;
    /** 每跳探测次数，默认 3 */
    queries: number;
    /** 最大跳数（最大 TTL），默认 30 */
    maxHops: number;
    /** IP 版本偏好 */
    ipVersion: "auto" | "ipv4" | "ipv6";
    /** IP 地理信息数据来源 */
    dataProvider: "LeoMoeAPI" | "ip-api.com" | "IPInfo" | "IPInsight" | "IP.SB" | "disable-geoip";
    /** 不解析 PTR 记录 */
    noRdns: boolean;
    /** 界面语言 */
    language: "cn" | "en";
    /** PoW 服务商（国内用户建议选 sakura） */
    powProvider: "api.nxtrace.org" | "sakura";
  };
  audit: {
    /** 审计日志保留天数，0 表示永不清理 */
    retentionDays: number;
  };
}

export interface AppPreferencesPatch {
  transfer?: {
    uploadDefaultDir?: string;
    downloadDefaultDir?: string;
  };
  remoteEdit?: {
    defaultEditorCommand?: string;
    editorMode?: "builtin" | "external";
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
  window?: {
    appearance?: WindowAppearance;
    minimizeToTray?: boolean;
    confirmBeforeClose?: boolean;
    backgroundImagePath?: string;
    backgroundOpacity?: number;
  };
  traceroute?: {
    nexttracePath?: string;
    protocol?: "icmp" | "tcp" | "udp";
    port?: number;
    queries?: number;
    maxHops?: number;
    ipVersion?: "auto" | "ipv4" | "ipv6";
    dataProvider?: "LeoMoeAPI" | "ip-api.com" | "IPInfo" | "IPInsight" | "IP.SB" | "disable-geoip";
    noRdns?: boolean;
    language?: "cn" | "en";
    powProvider?: "api.nxtrace.org" | "sakura";
  };
  audit?: {
    retentionDays?: number;
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

export interface ExportedConnection {
  name: string;
  host: string;
  port: number;
  username: string;
  authType: AuthType;
  password?: string;
  groupPath: string;
  tags: string[];
  notes?: string;
  favorite: boolean;
  terminalEncoding: TerminalEncoding;
  backspaceMode: BackspaceMode;
  deleteMode: DeleteMode;
  monitorSession: boolean;
}

export interface ConnectionExportFile {
  format: "nextshell-connections";
  version: 1;
  exportedAt: string;
  /**
   * When true, each connection's `password` field has been XOR-obfuscated
   * with SHA256(name+host+port) and encoded as base64 instead of stored
   * as plaintext. Only set on unencrypted exports.
   */
  passwordsObfuscated?: boolean;
  connections: ExportedConnection[];
}

export interface ConnectionImportEntry extends ExportedConnection {
  passwordUnavailable?: boolean;
  sourceFormat: "nextshell" | "finalshell";
}

export type ImportConflictPolicy = "skip" | "overwrite" | "duplicate";

export interface ConnectionImportResult {
  created: number;
  skipped: number;
  overwritten: number;
  failed: number;
  passwordsUnavailable: number;
  errors: string[];
}

export const DEFAULT_APP_PREFERENCES: AppPreferences = {
  transfer: {
    uploadDefaultDir: "~",
    downloadDefaultDir: "~/Downloads"
  },
  remoteEdit: {
    defaultEditorCommand: "",
    editorMode: "builtin"
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
  },
  window: {
    appearance: "system",
    minimizeToTray: false,
    confirmBeforeClose: true,
    backgroundImagePath: "",
    backgroundOpacity: 60
  },
  traceroute: {
    nexttracePath: "",
    protocol: "icmp",
    port: 0,
    queries: 3,
    maxHops: 30,
    ipVersion: "auto",
    dataProvider: "LeoMoeAPI",
    noRdns: false,
    language: "cn",
    powProvider: "api.nxtrace.org"
  },
  audit: {
    retentionDays: 7
  }
};
