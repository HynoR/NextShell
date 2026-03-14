/**
 * CloudSyncRuntime — per-workspace cloud sync runtime.
 *
 * Each instance manages a single workspace: pull timer, pending queue,
 * revision tracking, conflict state, retry/backoff.
 *
 * Only touches resources where:
 *   origin_kind = "cloud"
 *   origin_workspace_id = this.workspaceId
 *   origin_scope_key = this.scopeKey
 */

import { randomUUID } from "node:crypto";
import type {
  CloudSyncPendingOp,
  CloudSyncResourceStateV2,
  CloudSyncWorkspaceProfile,
  ConnectionProfile,
  RecycleBinEntry,
  SshKeyProfile,
} from "@nextshell/core";
import { buildResourceId, buildScopeKey, LOCAL_DEFAULT_SCOPE_KEY } from "@nextshell/core";
import {
  CloudSyncApiV2Client,
  type CloudSyncApiCredentials,
  type PullResponse,
  type PushConflictItem,
  type PushOperation,
  type PushResponse,
  type ServerSnapshotItem,
} from "./cloud-sync-api-v2";

export type RuntimeState = "idle" | "syncing" | "error" | "disabled";

export interface CloudSyncRuntimeStatus {
  workspaceId: string;
  state: RuntimeState;
  lastSyncAt: string | null;
  lastError: string | null;
  pendingCount: number;
  conflictCount: number;
  currentVersion: number | null;
}

export interface CloudSyncRuntimeDeps {
  /** Repository access */
  listConnections: () => ConnectionProfile[];
  listSshKeys: () => SshKeyProfile[];
  saveConnection: (conn: ConnectionProfile) => void;
  saveSshKey: (key: SshKeyProfile) => void;
  removeConnection: (id: string) => void;
  removeSshKey: (id: string) => void;

  /** Credential access */
  readCredential: (ref: string) => Promise<string | undefined>;

  /** Pending ops storage */
  listPendingOps: (workspaceId: string) => CloudSyncPendingOp[];
  savePendingOp: (op: CloudSyncPendingOp) => number;
  upsertPendingOp: (op: CloudSyncPendingOp) => number;
  updatePendingOp: (op: CloudSyncPendingOp) => void;
  removePendingOp: (id: number) => void;
  clearPendingOps: (workspaceId: string) => void;

  /** Resource state storage */
  listResourceStates: (workspaceId: string) => CloudSyncResourceStateV2[];
  getResourceState: (workspaceId: string, resourceType: string, resourceId: string) => CloudSyncResourceStateV2 | undefined;
  saveResourceState: (state: CloudSyncResourceStateV2) => void;
  removeResourceState: (workspaceId: string, resourceType: string, resourceId: string) => void;
  clearResourceStates: (workspaceId: string) => void;

  /** Recycle bin */
  saveRecycleBinEntry: (entry: RecycleBinEntry) => void;

  /** Workspace config updates */
  saveWorkspace: (ws: CloudSyncWorkspaceProfile) => void;

  /** Version persistence */
  getRuntimeCurrentVersion: (workspaceId: string) => number | null;
  saveRuntimeCurrentVersion: (workspaceId: string, currentVersion: number) => void;

  /** Credential access */
  getWorkspacePassword: (workspaceId: string) => Promise<string | undefined>;

  /** Event emitters */
  emitStatus: (status: CloudSyncRuntimeStatus) => void;
  emitApplied: (workspaceId: string) => void;
}

export class CloudSyncRuntime {
  readonly workspaceId: string;
  readonly scopeKey: string;

  private static readonly MAX_PUSH_BATCH_SIZE = 50;
  private static readonly SYNC_NOW_MIN_INTERVAL_MS = 10_000;

  private workspace: CloudSyncWorkspaceProfile;
  private state: RuntimeState = "idle";
  private lastError: string | null = null;
  private currentVersion: number | null = null;
  private pullTimer: ReturnType<typeof setTimeout> | undefined;
  private disposed = false;
  private syncInProgress = false;
  private consecutiveErrors = 0;
  private lastSyncNowAt = 0;

