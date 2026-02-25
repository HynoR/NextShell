import { z } from "zod";
import { DEFAULT_APP_PREFERENCES } from "../../core/src/index";

const trimToOptionalString = (value: unknown): unknown => {
  if (typeof value !== "string") {
    return value;
  }

  const trimmed = value.trim();
  return trimmed.length > 0 ? trimmed : undefined;
};

const trimToString = (value: unknown): string => {
  if (typeof value !== "string") {
    return "";
  }

  return value.trim();
};

const terminalColorSchema = z.string().regex(/^#[0-9a-fA-F]{6}$/);

const trimAndFilterStringArray = (value: unknown): unknown => {
  if (!Array.isArray(value)) {
    return value;
  }

  return value
    .map((item) => (typeof item === "string" ? item.trim() : item))
    .filter((item): item is string => typeof item === "string" && item.length > 0);
};

export const authTypeSchema = z.enum(["password", "privateKey", "agent"]);
export const proxyTypeSchema = z.enum(["socks4", "socks5"]);
export const terminalEncodingSchema = z.enum(["utf-8", "gb18030", "gbk", "big5"]);
export const backspaceModeSchema = z.enum(["ascii-backspace", "ascii-delete"]);
export const deleteModeSchema = z.enum(["vt220-delete", "ascii-delete", "ascii-backspace"]);
export const backupConflictPolicySchema = z.enum(["skip", "force"]);
export const restoreConflictPolicySchema = z.enum(["skip_older", "force"]);
export const windowAppearanceSchema = z.enum(["system", "light", "dark"]);

export const connectionListQuerySchema = z.object({
  keyword: z.string().trim().optional(),
  group: z.string().trim().optional(),
  favoriteOnly: z.boolean().optional().default(false)
});

export const connectionUpsertSchema = z.object({
  id: z.string().uuid().optional(),
  name: z.string().min(1),
  host: z.string().min(1),
  port: z.coerce.number().int().min(1).max(65535).default(22),
  username: z.preprocess(trimToString, z.string()),
  authType: authTypeSchema.default("password"),
  password: z.preprocess(trimToOptionalString, z.string().min(1).optional()),
  sshKeyId: z.string().uuid().optional(),
  hostFingerprint: z.preprocess(trimToOptionalString, z.string().min(1).optional()),
  strictHostKeyChecking: z.boolean().default(false),
  proxyId: z.string().uuid().optional(),
  terminalEncoding: terminalEncodingSchema.default("utf-8"),
  backspaceMode: backspaceModeSchema.default("ascii-backspace"),
  deleteMode: deleteModeSchema.default("vt220-delete"),
  groupPath: z.preprocess(
    (v) => typeof v === "string" ? v.trim() : v,
    z.string().min(1).refine((s) => s.startsWith("/"), { message: "分组路径必须以 / 开头" })
  ),
  tags: z.preprocess(trimAndFilterStringArray, z.array(z.string().min(1)).default([])),
  notes: z.preprocess(trimToOptionalString, z.string().optional()),
  favorite: z.boolean().default(false),
  monitorSession: z.boolean().default(false)
}).superRefine((value, ctx) => {
  if (value.authType === "privateKey" && !value.sshKeyId) {
    ctx.addIssue({
      code: z.ZodIssueCode.custom,
      message: "sshKeyId is required when authType is privateKey",
      path: ["sshKeyId"]
    });
  }
});

export const connectionRemoveSchema = z.object({
  id: z.string().uuid()
});

export const sessionAuthOverrideSchema = z.object({
  username: z.preprocess(trimToOptionalString, z.string().min(1).optional()),
  authType: z.enum(["password", "privateKey"]),
  password: z.preprocess(trimToOptionalString, z.string().min(1).optional()),
  sshKeyId: z.string().uuid().optional(),
  /** Temporary key content for retry (not persisted as entity) */
  privateKeyContent: z.preprocess(trimToOptionalString, z.string().min(1).optional()),
  passphrase: z.preprocess(trimToOptionalString, z.string().min(1).optional())
}).superRefine((value, ctx) => {
    if (value.authType === "password" && !value.password) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: "password is required when authType is password",
        path: ["password"]
      });
    }
  });

