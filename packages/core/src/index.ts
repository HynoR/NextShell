export type AuthType = "password" | "privateKey" | "agent" | "interactive";
export type ProxyType = "socks4" | "socks5";
export type TerminalEncoding = "utf-8" | "gb18030" | "gbk" | "big5";
export type BackspaceMode = "ascii-backspace" | "ascii-delete";
export type DeleteMode = "vt220-delete" | "ascii-delete" | "ascii-backspace";
export type SessionTarget = "remote" | "local";
export type LocalShellMode = "preset" | "custom";
export type LocalShellPreset = "system" | "powershell" | "cmd" | "zsh" | "sh" | "bash";
export type ShellIntegrationMode = "auto" | "off" | "manual";

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

export interface WorkspaceSecretEnvelopeShape {
  v: 1;
  alg: string;
  kdf: "scrypt";
  salt: string;
  iv: string;
  aad?: string;
  ciphertext: string;
  tag: string;
}

export interface WorkspaceRepoConnectionSnapshotItem {
  uuid: string;
  name: string;
  host: string;
  port: number;
  username: string;
  authType: AuthType;
  password?: WorkspaceSecretEnvelopeShape;
  sshKeyUuid?: string;
  hostFingerprint?: string;
  strictHostKeyChecking: boolean;
  proxyUuid?: string;
  keepAliveEnabled?: boolean;
  keepAliveIntervalSec?: number;
  terminalEncoding: TerminalEncoding;
  backspaceMode: BackspaceMode;
  deleteMode: DeleteMode;
  groupPath: string;
  tags: string[];
  notes?: string;
  favorite: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface WorkspaceRepoSshKeySnapshotItem {
  uuid: string;
  name: string;
  privateKey: WorkspaceSecretEnvelopeShape;
  passphrase?: WorkspaceSecretEnvelopeShape;
  createdAt: string;
  updatedAt: string;
}

export interface WorkspaceRepoProxySnapshotItem {
  uuid: string;
  name: string;
  proxyType: ProxyType;
  host: string;
  port: number;
  username?: string;
  password?: WorkspaceSecretEnvelopeShape;
  createdAt: string;
  updatedAt: string;
}

export interface WorkspaceRepoSnapshot {
  workspaceId: string;
  snapshotId: string;
  createdAt: string;
  connections: WorkspaceRepoConnectionSnapshotItem[];
  sshKeys: WorkspaceRepoSshKeySnapshotItem[];
  proxies: WorkspaceRepoProxySnapshotItem[];
}

export interface WorkspaceRepoLocalState {
  workspaceId: string;
  /** SHA-256 of sorted local resource identity/update tuples. */
  localFingerprint?: string;
  /** SHA-256 of sorted local command identity/update tuples. */
  localCommandsFingerprint?: string;
  /** Opaque server head token from the last successful sync. */
  remoteVersion?: string;
  remoteCommandsVersion?: string;
  lastSyncAt?: string;
  lastError?: string;
}

export interface WorkspaceRepoStatus {
  workspaceId: string;
  state: "idle" | "syncing" | "synced" | "error" | "disabled" | "diverged";
  lastSyncAt?: string;
  lastError?: string;
  commandsVersion?: string;
}

export type RecycleBinReason = "delete" | "danger_move";

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

export const LOCAL_DEFAULT_SCOPE_KEY = "local-default";

/** 构造 scopeKey: 对本地来说是 "local-default", 对云来说是 "<apiBaseUrl>-<workspaceName>" */
export const buildScopeKey = (origin: {
  kind: OriginKind;
  apiBaseUrl?: string;
  workspaceName?: string;
}): string => {
  if (origin.kind === "local") return LOCAL_DEFAULT_SCOPE_KEY;
  const base = (origin.apiBaseUrl ?? "").replace(/^https?:\/\//, "").replace(/[\/\s]+$/g, "");
  return `${base}-${origin.workspaceName ?? ""}`;
};

/** 构造 resourceId = "<scopeKey>-<uuidInScope>" */
export const buildResourceId = (scopeKey: string, uuidInScope: string): string =>
  `${scopeKey}-${uuidInScope}`;

/**
 * 用户自建的连接目录。取代靠 `groupPath` 字符串聚合出来的伪目录:目录成为实体后才能持久化空
 * 目录、重命名、删除与排序。
 *
 * `scopeKey` 是隔离边界(本地 / 某个云 workspace),与连接、密钥、代理用的是同一个口径
 * (`LOCAL_DEFAULT_SCOPE_KEY` 或 `buildScopeKey({ kind: "cloud", … })`);目录不跨 scope 引用。
 */
export interface ConnectionFolder {
  id: string;
  scopeKey: string;
  /** 顶层目录为 undefined。 */
  parentId?: string;
  name: string;
  /** 同级手工排序;相同值时按 name 兜底。 */
  sortIndex: number;
  createdAt: string;
  updatedAt: string;
}

/** SSH 密钥实体 — 独立于服务器连接，可被多个连接引用 */
export interface SshKeyProfile {
  id: string;
  name: string;
  /** 加密存储的密钥内容引用 (secret://sshkey-{id}) */
  keyContentRef: string;
  /** 加密存储的 passphrase 引用 (secret://sshkey-{id}-pass)，可选 */
  passphraseRef?: string;
  /** 保存时解析出的算法名，例如 ssh-ed25519 / ssh-rsa。 */
  keyType?: string;
  /** RSA 为模数位数，ed25519 恒为 256。 */
  keyBits?: number;
  /** 私钥自带的注释，通常是 user@host。 */
  keyComment?: string;
  /** OpenSSH 公钥指纹，与 `ssh-keygen -lf` 一致；导入时靠它自动重新绑定。 */
  fingerprint?: string;
  /** authorized_keys 可直接使用的单行公钥。私钥永远不出 vault。 */
  publicKeyLine?: string;
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

/** Agent（MCP）对单台主机的授权级别，缺省等同 "off"。 */
export type AgentAccessLevel = "off" | "readonly" | "full";

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
  /**
   * 派生投影：由 scope + 目录链算出来，写入时维护。
   * 之所以还留着，是因为云同步线协议、MCP 工具 schema 与导出文件格式都读它。
   * 本地的唯一真相是 `folderId`。
   */
  groupPath: string;
  /** 所属目录；顶层为 undefined。 */
  folderId?: string;
  tags: string[];
  notes?: string;
  favorite: boolean;
  monitorSession: boolean;
  /** 缺省（undefined）按 "off" 处理：未授权主机对 agent 完全不可见 */
  agentAccess?: AgentAccessLevel;
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
export type SessionType =
  "terminal" | "processManager" | "networkMonitor" | "editor" | "quickTransfer";

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

export interface MigrationRecord {
  version: number;
  name: string;
  appliedAt: string;
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
  appendCr?: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface WorkspaceCommandItem {
  id: string;
  workspaceId: string;
  name: string;
  description?: string;
  group: string;
  command: string;
  appendCr?: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface ScopedCommandItem extends SavedCommand {
  scope: "local" | "workspace";
  workspaceId?: string;
  workspaceName?: string;
}

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
    /** 是否允许远端程序通过 OSC 52 写入系统剪贴板 */
    oscClipboardWrite: boolean;
    /** 是否允许远端程序通过 OSC 52 读取系统剪贴板（默认关闭，开启需谨慎） */
    oscClipboardRead: boolean;
    /** 是否允许 OSC 9 / 777 桌面通知（窗口失焦时才弹出） */
    oscNotifications: boolean;
    /** 是否允许 OSC 0/2 修改会话标签标题 */
    oscTitleUpdates: boolean;
    /** 打开终端超链接前是否弹窗确认完整目标地址 */
    hyperlinkConfirm: boolean;
    /** Shell 集成注入策略：auto 自动检测注入 / manual 仅提供安装命令 / off 纯被动解析 */
    shellIntegration: ShellIntegrationMode;
    /** 设置了 APP 背景图时终端本体的透出行为 */
    wallpaper: {
      /** 终端画布是否透明，让 APP 背景图透出（无背景图时该项无效） */
      seeThrough: boolean;
      /**
       * 透出时是否仍加载 WebGL 渲染器。默认关闭：xterm 的
       * 透明 + 大流量输出字形残影修复（上游 #5847 / PR #5883）
       * 尚未进入 stable 版本，透出时走 DOM 渲染器更稳妥。
       */
      useWebgl: boolean;
    };
  };
  ssh: {
    /** 是否对所有连接启用 keepalive（发送空包） */
    keepAliveEnabled: boolean;
    /** Keepalive 间隔（秒） */
    keepAliveIntervalSec: number;
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
  connectionManager: {
    /** 对话框尺寸与栏宽都记住，避免每次打开都要重新拖。 */
    dialogWidth: number;
    dialogHeight: number;
    folderColumnWidth: number;
    detailColumnWidth: number;
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
  agent: {
    /**
     * Agent（MCP）端点总开关。默认关闭：关闭时主进程不监听任何 socket 或端口，
     * 下面的监听开关全部无效。
     */
    enabled: boolean;
    /** 是否监听 Unix socket / 命名管道（0600，靠 OS 授权，无 token 可泄） */
    socketEnabled: boolean;
    /** 是否额外监听 127.0.0.1 TCP（需 Bearer token），默认关闭 */
    tcpEnabled: boolean;
    /** TCP 监听端口，0 表示由系统分配 */
    tcpPort: number;
    /** 写操作（文件写入 / 传输 / PTY 注入）是否需要应用内确认 */
    confirmWrites: boolean;
    /** exec 命中未知命令（不在只读白名单也不在危险黑名单）时是否需要应用内确认 */
    confirmUnknownCommands: boolean;
    /** 允许 agent 读取的本地根目录，空数组表示不额外限制（默认拒绝清单仍生效） */
    allowedLocalRoots: string[];
    /** 单条 exec 的默认超时（秒） */
    execTimeoutSec: number;
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
    oscClipboardWrite?: boolean;
    oscClipboardRead?: boolean;
    oscNotifications?: boolean;
    oscTitleUpdates?: boolean;
    hyperlinkConfirm?: boolean;
    shellIntegration?: ShellIntegrationMode;
    wallpaper?: {
      seeThrough?: boolean;
      useWebgl?: boolean;
    };
  };
  ssh?: {
    keepAliveEnabled?: boolean;
    keepAliveIntervalSec?: number;
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
  connectionManager?: {
    dialogWidth?: number;
    dialogHeight?: number;
    folderColumnWidth?: number;
    detailColumnWidth?: number;
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
  agent?: {
    enabled?: boolean;
    socketEnabled?: boolean;
    tcpEnabled?: boolean;
    tcpPort?: number;
    confirmWrites?: boolean;
    confirmUnknownCommands?: boolean;
    allowedLocalRoots?: string[];
    execTimeoutSec?: number;
  };
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

/** Reference to an SSH key in an export file. Never contains private key material. */
export interface ExportedSshKeyRef {
  name: string;
  /**
   * OpenSSH 公钥指纹(`SHA256:…`,与 `ssh-keygen -lf` 一致),导入时靠它自动重新绑定。
   * 早于"保存即解析"的旧密钥没有指纹,那些条目退回按名称匹配。
   */
  fingerprint?: string;
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
  /** Key name + fingerprint so a later import can rebind without shipping the private key. */
  sshKeyRef?: ExportedSshKeyRef;
}

export interface ConnectionImportEntry extends ExportedConnection {
  passwordUnavailable?: boolean;
  sourceFormat: "nextshell" | "finalshell";
  sourceFileName?: string;
  sourceRelativePath?: string;
  /** Resolved or user-picked key id for this import batch. */
  sshKeyId?: string;
  /** True when private-key auth cannot be rebound automatically. */
  needsKeyRebind?: boolean;
  /** Original auth before import degraded it (e.g. FinalShell key auth → interactive). */
  originalAuth?: AuthType;
}

export interface ConnectionExportFile {
  format: "nextshell-connections";
  version: 1;
  exportedAt: string;
  /**
   * Legacy flag (read-only for backward-compatible import): each connection's
   * `password` was XOR-obfuscated with SHA256(name+host+port). The obfuscation
   * key is derived purely from fields present in the file, so it provided no
   * real confidentiality — newer exports no longer produce it (see
   * `passwordsOmitted`). Still honoured on import so old files keep working.
   */
  passwordsObfuscated?: boolean;
  /**
   * When true, this unencrypted export intentionally omits credential secrets
   * (`password`). To export connections together with their secrets, use an
   * encrypted export (AES-256-GCM) instead.
   */
  passwordsOmitted?: boolean;
  connections: ExportedConnection[];
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
    },
    oscClipboardWrite: true,
    oscClipboardRead: false,
    oscNotifications: true,
    oscTitleUpdates: true,
    hyperlinkConfirm: true,
    shellIntegration: "auto",
    wallpaper: {
      seeThrough: true,
      useWebgl: false
    }
  },
  ssh: {
    keepAliveEnabled: true,
    keepAliveIntervalSec: 15
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
  connectionManager: {
    dialogWidth: 1180,
    dialogHeight: 760,
    folderColumnWidth: 240,
    detailColumnWidth: 340
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
  agent: {
    enabled: false,
    socketEnabled: true,
    tcpEnabled: false,
    tcpPort: 0,
    confirmWrites: true,
    confirmUnknownCommands: true,
    allowedLocalRoots: [],
    execTimeoutSec: 60
  }
};
