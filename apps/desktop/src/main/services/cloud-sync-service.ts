import { Buffer } from "node:buffer";
import { z } from "zod";
import type {
  AppPreferences,
  AppPreferencesPatch,
  AuthType,
  CloudSyncResourceSyncState,
  ConnectionProfile,
  ProxyProfile,
  ProxyType,
  SshKeyProfile
} from "../../../../../packages/core/src/index";
import {
  authTypeSchema,
  cloudSyncAppliedEventSchema,
  cloudSyncConflictItemSchema,
  cloudSyncResolveConflictSchema,
  cloudSyncStatusSchema,
  proxyTypeSchema,
  type CloudSyncAppliedEvent,
  type CloudSyncConflictItem,
  type CloudSyncConfigureInput,
  type CloudSyncResolveConflictInput,
  type CloudSyncStatus
} from "../../../../../packages/shared/src/index";
import {
  KeytarPasswordCache,
  decryptWorkspaceSecret,
  encryptWorkspaceSecret,
  type CredentialVault,
  type WorkspaceSecretEnvelope
} from "../../../../../packages/security/src/index";

const workspaceSecretEnvelopeSchema = z.object({
  v: z.literal(1),
  alg: z.literal("aes-256-gcm"),
  kdf: z.literal("scrypt"),
  salt: z.string().min(1),
  iv: z.string().min(1),
  aad: z.string().optional(),
  ciphertext: z.string().min(1),
  tag: z.string().min(1)
});

const connectionPayloadSchema = z.object({
  id: z.string().uuid(),
  name: z.string().min(1),
  host: z.string().min(1),
  port: z.number().int().min(1).max(65535),
  username: z.string(),
  authType: authTypeSchema,
  credentialCipher: workspaceSecretEnvelopeSchema.nullable().optional(),
  sshKeyId: z.string().uuid().nullable().optional(),
  hostFingerprint: z.string().nullable().optional(),
  strictHostKeyChecking: z.boolean(),
  proxyId: z.string().uuid().nullable().optional(),
  keepAliveEnabled: z.boolean().nullable().optional(),
  keepAliveIntervalSec: z.number().int().min(5).max(600).nullable().optional(),
  groupPath: z.string().min(1),
  tags: z.array(z.string()).default([]),
  notes: z.string().nullable().optional(),
  favorite: z.boolean(),
  updatedAt: z.string().min(1)
});

const sshKeyPayloadSchema = z.object({
  id: z.string().uuid(),
  name: z.string().min(1),
  privateKeyCipher: workspaceSecretEnvelopeSchema,
  passphraseCipher: workspaceSecretEnvelopeSchema.nullable().optional(),
  updatedAt: z.string().min(1)
});

const proxyPayloadSchema = z.object({
  id: z.string().uuid(),
  name: z.string().min(1),
  proxyType: proxyTypeSchema,
  host: z.string().min(1),
  port: z.number().int().min(1).max(65535),
  username: z.string().nullable().optional(),
  passwordCipher: workspaceSecretEnvelopeSchema.nullable().optional(),
  updatedAt: z.string().min(1)
});

const connectionSnapshotItemSchema = z.object({
  payload: connectionPayloadSchema,
  revision: z.number().int().min(1),
  updatedAt: z.string().min(1)
});

const sshKeySnapshotItemSchema = z.object({
  payload: sshKeyPayloadSchema,
  revision: z.number().int().min(1),
  updatedAt: z.string().min(1)
});

const proxySnapshotItemSchema = z.object({
  payload: proxyPayloadSchema,
  revision: z.number().int().min(1),
  updatedAt: z.string().min(1)
});

const deletedSnapshotItemSchema = z.object({
  id: z.string().uuid(),
  revision: z.number().int().min(1),
  deletedAt: z.string().min(1)
});

const pullResponseSchema = z.object({
  ok: z.boolean(),
  workspace: z.string(),
  version: z.number().int().min(0),
  unchanged: z.boolean().default(false),
  serverTime: z.string().min(1),
  snapshot: z.object({
    connections: z.array(connectionSnapshotItemSchema).default([]),
    sshKeys: z.array(sshKeySnapshotItemSchema).default([]),
    proxies: z.array(proxySnapshotItemSchema).default([]),
    deleted: z.object({
      connections: z.array(deletedSnapshotItemSchema).default([]),
      sshKeys: z.array(deletedSnapshotItemSchema).default([]),
      proxies: z.array(deletedSnapshotItemSchema).default([])
    })
  }).optional()
});

const workspaceStatusResponseSchema = z.object({
  ok: z.boolean(),
  workspace: z.string(),
  version: z.number().int().min(0),
  serverTime: z.string().min(1)
});

const mutationResponseSchema = z.object({
  ok: z.boolean(),
  workspaceVersion: z.number().int().min(0),
  resourceRevision: z.number().int().min(1)
});

const conflictResponseSchema = z.object({
  ok: z.literal(false),
  error: z.literal("conflict"),
  conflict: z.object({
    resourceType: z.enum(["connection", "sshKey", "proxy"]),
    resourceId: z.string().uuid(),
    serverRevision: z.number().int().min(1),
    serverUpdatedAt: z.string().min(1).nullable().optional(),
    serverDeleted: z.boolean(),
    serverPayload: z.unknown().optional()
  })
});

const pendingQueueItemSchema = z.object({
  resourceType: z.enum(["connection", "sshKey", "proxy"]),
  resourceId: z.string().uuid(),
  action: z.enum(["upsert", "delete"]),
  baseRevision: z.number().int().min(1).nullable().default(null),
  force: z.boolean().default(false),
  seq: z.number().int().min(1),
  queuedAt: z.string().min(1),
  lastAttemptAt: z.string().min(1).optional(),
  lastError: z.string().min(1).optional()
});

const pendingQueueStateSchema = z.object({
  apiBaseUrl: z.string().trim().min(1),
  workspaceName: z.string().trim().min(1),
  nextSeq: z.number().int().min(1).default(1),
  items: z.array(pendingQueueItemSchema).default([])
});

type ConnectionPayload = z.infer<typeof connectionPayloadSchema>;
type SshKeyPayload = z.infer<typeof sshKeyPayloadSchema>;
type ProxyPayload = z.infer<typeof proxyPayloadSchema>;
type CloudSyncPendingQueueItem = z.infer<typeof pendingQueueItemSchema>;
type CloudSyncPendingQueueState = z.infer<typeof pendingQueueStateSchema>;
type ConnectionSnapshotItem = z.infer<typeof connectionSnapshotItemSchema>;
type SshKeySnapshotItem = z.infer<typeof sshKeySnapshotItemSchema>;
type ProxySnapshotItem = z.infer<typeof proxySnapshotItemSchema>;
type DeletedSnapshotItem = z.infer<typeof deletedSnapshotItemSchema>;
type ConflictResponse = z.infer<typeof conflictResponseSchema>;

type CloudSyncResourceType = CloudSyncPendingQueueItem["resourceType"];
type CloudSyncPendingAction = CloudSyncPendingQueueItem["action"];

const CLOUD_SYNC_PENDING_QUEUE_SETTING_KEY = "cloud_sync_pending_queue";

export interface CloudSyncApplyConnectionInput {
  id: string;
  name: string;
  host: string;
  port: number;
  username: string;
  authType: AuthType;
  password?: string;
  sshKeyId?: string;
  hostFingerprint?: string;
  strictHostKeyChecking: boolean;
  proxyId?: string;
  keepAliveEnabled?: boolean;
  keepAliveIntervalSec?: number;
  groupPath: string;
  tags: string[];
  notes?: string;
  favorite: boolean;
  updatedAt: string;
}

export interface CloudSyncApplySshKeyInput {
  id: string;
  name: string;
  keyContent: string;
  passphrase?: string;
  updatedAt: string;
}

export interface CloudSyncApplyProxyInput {
  id: string;
  name: string;
  proxyType: ProxyType;
  host: string;
  port: number;
  username?: string;
  password?: string;
  updatedAt: string;
}