export const sessionOpenSchema = z.object({
  connectionId: z.string().uuid(),
  sessionId: z.string().uuid().optional(),
  authOverride: sessionAuthOverrideSchema.optional()
});

export const sessionWriteSchema = z.object({
  sessionId: z.string().uuid(),
  data: z.string()
});

export const sessionResizeSchema = z.object({
  sessionId: z.string().uuid(),
  cols: z.number().int().min(20).max(500),
  rows: z.number().int().min(5).max(300)
});

export const sessionCloseSchema = z.object({
  sessionId: z.string().uuid()
});

export const sessionGetCwdSchema = z.object({
  connectionId: z.string().uuid()
});

export const sessionDataEventSchema = z.object({
  sessionId: z.string().uuid(),
  data: z.string()
});

export const sessionStatusEventSchema = z.object({
  sessionId: z.string().uuid(),
  status: z.enum(["connecting", "connected", "disconnected", "failed"]),
  reason: z.string().optional()
});

export const monitorSystemInfoSnapshotSchema = z.object({
  connectionId: z.string().uuid()
});

export const monitorSystemStartSchema = z.object({
  connectionId: z.string().uuid()
});

export const monitorSystemStopSchema = z.object({
  connectionId: z.string().uuid()
});

export const monitorSystemSelectInterfaceSchema = z.object({
  connectionId: z.string().uuid(),
  networkInterface: z.string().trim().min(1).max(64)
});

export const monitorProcessStartSchema = z.object({
  connectionId: z.string().uuid()
});

export const monitorProcessStopSchema = z.object({
  connectionId: z.string().uuid()
});

export const monitorProcessDetailSchema = z.object({
  connectionId: z.string().uuid(),
  pid: z.coerce.number().int().min(1)
});

export const monitorProcessKillSchema = z.object({
  connectionId: z.string().uuid(),
  pid: z.coerce.number().int().min(1),
  signal: z.enum(["SIGTERM", "SIGKILL"]).default("SIGTERM")
});

export const monitorNetworkStartSchema = z.object({
  connectionId: z.string().uuid()
});

export const monitorNetworkStopSchema = z.object({
  connectionId: z.string().uuid()
});

export const monitorNetworkConnectionsSchema = z.object({
  connectionId: z.string().uuid(),
  port: z.coerce.number().int().min(1).max(65535)
});

export const commandExecSchema = z.object({
  connectionId: z.string().uuid(),
  command: z.string().trim().min(1)
});

export const commandBatchExecSchema = z.object({
  command: z.string().trim().min(1),
  connectionIds: z.array(z.string().uuid()).min(1),
  maxConcurrency: z.coerce.number().int().min(1).max(50).default(5),
  retryCount: z.coerce.number().int().min(0).max(5).default(1)
});

export const auditListSchema = z.object({
  limit: z.coerce.number().int().min(1).max(500).default(100)
});

export const storageMigrationsSchema = z.object({});

export const sftpListSchema = z.object({
  connectionId: z.string().uuid(),
  path: z.string().min(1)
});

export const sftpUploadSchema = z.object({
  connectionId: z.string().uuid(),
  localPath: z.string().min(1),
  remotePath: z.string().min(1),
  taskId: z.string().uuid().optional()
});

export const sftpDownloadSchema = z.object({
  connectionId: z.string().uuid(),
  remotePath: z.string().min(1),
  localPath: z.string().min(1),
  taskId: z.string().uuid().optional()
});

export const sftpMkdirSchema = z.object({
  connectionId: z.string().uuid(),
  path: z.string().min(1)
});

export const sftpRenameSchema = z.object({
  connectionId: z.string().uuid(),
  fromPath: z.string().min(1),
  toPath: z.string().min(1)
});

export const sftpDeleteSchema = z.object({
  connectionId: z.string().uuid(),
  path: z.string().min(1),
  type: z.enum(["file", "directory", "link"])
});

export const commandHistoryListSchema = z.object({});

export const commandHistoryPushSchema = z.object({
  command: z.string().trim().min(1)
});