  private readonly api = new CloudSyncApiV2Client();

  constructor(
    workspace: CloudSyncWorkspaceProfile,
    private readonly deps: CloudSyncRuntimeDeps,
  ) {
    this.workspace = workspace;
    this.workspaceId = workspace.id;
    this.scopeKey = buildScopeKey({
      kind: "cloud",
      apiBaseUrl: workspace.apiBaseUrl,
      workspaceName: workspace.workspaceName,
    });
  }

  /** Start the runtime: load persisted version, schedule periodic pull */
  start(): void {
    if (this.disposed) return;
    this.currentVersion = this.deps.getRuntimeCurrentVersion(this.workspaceId);
    this.schedulePull();
  }

  /** Stop the runtime: cancel timer */
  stop(): void {
    if (this.pullTimer) {
      clearTimeout(this.pullTimer);
      this.pullTimer = undefined;
    }
  }

  dispose(): void {
    this.disposed = true;
    this.stop();
  }

  updateConfig(ws: CloudSyncWorkspaceProfile): void {
    this.workspace = ws;
    // Reschedule pull with new interval
    this.stop();
    if (ws.enabled && !this.disposed) {
      this.schedulePull();
    }
  }

  getStatus(): CloudSyncRuntimeStatus {
    const pendingOps = this.deps.listPendingOps(this.workspaceId);
    const resourceStates = this.deps.listResourceStates(this.workspaceId);
    const conflictCount = resourceStates.filter((s) => s.conflictRemoteRevision != null).length;

    return {
      workspaceId: this.workspaceId,
      state: this.state,
      lastSyncAt: this.workspace.lastSyncAt,
      lastError: this.lastError,
      pendingCount: pendingOps.length,
      conflictCount,
      currentVersion: this.currentVersion,
    };
  }

  /** Manually trigger a sync cycle (throttled to 10s minimum interval) */
  async syncNow(): Promise<void> {
    if (this.disposed || this.syncInProgress) return;
    const now = Date.now();
    if (now - this.lastSyncNowAt < CloudSyncRuntime.SYNC_NOW_MIN_INTERVAL_MS) return;
    this.lastSyncNowAt = now;
    await this.runSyncCycle();
  }

  /** Enqueue a pending operation for this workspace */
  enqueuePendingOp(
    resourceType: "server" | "sshKey",
    resourceId: string,
    action: "upsert" | "delete",
    baseRevision?: number | null,
  ): void {
    // Use provided baseRevision, or look up from resource state
    const resolvedBaseRevision = baseRevision ??
      this.deps.getResourceState(this.workspaceId, resourceType, resourceId)?.serverRevision ?? null;

    const op: CloudSyncPendingOp = {
      workspaceId: this.workspaceId,
      resourceType,
      resourceId,
      action,
      baseRevision: resolvedBaseRevision,
      force: false,
      queuedAt: new Date().toISOString(),
    };
    this.deps.upsertPendingOp(op);
  }

