import { z } from "zod";
import { DEFAULT_APP_PREFERENCES, type ConnectionImportEntry } from "../../core/src/index";

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

export const authTypeSchema = z.enum(["password", "privateKey", "agent", "interactive"]);
export const proxyTypeSchema = z.enum(["socks4", "socks5"]);
export const terminalEncodingSchema = z.enum(["utf-8", "gb18030", "gbk", "big5"]);
export const backspaceModeSchema = z.enum(["ascii-backspace", "ascii-delete"]);
export const deleteModeSchema = z.enum(["vt220-delete", "ascii-delete", "ascii-backspace"]);
export const windowAppearanceSchema = z.enum(["system", "light", "dark"]);
export const localShellModeSchema = z.enum(["preset", "custom"]);
export const localShellPresetSchema = z.enum(["system", "powershell", "cmd", "zsh", "sh", "bash"]);
export const shellIntegrationModeSchema = z.enum(["auto", "off", "manual"]);
export const agentAccessLevelSchema = z.enum(["off", "readonly", "full"]);
export const connectionListQuerySchema = z.object({
  keyword: z.string().trim().optional(),
  group: z.string().trim().optional(),
  favoriteOnly: z.boolean().optional().default(false)
});

export const connectionUpsertSchema = z
  .object({
    id: z.string().uuid().optional(),
    workspaceId: z.string().trim().min(1).optional(),
    name: z.string().min(1),
    host: z.string().min(1),
    port: z.coerce.number().int().min(1).max(65535).default(22),
    username: z.preprocess(trimToString, z.string()),
    authType: authTypeSchema.default("password"),
    password: z.preprocess(trimToOptionalString, z.string().min(1).optional()),
    sshKeyId: z.string().uuid().optional(),
    /**
     * 目标目录。三态：
     * - uuid   → 移到该目录，并由它派生 groupPath；
     * - `null` → 显式移到顶层（拖到目录树根节点、编辑器里清空「目录」都走它）；
     * - 省略   → 沿用已存目录与传入的 groupPath（旧调用方：导入、快速连接、认证改写）。
     * 没有 `null` 这一档时「移回顶层」会被 `?? current.folderId` 静默吃掉。
     */
    folderId: z.string().uuid().nullable().optional(),
    hostFingerprint: z.preprocess(trimToOptionalString, z.string().min(1).optional()),
    strictHostKeyChecking: z.boolean().default(false),
    proxyId: z.string().uuid().optional(),
    keepAliveEnabled: z.boolean().optional(),
    keepAliveIntervalSec: z.coerce.number().int().min(5).max(600).optional(),
    terminalEncoding: terminalEncodingSchema.default("utf-8"),
    backspaceMode: backspaceModeSchema.default("ascii-backspace"),
    deleteMode: deleteModeSchema.default("vt220-delete"),
    groupPath: z.preprocess(
      (v) => (typeof v === "string" ? v.trim() : v),
      z
        .string()
        .min(1)
        .refine((s) => s.startsWith("/"), { message: "分组路径必须以 / 开头" })
    ),
    tags: z.preprocess(trimAndFilterStringArray, z.array(z.string().min(1)).default([])),
    notes: z.preprocess(trimToOptionalString, z.string().optional()),
    favorite: z.boolean().default(false),
    monitorSession: z.boolean().default(false),
    // Optional on purpose: an omitted field must never grant access. The service keeps the
    // already-stored level and falls back to "off", so callers that predate this field
    // (quick connect, imports, auth-override rewrites) can never elevate a host silently.
    agentAccess: agentAccessLevelSchema.optional()
  })
  .superRefine((value, ctx) => {
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

const connectionBatchAuthTargetSchema = z.discriminatedUnion("type", [
  z.object({
    type: z.literal("connections"),
    connectionIds: z.array(z.string().uuid()).min(1)
  }),
  z.object({
    type: z.literal("group"),
    groupPath: z.string().trim().min(1)
  })
]);

const connectionBatchPasswordAuthSchema = z.object({
  authType: z.enum(["password", "interactive"]),
  password: z.preprocess(trimToOptionalString, z.string().min(1))
});

const connectionBatchPrivateKeyAuthSchema = z.object({
  authType: z.literal("privateKey"),
  sshKeyId: z.string().uuid()
});

const connectionBatchAgentAuthSchema = z.object({
  authType: z.literal("agent")
});

export const connectionBatchAuthUpdateSchema = z.object({
  target: connectionBatchAuthTargetSchema,
  auth: z.discriminatedUnion("authType", [
    connectionBatchPasswordAuthSchema,
    connectionBatchPrivateKeyAuthSchema,
    connectionBatchAgentAuthSchema
  ])
});

export const sessionAuthOverrideSchema = z
  .object({
    username: z.preprocess(trimToOptionalString, z.string().min(1).optional()),
    authType: z.enum(["password", "privateKey", "interactive"]),
    password: z.preprocess(trimToOptionalString, z.string().min(1).optional()),
    sshKeyId: z.string().uuid().optional(),
    /** Temporary key content for retry (not persisted as entity) */
    privateKeyContent: z.preprocess(trimToOptionalString, z.string().min(1).optional()),
    passphrase: z.preprocess(trimToOptionalString, z.string().min(1).optional())
  })
  .superRefine((value, ctx) => {
    if ((value.authType === "password" || value.authType === "interactive") && !value.password) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: "password is required when authType is password or interactive",
        path: ["password"]
      });
    }
  });

const remoteSessionOpenSchema = z.object({
  target: z.literal("remote"),
  connectionId: z.string().uuid(),
  sessionId: z.string().uuid().optional(),
  authOverride: sessionAuthOverrideSchema.optional()
});

const localSessionOpenSchema = z
  .object({
    target: z.literal("local"),
    sessionId: z.string().uuid().optional()
  })
  .strict();

export const sessionOpenSchema = z.discriminatedUnion("target", [
  remoteSessionOpenSchema,
  localSessionOpenSchema
]);