export const commandHistoryRemoveSchema = z.object({
  command: z.string().min(1)
});

export const commandHistoryClearSchema = z.object({});

export const savedCommandListSchema = z.object({
  keyword: z.string().trim().optional(),
  group: z.string().trim().optional()
});

export const savedCommandUpsertSchema = z.object({
  id: z.string().uuid().optional(),
  name: z.string().trim().min(1),
  description: z.preprocess(trimToOptionalString, z.string().optional()),
  group: z.string().trim().min(1).default("默认"),
  command: z.string().trim().min(1),
  isTemplate: z.boolean().default(false)
});

export const savedCommandRemoveSchema = z.object({
  id: z.string().uuid()
});

export const sftpEditOpenSchema = z.object({
  connectionId: z.string().uuid(),
  remotePath: z.string().min(1),
  editorCommand: z.string()
});

export const sftpEditOpenBuiltinSchema = z.object({
  connectionId: z.string().uuid(),
  remotePath: z.string().min(1)
});

export const sftpEditSaveBuiltinSchema = z.object({
  editId: z.string().uuid(),
  connectionId: z.string().uuid(),
  remotePath: z.string().min(1),
  content: z.string()
});

export const sftpEditStopSchema = z.object({
  editId: z.string().uuid()
});

export const sftpEditStatusEventSchema = z.object({
  editId: z.string().uuid(),
  connectionId: z.string().uuid(),
  remotePath: z.string(),
  status: z.enum(["downloading", "editing", "uploading", "synced", "error", "closed"]),
  message: z.string().optional()
});

export const sftpEditSessionInfoSchema = z.object({
  editId: z.string().uuid(),
  connectionId: z.string().uuid(),
  remotePath: z.string(),
  localPath: z.string(),
  status: z.enum(["editing", "uploading"]),
  lastActivityAt: z.number()
});