  /** Resolve a conflict for a specific resource */
  async resolveConflict(
    resourceType: "server" | "sshKey",
    resourceId: string,
    strategy: "keep_local" | "accept_remote",
  ): Promise<void> {
    const state = this.deps.getResourceState(this.workspaceId, resourceType, resourceId);
    if (!state || state.conflictRemoteRevision == null) return;

    if (strategy === "accept_remote") {
      // Put current local version into recycle bin, apply remote
      this.saveCurrentToRecycleBin(resourceType, resourceId, "conflict_accept_remote");

      if (state.conflictRemoteDeleted) {
        // Remote deleted: remove local resource
        if (resourceType === "server") this.deps.removeConnection(resourceId);
        else this.deps.removeSshKey(resourceId);
      } else if (state.conflictRemotePayloadJson) {
        // Apply remote payload
        this.applyRemotePayload(resourceType, resourceId, state.conflictRemotePayloadJson);
      }

      // Clear conflict, update revision
      this.deps.saveResourceState({
        ...state,
        serverRevision: state.conflictRemoteRevision,
        conflictRemoteRevision: undefined,
        conflictRemotePayloadJson: undefined,
        conflictRemoteUpdatedAt: undefined,
        conflictRemoteDeleted: false,
        conflictDetectedAt: undefined,
      });
    } else {
      // keep_local: put remote conflict version into recycle bin, push local
      if (state.conflictRemotePayloadJson) {
        const entry: RecycleBinEntry = {
          id: randomUUID(),
          resourceType,
          displayName: `[云端冲突版本] ${resourceId}`,
          originalResourceId: resourceId,
          originalScopeKey: this.scopeKey,
          reason: "conflict_keep_local",
          snapshotJson: state.conflictRemotePayloadJson,
          createdAt: new Date().toISOString(),
        };
        this.deps.saveRecycleBinEntry(entry);
      }

      // Clear conflict state
      this.deps.saveResourceState({
        ...state,
        conflictRemoteRevision: undefined,
        conflictRemotePayloadJson: undefined,
        conflictRemoteUpdatedAt: undefined,
        conflictRemoteDeleted: false,
        conflictDetectedAt: undefined,
      });

      // Re-queue for push with force
      this.enqueuePendingOp(resourceType, resourceId, "upsert", state.serverRevision ?? null);
    }

    this.deps.emitStatus(this.getStatus());
    this.deps.emitApplied(this.workspaceId);
  }

  // ── Private ───────────────────────────────────────────────────────────────

  private schedulePull(): void {
    const baseMs = (this.workspace.pullIntervalSec ?? 60) * 1000;
    const backoffMs = Math.min(baseMs * Math.pow(2, this.consecutiveErrors), 15 * 60 * 1000);
    this.pullTimer = setTimeout(() => {
      if (!this.syncInProgress && !this.disposed) {
        this.runSyncCycle()
          .catch(() => {})
          .finally(() => {
            if (!this.disposed) this.schedulePull();
          });
      } else if (!this.disposed) {
        this.schedulePull();
      }
    }, backoffMs);
  }

  private async runSyncCycle(): Promise<void> {
    if (this.syncInProgress || this.disposed) return;
    this.syncInProgress = true;
    this.state = "syncing";
    this.deps.emitStatus(this.getStatus());

    try {
      const password = await this.deps.getWorkspacePassword(this.workspaceId);
      if (!password) {
        throw new Error("Workspace password not available");
      }

      const creds: CloudSyncApiCredentials = {
        apiBaseUrl: this.workspace.apiBaseUrl,
        workspaceName: this.workspace.workspaceName,
        workspacePassword: password,
        ignoreTlsErrors: this.workspace.ignoreTlsErrors,
      };

      // 1. Flush pending queue (push local changes)
      await this.flushPendingQueue(creds);

      // 2. Pull remote changes
      await this.runPull(creds);

      // Update last sync timestamp
      this.workspace = { ...this.workspace, lastSyncAt: new Date().toISOString(), lastError: null };
      this.deps.saveWorkspace(this.workspace);

      this.state = "idle";
      this.lastError = null;
      this.consecutiveErrors = 0;

      // Persist version for restart recovery
      if (this.currentVersion != null) {
        this.deps.saveRuntimeCurrentVersion(this.workspaceId, this.currentVersion);
      }
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : String(err);
      this.state = "error";
      this.lastError = errorMsg;
      this.consecutiveErrors++;
      this.workspace = { ...this.workspace, lastError: errorMsg };
      this.deps.saveWorkspace(this.workspace);
    } finally {
      this.syncInProgress = false;
      this.deps.emitStatus(this.getStatus());
    }
  }