interface CloudSyncHttpRequest {
  apiBaseUrl: string;
  workspaceName: string;
  workspacePassword: string;
  pathname: string;
  payload: unknown;
  ignoreTlsErrors: boolean;
}

export interface CloudSyncServiceOptions {
  keytarServiceName: string;
  getPreferences: () => AppPreferences;
  savePreferencesPatch: (patch: AppPreferencesPatch, options?: { reconfigureCloudSync?: boolean }) => AppPreferences;
  vault: CredentialVault;
  listConnections: () => ConnectionProfile[];
  listSshKeys: () => SshKeyProfile[];
  listProxies: () => ProxyProfile[];
  applyConnectionFromCloudSync: (input: CloudSyncApplyConnectionInput) => Promise<void>;
  applySshKeyFromCloudSync: (input: CloudSyncApplySshKeyInput) => Promise<void>;
  applyProxyFromCloudSync: (input: CloudSyncApplyProxyInput) => Promise<void>;
  removeConnectionFromCloudSync: (id: string) => Promise<void>;
  removeSshKeyFromCloudSync: (id: string) => Promise<void>;
  removeProxyFromCloudSync: (id: string) => Promise<void>;
  emitStatus: (status: CloudSyncStatus) => void;
  emitApplied: (event: CloudSyncAppliedEvent) => void;
  loadPendingQueueState?: () => unknown;
  savePendingQueueState?: (state: unknown | undefined) => void;
  listResourceStates?: () => CloudSyncResourceSyncState[];
  getResourceState?: (resourceType: CloudSyncResourceType, resourceId: string) => CloudSyncResourceSyncState | undefined;
  saveResourceState?: (state: CloudSyncResourceSyncState) => void;
  removeResourceState?: (resourceType: CloudSyncResourceType, resourceId: string) => void;
  requestJson?: <T>(request: CloudSyncHttpRequest, schema: z.ZodType<T>) => Promise<T>;
}

interface CloudSyncCredentials {
  apiBaseUrl: string;
  workspaceName: string;
  workspacePassword: string;
  ignoreTlsErrors: boolean;
}

const normalizeApiBaseUrl = (value: string): string => value.trim().replace(/\/+$/, "");
const normalizeGroupPath = (value: string): string => {
  const trimmed = value.trim();
  if (!trimmed || trimmed === "/") {
    return "/";
  }
  return trimmed.replace(/\/+$/, "");
};
const workspaceGroupPathPrefix = (workspaceName: string): string => normalizeGroupPath(`/workspace/${workspaceName.trim()}`);
const isConnectionGroupPathInScope = (groupPath: string, workspaceName: string): boolean => {
  const prefix = workspaceGroupPathPrefix(workspaceName);
  if (prefix === "/" || workspaceName.trim().length === 0) {
    return false;
  }
  const normalizedGroupPath = normalizeGroupPath(groupPath);
  return normalizedGroupPath === prefix || normalizedGroupPath.startsWith(`${prefix}/`);
};

const keytarAccountForWorkspace = (apiBaseUrl: string, workspaceName: string): string => {
  return `cloud-sync:${normalizeApiBaseUrl(apiBaseUrl)}::${workspaceName.trim()}`;
};

const queueScopeKey = (apiBaseUrl: string, workspaceName: string): string => {
  return `${normalizeApiBaseUrl(apiBaseUrl)}::${workspaceName.trim()}`;
};

const normalizeErrorMessage = (error: unknown, fallback = "Cloud sync request failed"): string => {
  if (error instanceof Error && error.message.trim()) {
    return error.message;
  }
  if (typeof error === "string" && error.trim()) {
    return error;
  }
  return fallback;
};

class CloudSyncConflictError extends Error {
  readonly conflict: ConflictResponse["conflict"];

  constructor(conflict: ConflictResponse["conflict"]) {
    super(`Cloud sync conflict for ${conflict.resourceType}:${conflict.resourceId}`);
    this.name = "CloudSyncConflictError";
    this.conflict = conflict;
  }
}

const parseCloudSyncConflictError = (error: unknown): ConflictResponse["conflict"] | undefined => {
  if (error instanceof CloudSyncConflictError) {
    return error.conflict;
  }

  const message = error instanceof Error
    ? error.message
    : typeof error === "string"
      ? error
      : undefined;

  if (!message) {
    return undefined;
  }

  try {
    return conflictResponseSchema.parse(JSON.parse(message)).conflict;
  } catch {
    return undefined;
  }
};

const comparePendingItems = (left: CloudSyncPendingQueueItem, right: CloudSyncPendingQueueItem): number => {
  const rank = (item: CloudSyncPendingQueueItem): number => {
    if (item.action === "upsert") {
      switch (item.resourceType) {
        case "sshKey":
          return 1;
        case "proxy":
          return 2;
        case "connection":
          return 3;
      }
    }

    switch (item.resourceType) {
      case "connection":
        return 4;
      case "proxy":
        return 5;
      case "sshKey":
        return 6;
    }
  };

  const rankDiff = rank(left) - rank(right);
  if (rankDiff !== 0) {
    return rankDiff;
  }
  return left.seq - right.seq;
};