export const sessionWriteSchema = z.object({
  sessionId: z.string().uuid(),
  data: z.string().max(1024 * 1024),
  /**
   * "protocol" marks writes the client generates on the user's behalf (OSC
   * query replies, clipboard read answers). "agent" marks MCP-driven injection,
   * which the main process never accepts over this channel — it is produced
   * inside the main process and only ever appears on the outbound activity path.
   * Only "user" writes represent real keystrokes, which is what shell-integration
   * injection must not collide with, and what preempts agent injection.
   * Optional so every existing caller keeps defaulting to "user".
   */
  origin: z.enum(["user", "protocol", "agent"]).optional()
});

export const sessionResizeSchema = z.object({
  sessionId: z.string().uuid(),
  cols: z.number().int().min(20).max(500),
  rows: z.number().int().min(5).max(300)
});

export const sessionCloseSchema = z.object({
  sessionId: z.string().uuid()
});

export const sessionGetHomeDirSchema = z.object({
  connectionId: z.string().uuid()
});

// The delivery-ack protocol only exists for the ordered terminal byte stream;
// monitor snapshots are pushed directly without acks.
export const streamKindSchema = z.enum(["session"]);

// Sliding-window ack: the renderer batches acknowledgements, so a single ack
// may cover several delivered frames. `deliveryId` is the HIGHEST delivery id
// processed so far and `consumedBytes` is the DELTA of bytes consumed since
// the previous ack (the sum of the reported byteLength of every frame the
// batch covers) — not the byte length of the frame named by `deliveryId`.
export const streamDeliveryAckSchema = z.object({
  streamKind: streamKindSchema,
  streamId: z.string().min(1),
  deliveryId: z.number().int().min(1),
  consumedBytes: z.number().int().min(0)
});