  private async flushPendingQueue(creds: CloudSyncApiCredentials): Promise<void> {
    const allPendingOps = this.deps.listPendingOps(this.workspaceId);
    if (allPendingOps.length === 0) return;

    for (let i = 0; i < allPendingOps.length; i += CloudSyncRuntime.MAX_PUSH_BATCH_SIZE) {
      const batch = allPendingOps.slice(i, i + CloudSyncRuntime.MAX_PUSH_BATCH_SIZE);

      // Build push operations for this batch
      const operations: PushOperation[] = [];
      const validOps: CloudSyncPendingOp[] = [];
      for (const op of batch) {
        const pushType = op.action === "upsert"
          ? (op.resourceType === "server" ? "upsertServer" : "upsertSshKey")
          : (op.resourceType === "server" ? "deleteServer" : "deleteSshKey");

        let payload: Record<string, unknown> | undefined;
        if (op.action === "upsert") {
          payload = await this.serializeResourceForPush(op.resourceType, op.resourceId);
          if (!payload) {
            // Resource no longer exists locally, skip this op
            this.deps.removePendingOp(op.id!);
            continue;
          }
        }

        operations.push({
          type: pushType,
          uuid: op.resourceId,
          baseRevision: op.baseRevision,
          payload,
        });
        validOps.push(op);
      }

      if (operations.length === 0) continue;

      const response: PushResponse = await this.api.push(
        creds,
        this.currentVersion ?? 0,
        operations,
      );

      if (response.ok) {
        // Success: update revisions and remove pending ops
        this.currentVersion = response.workspaceVersion;
        for (const result of response.results) {
          const resourceType = result.type.includes("Server") ? "server" : "sshKey";
          const state = this.deps.getResourceState(this.workspaceId, resourceType, result.uuid);
          this.deps.saveResourceState({
            workspaceId: this.workspaceId,
            resourceType: resourceType as "server" | "sshKey",
            resourceId: result.uuid,
            serverRevision: result.revision,
            conflictRemoteRevision: state?.conflictRemoteRevision,
            conflictRemotePayloadJson: state?.conflictRemotePayloadJson,
            conflictRemoteUpdatedAt: state?.conflictRemoteUpdatedAt,
            conflictRemoteDeleted: state?.conflictRemoteDeleted ?? false,
            conflictDetectedAt: state?.conflictDetectedAt,
          });
        }
        // Remove all flushed pending ops in this batch
        for (const op of validOps) {
          if (op.id != null) this.deps.removePendingOp(op.id);
        }
      } else {
        // Conflict: record conflict state
        this.currentVersion = response.workspaceVersion;
        const conflictResourceIds = new Set(
          response.conflicts.map((c) => c.uuid)
        );
        for (const conflict of response.conflicts) {
          const resourceType = conflict.resourceType;
          this.deps.saveResourceState({
            workspaceId: this.workspaceId,
            resourceType,
            resourceId: conflict.uuid,
            serverRevision: conflict.serverRevision,
            conflictRemoteRevision: conflict.serverRevision,
            conflictRemotePayloadJson: conflict.serverPayload ? JSON.stringify(conflict.serverPayload) : undefined,
            conflictRemoteUpdatedAt: conflict.serverUpdatedAt ?? undefined,
            conflictRemoteDeleted: conflict.serverDeleted,
            conflictDetectedAt: new Date().toISOString(),
          });
        }
        // Remove non-conflicting ops (they were accepted); keep only conflicting ones
        for (const op of validOps) {
          if (op.id != null && !conflictResourceIds.has(op.resourceId)) {
            this.deps.removePendingOp(op.id);
          }
        }
        // Stop processing further batches on conflict — wait for next sync cycle
        break;
      }
    }
  }