export const appPreferencesSchema = z.object({
  transfer: z.object({
    uploadDefaultDir: z.string().min(1).default(DEFAULT_APP_PREFERENCES.transfer.uploadDefaultDir),
    downloadDefaultDir: z.string().min(1).default(DEFAULT_APP_PREFERENCES.transfer.downloadDefaultDir)
  }).default(DEFAULT_APP_PREFERENCES.transfer),
  remoteEdit: z.object({
    defaultEditorCommand: z.string().default(DEFAULT_APP_PREFERENCES.remoteEdit.defaultEditorCommand),
    editorMode: z.enum(["builtin", "external"]).default(DEFAULT_APP_PREFERENCES.remoteEdit.editorMode)
  }).default(DEFAULT_APP_PREFERENCES.remoteEdit),
  commandCenter: z.object({
    rememberTemplateParams: z.boolean().default(DEFAULT_APP_PREFERENCES.commandCenter.rememberTemplateParams),
    batchMaxConcurrency: z.coerce.number().int().min(1).max(50).default(DEFAULT_APP_PREFERENCES.commandCenter.batchMaxConcurrency),
    batchRetryCount: z.coerce.number().int().min(0).max(5).default(DEFAULT_APP_PREFERENCES.commandCenter.batchRetryCount)
  }).default(DEFAULT_APP_PREFERENCES.commandCenter),
  terminal: z.object({
    backgroundColor: terminalColorSchema.default(DEFAULT_APP_PREFERENCES.terminal.backgroundColor),
    foregroundColor: terminalColorSchema.default(DEFAULT_APP_PREFERENCES.terminal.foregroundColor),
    fontSize: z.coerce.number().int().min(10).max(24).default(DEFAULT_APP_PREFERENCES.terminal.fontSize),
    lineHeight: z.coerce.number().min(1).max(2).default(DEFAULT_APP_PREFERENCES.terminal.lineHeight)
  }).default(DEFAULT_APP_PREFERENCES.terminal),
  backup: z.object({
    remotePath: z.string().default(DEFAULT_APP_PREFERENCES.backup.remotePath),
    rclonePath: z.string().default(DEFAULT_APP_PREFERENCES.backup.rclonePath),
    defaultBackupConflictPolicy: backupConflictPolicySchema.default(DEFAULT_APP_PREFERENCES.backup.defaultBackupConflictPolicy),
    defaultRestoreConflictPolicy: restoreConflictPolicySchema.default(DEFAULT_APP_PREFERENCES.backup.defaultRestoreConflictPolicy),
    rememberPassword: z.boolean().default(DEFAULT_APP_PREFERENCES.backup.rememberPassword),
    lastBackupAt: z.string().nullable().default(DEFAULT_APP_PREFERENCES.backup.lastBackupAt)
  }).default(DEFAULT_APP_PREFERENCES.backup),
  window: z.object({
    appearance: windowAppearanceSchema.default(DEFAULT_APP_PREFERENCES.window.appearance),
    minimizeToTray: z.boolean().default(DEFAULT_APP_PREFERENCES.window.minimizeToTray),
    confirmBeforeClose: z.boolean().default(DEFAULT_APP_PREFERENCES.window.confirmBeforeClose),
    backgroundImagePath: z.string().default(DEFAULT_APP_PREFERENCES.window.backgroundImagePath),
    backgroundOpacity: z.coerce.number().int().min(30).max(80).default(DEFAULT_APP_PREFERENCES.window.backgroundOpacity)
  }).default(DEFAULT_APP_PREFERENCES.window),
  traceroute: z.object({
    nexttracePath: z.string().default(DEFAULT_APP_PREFERENCES.traceroute.nexttracePath),
    protocol: z.enum(["icmp", "tcp", "udp"]).default(DEFAULT_APP_PREFERENCES.traceroute.protocol),
    port: z.coerce.number().int().min(0).max(65535).default(DEFAULT_APP_PREFERENCES.traceroute.port),
    queries: z.coerce.number().int().min(1).max(10).default(DEFAULT_APP_PREFERENCES.traceroute.queries),
    maxHops: z.coerce.number().int().min(1).max(64).default(DEFAULT_APP_PREFERENCES.traceroute.maxHops),
    ipVersion: z.enum(["auto", "ipv4", "ipv6"]).default(DEFAULT_APP_PREFERENCES.traceroute.ipVersion),
    dataProvider: z.enum(["LeoMoeAPI", "ip-api.com", "IPInfo", "IPInsight", "IP.SB", "disable-geoip"]).default(DEFAULT_APP_PREFERENCES.traceroute.dataProvider),
    noRdns: z.boolean().default(DEFAULT_APP_PREFERENCES.traceroute.noRdns),
    language: z.enum(["cn", "en"]).default(DEFAULT_APP_PREFERENCES.traceroute.language),
    powProvider: z.enum(["api.nxtrace.org", "sakura"]).default(DEFAULT_APP_PREFERENCES.traceroute.powProvider)
  }).default(DEFAULT_APP_PREFERENCES.traceroute),
  audit: z.object({
    retentionDays: z.coerce.number().int().min(0).max(365).default(DEFAULT_APP_PREFERENCES.audit.retentionDays)
  }).default(DEFAULT_APP_PREFERENCES.audit)
}).default(DEFAULT_APP_PREFERENCES);

