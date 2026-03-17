export type AuthType = "password" | "privateKey" | "agent" | "interactive";
export type ProxyType = "socks4" | "socks5";
export type TerminalEncoding = "utf-8" | "gb18030" | "gbk" | "big5";
export type BackspaceMode = "ascii-backspace" | "ascii-delete";
export type DeleteMode = "vt220-delete" | "ascii-delete" | "ascii-backspace";
export type SessionTarget = "remote" | "local";
export type LocalShellMode = "preset" | "custom";
export type LocalShellPreset = "system" | "powershell" | "cmd" | "zsh" | "sh" | "bash";

// ────── Cloud Sync v2: Resource Origin Model ──────

export type OriginKind = "local" | "cloud";

export interface ResourceOrigin {
  kind: OriginKind;
  scopeKey: string;
  workspaceId?: string;
}

/** 云同步 workspace 配置（多 workspace 并发模型） */
export interface CloudSyncWorkspaceProfile {
  id: string;
  apiBaseUrl: string;
  workspaceName: string;
  displayName: string;
  pullIntervalSec: number;
  ignoreTlsErrors: boolean;
  enabled: boolean;
  createdAt: string;
  updatedAt: string;
  lastSyncAt: string | null;
  lastError: string | null;
}

export type RecycleBinReason = "delete" | "conflict_accept_remote" | "conflict_keep_local" | "danger_move";

/** 回收站条目 — 物理隔离存储，恢复时总是创建新副本 */
export interface RecycleBinEntry {
  id: string;
  resourceType: "server" | "sshKey";
  displayName: string;
  originalResourceId: string;
  originalScopeKey: string;
  reason: RecycleBinReason;
  snapshotJson: string;
  createdAt: string;
}

/** 带 workspace 作用域的 pending 操作 */
export interface CloudSyncPendingOp {
  id?: number;
  workspaceId: string;
  resourceType: "server" | "sshKey";
  resourceId: string;
  action: "upsert" | "delete";
  baseRevision: number | null;
  force: boolean;
  payloadJson?: string;
  queuedAt: string;
  lastAttemptAt?: string;
  lastError?: string;
}

/** 带 workspace 作用域的资源同步状态 */
export interface CloudSyncResourceStateV2 {
  workspaceId: string;
  resourceType: "server" | "sshKey";
  resourceId: string;
  serverRevision?: number;
  conflictRemoteRevision?: number;
  conflictRemotePayloadJson?: string;
  conflictRemoteUpdatedAt?: string;
  conflictRemoteDeleted: boolean;
  conflictDetectedAt?: string;
}

export const LOCAL_DEFAULT_SCOPE_KEY = "local-default";