export const sessionDataEventSchema = z.object({
  sessionId: z.string().uuid(),
  data: z.string(),
  deliveryId: z.number().int().min(1),
  byteLength: z.number().int().min(0)
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

const remoteEntryNameSchema = z
  .string()
  .trim()
  .min(1)
  .refine((name) => !name.includes("/"), { message: "文件名不能包含 /" })
  .refine((name) => name !== "." && name !== "..", { message: "文件名无效" });

export const sftpDownloadPackedSchema = z.object({
  connectionId: z.string().uuid(),
  remoteDir: z.string().min(1),
  entryNames: z.array(remoteEntryNameSchema).min(1).max(500),
  localDir: z.string().min(1),
  archiveName: z.string().trim().min(1).optional(),
  taskId: z.string().uuid().optional()
});

export const sftpUploadPackedSchema = z.object({
  connectionId: z.string().uuid(),
  localPaths: z.array(z.string().min(1)).min(1).max(500),
  remoteDir: z.string().min(1),
  archiveName: z.string().trim().min(1).optional(),
  taskId: z.string().uuid().optional()
});

export const sftpTransferPackedSchema = z.object({
  sourceConnectionId: z.string().uuid(),
  sourceDir: z.string().min(1),
  entryNames: z.array(remoteEntryNameSchema).min(1).max(500),
  targetConnectionId: z.string().uuid(),
  targetDir: z.string().min(1),
  archiveName: z.string().trim().min(1).optional(),
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

export const savedCommandUpsertSchema = z.object({
  id: z.string().uuid().optional(),
  workspaceId: z.string().trim().min(1).optional(),
  name: z.string().trim().min(1),
  description: z.preprocess(trimToOptionalString, z.string().optional()),
  group: z.string().trim().min(1).default("默认"),
  command: z.string().trim().min(1),
  appendCr: z.boolean().optional()
});

export const savedCommandRemoveSchema = z.object({
  id: z.string().uuid(),
  workspaceId: z.string().trim().min(1).optional()
});

export const sftpEditOpenSchema = z.object({
  connectionId: z.string().uuid(),
  remotePath: z.string().min(1),
  editorCommand: z.string()
});

export const sftpEditReadFileSchema = z.object({
  connectionId: z.string().uuid(),
  remotePath: z.string().min(1)
});

export const sftpEditWriteFileSchema = z.object({
  connectionId: z.string().uuid(),
  remotePath: z.string().min(1),
  content: z.string().max(10 * 1024 * 1024)
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

const localShellSchema = z
  .object({
    mode: localShellModeSchema.default(DEFAULT_APP_PREFERENCES.terminal.localShell.mode),
    preset: localShellPresetSchema.default(DEFAULT_APP_PREFERENCES.terminal.localShell.preset),
    customPath: z.string().default(DEFAULT_APP_PREFERENCES.terminal.localShell.customPath)
  })
  .superRefine((value, ctx) => {
    if (value.mode === "custom" && value.customPath.trim().length === 0) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: "customPath is required when localShell mode is custom",
        path: ["customPath"]
      });
    }
  });

export const appPreferencesSchema = z
  .object({
    transfer: z
      .object({
        uploadDefaultDir: z
          .string()
          .min(1)
          .default(DEFAULT_APP_PREFERENCES.transfer.uploadDefaultDir),
        downloadDefaultDir: z
          .string()
          .min(1)
          .default(DEFAULT_APP_PREFERENCES.transfer.downloadDefaultDir)
      })
      .default(DEFAULT_APP_PREFERENCES.transfer),
    remoteEdit: z
      .object({
        defaultEditorCommand: z
          .string()
          .default(DEFAULT_APP_PREFERENCES.remoteEdit.defaultEditorCommand),
        editorMode: z
          .enum(["builtin", "external"])
          .default(DEFAULT_APP_PREFERENCES.remoteEdit.editorMode)
      })
      .default(DEFAULT_APP_PREFERENCES.remoteEdit),
    terminal: z
      .object({
        backgroundColor: terminalColorSchema.default(
          DEFAULT_APP_PREFERENCES.terminal.backgroundColor
        ),
        foregroundColor: terminalColorSchema.default(
          DEFAULT_APP_PREFERENCES.terminal.foregroundColor
        ),
        fontSize: z.coerce
          .number()
          .int()
          .min(10)
          .max(24)
          .default(DEFAULT_APP_PREFERENCES.terminal.fontSize),
        lineHeight: z.coerce
          .number()
          .min(1)
          .max(2)
          .default(DEFAULT_APP_PREFERENCES.terminal.lineHeight),
        fontFamily: z.string().trim().min(1).default(DEFAULT_APP_PREFERENCES.terminal.fontFamily),
        localShell: localShellSchema.default(DEFAULT_APP_PREFERENCES.terminal.localShell),
        oscClipboardWrite: z.boolean().default(DEFAULT_APP_PREFERENCES.terminal.oscClipboardWrite),
        oscClipboardRead: z.boolean().default(DEFAULT_APP_PREFERENCES.terminal.oscClipboardRead),
        oscNotifications: z.boolean().default(DEFAULT_APP_PREFERENCES.terminal.oscNotifications),
        oscTitleUpdates: z.boolean().default(DEFAULT_APP_PREFERENCES.terminal.oscTitleUpdates),
        hyperlinkConfirm: z.boolean().default(DEFAULT_APP_PREFERENCES.terminal.hyperlinkConfirm),
        shellIntegration: shellIntegrationModeSchema.default(
          DEFAULT_APP_PREFERENCES.terminal.shellIntegration
        ),
        wallpaper: z
          .object({
            seeThrough: z.boolean().default(DEFAULT_APP_PREFERENCES.terminal.wallpaper.seeThrough),
            useWebgl: z.boolean().default(DEFAULT_APP_PREFERENCES.terminal.wallpaper.useWebgl)
          })
          .default(DEFAULT_APP_PREFERENCES.terminal.wallpaper)
      })
      .default(DEFAULT_APP_PREFERENCES.terminal),
    ssh: z
      .object({
        keepAliveEnabled: z.boolean().default(DEFAULT_APP_PREFERENCES.ssh.keepAliveEnabled),
        keepAliveIntervalSec: z.coerce
          .number()
          .int()
          .min(5)
          .max(600)
          .default(DEFAULT_APP_PREFERENCES.ssh.keepAliveIntervalSec)
      })
      .default(DEFAULT_APP_PREFERENCES.ssh),
    window: z
      .object({
        appearance: windowAppearanceSchema.default(DEFAULT_APP_PREFERENCES.window.appearance),
        minimizeToTray: z.boolean().default(DEFAULT_APP_PREFERENCES.window.minimizeToTray),
        confirmBeforeClose: z.boolean().default(DEFAULT_APP_PREFERENCES.window.confirmBeforeClose),
        backgroundImagePath: z.string().default(DEFAULT_APP_PREFERENCES.window.backgroundImagePath),
        backgroundOpacity: z.coerce
          .number()
          .int()
          .min(30)
          .max(80)
          .default(DEFAULT_APP_PREFERENCES.window.backgroundOpacity),
        leftSidebarDefaultCollapsed: z
          .boolean()
          .default(DEFAULT_APP_PREFERENCES.window.leftSidebarDefaultCollapsed),
        bottomWorkbenchDefaultCollapsed: z
          .boolean()
          .default(DEFAULT_APP_PREFERENCES.window.bottomWorkbenchDefaultCollapsed)
      })
      .default(DEFAULT_APP_PREFERENCES.window),
    connectionManager: z
      .object({
        // 尺寸夹在可用范围内：存坏的值不该让对话框缩成一条缝或撑出屏幕。
        dialogWidth: z.coerce
          .number()
          .int()
          .min(900)
          .max(3840)
          .default(DEFAULT_APP_PREFERENCES.connectionManager.dialogWidth),
        dialogHeight: z.coerce
          .number()
          .int()
          .min(560)
          .max(2160)
          .default(DEFAULT_APP_PREFERENCES.connectionManager.dialogHeight),
        folderColumnWidth: z.coerce
          .number()
          .int()
          .min(160)
          .max(520)
          .default(DEFAULT_APP_PREFERENCES.connectionManager.folderColumnWidth),
        detailColumnWidth: z.coerce
          .number()
          .int()
          .min(260)
          .max(720)
          .default(DEFAULT_APP_PREFERENCES.connectionManager.detailColumnWidth)
      })
      .default(DEFAULT_APP_PREFERENCES.connectionManager),
    traceroute: z
      .object({
        nexttracePath: z.string().default(DEFAULT_APP_PREFERENCES.traceroute.nexttracePath),
        powProvider: z
          .enum(["api.nxtrace.org", "sakura"])
          .default(DEFAULT_APP_PREFERENCES.traceroute.powProvider)
      })
      .default(DEFAULT_APP_PREFERENCES.traceroute),
    agent: z
      .object({
        enabled: z.boolean().default(DEFAULT_APP_PREFERENCES.agent.enabled),
        socketEnabled: z.boolean().default(DEFAULT_APP_PREFERENCES.agent.socketEnabled),
        tcpEnabled: z.boolean().default(DEFAULT_APP_PREFERENCES.agent.tcpEnabled),
        tcpPort: z.coerce
          .number()
          .int()
          .min(0)
          .max(65535)
          .default(DEFAULT_APP_PREFERENCES.agent.tcpPort),
        confirmWrites: z.boolean().default(DEFAULT_APP_PREFERENCES.agent.confirmWrites),
        confirmUnknownCommands: z
          .boolean()
          .default(DEFAULT_APP_PREFERENCES.agent.confirmUnknownCommands),
        allowedLocalRoots: z.preprocess(
          trimAndFilterStringArray,
          z.array(z.string().min(1)).default(DEFAULT_APP_PREFERENCES.agent.allowedLocalRoots)
        ),
        execTimeoutSec: z.coerce
          .number()
          .int()
          .min(1)
          .max(3600)
          .default(DEFAULT_APP_PREFERENCES.agent.execTimeoutSec)
      })
      .default(DEFAULT_APP_PREFERENCES.agent)
  })
  .default(DEFAULT_APP_PREFERENCES);

export const appPreferencesPatchSchema = z.object({
  transfer: z
    .object({
      uploadDefaultDir: z.string().min(1).optional(),
      downloadDefaultDir: z.string().min(1).optional()
    })
    .optional(),
  remoteEdit: z
    .object({
      defaultEditorCommand: z.string().optional(),
      editorMode: z.enum(["builtin", "external"]).optional()
    })
    .optional(),
  terminal: z
    .object({
      backgroundColor: terminalColorSchema.optional(),
      foregroundColor: terminalColorSchema.optional(),
      fontSize: z.coerce.number().int().min(10).max(24).optional(),
      lineHeight: z.coerce.number().min(1).max(2).optional(),
      fontFamily: z.string().trim().min(1).optional(),
      localShell: z
        .object({
          mode: localShellModeSchema.optional(),
          preset: localShellPresetSchema.optional(),
          customPath: z.string().optional()
        })
        .superRefine((value, ctx) => {
          if (
            value.mode === "custom" &&
            (!value.customPath || value.customPath.trim().length === 0)
          ) {
            ctx.addIssue({
              code: z.ZodIssueCode.custom,
              message: "customPath is required when localShell mode is custom",
              path: ["customPath"]
            });
          }
        })
        .optional(),
      oscClipboardWrite: z.boolean().optional(),
      oscClipboardRead: z.boolean().optional(),
      oscNotifications: z.boolean().optional(),
      oscTitleUpdates: z.boolean().optional(),
      hyperlinkConfirm: z.boolean().optional(),
      shellIntegration: shellIntegrationModeSchema.optional(),
      wallpaper: z
        .object({
          seeThrough: z.boolean().optional(),
          useWebgl: z.boolean().optional()
        })
        .optional()
    })
    .optional(),
  ssh: z
    .object({
      keepAliveEnabled: z.boolean().optional(),
      keepAliveIntervalSec: z.coerce.number().int().min(5).max(600).optional()
    })
    .optional(),
  window: z
    .object({
      appearance: windowAppearanceSchema.optional(),
      minimizeToTray: z.boolean().optional(),
      confirmBeforeClose: z.boolean().optional(),
      backgroundImagePath: z.string().optional(),
      backgroundOpacity: z.coerce.number().int().min(30).max(80).optional(),
      leftSidebarDefaultCollapsed: z.boolean().optional(),
      bottomWorkbenchDefaultCollapsed: z.boolean().optional()
    })
    .optional(),
  connectionManager: z
    .object({
      dialogWidth: z.coerce.number().int().min(900).max(3840).optional(),
      dialogHeight: z.coerce.number().int().min(560).max(2160).optional(),
      folderColumnWidth: z.coerce.number().int().min(160).max(520).optional(),
      detailColumnWidth: z.coerce.number().int().min(260).max(720).optional()
    })
    .optional(),
  traceroute: z
    .object({
      nexttracePath: z.string().optional(),
      powProvider: z.enum(["api.nxtrace.org", "sakura"]).optional()
    })
    .optional(),
  agent: z
    .object({
      enabled: z.boolean().optional(),
      socketEnabled: z.boolean().optional(),
      tcpEnabled: z.boolean().optional(),
      tcpPort: z.coerce.number().int().min(0).max(65535).optional(),
      confirmWrites: z.boolean().optional(),
      confirmUnknownCommands: z.boolean().optional(),
      allowedLocalRoots: z
        .preprocess(trimAndFilterStringArray, z.array(z.string().min(1)))
        .optional(),
      execTimeoutSec: z.coerce.number().int().min(1).max(3600).optional()
    })
    .optional()
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

// ── Terminal integration (OSC 9/777 notifications, OSC 9;4 taskbar progress) ──
export const terminalNotificationSchema = z.object({
  sessionId: z.string().min(1),
  title: z.string().trim().min(1).max(200).optional(),
  body: z.string().trim().min(1).max(500)
});

export const terminalProgressStateSchema = z.enum([
  "none",
  "normal",
  "error",
  "indeterminate",
  "paused"
]);

export const terminalProgressSchema = z.object({
  sessionId: z.string().min(1),
  state: terminalProgressStateSchema,
  value: z.coerce.number().min(0).max(100).optional()
});

/** Main→renderer event payload emitted when the user clicks a terminal notification. */
export const terminalNotificationActionEventSchema = z.object({
  sessionId: z.string().min(1)
});

export const sftpTransferStatusEventSchema = z.object({
  taskId: z.string().uuid().optional(),
  /**
   * 谁发起的。缺省视作 "user"——只有 Agent 发起的传输才会被主进程打上 "agent"，
   * 传输队列据此显示徽标，让人一眼看出这条不是自己点的。
   */
  origin: z.enum(["user", "agent"]).optional(),
  direction: z.enum(["upload", "download"]),
  connectionId: z.string().uuid(),
  remotePath: z.string().min(1),
  localPath: z.string().min(1),
  status: z.enum(["queued", "running", "success", "failed", "cancelled"]),
  progress: z.coerce.number().min(0).max(100).default(0),
  transferredBytes: z.coerce.number().min(0).optional(),
  totalBytes: z.coerce.number().min(0).optional(),
  speedBytesPerSec: z.coerce.number().min(0).optional(),
  message: z.string().optional(),
  error: z.string().optional()
});

export const sftpTransferCancelSchema = z.object({
  taskId: z.string().uuid()
});

// ─── Agent 接入（应用内 MCP 端点）────────────────────────────────────────────

export const agentClientKindSchema = z.enum(["claude-code", "claude-desktop", "cursor", "generic"]);

export const agentStatusSchema = z.object({});
export const agentEnableSchema = z.object({});
export const agentDisableSchema = z.object({});
export const agentRotateTokenSchema = z.object({});
export const agentCopyClientConfigSchema = z.object({
  client: agentClientKindSchema.default("claude-code")
});
export const agentInstallCursorSchema = z.object({});
export const agentInstallClaudeDesktopSchema = z.object({});
export const agentExportMcpbSchema = z.object({});

export const agentPromptRequestSchema = z.object({
  id: z.string().uuid(),
  kind: z.enum(["confirm", "select", "text"]),
  title: z.string().trim().min(1).max(160),
  message: z.string().trim().min(1).max(4000),
  details: z.string().max(16_000).optional(),
  choices: z.array(z.string().trim().min(1).max(200)).min(1).max(20).optional(),
  placeholder: z.string().max(300).optional(),
  sensitive: z.boolean().optional(),
  allowRemember: z.boolean().optional()
});

export const agentPromptResponseSchema = z.object({
  id: z.string().uuid(),
  canceled: z.boolean(),
  value: z.string().max(16_000).optional(),
  rememberForSession: z.boolean().optional()
});

/** 全局断闸：把 Agent 的所有工具调用立刻掐断，或重新放行。 */
export const agentSetHaltedSchema = z.object({
  halted: z.boolean()
});

/** 主进程 → 渲染进程：某个会话正被 Agent 驱动（或不再被驱动），用于标签徽标。 */
export const agentSessionControlEventSchema = z.object({
  sessionId: z.string().min(1),
  /** null 表示 Agent 已交还控制权 */
  clientName: z.string().nullable(),
  controlled: z.boolean()
});

/** 主进程 → 渲染进程：切到该标签并把窗口置顶（`session_focus`）。 */
export const agentSessionFocusEventSchema = z.object({
  sessionId: z.string().min(1)
});

export const agentActivityEventSchema = z.object({
  id: z.string().min(1),
  clientName: z.string().nullable(),
  tool: z.string().min(1),
  status: z.enum(["running", "succeeded", "failed"]),
  connectionId: z.string().uuid().optional(),
  summary: z.string().max(1000),
  createdAt: z.string()
});

export type AgentClientKind = z.infer<typeof agentClientKindSchema>;
export type AgentStatusInput = z.infer<typeof agentStatusSchema>;
export type AgentEnableInput = z.infer<typeof agentEnableSchema>;
export type AgentDisableInput = z.infer<typeof agentDisableSchema>;
export type AgentRotateTokenInput = z.infer<typeof agentRotateTokenSchema>;
export type AgentCopyClientConfigInput = z.infer<typeof agentCopyClientConfigSchema>;
export type AgentInstallCursorInput = z.infer<typeof agentInstallCursorSchema>;
export type AgentInstallClaudeDesktopInput = z.infer<typeof agentInstallClaudeDesktopSchema>;
export type AgentExportMcpbInput = z.infer<typeof agentExportMcpbSchema>;
export type AgentPromptRequest = z.infer<typeof agentPromptRequestSchema>;
export type AgentPromptResponse = z.infer<typeof agentPromptResponseSchema>;
export type AgentActivityEvent = z.infer<typeof agentActivityEventSchema>;
export type AgentSetHaltedInput = z.infer<typeof agentSetHaltedSchema>;
export type AgentSessionControlEvent = z.infer<typeof agentSessionControlEventSchema>;
export type AgentSessionFocusEvent = z.infer<typeof agentSessionFocusEventSchema>;

export interface AgentConnectedClient {
  /** MCP session id */
  id: string;
  /** initialize 上报的客户端名称，未知时为 null */
  name: string | null;
  version: string | null;
  transport: "socket" | "tcp";
  connectedAt: string;
}

export interface AgentEndpointStatus {
  /** 偏好里的总开关 */
  enabled: boolean;
  /** 端点当前是否真的在监听 */
  listening: boolean;
  /** Unix socket / 命名管道路径，未监听时为 null */
  socketPath: string | null;
  /** 实际生效的 loopback TCP 端口（0 端口偏好会被解析成真实端口），未监听时为 null */
  tcpPort: number | null;
  /**
   * TCP 监听的 Bearer token。仅 TCP 开启时非空——socket 监听靠 0600 文件权限授权，
   * 不签发 token。凭据（密码 / 私钥 / 设备密钥）永远不会出现在本结构里。
   */
  token: string | null;
  /** 端点发现文件路径（<userData>/mcp/endpoint.json） */
  endpointFilePath: string;
  clients: AgentConnectedClient[];
  /** 上一次启动 / 监听失败的原因，无错误时为 null */
  lastError: string | null;
  /** 全局断闸是否已拉下：为 true 时端点仍在监听，但所有工具调用立即被拒 */
  halted: boolean;
}

export interface AgentClientConfigResult {
  /** 配置由主进程直接写入系统剪贴板 */
  ok: true;
  /** 可直接粘贴执行的 `claude mcp add` 命令 */
  command: string;
  /** MCP 客户端配置文件里的 mcpServers 片段 */
  json: string;
}

/** Cursor 一键安装：主进程已用系统默认处理器打开 deeplink。 */
export interface AgentInstallCursorResult {
  ok: true;
  /** 已打开的 `cursor://` deeplink，供界面展示与手动兜底 */
  deeplink: string;
}

/** 写入 Claude Desktop 配置文件的结果。 */
export interface AgentInstallClaudeDesktopResult {
  ok: true;
  /** 实际写入的 claude_desktop_config.json 绝对路径 */
  configPath: string;
}

/** 导出 `.mcpb` 一键安装包的结果；用户取消保存对话框时 `canceled: true`。 */
export type AgentExportMcpbResult = { ok: true; filePath: string } | { ok: false; canceled: true };

// ─── Connection Folders ─────────────────────────────────────────────────────

/**
 * 目录名是树上的可寻址标识,所以拒绝空白与路径分隔符:带 `/` 的名字会和派生出来的 groupPath
 * 投影产生歧义。作用域一律由调用方显式给出,目录不跨隔离域嵌套。
 */
const folderNameSchema = z.preprocess(
  (value) => (typeof value === "string" ? value.trim() : value),
  z
    .string()
    .min(1, "目录名称不能为空")
    .refine((name) => !name.includes("/") && !name.includes("\\"), {
      message: "目录名称不能包含 / 或 \\"
    })
);

export const connectionFolderListSchema = z.object({
  scopeKey: z.string().trim().min(1).optional()
});

export const connectionFolderCreateSchema = z.object({
  scopeKey: z.string().trim().min(1),
  name: folderNameSchema,
  parentId: z.string().uuid().optional(),
  sortIndex: z.coerce.number().int().min(0).optional()
});

export const connectionFolderRenameSchema = z.object({
  id: z.string().uuid(),
  name: folderNameSchema
});

export const connectionFolderMoveSchema = z.object({
  id: z.string().uuid(),
  /** 省略表示移到顶层。 */
  parentId: z.string().uuid().optional()
});

export const connectionFolderReorderSchema = z.object({
  id: z.string().uuid(),
  sortIndex: z.coerce.number().int().min(0)
});

export const connectionFolderRemoveSchema = z.object({
  id: z.string().uuid()
});

// ─── SSH Key Management ─────────────────────────────────────────────────────

export const sshKeyListSchema = z.object({});

export const sshKeyUpsertSchema = z.object({
  id: z.string().uuid().optional(),
  workspaceId: z.string().trim().min(1).optional(),
  name: z.string().trim().min(1),
  keyContent: z.preprocess(trimToOptionalString, z.string().min(1).optional()),
  passphrase: z.preprocess(trimToOptionalString, z.string().min(1).optional())
});

export const sshKeyAlgorithmSchema = z.enum(["ed25519", "rsa-2048", "rsa-4096"]);

export const sshKeyGenerateSchema = z.object({
  name: z.string().trim().min(1),
  algorithm: sshKeyAlgorithmSchema.default("ed25519"),
  /** 写进私钥的注释，通常是 user@host；留空则不写。 */
  comment: z.preprocess(trimToOptionalString, z.string().min(1).optional()),
  workspaceId: z.string().trim().min(1).optional()
});

export const sshKeyUsageSchema = z.object({
  id: z.string().uuid()
});

/** 引用某把密钥的连接，用于删除前告知与密钥详情页。 */
export interface SshKeyUsageItem {
  id: string;
  name: string;
  groupPath: string;
}

export const sshKeyRemoveSchema = z.object({
  id: z.string().uuid(),
  force: z.boolean().default(false)
});

// ─── Proxy Management ───────────────────────────────────────────────────────

export const proxyListSchema = z.object({});

export const proxyUpsertSchema = z
  .object({
    id: z.string().uuid().optional(),
    workspaceId: z.string().trim().min(1).optional(),
    name: z.string().trim().min(1),
    proxyType: proxyTypeSchema,
    host: z.string().trim().min(1),
    port: z.coerce.number().int().min(1).max(65535),
    username: z.preprocess(trimToOptionalString, z.string().min(1).optional()),
    password: z.preprocess(trimToOptionalString, z.string().min(1).optional())
  })
  .superRefine((value, ctx) => {
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
  connectionId: z.string().uuid()
});

export const connectionImportPreviewSchema = z.object({
  filePath: z.string().min(1),
  decryptionPassword: z.preprocess(trimToOptionalString, z.string().min(1).optional())
});

export const connectionImportFinalShellPreviewSchema = z.object({
  filePath: z.string().min(1)
});

export const connectionImportSourceSchema = z.enum(["nextshell", "finalshell"]);

export const connectionImportDirectoryPreviewSchema = z.object({
  directoryPath: z.string().min(1),
  source: connectionImportSourceSchema,
  decryptionPassword: z.preprocess(trimToOptionalString, z.string().min(1).optional())
});

const exportedSshKeyRefSchema = z.object({
  name: z.string().min(1),
  fingerprint: z.preprocess(trimToOptionalString, z.string().min(1).optional())
});

export const connectionImportExecuteSchema = z.object({
  entries: z.array(
    z.object({
      name: z.string().min(1),
      host: z.string().min(1),
      port: z.coerce.number().int().min(1).max(65535),
      username: z.string(),
      authType: authTypeSchema,
      password: z.string().optional(),
      sshKeyId: z.string().uuid().optional(),
      sshKeyRef: exportedSshKeyRefSchema.optional(),
      keepAliveEnabled: z.boolean().optional(),
      keepAliveIntervalSec: z.coerce.number().int().min(5).max(600).optional(),
      groupPath: z.string().min(1),
      tags: z.array(z.string()).default([]),
      notes: z.string().optional(),
      favorite: z.boolean().default(false),
      terminalEncoding: terminalEncodingSchema.default("utf-8"),
      backspaceMode: backspaceModeSchema.default("ascii-backspace"),
      deleteMode: deleteModeSchema.default("vt220-delete"),
      monitorSession: z.boolean().default(false)
    })
  ),
  conflictPolicy: z.enum(["skip", "overwrite", "duplicate"]).default("skip"),
  /**
   * 导入落点。每条 entry 的 groupPath 会被物化成真实目录链,整条链挂在这个目录之下;
   * 省略则挂在本地根。目录必须存在且属于本地作用域——导入执行链路只写本地。
   */
  targetFolderId: z.string().uuid().optional(),
  /**
   * `entries[].groupPath` 的来源格式,决定物化目录链前剥不剥线格式前缀:
   * - `wire`(省略时的行为)——路径来自导出文件 / 云快照 / FinalShell 的 `/import/finalshell`
   *   常量,`/server`、`/workspace/<slug>`、`/import` 是线格式前缀,必须剥掉;
   * - `literal`——路径由目录扫描导入按磁盘相对路径拼出,没有任何前缀,一段都不能剥:
   *   用户把顶层目录叫 `server` 完全合法,剥掉会吞掉一整层。
   */
  groupPathFormat: z.enum(["wire", "literal"]).optional()
});

export type ConnectionListQueryInput = z.infer<typeof connectionListQuerySchema>;
export type ConnectionUpsertInput = z.infer<typeof connectionUpsertSchema>;
export type ConnectionRemoveInput = z.infer<typeof connectionRemoveSchema>;
export type ConnectionBatchAuthUpdateInput = z.infer<typeof connectionBatchAuthUpdateSchema>;
export type SessionOpenInput = z.infer<typeof sessionOpenSchema>;
export type SessionAuthOverrideInput = z.infer<typeof sessionAuthOverrideSchema>;
export type SessionWriteInput = z.infer<typeof sessionWriteSchema>;
export type SessionResizeInput = z.infer<typeof sessionResizeSchema>;
export type SessionCloseInput = z.infer<typeof sessionCloseSchema>;
export type SessionGetHomeDirInput = z.infer<typeof sessionGetHomeDirSchema>;
export type StreamKind = z.infer<typeof streamKindSchema>;
export type StreamDeliveryAckInput = z.infer<typeof streamDeliveryAckSchema>;
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
export type SftpListInput = z.infer<typeof sftpListSchema>;
export type SftpUploadInput = z.infer<typeof sftpUploadSchema>;
export type SftpDownloadInput = z.infer<typeof sftpDownloadSchema>;
export type SftpUploadPackedInput = z.infer<typeof sftpUploadPackedSchema>;
export type SftpDownloadPackedInput = z.infer<typeof sftpDownloadPackedSchema>;
export type SftpTransferPackedInput = z.infer<typeof sftpTransferPackedSchema>;
export type SftpMkdirInput = z.infer<typeof sftpMkdirSchema>;
export type SftpRenameInput = z.infer<typeof sftpRenameSchema>;
export type SftpDeleteInput = z.infer<typeof sftpDeleteSchema>;
export type CommandHistoryListInput = z.infer<typeof commandHistoryListSchema>;
export type CommandHistoryPushInput = z.infer<typeof commandHistoryPushSchema>;
export type CommandHistoryRemoveInput = z.infer<typeof commandHistoryRemoveSchema>;
export type CommandHistoryClearInput = z.infer<typeof commandHistoryClearSchema>;
export type SavedCommandUpsertInput = z.infer<typeof savedCommandUpsertSchema>;
export type SavedCommandRemoveInput = z.infer<typeof savedCommandRemoveSchema>;
export type SftpEditOpenInput = z.infer<typeof sftpEditOpenSchema>;
export type SftpEditReadFileInput = z.infer<typeof sftpEditReadFileSchema>;
export type SftpEditWriteFileInput = z.infer<typeof sftpEditWriteFileSchema>;
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
export type TerminalNotificationInput = z.infer<typeof terminalNotificationSchema>;
export type TerminalProgressState = z.infer<typeof terminalProgressStateSchema>;
export type TerminalProgressInput = z.infer<typeof terminalProgressSchema>;
export type TerminalNotificationActionEvent = z.infer<typeof terminalNotificationActionEventSchema>;
export type SftpTransferStatusEvent = z.infer<typeof sftpTransferStatusEventSchema>;
export type SftpTransferCancelInput = z.infer<typeof sftpTransferCancelSchema>;
export type SshKeyListInput = z.infer<typeof sshKeyListSchema>;
export type ConnectionFolderListInput = z.infer<typeof connectionFolderListSchema>;
export type ConnectionFolderCreateInput = z.infer<typeof connectionFolderCreateSchema>;
export type ConnectionFolderRenameInput = z.infer<typeof connectionFolderRenameSchema>;
export type ConnectionFolderMoveInput = z.infer<typeof connectionFolderMoveSchema>;
export type ConnectionFolderReorderInput = z.infer<typeof connectionFolderReorderSchema>;
export type ConnectionFolderRemoveInput = z.infer<typeof connectionFolderRemoveSchema>;
export type SshKeyUpsertInput = z.infer<typeof sshKeyUpsertSchema>;
export type SshKeyRemoveInput = z.infer<typeof sshKeyRemoveSchema>;
export type SshKeyGenerateInput = z.infer<typeof sshKeyGenerateSchema>;
export type SshKeyUsageInput = z.infer<typeof sshKeyUsageSchema>;
export type ProxyListInput = z.infer<typeof proxyListSchema>;
export type ProxyUpsertInput = z.infer<typeof proxyUpsertSchema>;
export type ProxyRemoveInput = z.infer<typeof proxyRemoveSchema>;
export type ConnectionExportInput = z.infer<typeof connectionExportSchema>;
export type ConnectionExportBatchInput = z.infer<typeof connectionExportBatchSchema>;
export type ConnectionRevealPasswordInput = z.infer<typeof connectionRevealPasswordSchema>;
export type ConnectionImportPreviewInput = z.infer<typeof connectionImportPreviewSchema>;
export type ConnectionImportFinalShellPreviewInput = z.infer<
  typeof connectionImportFinalShellPreviewSchema
>;
export type ConnectionImportSource = z.infer<typeof connectionImportSourceSchema>;
export type ConnectionImportDirectoryPreviewInput = z.infer<
  typeof connectionImportDirectoryPreviewSchema
>;
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

export interface ConnectionImportDirectoryPreviewFile {
  filePath: string;
  fileName: string;
  relativePath: string;
  groupPath: string;
  entries: ConnectionImportEntry[];
}

export interface ConnectionImportDirectoryPreviewResult {
  directoryPath: string;
  source: ConnectionImportSource;
  totalFiles: number;
  importedFiles: number;
  skippedFiles: number;
  entries: ConnectionImportEntry[];
  files: ConnectionImportDirectoryPreviewFile[];
  warnings: string[];
}

export interface ConnectionBatchAuthUpdateResult {
  total: number;
  updated: number;
  failed: number;
  errors: string[];
}

export interface ConnectionRevealPasswordResult {
  password: string;
}

export const updateCheckSchema = z.object({});
export type UpdateCheckInput = z.infer<typeof updateCheckSchema>;

/**
 * Renderer-side error report forwarded into the main-process electron-log
 * file. Renderer console output is invisible in packaged builds, so without
 * this a frozen/broken UI leaves no trace next to the main-process logs. The
 * length caps bound a single report; the renderer additionally caps how many
 * reports it sends per lifetime.
 */
export const rendererErrorReportSchema = z.object({
  source: z.enum(["window-error", "unhandled-rejection", "react-error-boundary"]),
  message: z.string().min(1).max(2000),
  stack: z.string().max(8000).optional(),
  componentStack: z.string().max(8000).optional()
});
export type RendererErrorReportInput = z.infer<typeof rendererErrorReportSchema>;

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

export type PingResult = { ok: true; avgMs: number } | { ok: false; error: string };

// ─── Traceroute ──────────────────────────────────────────────────────────

export const tracerouteRunSchema = z.object({
  host: z.string().trim().min(1).max(253),
  /** Caller-generated id so emitted events can be attributed back to this exact run. */
  runId: z.string().trim().min(1).max(64)
});
export type TracerouteRunInput = z.infer<typeof tracerouteRunSchema>;

/** Every event carries the run it belongs to so concurrent panes never consume each other's hops. */
interface TracerouteEventScope {
  host: string;
  runId: string;
}

export type TracerouteEvent =
  | (TracerouteEventScope & { type: "data"; line: string })
  | (TracerouteEventScope & { type: "done"; exitCode: number | null })
  | (TracerouteEventScope & { type: "error"; message: string });

// ─── Cloud Sync: Workspace Management ────────────────────────────────────

export const cloudSyncWorkspaceListSchema = z.object({});

export const cloudSyncWorkspaceAddSchema = z.object({
  apiBaseUrl: z.string().trim().min(1).max(500),
  workspaceName: z.string().trim().min(1).max(200),
  displayName: z.string().trim().max(200).optional(),
  workspacePassword: z.string().min(1).max(200),
  pullIntervalSec: z.number().int().min(10).max(86400).optional(),
  ignoreTlsErrors: z.boolean().optional(),
  enabled: z.boolean().optional()
});

export const cloudSyncWorkspaceUpdateSchema = z.object({
  id: z.string().trim().min(1),
  apiBaseUrl: z.string().trim().min(1).max(500),
  workspaceName: z.string().trim().min(1).max(200),
  displayName: z.string().trim().max(200).optional(),
  workspacePassword: z.string().min(1).max(200).optional(),
  pullIntervalSec: z.number().int().min(10).max(86400).optional(),
  ignoreTlsErrors: z.boolean().optional(),
  enabled: z.boolean().optional()
});

export const cloudSyncWorkspaceRemoveSchema = z.object({
  id: z.string().trim().min(1)
});

export const cloudSyncWorkspaceTokenDraftSchema = z.object({
  apiBaseUrl: z
    .string()
    .trim()
    .min(1)
    .max(500)
    .transform((value) => value.replace(/\/+$/, "")),
  workspaceName: z.string().trim().min(1).max(200),
  displayName: z.string().trim().max(200),
  workspacePassword: z.string().min(1).max(200),
  pullIntervalSec: z.number().int().min(10).max(86400),
  ignoreTlsErrors: z.boolean(),
  enabled: z.boolean()
});

export const cloudSyncWorkspaceExportTokenSchema = z.object({
  id: z.string().trim().min(1)
});

export const cloudSyncWorkspaceParseTokenSchema = z.object({
  token: z.string().trim().min(1)
});

export const cloudSyncStatusSchema = z.object({});

export const cloudSyncSyncNowSchema = z.object({
  workspaceId: z.string().trim().min(1).optional(),
  mode: z.enum(["cloud-wins", "local-wins"]).optional()
});

export const cloudSyncTestConnectionSchema = z.object({
  apiBaseUrl: z.string().trim().min(1).max(500),
  workspaceName: z.string().trim().min(1).max(200),
  workspacePassword: z.string().min(1).max(200),
  ignoreTlsErrors: z.boolean().optional()
});

export type CloudSyncWorkspaceListInput = z.infer<typeof cloudSyncWorkspaceListSchema>;
export type CloudSyncWorkspaceAddInput = z.infer<typeof cloudSyncWorkspaceAddSchema>;
export type CloudSyncWorkspaceUpdateInput = z.infer<typeof cloudSyncWorkspaceUpdateSchema>;
export type CloudSyncWorkspaceRemoveInput = z.infer<typeof cloudSyncWorkspaceRemoveSchema>;
export type CloudSyncWorkspaceTokenDraft = z.infer<typeof cloudSyncWorkspaceTokenDraftSchema>;
export type CloudSyncWorkspaceExportTokenInput = z.infer<
  typeof cloudSyncWorkspaceExportTokenSchema
>;
export type CloudSyncWorkspaceParseTokenInput = z.infer<typeof cloudSyncWorkspaceParseTokenSchema>;
export type CloudSyncStatusInput = z.infer<typeof cloudSyncStatusSchema>;
export type CloudSyncSyncNowInput = z.infer<typeof cloudSyncSyncNowSchema>;
export type CloudSyncTestConnectionInput = z.infer<typeof cloudSyncTestConnectionSchema>;

// ─── Resource Operations ─────────────────────────────────────────────────

export const resourceCopyConnectionSchema = z.object({
  sourceId: z.string().trim().min(1),
  targetOriginKind: z.enum(["local", "cloud"]),
  targetWorkspaceId: z.string().trim().min(1).optional(),
  /**
   * 目标作用域内的目录 id。目录才是落点的真相,groupPath 由它投影出来——只传名字的话
   * 嵌套目录 `a/b` 会丢掉 `a`,复制落点与所选不符。
   */
  targetFolderId: z.string().uuid().optional(),
  /** 旧调用方的路径字符串写法。仅在没有 targetFolderId 时生效。 */
  targetGroupSubPath: z.string().trim().max(500).optional()
});

export type ResourceCopyConnectionInput = z.infer<typeof resourceCopyConnectionSchema>;

// ─── Recycle Bin ─────────────────────────────────────────────────────────

export const recycleBinListSchema = z.object({});

export const recycleBinRestoreSchema = z.object({
  recycleBinEntryId: z.string().trim().min(1),
  /** 省略时按条目被删除时所在的来源范围恢复；显式给出才覆盖。 */
  targetOriginKind: z.enum(["local", "cloud"]).optional(),
  targetWorkspaceId: z.string().trim().min(1).optional()
});

export const recycleBinPurgeSchema = z.object({
  id: z.string().trim().min(1)
});

export const recycleBinClearSchema = z.object({});

export type RecycleBinListInput = z.infer<typeof recycleBinListSchema>;
export type RecycleBinRestoreInput = z.infer<typeof recycleBinRestoreSchema>;
export type RecycleBinPurgeInput = z.infer<typeof recycleBinPurgeSchema>;
export type RecycleBinClearInput = z.infer<typeof recycleBinClearSchema>;