export const appPreferencesPatchSchema = z.object({
  transfer: z.object({
    uploadDefaultDir: z.string().min(1).optional(),
    downloadDefaultDir: z.string().min(1).optional()
  }).optional(),
  remoteEdit: z.object({
    defaultEditorCommand: z.string().optional(),
    editorMode: z.enum(["builtin", "external"]).optional()
  }).optional(),
  commandCenter: z.object({
    rememberTemplateParams: z.boolean().optional(),
    batchMaxConcurrency: z.coerce.number().int().min(1).max(50).optional(),
    batchRetryCount: z.coerce.number().int().min(0).max(5).optional()
  }).optional(),
  terminal: z.object({
    backgroundColor: terminalColorSchema.optional(),
    foregroundColor: terminalColorSchema.optional(),
    fontSize: z.coerce.number().int().min(10).max(24).optional(),
    lineHeight: z.coerce.number().min(1).max(2).optional()
  }).optional(),
  backup: z.object({
    remotePath: z.string().optional(),
    rclonePath: z.string().optional(),
    defaultBackupConflictPolicy: backupConflictPolicySchema.optional(),
    defaultRestoreConflictPolicy: restoreConflictPolicySchema.optional(),
    rememberPassword: z.boolean().optional(),
    lastBackupAt: z.string().nullable().optional()
  }).optional(),
  window: z.object({
    appearance: windowAppearanceSchema.optional(),
    minimizeToTray: z.boolean().optional(),
    confirmBeforeClose: z.boolean().optional(),
    backgroundImagePath: z.string().optional(),
    backgroundOpacity: z.coerce.number().int().min(30).max(80).optional()
  }).optional(),
  traceroute: z.object({
    nexttracePath: z.string().optional(),
    protocol: z.enum(["icmp", "tcp", "udp"]).optional(),
    port: z.coerce.number().int().min(0).max(65535).optional(),
    queries: z.coerce.number().int().min(1).max(10).optional(),
    maxHops: z.coerce.number().int().min(1).max(64).optional(),
    ipVersion: z.enum(["auto", "ipv4", "ipv6"]).optional(),
    dataProvider: z.enum(["LeoMoeAPI", "ip-api.com", "IPInfo", "IPInsight", "IP.SB", "disable-geoip"]).optional(),
    noRdns: z.boolean().optional(),
    language: z.enum(["cn", "en"]).optional(),
    powProvider: z.enum(["api.nxtrace.org", "sakura"]).optional()
  }).optional(),
  audit: z.object({
    retentionDays: z.coerce.number().int().min(0).max(365).optional()
  }).optional()
});

export const settingsGetSchema = z.object({});
export const settingsUpdateSchema = appPreferencesPatchSchema;

export const dialogOpenFilesSchema = z.object({
  title: z.string().trim().min(1).optional(),
  defaultPath: z.string().trim().min(1).optional(),
  filters: z
    .array(
      z.object({
        name: z.string().trim().min(1),
        extensions: z.array(z.string().trim().min(1)).min(1)
      })
    )
    .min(1)
    .optional(),
  multi: z.boolean().default(true)
});

export const dialogOpenDirectorySchema = z.object({
  title: z.string().trim().min(1).optional(),
  defaultPath: z.string().trim().min(1).optional()
});

export const dialogOpenPathSchema = z.object({
  path: z.string().trim().min(1),
  revealInFolder: z.boolean().default(false)
});

export const sftpTransferStatusEventSchema = z.object({
  taskId: z.string().uuid().optional(),
  direction: z.enum(["upload", "download"]),
  connectionId: z.string().uuid(),
  remotePath: z.string().min(1),
  localPath: z.string().min(1),
  status: z.enum(["queued", "running", "success", "failed"]),
  progress: z.coerce.number().min(0).max(100).default(0),
  message: z.string().optional(),
  error: z.string().optional()
});

export const backupListSchema = z.object({});

export const backupRunSchema = z.object({
  conflictPolicy: backupConflictPolicySchema.default("skip")
});

export const backupRestoreSchema = z.object({
  archiveId: z.string().min(1),
  conflictPolicy: restoreConflictPolicySchema.default("skip_older")
});

export const backupPasswordSetSchema = z.object({
  password: z.string().min(6, "云存档密码至少6个字符"),
  confirmPassword: z.string().min(1)
}).refine((data) => data.password === data.confirmPassword, {
  message: "两次输入的密码不一致",
  path: ["confirmPassword"]
});

export const backupPasswordUnlockSchema = z.object({
  password: z.string().min(1)
});

export const backupPasswordClearRememberedSchema = z.object({});

export const backupPasswordStatusSchema = z.object({});

export const masterPasswordSetSchema = backupPasswordSetSchema;
export const masterPasswordUnlockSchema = backupPasswordUnlockSchema;
export const masterPasswordClearRememberedSchema = backupPasswordClearRememberedSchema;
export const masterPasswordStatusSchema = backupPasswordStatusSchema;
export const masterPasswordGetCachedSchema = z.object({});

export const templateParamsListSchema = z.object({
  commandId: z.string().uuid().optional()
});

export const templateParamsUpsertSchema = z.object({
  commandId: z.string().uuid(),
  params: z.record(z.string(), z.string())
});