  private async runPull(creds: CloudSyncApiCredentials): Promise<void> {
    const response: PullResponse = await this.api.pull(creds, this.currentVersion ?? 0);
    this.currentVersion = response.workspaceVersion;

    // Batch-get pending ops once before iterating
    const allPendingOps = this.deps.listPendingOps(this.workspaceId);

    // Apply servers
    for (const item of response.servers) {
      this.applyPulledResource("server", item, allPendingOps);
    }

    // Apply SSH keys
    for (const item of response.sshKeys) {
      this.applyPulledResource("sshKey", item, allPendingOps);
    }

    // Apply deletions
    for (const deleted of response.deleted) {
      this.applyPulledDeletion(deleted.resourceType, deleted.uuid, deleted.revision);
    }

    this.deps.emitApplied(this.workspaceId);
  }

  private applyPulledResource(resourceType: "server" | "sshKey", item: ServerSnapshotItem, pendingOps: CloudSyncPendingOp[]): void {
    const existingState = this.deps.getResourceState(this.workspaceId, resourceType, item.uuid);

    // Check for local pending changes → potential conflict
    const hasPending = pendingOps.some(
      (op) => op.resourceType === resourceType && op.resourceId === item.uuid
    );

    if (hasPending && existingState?.serverRevision != null && item.revision > existingState.serverRevision) {
      // Conflict: remote has newer version while we have local pending changes
      this.deps.saveResourceState({
        workspaceId: this.workspaceId,
        resourceType,
        resourceId: item.uuid,
        serverRevision: existingState.serverRevision,
        conflictRemoteRevision: item.revision,
        conflictRemotePayloadJson: JSON.stringify(item.payload),
        conflictRemoteUpdatedAt: item.updatedAt,
        conflictRemoteDeleted: false,
        conflictDetectedAt: new Date().toISOString(),
      });
      return;
    }

    // No conflict: apply directly
    this.applyRemotePayload(resourceType, item.uuid, JSON.stringify(item.payload));

    // Update resource state
    this.deps.saveResourceState({
      workspaceId: this.workspaceId,
      resourceType,
      resourceId: item.uuid,
      serverRevision: item.revision,
      conflictRemoteDeleted: false,
    });
  }

  private applyPulledDeletion(resourceType: "server" | "sshKey", uuid: string, revision: number): void {
    // Move to recycle bin before deleting
    this.saveCurrentToRecycleBin(resourceType, uuid, "delete");

    if (resourceType === "server") {
      this.deps.removeConnection(uuid);
    } else {
      this.deps.removeSshKey(uuid);
    }

    // Update resource state with tombstone
    this.deps.saveResourceState({
      workspaceId: this.workspaceId,
      resourceType,
      resourceId: uuid,
      serverRevision: revision,
      conflictRemoteDeleted: false,
    });
  }

  private applyRemotePayload(resourceType: "server" | "sshKey", uuid: string, payloadJson: string): void {
    try {
      const payload = JSON.parse(payloadJson) as Record<string, unknown>;

      if (resourceType === "server") {
        const now = new Date().toISOString();
        const conn: ConnectionProfile = {
          id: uuid,
          name: String(payload.name ?? ""),
          host: String(payload.host ?? ""),
          port: Number(payload.port ?? 22),
          username: String(payload.username ?? "root"),
          authType: (payload.authType as ConnectionProfile["authType"]) ?? "password",
          strictHostKeyChecking: Boolean(payload.strictHostKeyChecking),
          groupPath: String(payload.groupPath ?? `/workspace/${this.workspace.workspaceName}`),
          tags: Array.isArray(payload.tags) ? payload.tags.filter((t): t is string => typeof t === "string") : [],
          notes: typeof payload.notes === "string" ? payload.notes : undefined,
          favorite: Boolean(payload.favorite),
          monitorSession: false,
          terminalEncoding: "utf-8",
          backspaceMode: "ascii-backspace",
          deleteMode: "vt220-delete",
          createdAt: typeof payload.createdAt === "string" ? payload.createdAt : now,
          updatedAt: typeof payload.updatedAt === "string" ? payload.updatedAt : now,
          sshKeyId: typeof payload.sshKeyUuid === "string" ? payload.sshKeyUuid : undefined,
          // Origin fields
          resourceId: buildResourceId(this.scopeKey, uuid),
          uuidInScope: uuid,
          originKind: "cloud",
          originScopeKey: this.scopeKey,
          originWorkspaceId: this.workspaceId,
        };
        this.deps.saveConnection(conn);
      } else {
        const now = new Date().toISOString();
        const key: SshKeyProfile = {
          id: uuid,
          name: String(payload.name ?? ""),
          keyContentRef: `secret://sshkey-${uuid}`,
          passphraseRef: payload.passphraseCipher ? `secret://sshkey-${uuid}-pass` : undefined,
          createdAt: typeof payload.createdAt === "string" ? payload.createdAt : now,
          updatedAt: typeof payload.updatedAt === "string" ? payload.updatedAt : now,
          // Origin fields
          resourceId: buildResourceId(this.scopeKey, uuid),
          uuidInScope: uuid,
          originKind: "cloud",
          originScopeKey: this.scopeKey,
          originWorkspaceId: this.workspaceId,
        };
        this.deps.saveSshKey(key);
      }
    } catch {
      // Invalid payload, skip
    }
  }