/** 构造 scopeKey: 对本地来说是 "local-default", 对云来说是 "<apiBaseUrl>-<workspaceName>" */
export const buildScopeKey = (origin: { kind: OriginKind; apiBaseUrl?: string; workspaceName?: string }): string => {
  if (origin.kind === "local") return LOCAL_DEFAULT_SCOPE_KEY;
  const base = (origin.apiBaseUrl ?? "").replace(/^https?:\/\//, "").replace(/[\/\s]+$/g, "");
  return `${base}-${origin.workspaceName ?? ""}`;
};

/** 构造 resourceId = "<scopeKey>-<uuidInScope>" */
export const buildResourceId = (scopeKey: string, uuidInScope: string): string =>
  `${scopeKey}-${uuidInScope}`;

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
  /** 全局唯一资源 ID = "<scopeKey>-<uuidInScope>" */
  resourceId?: string;
  /** 等于 id，scope 内的 UUID */
  uuidInScope?: string;
  /** 来源类型 */
  originKind?: OriginKind;
  /** 来源 scope key */
  originScopeKey?: string;
  /** 云来源时指向 cloud_sync_workspaces.id */
  originWorkspaceId?: string;
  /** 副本溯源 */
  copiedFromResourceId?: string;
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
  /** 是否覆盖全局 keepalive 设置（空表示跟随全局） */
  keepAliveEnabled?: boolean;
  /** Keepalive 间隔（秒），空表示跟随全局 */
  keepAliveIntervalSec?: number;
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
  /** 全局唯一资源 ID = "<scopeKey>-<uuidInScope>" */
  resourceId?: string;
  /** 等于 id，scope 内的 UUID */
  uuidInScope?: string;
  /** 来源类型 */
  originKind?: OriginKind;
  /** 来源 scope key */
  originScopeKey?: string;
  /** 云来源时指向 cloud_sync_workspaces.id */
  originWorkspaceId?: string;
  /** 引用 SSH 密钥的 resourceId（替代原裸 sshKeyId 做跨来源引用） */
  sshKeyResourceId?: string;
  /** 副本溯源 */
  copiedFromResourceId?: string;
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
  target: SessionTarget;
  connectionId?: string;
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

export const MAX_COMMAND_HISTORY_ENTRIES = 500;

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
    batchMaxConcurrency: number;
    batchRetryCount: number;
  };
  terminal: {
    backgroundColor: string;
    foregroundColor: string;
    fontSize: number;
    lineHeight: number;
    fontFamily: string;
    localShell: {
      mode: LocalShellMode;
      preset: LocalShellPreset;
      customPath: string;
    };
  };
  ssh: {
    /** 是否对所有连接启用 keepalive（发送空包） */
    keepAliveEnabled: boolean;
    /** Keepalive 间隔（秒） */
    keepAliveIntervalSec: number;
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
    /** 左侧工作区边栏默认是否折叠 */
    leftSidebarDefaultCollapsed: boolean;
    /** 底部工作台默认是否折叠 */
    bottomWorkbenchDefaultCollapsed: boolean;
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
    /** 是否在终端下方显示路由追踪标签卡片 */
    showTracerouteTab: boolean;
  };
  audit: {
    /** 是否启用审计日志记录 */
    enabled: boolean;
    /** 审计日志保留天数，0 表示永不清理 */
    retentionDays: number;
  };
  ai: AiPreferences;
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
    batchMaxConcurrency?: number;
    batchRetryCount?: number;
  };
  terminal?: {
    backgroundColor?: string;
    foregroundColor?: string;
    fontSize?: number;
    lineHeight?: number;
    fontFamily?: string;
    localShell?: {
      mode?: LocalShellMode;
      preset?: LocalShellPreset;
      customPath?: string;
    };
  };
  ssh?: {
    keepAliveEnabled?: boolean;
    keepAliveIntervalSec?: number;
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
    leftSidebarDefaultCollapsed?: boolean;
    bottomWorkbenchDefaultCollapsed?: boolean;
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
    showTracerouteTab?: boolean;
  };
  audit?: {
    enabled?: boolean;
    retentionDays?: number;
  };
  ai?: {
    enabled?: boolean;
    activeProviderId?: string;
    providers?: AiProviderConfig[];
    systemPromptOverride?: string;
    executionTimeoutSec?: number;
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
  keepAliveEnabled?: boolean;
  keepAliveIntervalSec?: number;
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

// ────── AI Assistant ──────

export type AiProviderType = "openai" | "anthropic" | "gemini";

export interface AiProviderConfig {
  id: string;
  type: AiProviderType;
  name: string;
  baseUrl: string;
  model: string;
  /** secret:// 引用，密钥不明文存储 */
  apiKeyRef?: string;
  enabled: boolean;
}

export interface AiPreferences {
  enabled: boolean;
  activeProviderId?: string;
  providers: AiProviderConfig[];
  systemPromptOverride?: string;
  /** 命令执行超时（秒） */
  executionTimeoutSec: number;
  /** 模型请求超时（秒） */
  providerRequestTimeoutSec: number;
  /** 模型请求失败后的最大重试次数 */
  providerMaxRetries: number;
}

export type AiChatRole = "user" | "assistant" | "system";
export type AiMessageKind = "chat" | "execution_result";
export type AiMessageType = "user_prompt" | "assistant_reply" | "execution_result" | "system_note";

type AiMessageRoleLike = {
  role: AiChatRole;
  type?: AiMessageType;
  kind?: AiMessageKind;
};

export const resolveAiMessageType = (message: AiMessageRoleLike): AiMessageType => {
  if (message.type) return message.type;
  if (message.kind === "execution_result") return "execution_result";
  if (message.role === "assistant") return "assistant_reply";
  if (message.role === "system") return "system_note";
  return "user_prompt";
};

export const isAiExecutionResultMessage = (message: AiMessageRoleLike): boolean => {
  return resolveAiMessageType(message) === "execution_result";
};

export const isAiUserPromptMessage = (message: AiMessageRoleLike): boolean => {
  return resolveAiMessageType(message) === "user_prompt";
};

export const isAiAssistantReplyMessage = (message: AiMessageRoleLike): boolean => {
  return resolveAiMessageType(message) === "assistant_reply";
};

export const isAiSystemNoteMessage = (message: AiMessageRoleLike): boolean => {
  return resolveAiMessageType(message) === "system_note";
};

export const getAiMessageCanonicalRole = (message: AiMessageRoleLike): AiChatRole => {
  const type = resolveAiMessageType(message);
  if (type === "assistant_reply") return "assistant";
  if (type === "user_prompt") return "user";
  return "system";
};

export const getAiMessageModelRole = (message: AiMessageRoleLike): AiChatRole => {
  const type = resolveAiMessageType(message);
  if (type === "assistant_reply") return "assistant";
  if (type === "system_note") return "system";
  return "user";
};

export interface AiChatMessage {
  id: string;
  role: AiChatRole;
  /** 业务语义字段，前端与历史恢复逻辑应优先使用它判断消息类型 */
  type: AiMessageType;
  /** 兼容旧历史结构，新增写入不再依赖该字段 */
  kind?: AiMessageKind;
  content: string;
  timestamp: string;
  /** 若包含执行计划，存储在此 */
  plan?: AiExecutionPlan;
  /** 执行进度快照 */
  executionProgress?: AiExecutionProgress;
}

export interface AiExecutionStep {
  step: number;
  command: string;
  description: string;
  risky: boolean;
}

export interface AiExecutionPlan {
  steps: AiExecutionStep[];
  summary: string;
}

export type AiStepStatus = "pending" | "running" | "success" | "failed" | "skipped";

export interface AiStepResult {
  step: number;
  status: AiStepStatus;
  output?: string;
  error?: string;
}

export interface AiExecutionProgress {
  planSummary: string;
  steps: AiStepResult[];
  currentStep: number;
  completed: boolean;
}

export interface AiConversation {
  id: string;
  title: string;
  messages: AiChatMessage[];
  sessionId?: string;
  connectionId?: string;
  createdAt: string;
  updatedAt: string;
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
    rememberTemplateParams: true,
    batchMaxConcurrency: 5,
    batchRetryCount: 1
  },
  terminal: {
    backgroundColor: "#000000",
    foregroundColor: "#d8eaff",
    fontSize: 14,
    lineHeight: 1.2,
    fontFamily: "JetBrains Mono, Menlo, Monaco, monospace",
    localShell: {
      mode: "preset",
      preset: "system",
      customPath: ""
    }
  },
  ssh: {
    keepAliveEnabled: true,
    keepAliveIntervalSec: 15
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
    backgroundOpacity: 60,
    leftSidebarDefaultCollapsed: false,
    bottomWorkbenchDefaultCollapsed: false
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
    powProvider: "api.nxtrace.org",
    showTracerouteTab: true
  },
  audit: {
    enabled: false,
    retentionDays: 7
  },
  ai: {
    enabled: false,
    activeProviderId: undefined,
    providers: [],
    systemPromptOverride: undefined,
    executionTimeoutSec: 30,
    providerRequestTimeoutSec: 30,
    providerMaxRetries: 1
  }
};

export const normalizeBatchMaxConcurrency = (
  value: number | undefined,
  fallback: number
): number => {
  if (!Number.isInteger(value) || (value ?? 0) < 1 || (value ?? 0) > 50) {
    return fallback;
  }
  return value as number;
};

export const normalizeBatchRetryCount = (
  value: number | undefined,
  fallback: number
): number => {
  if (!Number.isInteger(value) || (value ?? 0) < 0 || (value ?? 0) > 5) {
    return fallback;
  }
  return value as number;
};