export const templateParamsClearSchema = z.object({
  commandId: z.string().uuid()
});

// ─── SSH Key Management ─────────────────────────────────────────────────────

export const sshKeyListSchema = z.object({});

export const sshKeyUpsertSchema = z.object({
  id: z.string().uuid().optional(),
  name: z.string().trim().min(1),
  keyContent: z.preprocess(trimToOptionalString, z.string().min(1).optional()),
  passphrase: z.preprocess(trimToOptionalString, z.string().min(1).optional())
});

export const sshKeyRemoveSchema = z.object({
  id: z.string().uuid(),
  force: z.boolean().default(false)
});

// ─── Proxy Management ───────────────────────────────────────────────────────

export const proxyListSchema = z.object({});

export const proxyUpsertSchema = z.object({
  id: z.string().uuid().optional(),
  name: z.string().trim().min(1),
  proxyType: proxyTypeSchema,
  host: z.string().trim().min(1),
  port: z.coerce.number().int().min(1).max(65535),
  username: z.preprocess(trimToOptionalString, z.string().min(1).optional()),
  password: z.preprocess(trimToOptionalString, z.string().min(1).optional())
}).superRefine((value, ctx) => {
  if (value.proxyType === "socks4" && value.password) {
    ctx.addIssue({
      code: z.ZodIssueCode.custom,
      message: "SOCKS4 does not support password authentication",
      path: ["password"]
    });
  }
});

export const proxyRemoveSchema = z.object({
  id: z.string().uuid(),
  force: z.boolean().default(false)
});

// ─── Connection Import/Export ────────────────────────────────────────────────

export const connectionExportSchema = z.object({
  connectionIds: z.array(z.string().uuid()).min(1),
  encryptionPassword: z.preprocess(trimToOptionalString, z.string().min(6).optional())
});

export const connectionExportBatchSchema = z.object({
  connectionIds: z.array(z.string().uuid()).min(1),
  directoryPath: z.string().min(1),
  encryptionPassword: z.preprocess(trimToOptionalString, z.string().min(6).optional())
});

export const connectionRevealPasswordSchema = z.object({
  connectionId: z.string().uuid(),
  masterPassword: z.preprocess(trimToOptionalString, z.string().min(1).optional())
});

export const connectionImportPreviewSchema = z.object({
  filePath: z.string().min(1),
  decryptionPassword: z.preprocess(trimToOptionalString, z.string().min(1).optional())
});

export const connectionImportFinalShellPreviewSchema = z.object({
  filePath: z.string().min(1)
});

export const connectionImportExecuteSchema = z.object({
  entries: z.array(z.object({
    name: z.string().min(1),
    host: z.string().min(1),
    port: z.coerce.number().int().min(1).max(65535),
    username: z.string(),
    authType: authTypeSchema,
    password: z.string().optional(),
    groupPath: z.string().min(1),
    tags: z.array(z.string()).default([]),
    notes: z.string().optional(),
    favorite: z.boolean().default(false),
    terminalEncoding: terminalEncodingSchema.default("utf-8"),
    backspaceMode: backspaceModeSchema.default("ascii-backspace"),
    deleteMode: deleteModeSchema.default("vt220-delete"),
    monitorSession: z.boolean().default(false),
  })),
  conflictPolicy: z.enum(["skip", "overwrite", "duplicate"]).default("skip")
});