  private saveCurrentToRecycleBin(
    resourceType: "server" | "sshKey",
    resourceId: string,
    reason: RecycleBinEntry["reason"],
  ): void {
    try {
      let snapshot: string | undefined;
      let displayName = resourceId;

      if (resourceType === "server") {
        const conns = this.deps.listConnections();
        const conn = conns.find((c) => c.id === resourceId);
        if (conn) {
          snapshot = JSON.stringify(conn);
          displayName = conn.name || conn.host;
        }
      } else {
        const keys = this.deps.listSshKeys();
        const key = keys.find((k) => k.id === resourceId);
        if (key) {
          snapshot = JSON.stringify(key);
          displayName = key.name;
        }
      }

      if (!snapshot) return; // Resource not found locally, nothing to recycle

      const entry: RecycleBinEntry = {
        id: randomUUID(),
        resourceType,
        displayName,
        originalResourceId: buildResourceId(this.scopeKey, resourceId),
        originalScopeKey: this.scopeKey,
        reason,
        snapshotJson: snapshot,
        createdAt: new Date().toISOString(),
      };
      this.deps.saveRecycleBinEntry(entry);
    } catch {
      // Best effort: don't block sync if recycle bin fails
    }
  }

  private async serializeResourceForPush(
    resourceType: "server" | "sshKey",
    resourceId: string,
  ): Promise<Record<string, unknown> | undefined> {
    if (resourceType === "server") {
      const conns = this.deps.listConnections();
      const conn = conns.find((c) => c.id === resourceId);
      if (!conn) return undefined;
      return {
        name: conn.name,
        host: conn.host,
        port: conn.port,
        username: conn.username,
        authType: conn.authType,
        sshKeyUuid: conn.sshKeyId,
        groupPath: conn.groupPath,
        tags: conn.tags,
        notes: conn.notes,
        favorite: conn.favorite,
        strictHostKeyChecking: conn.strictHostKeyChecking,
        updatedAt: conn.updatedAt,
      };
    } else {
      const keys = this.deps.listSshKeys();
      const key = keys.find((k) => k.id === resourceId);
      if (!key) return undefined;

      // Include encrypted key content for server-side storage
      let keyCipher: string | undefined;
      let passphraseCipher: string | undefined;
      if (key.keyContentRef) {
        try { keyCipher = await this.deps.readCredential(key.keyContentRef); } catch { /* skip */ }
      }
      if (key.passphraseRef) {
        try { passphraseCipher = await this.deps.readCredential(key.passphraseRef); } catch { /* skip */ }
      }

      return {
        name: key.name,
        updatedAt: key.updatedAt,
        keyCipher,
        passphraseCipher,
      };
    }
  }
}