const defaultRequestJson = async <T>(
  request: CloudSyncHttpRequest,
  schema: z.ZodType<T>
): Promise<T> => {
  const response = await fetch(`${normalizeApiBaseUrl(request.apiBaseUrl)}${request.pathname}`, {
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
    if (response.status === 409) {
      try {
        const conflict = conflictResponseSchema.parse(JSON.parse(bodyText)).conflict;
        throw new CloudSyncConflictError(conflict);
      } catch (error) {
        if (error instanceof CloudSyncConflictError) {
          throw error;
        }
      }
    }
    const message = bodyText.trim() || `HTTP ${response.status}`;
    throw new Error(message);
  }

  return schema.parse(await response.json());
};

interface PendingExecutionResult {
  mutation: z.infer<typeof mutationResponseSchema> | null;
  clearResourceState?: boolean;
}

export class CloudSyncService {
  private readonly options: CloudSyncServiceOptions;
  private timer: ReturnType<typeof setInterval> | undefined;
  private queue: Promise<void> = Promise.resolve();
  private readonly resourceStateFallback = new Map<string, CloudSyncResourceSyncState>();
  private runtimeState: CloudSyncStatus["state"] = "disabled";
  private lastError: string | null = null;
  private hasWorkspacePassword = false;
  private currentVersion: number | null = null;
  private pendingQueueState: CloudSyncPendingQueueState | undefined;
  private pendingQueueScopeKey: string | null = null;

  constructor(options: CloudSyncServiceOptions) {
    this.options = options;
  }

  initialize(): void {
    void this.refreshFromPreferences({ triggerPull: true });
  }

  async dispose(): Promise<void> {
    this.stopTimer();
    await this.queue.catch(() => undefined);
  }

  async configure(input: CloudSyncConfigureInput): Promise<CloudSyncStatus> {
    const apiBaseUrl = normalizeApiBaseUrl(input.apiBaseUrl);
    const workspaceName = input.workspaceName.trim();
    const nextAccount = keytarAccountForWorkspace(apiBaseUrl, workspaceName);
    const previousPrefs = this.options.getPreferences().cloudSync;
    const previousAccount = previousPrefs.apiBaseUrl && previousPrefs.workspaceName
      ? keytarAccountForWorkspace(previousPrefs.apiBaseUrl, previousPrefs.workspaceName)
      : undefined;
    const keytarCache = new KeytarPasswordCache(this.options.keytarServiceName, nextAccount);

    if (!keytarCache.isAvailable()) {
      throw new Error("系统钥匙串不可用，暂时无法启用云同步。");
    }

    await this.withQueue(async () => {
      this.transition("syncing", null);
      try {
        await this.postJson(
          {
            apiBaseUrl,
            workspaceName,
            workspacePassword: input.workspacePassword,
            ignoreTlsErrors: input.ignoreTlsErrors
          },
          "/api/v1/sync/workspace/status",
          {},
          workspaceStatusResponseSchema
        );

        if (previousAccount && previousAccount !== nextAccount) {
          await new KeytarPasswordCache(this.options.keytarServiceName, previousAccount).clear();
        }

        const previousScopeKey = previousPrefs.apiBaseUrl && previousPrefs.workspaceName
          ? queueScopeKey(previousPrefs.apiBaseUrl, previousPrefs.workspaceName)
          : null;
        const nextScopeKey = queueScopeKey(apiBaseUrl, workspaceName);
        if (previousScopeKey && previousScopeKey !== nextScopeKey) {
          this.clearPendingQueueState();
          this.clearAllResourceStates();
        } else {
          this.ensurePendingQueueState(apiBaseUrl, workspaceName);
        }

        await keytarCache.remember(input.workspacePassword);
        this.hasWorkspacePassword = true;
        this.options.savePreferencesPatch({
          cloudSync: {
            enabled: true,
            apiBaseUrl,
            workspaceName,
            pullIntervalSec: input.pullIntervalSec,
            ignoreTlsErrors: input.ignoreTlsErrors
          }
        }, { reconfigureCloudSync: false });
        await this.runSyncCycleInternal({
          apiBaseUrl,
          workspaceName,
          workspacePassword: input.workspacePassword,
          ignoreTlsErrors: input.ignoreTlsErrors
        });
        this.ensureTimer(input.pullIntervalSec);
      } catch (error) {
        this.transition("error", normalizeErrorMessage(error, "云同步配置失败"));
        throw error;
      }
    });

    return this.status();
  }

  async disable(): Promise<{ ok: true }> {
    const prefs = this.options.getPreferences().cloudSync;
    const account = prefs.apiBaseUrl && prefs.workspaceName
      ? keytarAccountForWorkspace(prefs.apiBaseUrl, prefs.workspaceName)
      : undefined;

    await this.withQueue(async () => {
      this.stopTimer();
      if (account) {
        await new KeytarPasswordCache(this.options.keytarServiceName, account).clear();
      }
      this.hasWorkspacePassword = false;
      this.currentVersion = null;
      this.options.savePreferencesPatch({
        cloudSync: {
          enabled: false
        }
      }, { reconfigureCloudSync: false });
      this.transition("disabled", null);
    });

    return { ok: true };
  }

  async status(): Promise<CloudSyncStatus> {
    await this.refreshPasswordPresence();
    this.pruneDeprecatedProxySyncState();
    return this.emitCurrentStatus();
  }

  async syncNow(): Promise<{ ok: true }> {
    await this.withQueue(async () => {
      try {
        const credentials = await this.getStoredCredentials();
        await this.runSyncCycleInternal(credentials);
      } catch (error) {
        this.transition("error", normalizeErrorMessage(error, "云同步拉取失败"));
        throw error;
      }
    });
    return { ok: true };
  }

  async listConflicts(): Promise<CloudSyncConflictItem[]> {
    this.pruneDeprecatedProxySyncState();
    const items = this.listConflictStates().map((state) => cloudSyncConflictItemSchema.parse({
      resourceType: state.resourceType,
      resourceId: state.resourceId,
      displayName: this.resolveConflictDisplayName(state),
      localUpdatedAt: this.resolveLocalUpdatedAt(state.resourceType, state.resourceId),
      serverUpdatedAt: state.conflictRemoteUpdatedAt ?? null,
      serverDeleted: state.conflictRemoteDeleted,
      hasPendingLocalChange: this.findPendingItem(state.resourceType, state.resourceId) !== undefined
    }));

    items.sort((left, right) => {
      const leftTime = left.serverUpdatedAt ?? left.localUpdatedAt ?? "";
      const rightTime = right.serverUpdatedAt ?? right.localUpdatedAt ?? "";
      return rightTime.localeCompare(leftTime);
    });
    return items;
  }

  async resolveConflict(input: CloudSyncResolveConflictInput): Promise<{ ok: true }> {
    const payload = cloudSyncResolveConflictSchema.parse(input);
    await this.withQueue(async () => {
      const state = this.getResourceState(payload.resourceType, payload.resourceId);
      if (!state?.conflictRemoteRevision) {
        return;
      }

      const credentials = await this.getStoredCredentials();
      if (payload.strategy === "overwrite_local") {
        await this.applyConflictOverwriteLocal(payload.resourceType, payload.resourceId, state, credentials.workspacePassword);
      } else {
        await this.applyConflictKeepLocal(payload.resourceType, payload.resourceId, state, credentials);
      }
      this.transition("idle", null);
    });
    return { ok: true };
  }

  async pushConnectionUpsert(profile: ConnectionProfile): Promise<void> {
    const prefs = this.options.getPreferences().cloudSync;
    if (!prefs.enabled) {
      return;
    }

    await this.withQueue(async () => {
      const credentials = await this.getStoredCredentials();
      this.pruneDeprecatedProxySyncState();
      if (this.isConnectionProfileInScope(profile, credentials.workspaceName)) {
        this.queuePendingItem(credentials.apiBaseUrl, credentials.workspaceName, "connection", profile.id, "upsert");
      } else if (this.hasTrackedRemoteConnection(profile.id)) {
        this.queuePendingItem(credentials.apiBaseUrl, credentials.workspaceName, "connection", profile.id, "delete");
      } else {
        this.discardResourceSyncState("connection", profile.id);
      }
      await this.flushPendingQueueInternal(credentials);
      this.transition("idle", null);
    }).catch((error) => {
      this.transition("error", normalizeErrorMessage(error, "连接云同步失败"));
    });
  }

  async pushConnectionDelete(id: string): Promise<void> {
    const prefs = this.options.getPreferences().cloudSync;
    if (!prefs.enabled) {
      this.discardResourceSyncState("connection", id);
      return;
    }

    await this.withQueue(async () => {
      const credentials = await this.getStoredCredentials();
      this.pruneDeprecatedProxySyncState();
      if (this.hasTrackedRemoteConnection(id)) {
        this.queuePendingItem(credentials.apiBaseUrl, credentials.workspaceName, "connection", id, "delete");
        await this.flushPendingQueueInternal(credentials);
      } else {
        this.discardResourceSyncState("connection", id);
      }
      this.transition("idle", null);
    }).catch((error) => {
      this.transition("error", normalizeErrorMessage(error, "连接删除同步失败"));
    });
  }

  async pushSshKeyUpsert(profile: SshKeyProfile): Promise<void> {
    await this.enqueueAndFlush("sshKey", profile.id, "upsert", "SSH 密钥云同步失败");
  }

  async pushSshKeyDelete(id: string): Promise<void> {
    await this.enqueueAndFlush("sshKey", id, "delete", "SSH 密钥删除同步失败");
  }

  async pushProxyUpsert(profile: ProxyProfile): Promise<void> {
    void profile;
    this.pruneDeprecatedProxySyncState();
  }

  async pushProxyDelete(id: string): Promise<void> {
    this.discardResourceSyncState("proxy", id);
  }

  async refreshFromPreferences(options: { triggerPull: boolean }): Promise<void> {
    const prefs = this.options.getPreferences().cloudSync;

    if (!prefs.enabled) {
      this.stopTimer();
      this.hasWorkspacePassword = false;
      this.currentVersion = null;
      this.emitCurrentStatus();
      this.transition("disabled", null);
      return;
    }

    await this.refreshPasswordPresence();
    this.ensurePendingQueueState(prefs.apiBaseUrl, prefs.workspaceName);
    this.pruneDeprecatedProxySyncState();
    this.ensureTimer(prefs.pullIntervalSec);

    if (!this.hasWorkspacePassword) {
      const keytarProbe = new KeytarPasswordCache(this.options.keytarServiceName, "cloud-sync:availability");
      if (!keytarProbe.isAvailable()) {
        this.stopTimer();
        this.options.savePreferencesPatch({
          cloudSync: { enabled: false }
        }, { reconfigureCloudSync: false });
        this.transition("disabled", "系统钥匙串不可用，云同步已自动关闭。请在钥匙串恢复后重新启用。");
        return;
      }
      this.transition("error", "未找到云同步 workspace 密码，请重新配置。");
      return;
    }

    if (options.triggerPull) {
      await this.withQueue(async () => {
        const credentials = await this.getStoredCredentials();
        await this.runSyncCycleInternal(credentials);
      }).catch((error) => {
        this.transition("error", normalizeErrorMessage(error, "云同步拉取失败"));
      });
      return;
    }

    this.transition("idle", this.lastError);
  }

  private async enqueueAndFlush(
    resourceType: CloudSyncResourceType,
    resourceId: string,
    action: CloudSyncPendingAction,
    fallbackError: string
  ): Promise<void> {
    const prefs = this.options.getPreferences().cloudSync;
    if (!prefs.enabled) {
      return;
    }

    await this.withQueue(async () => {
      const credentials = await this.getStoredCredentials();
      this.pruneDeprecatedProxySyncState();
      this.queuePendingItem(credentials.apiBaseUrl, credentials.workspaceName, resourceType, resourceId, action);
      await this.flushPendingQueueInternal(credentials);
      this.transition("idle", null);
    }).catch((error) => {
      this.transition("error", normalizeErrorMessage(error, fallbackError));
    });
  }

  private async runSyncCycleInternal(credentials: CloudSyncCredentials): Promise<void> {
    this.pruneDeprecatedProxySyncState();
    this.transition("syncing", null);
    await this.flushPendingQueueInternal(credentials);
    await this.runPullInternal(credentials);
  }

  private async runPullInternal(credentials: CloudSyncCredentials): Promise<void> {
    const pullStartedAt = new Date().toISOString();
    const response = await this.postJson(
      credentials,
      "/api/v1/sync/pull",
      {
        knownVersion: this.currentVersion ?? 0
      },
      pullResponseSchema
    );

    await this.persistLastSyncAt(response.serverTime);

    if (response.unchanged || !response.snapshot) {
      this.currentVersion = response.version;
      this.transition("idle", null);
      return;
    }

    await this.applySnapshot(response.snapshot, credentials, pullStartedAt);
    this.currentVersion = response.version;
    const event = cloudSyncAppliedEventSchema.parse({
      appliedAt: response.serverTime,
      version: response.version
    });
    this.options.emitApplied(event);
    this.transition("idle", null);
  }

  private async flushPendingQueueInternal(credentials: CloudSyncCredentials): Promise<void> {
    const queueState = this.ensurePendingQueueState(credentials.apiBaseUrl, credentials.workspaceName);
    if (!queueState) {
      return;
    }
    if (queueState.items.length === 0) {
      return;
    }

    const ordered = [...queueState.items].sort(comparePendingItems);
    for (const item of ordered) {
      const currentItem = queueState.items.find((entry) =>
        entry.resourceType === item.resourceType &&
        entry.resourceId === item.resourceId
      );
      if (!currentItem) {
        continue;
      }
      if (this.getResourceState(currentItem.resourceType, currentItem.resourceId)?.conflictRemoteRevision) {
        continue;
      }

      currentItem.lastAttemptAt = new Date().toISOString();
      currentItem.lastError = undefined;
      this.persistPendingQueueState();

      try {
        const result = await this.executePendingItem(currentItem, credentials);
        queueState.items = queueState.items.filter((entry) =>
          !(entry.resourceType === currentItem.resourceType && entry.resourceId === currentItem.resourceId)
        );
        this.persistPendingQueueState();
        if (result.mutation) {
          this.markSuccessfulMutation(result.mutation.workspaceVersion);
          if (result.clearResourceState) {
            this.removeResourceState(currentItem.resourceType, currentItem.resourceId);
          } else {
            this.upsertResolvedResourceState(currentItem.resourceType, currentItem.resourceId, result.mutation.resourceRevision);
          }
        } else if (result.clearResourceState) {
          this.removeResourceState(currentItem.resourceType, currentItem.resourceId);
        }
      } catch (error) {
        const conflict = parseCloudSyncConflictError(error);
        if (conflict) {
          currentItem.lastAttemptAt = new Date().toISOString();
          currentItem.lastError = "conflict";
          this.saveConflictState(currentItem.resourceType, currentItem.resourceId, conflict);
          this.persistPendingQueueState();
          this.emitCurrentStatus();
          continue;
        }
        currentItem.lastAttemptAt = new Date().toISOString();
        currentItem.lastError = normalizeErrorMessage(error, "待同步队列补发失败");
        this.persistPendingQueueState();
        continue;
      }
    }
  }

  private async executePendingItem(
    item: CloudSyncPendingQueueItem,
    credentials: CloudSyncCredentials
  ): Promise<PendingExecutionResult> {
    switch (item.resourceType) {
      case "connection":
        return item.action === "upsert"
          ? this.executeConnectionUpsert(item, credentials)
          : this.executeConnectionDelete(item, credentials);
      case "sshKey":
        return item.action === "upsert"
          ? this.executeSshKeyUpsert(item, credentials)
          : this.executeDelete("/api/v1/sync/ssh-keys/delete", item, credentials);
      case "proxy":
        return { mutation: null, clearResourceState: true };
    }
  }

  private async executeConnectionUpsert(
    item: CloudSyncPendingQueueItem,
    credentials: CloudSyncCredentials
  ): Promise<PendingExecutionResult> {
    const id = item.resourceId;
    const profile = this.options.listConnections().find((item) => item.id === id);
    if (!profile) {
      return { mutation: null, clearResourceState: !this.hasTrackedRemoteConnection(id) };
    }
    if (!this.isConnectionProfileInScope(profile, credentials.workspaceName)) {
      return this.executeConnectionExitedScope(item, credentials);
    }

    const payload = await this.serializeConnection(profile, credentials.workspacePassword);
    const response = await this.postJson(
      credentials,
      "/api/v1/sync/connections/upsert",
      {
        baseRevision: item.baseRevision ?? null,
        force: item.force,
        connection: payload
      },
      mutationResponseSchema
    );
    return { mutation: response };
  }

  private async executeSshKeyUpsert(
    item: CloudSyncPendingQueueItem,
    credentials: CloudSyncCredentials
  ): Promise<PendingExecutionResult> {
    const id = item.resourceId;
    const profile = this.options.listSshKeys().find((item) => item.id === id);
    if (!profile) {
      return { mutation: null };
    }

    const payload = await this.serializeSshKey(profile, credentials.workspacePassword);
    const response = await this.postJson(
      credentials,
      "/api/v1/sync/ssh-keys/upsert",
      {
        baseRevision: item.baseRevision ?? null,
        force: item.force,
        sshKey: payload
      },
      mutationResponseSchema
    );
    return { mutation: response };
  }

  private async executeDelete(
    pathname: string,
    item: CloudSyncPendingQueueItem,
    credentials: CloudSyncCredentials
  ): Promise<PendingExecutionResult> {
    const response = await this.postJson(
      credentials,
      pathname,
      {
        baseRevision: item.baseRevision ?? null,
        force: item.force,
        id: item.resourceId
      },
      mutationResponseSchema
    );
    return { mutation: response };
  }

  private async executeConnectionDelete(
    item: CloudSyncPendingQueueItem,
    credentials: CloudSyncCredentials
  ): Promise<PendingExecutionResult> {
    return this.executeDelete("/api/v1/sync/connections/delete", item, credentials);
  }

  private async executeConnectionExitedScope(
    item: CloudSyncPendingQueueItem,
    credentials: CloudSyncCredentials
  ): Promise<PendingExecutionResult> {
    if (!this.hasTrackedRemoteConnection(item.resourceId)) {
      return { mutation: null, clearResourceState: true };
    }

    const deleteItem: CloudSyncPendingQueueItem = {
      ...item,
      action: "delete",
      baseRevision: item.baseRevision ?? this.getResourceState("connection", item.resourceId)?.serverRevision ?? null,
    };
    const result = await this.executeConnectionDelete(deleteItem, credentials);
    return {
      ...result,
      clearResourceState: true,
    };
  }

  private async applySnapshot(
    snapshot: NonNullable<z.infer<typeof pullResponseSchema>["snapshot"]>,
    credentials: CloudSyncCredentials,
    pullStartedAt: string
  ): Promise<void> {
    const { workspacePassword, apiBaseUrl, workspaceName } = credentials;
    const scopedConnections = snapshot.connections.filter((item) =>
      isConnectionGroupPathInScope(item.payload.groupPath, workspaceName)
    );
    const scopedDeletedConnections = snapshot.deleted.connections.filter((item) =>
      this.shouldProcessConnectionDeletion(item.id, workspaceName)
    );
    const remoteConnectionIds = new Set(scopedConnections.map((item) => item.payload.id));
    const remoteSshKeyIds = new Set(snapshot.sshKeys.map((item) => item.payload.id));
    const deletedConnectionIds = new Set(scopedDeletedConnections.map((item) => item.id));
    const deletedSshKeyIds = new Set(snapshot.deleted.sshKeys.map((item) => item.id));

    for (const sshKey of snapshot.sshKeys) {
      const local = this.options.listSshKeys().find((k) => k.id === sshKey.payload.id);
      if (local && local.updatedAt > pullStartedAt) {
        this.queuePendingItem(apiBaseUrl, workspaceName, "sshKey", sshKey.payload.id, "upsert");
        continue;
      }
      await this.applyRemoteSshKeySnapshotItem(sshKey, workspacePassword);
    }
    for (const connection of scopedConnections) {
      const local = this.options.listConnections().find((c) => c.id === connection.payload.id);
      if (local && local.updatedAt > pullStartedAt) {
        this.queuePendingItem(apiBaseUrl, workspaceName, "connection", connection.payload.id, "upsert");
        continue;
      }
      await this.applyRemoteConnectionSnapshotItem(connection, workspacePassword);
    }

    for (const tombstone of scopedDeletedConnections) {
      const local = this.options.listConnections().find((connection) => connection.id === tombstone.id);
      if (local && local.updatedAt > pullStartedAt) {
        this.queuePendingItem(apiBaseUrl, workspaceName, "connection", tombstone.id, "upsert");
        continue;
      }
      await this.applyRemoteConnectionDelete(tombstone);
    }
    for (const tombstone of snapshot.deleted.sshKeys) {
      const local = this.options.listSshKeys().find((sshKey) => sshKey.id === tombstone.id);
      if (local && local.updatedAt > pullStartedAt) {
        this.queuePendingItem(apiBaseUrl, workspaceName, "sshKey", tombstone.id, "upsert");
        continue;
      }
      await this.applyRemoteSshKeyDelete(tombstone);
    }

    for (const connection of this.options.listConnections()) {
      if (
        this.isConnectionProfileInScope(connection, workspaceName) &&
        !remoteConnectionIds.has(connection.id) &&
        !deletedConnectionIds.has(connection.id) &&
        !this.findPendingItem("connection", connection.id)
      ) {
        await this.options.removeConnectionFromCloudSync(connection.id);
        this.removeResourceState("connection", connection.id);
      }
    }
    for (const sshKey of this.options.listSshKeys()) {
      if (
        !remoteSshKeyIds.has(sshKey.id) &&
        !deletedSshKeyIds.has(sshKey.id) &&
        !this.findPendingItem("sshKey", sshKey.id)
      ) {
        await this.options.removeSshKeyFromCloudSync(sshKey.id);
        this.removeResourceState("sshKey", sshKey.id);
      }
    }
  }

  private shouldDeferRemoteApply(
    resourceType: CloudSyncResourceType,
    resourceId: string,
    remoteRevision: number
  ): boolean {
    const state = this.getResourceState(resourceType, resourceId);
    const pending = this.findPendingItem(resourceType, resourceId);
    if (!pending) {
      return typeof state?.conflictRemoteRevision === "number";
    }
    const baseRevision = pending.baseRevision ?? state?.serverRevision ?? 0;
    return baseRevision < remoteRevision || typeof state?.conflictRemoteRevision === "number";
  }

  private async applyRemoteConnectionSnapshotItem(
    item: ConnectionSnapshotItem,
    workspacePassword: string
  ): Promise<void> {
    if (this.shouldDeferRemoteApply("connection", item.payload.id, item.revision)) {
      this.saveConflictState("connection", item.payload.id, {
        resourceType: "connection",
        resourceId: item.payload.id,
        serverRevision: item.revision,
        serverUpdatedAt: item.updatedAt,
        serverDeleted: false,
        serverPayload: item.payload
      });
      return;
    }
    await this.options.applyConnectionFromCloudSync(await this.deserializeConnection(item.payload, workspacePassword));
    this.upsertResolvedResourceState("connection", item.payload.id, item.revision);
  }

  private async applyRemoteSshKeySnapshotItem(
    item: SshKeySnapshotItem,
    workspacePassword: string
  ): Promise<void> {
    if (this.shouldDeferRemoteApply("sshKey", item.payload.id, item.revision)) {
      this.saveConflictState("sshKey", item.payload.id, {
        resourceType: "sshKey",
        resourceId: item.payload.id,
        serverRevision: item.revision,
        serverUpdatedAt: item.updatedAt,
        serverDeleted: false,
        serverPayload: item.payload
      });
      return;
    }
    await this.options.applySshKeyFromCloudSync(await this.deserializeSshKey(item.payload, workspacePassword));
    this.upsertResolvedResourceState("sshKey", item.payload.id, item.revision);
  }

  private async applyRemoteConnectionDelete(item: DeletedSnapshotItem): Promise<void> {
    if (this.shouldDeferRemoteApply("connection", item.id, item.revision)) {
      this.saveConflictState("connection", item.id, {
        resourceType: "connection",
        resourceId: item.id,
        serverRevision: item.revision,
        serverUpdatedAt: item.deletedAt,
        serverDeleted: true
      });
      return;
    }
    await this.options.removeConnectionFromCloudSync(item.id);
    this.upsertResolvedResourceState("connection", item.id, item.revision);
  }

  private async applyRemoteSshKeyDelete(item: DeletedSnapshotItem): Promise<void> {
    if (this.shouldDeferRemoteApply("sshKey", item.id, item.revision)) {
      this.saveConflictState("sshKey", item.id, {
        resourceType: "sshKey",
        resourceId: item.id,
        serverRevision: item.revision,
        serverUpdatedAt: item.deletedAt,
        serverDeleted: true
      });
      return;
    }
    await this.options.removeSshKeyFromCloudSync(item.id);
    this.upsertResolvedResourceState("sshKey", item.id, item.revision);
  }

  private async applyConflictOverwriteLocal(
    resourceType: CloudSyncResourceType,
    resourceId: string,
    state: CloudSyncResourceSyncState,
    workspacePassword: string
  ): Promise<void> {
    if (!state.conflictRemoteRevision) {
      return;
    }
    if (state.conflictRemoteDeleted) {
      switch (resourceType) {
        case "connection":
          await this.options.removeConnectionFromCloudSync(resourceId);
          break;
        case "sshKey":
          await this.options.removeSshKeyFromCloudSync(resourceId);
          break;
        case "proxy":
          this.discardResourceSyncState("proxy", resourceId);
          break;
      }
    } else if (state.conflictRemotePayloadJson) {
      switch (resourceType) {
        case "connection":
          await this.options.applyConnectionFromCloudSync(
            await this.deserializeConnection(
              connectionPayloadSchema.parse(JSON.parse(state.conflictRemotePayloadJson)),
              workspacePassword
            )
          );
          break;
        case "sshKey":
          await this.options.applySshKeyFromCloudSync(
            await this.deserializeSshKey(
              sshKeyPayloadSchema.parse(JSON.parse(state.conflictRemotePayloadJson)),
              workspacePassword
            )
          );
          break;
        case "proxy":
          this.discardResourceSyncState("proxy", resourceId);
          break;
      }
    }

    this.removePendingItem(resourceType, resourceId);
    this.upsertResolvedResourceState(resourceType, resourceId, state.conflictRemoteRevision);
    this.options.emitApplied(cloudSyncAppliedEventSchema.parse({
      appliedAt: new Date().toISOString(),
      version: this.currentVersion ?? 0
    }));
  }

  private async applyConflictKeepLocal(
    resourceType: CloudSyncResourceType,
    resourceId: string,
    state: CloudSyncResourceSyncState,
    credentials: CloudSyncCredentials
  ): Promise<void> {
    const existing = this.findPendingItem(resourceType, resourceId);
    const item = existing ?? this.queuePendingItem(
      credentials.apiBaseUrl,
      credentials.workspaceName,
      resourceType,
      resourceId,
      this.hasLocalResource(resourceType, resourceId) ? "upsert" : "delete",
      state.conflictRemoteRevision ?? null
    );
    if (!item) {
      return;
    }
    item.baseRevision = state.conflictRemoteRevision ?? null;
    item.force = true;
    item.lastError = undefined;
    item.lastAttemptAt = undefined;
    this.persistPendingQueueState();

    try {
      const result = await this.executePendingItem(item, credentials);
      if (result.mutation) {
        this.removePendingItem(resourceType, resourceId);
        this.markSuccessfulMutation(result.mutation.workspaceVersion);
        if (result.clearResourceState) {
          this.removeResourceState(resourceType, resourceId);
        } else {
          this.upsertResolvedResourceState(resourceType, resourceId, result.mutation.resourceRevision);
        }
      } else if (result.clearResourceState) {
        this.removePendingItem(resourceType, resourceId);
        this.removeResourceState(resourceType, resourceId);
      }
    } catch (error) {
      const conflict = parseCloudSyncConflictError(error);
      if (conflict) {
        if (resourceType === "proxy") {
          this.discardResourceSyncState("proxy", resourceId);
          return;
        }
        this.saveConflictState(resourceType, resourceId, conflict);
        this.emitCurrentStatus();
        return;
      }
      throw error;
    }
  }

  private isConnectionProfileInScope(profile: ConnectionProfile, workspaceName: string): boolean {
    return isConnectionGroupPathInScope(profile.groupPath, workspaceName);
  }

  private shouldProcessConnectionDeletion(resourceId: string, workspaceName: string): boolean {
    return this.shouldExposeConnectionState(resourceId, workspaceName);
  }

  private shouldExposeConnectionState(
    resourceId: string,
    workspaceName: string,
    state = this.getResourceState("connection", resourceId)
  ): boolean {
    const local = this.options.listConnections().find((item) => item.id === resourceId);
    if (local) {
      return this.isConnectionProfileInScope(local, workspaceName);
    }
    if (state?.conflictRemotePayloadJson) {
      try {
        const payload = connectionPayloadSchema.parse(JSON.parse(state.conflictRemotePayloadJson));
        return isConnectionGroupPathInScope(payload.groupPath, workspaceName);
      } catch {
        return false;
      }
    }
    return this.hasTrackedRemoteConnection(resourceId);
  }

  private hasTrackedRemoteConnection(resourceId: string): boolean {
    const state = this.getResourceState("connection", resourceId);
    const pending = this.findPendingItem("connection", resourceId);
    return (
      typeof state?.serverRevision === "number" ||
      typeof state?.conflictRemoteRevision === "number" ||
      typeof pending?.baseRevision === "number"
    );
  }

  private discardResourceSyncState(resourceType: CloudSyncResourceType, resourceId: string): void {
    let changed = false;
    if (this.pendingQueueState) {
      const nextItems = this.pendingQueueState.items.filter((item) =>
        !(item.resourceType === resourceType && item.resourceId === resourceId)
      );
      if (nextItems.length !== this.pendingQueueState.items.length) {
        this.pendingQueueState.items = nextItems;
        this.persistPendingQueueState();
        changed = true;
      }
    }
    if (this.getResourceState(resourceType, resourceId)) {
      changed = true;
    }
    this.removeResourceState(resourceType, resourceId);
    if (changed) {
      this.emitCurrentStatus();
    }
  }

  private pruneDeprecatedProxySyncState(): void {
    let changed = false;
    if (this.pendingQueueState) {
      const nextItems = this.pendingQueueState.items.filter((item) => item.resourceType !== "proxy");
      if (nextItems.length !== this.pendingQueueState.items.length) {
        this.pendingQueueState.items = nextItems;
        this.persistPendingQueueState();
        changed = true;
      }
    }
    for (const state of this.listResourceStates()) {
      if (state.resourceType === "proxy") {
        this.removeResourceState("proxy", state.resourceId);
        changed = true;
      }
    }
    if (changed) {
      this.emitCurrentStatus();
    }
  }

  private hasLocalResource(resourceType: CloudSyncResourceType, resourceId: string): boolean {
    switch (resourceType) {
      case "connection":
        return this.options.listConnections().some((item) => item.id === resourceId);
      case "sshKey":
        return this.options.listSshKeys().some((item) => item.id === resourceId);
      case "proxy":
        return this.options.listProxies().some((item) => item.id === resourceId);
    }
  }

  private async deserializeConnection(
    payload: ConnectionPayload,
    workspacePassword: string
  ): Promise<CloudSyncApplyConnectionInput> {
    const password = payload.credentialCipher
      ? await this.decryptEnvelope(payload.credentialCipher, workspacePassword)
      : undefined;

    return {
      id: payload.id,
      name: payload.name,
      host: payload.host,
      port: payload.port,
      username: payload.username,
      authType: payload.authType,
      password,
      sshKeyId: payload.sshKeyId ?? undefined,
      hostFingerprint: payload.hostFingerprint ?? undefined,
      strictHostKeyChecking: payload.strictHostKeyChecking,
      proxyId: undefined,
      keepAliveEnabled: payload.keepAliveEnabled ?? undefined,
      keepAliveIntervalSec: payload.keepAliveIntervalSec ?? undefined,
      groupPath: payload.groupPath,
      tags: payload.tags,
      notes: payload.notes ?? undefined,
      favorite: payload.favorite,
      updatedAt: payload.updatedAt
    };
  }

  private async deserializeSshKey(
    payload: SshKeyPayload,
    workspacePassword: string
  ): Promise<CloudSyncApplySshKeyInput> {
    return {
      id: payload.id,
      name: payload.name,
      keyContent: await this.decryptEnvelope(payload.privateKeyCipher, workspacePassword),
      passphrase: payload.passphraseCipher
        ? await this.decryptEnvelope(payload.passphraseCipher, workspacePassword)
        : undefined,
      updatedAt: payload.updatedAt
    };
  }

  private async deserializeProxy(
    payload: ProxyPayload,
    workspacePassword: string
  ): Promise<CloudSyncApplyProxyInput> {
    return {
      id: payload.id,
      name: payload.name,
      proxyType: payload.proxyType,
      host: payload.host,
      port: payload.port,
      username: payload.username ?? undefined,
      password: payload.passwordCipher
        ? await this.decryptEnvelope(payload.passwordCipher, workspacePassword)
        : undefined,
      updatedAt: payload.updatedAt
    };
  }

  private async serializeConnection(
    profile: ConnectionProfile,
    workspacePassword: string
  ): Promise<Record<string, unknown>> {
    let credentialCipher: WorkspaceSecretEnvelope | null = null;
    if ((profile.authType === "password" || profile.authType === "interactive") && profile.credentialRef) {
      const password = await this.options.vault.readCredential(profile.credentialRef);
      if (!password) {
        throw new Error(`无法读取连接 ${profile.name} 的密码，云同步已中止。`);
      }
      credentialCipher = await encryptWorkspaceSecret(password, workspacePassword, `connection:${profile.id}`);
    }

    return {
      id: profile.id,
      name: profile.name,
      host: profile.host,
      port: profile.port,
      username: profile.username,
      authType: profile.authType,
      credentialCipher,
      sshKeyId: profile.sshKeyId ?? null,
      hostFingerprint: profile.hostFingerprint ?? null,
      strictHostKeyChecking: profile.strictHostKeyChecking,
      proxyId: null,
      keepAliveEnabled: profile.keepAliveEnabled ?? null,
      keepAliveIntervalSec: profile.keepAliveIntervalSec ?? null,
      groupPath: profile.groupPath,
      tags: profile.tags,
      notes: profile.notes ?? null,
      favorite: profile.favorite,
      updatedAt: profile.updatedAt
    };
  }

  private async serializeSshKey(
    profile: SshKeyProfile,
    workspacePassword: string
  ): Promise<Record<string, unknown>> {
    const keyContent = await this.options.vault.readCredential(profile.keyContentRef);
    if (!keyContent) {
      throw new Error(`无法读取 SSH 密钥 ${profile.name} 的私钥内容，云同步已中止。`);
    }
    const passphrase = profile.passphraseRef
      ? await this.options.vault.readCredential(profile.passphraseRef)
      : undefined;

    return {
      id: profile.id,
      name: profile.name,
      privateKeyCipher: await encryptWorkspaceSecret(keyContent, workspacePassword, `ssh-key:${profile.id}:privateKey`),
      passphraseCipher: passphrase
        ? await encryptWorkspaceSecret(passphrase, workspacePassword, `ssh-key:${profile.id}:passphrase`)
        : null,
      updatedAt: profile.updatedAt
    };
  }

  private async serializeProxy(
    profile: ProxyProfile,
    workspacePassword: string
  ): Promise<Record<string, unknown>> {
    const password = profile.credentialRef
      ? await this.options.vault.readCredential(profile.credentialRef)
      : undefined;

    return {
      id: profile.id,
      name: profile.name,
      proxyType: profile.proxyType,
      host: profile.host,
      port: profile.port,
      username: profile.username ?? null,
      passwordCipher: password
        ? await encryptWorkspaceSecret(password, workspacePassword, `proxy:${profile.id}`)
        : null,
      updatedAt: profile.updatedAt
    };
  }

  private async decryptEnvelope(envelope: WorkspaceSecretEnvelope, workspacePassword: string): Promise<string> {
    return decryptWorkspaceSecret(envelope, workspacePassword);
  }

  private async getStoredCredentials(): Promise<CloudSyncCredentials> {
    const prefs = this.options.getPreferences().cloudSync;
    if (!prefs.enabled) {
      throw new Error("云同步尚未启用。");
    }
    const apiBaseUrl = normalizeApiBaseUrl(prefs.apiBaseUrl);
    const workspaceName = prefs.workspaceName.trim();
    if (!apiBaseUrl || !workspaceName) {
      throw new Error("云同步配置不完整。");
    }

    const password = await new KeytarPasswordCache(
      this.options.keytarServiceName,
      keytarAccountForWorkspace(apiBaseUrl, workspaceName)
    ).recall();

    if (!password) {
      this.hasWorkspacePassword = false;
      throw new Error("未找到云同步 workspace 密码，请重新配置。");
    }

    this.hasWorkspacePassword = true;
    return {
      apiBaseUrl,
      workspaceName,
      workspacePassword: password,
      ignoreTlsErrors: prefs.ignoreTlsErrors
    };
  }

  private async refreshPasswordPresence(): Promise<void> {
    const prefs = this.options.getPreferences().cloudSync;
    if (!prefs.apiBaseUrl || !prefs.workspaceName) {
      this.hasWorkspacePassword = false;
      return;
    }
    const cache = new KeytarPasswordCache(
      this.options.keytarServiceName,
      keytarAccountForWorkspace(prefs.apiBaseUrl, prefs.workspaceName)
    );
    this.hasWorkspacePassword = Boolean(await cache.recall());
  }

  private async persistLastSyncAt(lastSyncAt: string): Promise<void> {
    this.options.savePreferencesPatch({
      cloudSync: {
        lastSyncAt
      }
    }, { reconfigureCloudSync: false });
  }

  private markSuccessfulMutation(version: number): void {
    this.currentVersion = version;
    const now = new Date().toISOString();
    void this.persistLastSyncAt(now);
  }

  private ensureTimer(pullIntervalSec: number): void {
    const intervalMs = pullIntervalSec * 1000;
    if (this.timer) {
      clearInterval(this.timer);
    }
    this.timer = setInterval(() => {
      void this.syncNow().catch(() => undefined);
    }, intervalMs);
  }

  private stopTimer(): void {
    if (this.timer) {
      clearInterval(this.timer);
      this.timer = undefined;
    }
  }

  private transition(state: CloudSyncStatus["state"], lastError: string | null): void {
    this.runtimeState = state;
    this.lastError = lastError;
    this.emitCurrentStatus();
  }

  private listResourceStates(): CloudSyncResourceSyncState[] {
    if (this.options.listResourceStates) {
      return this.options.listResourceStates();
    }
    return Array.from(this.resourceStateFallback.values());
  }

  private listConflictStates(): CloudSyncResourceSyncState[] {
    const workspaceName = this.options.getPreferences().cloudSync.workspaceName;
    return this.listResourceStates().filter((state) => {
      if (typeof state.conflictRemoteRevision !== "number" || state.resourceType === "proxy") {
        return false;
      }
      if (state.resourceType !== "connection") {
        return true;
      }
      return this.shouldExposeConnectionState(state.resourceId, workspaceName, state);
    });
  }

  private getResourceState(
    resourceType: CloudSyncResourceType,
    resourceId: string
  ): CloudSyncResourceSyncState | undefined {
    if (this.options.getResourceState) {
      return this.options.getResourceState(resourceType, resourceId);
    }
    return this.resourceStateFallback.get(`${resourceType}:${resourceId}`);
  }

  private saveResourceState(state: CloudSyncResourceSyncState): void {
    if (this.options.saveResourceState) {
      this.options.saveResourceState(state);
      return;
    }
    this.resourceStateFallback.set(`${state.resourceType}:${state.resourceId}`, state);
  }

  private removeResourceState(resourceType: CloudSyncResourceType, resourceId: string): void {
    if (this.options.removeResourceState) {
      this.options.removeResourceState(resourceType, resourceId);
      return;
    }
    this.resourceStateFallback.delete(`${resourceType}:${resourceId}`);
  }

  private clearAllResourceStates(): void {
    for (const state of this.listResourceStates()) {
      this.removeResourceState(state.resourceType, state.resourceId);
    }
  }

  private findPendingItem(
    resourceType: CloudSyncResourceType,
    resourceId: string
  ): CloudSyncPendingQueueItem | undefined {
    return this.pendingQueueState?.items.find((item) => item.resourceType === resourceType && item.resourceId === resourceId);
  }

  private removePendingItem(resourceType: CloudSyncResourceType, resourceId: string): void {
    if (!this.pendingQueueState) {
      return;
    }
    this.pendingQueueState.items = this.pendingQueueState.items.filter((item) =>
      !(item.resourceType === resourceType && item.resourceId === resourceId)
    );
    this.persistPendingQueueState();
    this.emitCurrentStatus();
  }

  private resolveLocalUpdatedAt(resourceType: CloudSyncResourceType, resourceId: string): string | null {
    switch (resourceType) {
      case "connection":
        return this.options.listConnections().find((item) => item.id === resourceId)?.updatedAt ?? null;
      case "sshKey":
        return this.options.listSshKeys().find((item) => item.id === resourceId)?.updatedAt ?? null;
      case "proxy":
        return null;
    }
  }

  private resolveConflictDisplayName(state: CloudSyncResourceSyncState): string {
    switch (state.resourceType) {
      case "connection": {
        const local = this.options.listConnections().find((item) => item.id === state.resourceId);
        if (local) {
          return local.name;
        }
        if (state.conflictRemotePayloadJson) {
          try {
            return connectionPayloadSchema.parse(JSON.parse(state.conflictRemotePayloadJson)).name;
          } catch {
            return state.resourceId;
          }
        }
        return state.resourceId;
      }
      case "sshKey": {
        const local = this.options.listSshKeys().find((item) => item.id === state.resourceId);
        if (local) {
          return local.name;
        }
        if (state.conflictRemotePayloadJson) {
          try {
            return sshKeyPayloadSchema.parse(JSON.parse(state.conflictRemotePayloadJson)).name;
          } catch {
            return state.resourceId;
          }
        }
        return state.resourceId;
      }
      case "proxy": {
        return state.resourceId;
      }
    }
  }

  private upsertResolvedResourceState(
    resourceType: CloudSyncResourceType,
    resourceId: string,
    serverRevision: number
  ): void {
    this.saveResourceState({
      resourceType,
      resourceId,
      serverRevision,
      conflictRemoteRevision: undefined,
      conflictRemotePayloadJson: undefined,
      conflictRemoteUpdatedAt: undefined,
      conflictRemoteDeleted: false,
      conflictDetectedAt: undefined
    });
  }

  private saveConflictState(
    resourceType: CloudSyncResourceType,
    resourceId: string,
    conflict: ConflictResponse["conflict"]
  ): void {
    this.saveResourceState({
      resourceType,
      resourceId,
      serverRevision: this.getResourceState(resourceType, resourceId)?.serverRevision,
      conflictRemoteRevision: conflict.serverRevision,
      conflictRemotePayloadJson: conflict.serverPayload === undefined ? undefined : JSON.stringify(conflict.serverPayload),
      conflictRemoteUpdatedAt: conflict.serverUpdatedAt ?? undefined,
      conflictRemoteDeleted: conflict.serverDeleted,
      conflictDetectedAt: new Date().toISOString()
    });
  }

  private emitCurrentStatus(): CloudSyncStatus {
    const prefs = this.options.getPreferences().cloudSync;
    this.ensurePendingQueueState(prefs.apiBaseUrl, prefs.workspaceName);
    const status = cloudSyncStatusSchema.parse({
      enabled: prefs.enabled,
      configured: prefs.apiBaseUrl.trim().length > 0 && prefs.workspaceName.trim().length > 0,
      state: prefs.enabled ? this.runtimeState : "disabled",
      apiBaseUrl: prefs.apiBaseUrl,
      workspaceName: prefs.workspaceName,
      pullIntervalSec: prefs.pullIntervalSec,
      ignoreTlsErrors: prefs.ignoreTlsErrors,
      lastSyncAt: prefs.lastSyncAt,
      lastError: this.lastError,
      keytarAvailable: new KeytarPasswordCache(this.options.keytarServiceName, "cloud-sync:availability").isAvailable(),
      hasWorkspacePassword: this.hasWorkspacePassword,
      currentVersion: this.currentVersion,
      pendingCount: this.pendingQueueState?.items.length ?? 0,
      conflictCount: this.listConflictStates().length
    });
    this.options.emitStatus(status);
    return status;
  }

  private ensurePendingQueueState(apiBaseUrl: string, workspaceName: string): CloudSyncPendingQueueState | undefined {
    const normalizedApiBaseUrl = normalizeApiBaseUrl(apiBaseUrl);
    const normalizedWorkspaceName = workspaceName.trim();
    if (!normalizedApiBaseUrl || !normalizedWorkspaceName) {
      this.pendingQueueState = undefined;
      this.pendingQueueScopeKey = null;
      return undefined;
    }

    const scope = queueScopeKey(normalizedApiBaseUrl, normalizedWorkspaceName);
    if (this.pendingQueueScopeKey === scope && this.pendingQueueState) {
      return this.pendingQueueState;
    }

    const parsed = pendingQueueStateSchema.safeParse(this.options.loadPendingQueueState?.());
    if (parsed.success) {
      const loadedScope = queueScopeKey(parsed.data.apiBaseUrl, parsed.data.workspaceName);
      if (loadedScope === scope) {
        const nextSeq = Math.max(
          parsed.data.nextSeq,
          ...parsed.data.items.map((item) => item.seq + 1),
          1
        );
        this.pendingQueueState = {
          apiBaseUrl: normalizeApiBaseUrl(parsed.data.apiBaseUrl),
          workspaceName: parsed.data.workspaceName.trim(),
          nextSeq,
          items: [...parsed.data.items].sort(comparePendingItems)
        };
        this.pendingQueueScopeKey = scope;
        return this.pendingQueueState;
      }
    }

    this.pendingQueueState = {
      apiBaseUrl: normalizedApiBaseUrl,
      workspaceName: normalizedWorkspaceName,
      nextSeq: 1,
      items: []
    };
    this.pendingQueueScopeKey = scope;
    return this.pendingQueueState;
  }

  private queuePendingItem(
    apiBaseUrl: string,
    workspaceName: string,
    resourceType: CloudSyncResourceType,
    resourceId: string,
    action: CloudSyncPendingAction,
    baseRevisionOverride?: number | null
  ): CloudSyncPendingQueueItem | undefined {
    const queueState = this.ensurePendingQueueState(apiBaseUrl, workspaceName);
    if (!queueState) {
      return undefined;
    }

    const now = new Date().toISOString();
    const existing = queueState.items.find((item) => item.resourceType === resourceType && item.resourceId === resourceId);
    const initialBaseRevision = baseRevisionOverride ?? this.getResourceState(resourceType, resourceId)?.serverRevision ?? null;
    const nextAction = existing
      ? action === "delete"
        ? "delete"
      : "upsert"
      : action;

    if (existing) {
      existing.action = nextAction;
      existing.seq = queueState.nextSeq++;
      existing.queuedAt = now;
      existing.force = false;
      existing.baseRevision = existing.baseRevision ?? initialBaseRevision;
      existing.lastAttemptAt = undefined;
      existing.lastError = undefined;
    } else {
      queueState.items.push({
        resourceType,
        resourceId,
        action: nextAction,
        baseRevision: initialBaseRevision,
        force: false,
        seq: queueState.nextSeq++,
        queuedAt: now
      });
    }

    queueState.items.sort(comparePendingItems);
    this.persistPendingQueueState();
    this.emitCurrentStatus();
    return queueState.items.find((item) => item.resourceType === resourceType && item.resourceId === resourceId);
  }

  private persistPendingQueueState(): void {
    if (!this.options.savePendingQueueState) {
      return;
    }

    const queueState = this.pendingQueueState;
    if (!queueState || queueState.items.length === 0) {
      this.options.savePendingQueueState(undefined);
      return;
    }

    this.options.savePendingQueueState({
      apiBaseUrl: queueState.apiBaseUrl,
      workspaceName: queueState.workspaceName,
      nextSeq: queueState.nextSeq,
      items: [...queueState.items].sort(comparePendingItems)
    });
  }

  private clearPendingQueueState(): void {
    this.pendingQueueState = undefined;
    this.pendingQueueScopeKey = null;
    this.options.savePendingQueueState?.(undefined);
  }

  private async withQueue(run: () => Promise<void>): Promise<void> {
    const next = this.queue.then(run, run);
    this.queue = next.catch(() => undefined);
    await next;
  }

  private async postJson<T>(
    credentials: CloudSyncCredentials,
    pathname: string,
    payload: unknown,
    schema: z.ZodType<T>
  ): Promise<T> {
    const executor = this.options.requestJson ?? defaultRequestJson;
    return executor(
      {
        apiBaseUrl: credentials.apiBaseUrl,
        workspaceName: credentials.workspaceName,
        workspacePassword: credentials.workspacePassword,
        pathname,
        payload,
        ignoreTlsErrors: credentials.ignoreTlsErrors
      },
      schema
    );
  }
}

export {
  CLOUD_SYNC_PENDING_QUEUE_SETTING_KEY
};