export type ConnectionListQueryInput = z.infer<typeof connectionListQuerySchema>;
export type ConnectionUpsertInput = z.infer<typeof connectionUpsertSchema>;
export type ConnectionRemoveInput = z.infer<typeof connectionRemoveSchema>;
export type SessionOpenInput = z.infer<typeof sessionOpenSchema>;
export type SessionAuthOverrideInput = NonNullable<SessionOpenInput["authOverride"]>;
export type SessionWriteInput = z.infer<typeof sessionWriteSchema>;
export type SessionResizeInput = z.infer<typeof sessionResizeSchema>;
export type SessionCloseInput = z.infer<typeof sessionCloseSchema>;
export type SessionGetCwdInput = z.infer<typeof sessionGetCwdSchema>;
export type SessionDataEvent = z.infer<typeof sessionDataEventSchema>;
export type SessionStatusEvent = z.infer<typeof sessionStatusEventSchema>;
export type MonitorSystemInfoSnapshotInput = z.infer<typeof monitorSystemInfoSnapshotSchema>;
export type MonitorSystemStartInput = z.infer<typeof monitorSystemStartSchema>;
export type MonitorSystemStopInput = z.infer<typeof monitorSystemStopSchema>;
export type MonitorSystemSelectInterfaceInput = z.infer<typeof monitorSystemSelectInterfaceSchema>;
export type MonitorProcessStartInput = z.infer<typeof monitorProcessStartSchema>;
export type MonitorProcessStopInput = z.infer<typeof monitorProcessStopSchema>;
export type MonitorProcessDetailInput = z.infer<typeof monitorProcessDetailSchema>;
export type MonitorProcessKillInput = z.infer<typeof monitorProcessKillSchema>;
export type MonitorNetworkStartInput = z.infer<typeof monitorNetworkStartSchema>;
export type MonitorNetworkStopInput = z.infer<typeof monitorNetworkStopSchema>;
export type MonitorNetworkConnectionsInput = z.infer<typeof monitorNetworkConnectionsSchema>;
export type CommandExecInput = z.infer<typeof commandExecSchema>;
export type CommandBatchExecInput = z.infer<typeof commandBatchExecSchema>;
export type AuditListInput = z.infer<typeof auditListSchema>;
export type StorageMigrationsInput = z.infer<typeof storageMigrationsSchema>;
export type SftpListInput = z.infer<typeof sftpListSchema>;
export type SftpUploadInput = z.infer<typeof sftpUploadSchema>;
export type SftpDownloadInput = z.infer<typeof sftpDownloadSchema>;
export type SftpMkdirInput = z.infer<typeof sftpMkdirSchema>;
export type SftpRenameInput = z.infer<typeof sftpRenameSchema>;
export type SftpDeleteInput = z.infer<typeof sftpDeleteSchema>;
export type CommandHistoryListInput = z.infer<typeof commandHistoryListSchema>;
export type CommandHistoryPushInput = z.infer<typeof commandHistoryPushSchema>;
export type CommandHistoryRemoveInput = z.infer<typeof commandHistoryRemoveSchema>;
export type CommandHistoryClearInput = z.infer<typeof commandHistoryClearSchema>;
export type SavedCommandListInput = z.infer<typeof savedCommandListSchema>;
export type SavedCommandUpsertInput = z.infer<typeof savedCommandUpsertSchema>;
export type SavedCommandRemoveInput = z.infer<typeof savedCommandRemoveSchema>;
export type SftpEditOpenInput = z.infer<typeof sftpEditOpenSchema>;
export type SftpEditOpenBuiltinInput = z.infer<typeof sftpEditOpenBuiltinSchema>;
export type SftpEditSaveBuiltinInput = z.infer<typeof sftpEditSaveBuiltinSchema>;
export type SftpEditStopInput = z.infer<typeof sftpEditStopSchema>;
export type SftpEditStatusEvent = z.infer<typeof sftpEditStatusEventSchema>;
export type SftpEditSessionInfo = z.infer<typeof sftpEditSessionInfoSchema>;
export type AppPreferences = z.infer<typeof appPreferencesSchema>;
export type AppPreferencesPatchInput = z.infer<typeof appPreferencesPatchSchema>;
/** @alias AppPreferencesPatchInput */
export type SettingsUpdateInput = AppPreferencesPatchInput;
export type DialogOpenFilesInput = z.infer<typeof dialogOpenFilesSchema>;
export type DialogOpenDirectoryInput = z.infer<typeof dialogOpenDirectorySchema>;
export type DialogOpenPathInput = z.infer<typeof dialogOpenPathSchema>;
export type SftpTransferStatusEvent = z.infer<typeof sftpTransferStatusEventSchema>;
export type BackupListInput = z.infer<typeof backupListSchema>;
export type BackupRunInput = z.infer<typeof backupRunSchema>;
export type BackupRestoreInput = z.infer<typeof backupRestoreSchema>;
export type BackupPasswordSetInput = z.infer<typeof backupPasswordSetSchema>;
export type BackupPasswordUnlockInput = z.infer<typeof backupPasswordUnlockSchema>;
export type BackupPasswordClearRememberedInput = z.infer<typeof backupPasswordClearRememberedSchema>;
export type BackupPasswordStatusInput = z.infer<typeof backupPasswordStatusSchema>;
export type MasterPasswordSetInput = z.infer<typeof masterPasswordSetSchema>;
export type MasterPasswordUnlockInput = z.infer<typeof masterPasswordUnlockSchema>;
export type MasterPasswordClearRememberedInput = z.infer<typeof masterPasswordClearRememberedSchema>;
export type MasterPasswordStatusInput = z.infer<typeof masterPasswordStatusSchema>;
export type MasterPasswordGetCachedInput = z.infer<typeof masterPasswordGetCachedSchema>;
export type TemplateParamsListInput = z.infer<typeof templateParamsListSchema>;
export type TemplateParamsUpsertInput = z.infer<typeof templateParamsUpsertSchema>;
export type TemplateParamsClearInput = z.infer<typeof templateParamsClearSchema>;
export type SshKeyListInput = z.infer<typeof sshKeyListSchema>;
export type SshKeyUpsertInput = z.infer<typeof sshKeyUpsertSchema>;
export type SshKeyRemoveInput = z.infer<typeof sshKeyRemoveSchema>;
export type ProxyListInput = z.infer<typeof proxyListSchema>;
export type ProxyUpsertInput = z.infer<typeof proxyUpsertSchema>;
export type ProxyRemoveInput = z.infer<typeof proxyRemoveSchema>;
export type ConnectionExportInput = z.infer<typeof connectionExportSchema>;
export type ConnectionExportBatchInput = z.infer<typeof connectionExportBatchSchema>;
export type ConnectionRevealPasswordInput = z.infer<typeof connectionRevealPasswordSchema>;
export type ConnectionImportPreviewInput = z.infer<typeof connectionImportPreviewSchema>;
export type ConnectionImportFinalShellPreviewInput = z.infer<typeof connectionImportFinalShellPreviewSchema>;
export type ConnectionImportExecuteInput = z.infer<typeof connectionImportExecuteSchema>;

export interface ConnectionExportBatchFileItem {
  connectionId: string;
  filePath: string;
  fileName: string;
}

export interface ConnectionExportBatchResult {
  total: number;
  exported: number;
  failed: number;
  encrypted: boolean;
  directoryPath: string;
  files: ConnectionExportBatchFileItem[];
  errors: string[];
}

export interface MasterPasswordStatusResult {
  isSet: boolean;
  isUnlocked: boolean;
  keytarAvailable: boolean;
}

export interface MasterPasswordCachedResult {
  password?: string;
}

export interface ConnectionRevealPasswordResult {
  password: string;
}

export const updateCheckSchema = z.object({});
export type UpdateCheckInput = z.infer<typeof updateCheckSchema>;

export interface DebugLogEntry {
  id: string;
  timestamp: number;
  connectionId: string;
  command: string;
  stdout: string;
  exitCode: number;
  durationMs: number;
  ok: boolean;
  error?: string;
}

export interface UpdateCheckResult {
  currentVersion: string;
  latestVersion: string | null;
  hasUpdate: boolean;
  releaseUrl: string | null;
  error: string | null;
}

export const pingRequestSchema = z.object({
  host: z.string().trim().min(1).max(253)
});
export type PingRequestInput = z.infer<typeof pingRequestSchema>;

export type PingResult =
  | { ok: true; avgMs: number }
  | { ok: false; error: string };

// ─── Traceroute ──────────────────────────────────────────────────────────

export const tracerouteRunSchema = z.object({
  host: z.string().trim().min(1).max(253)
});
export type TracerouteRunInput = z.infer<typeof tracerouteRunSchema>;

export type TracerouteEvent =
  | { type: "data"; line: string }
  | { type: "done"; exitCode: number | null }
  | { type: "error"; message: string };
