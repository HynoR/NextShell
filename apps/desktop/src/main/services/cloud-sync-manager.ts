/**
 * CloudSyncManager — orchestrates multiple CloudSyncRuntime instances.
 *
 * Responsibilities:
 * - Manages workspace registry (CRUD for CloudSyncWorkspaceProfile)
 * - Starts/stops per-workspace runtimes
 * - Forwards push-on-mutation events from ConnectionService to the correct runtime
 * - Exposes aggregated status & conflict list
 * - Provides the same public surface used by IPC handlers
 */

import type {
  CloudSyncPendingOp,
  CloudSyncResourceStateV2,
  CloudSyncWorkspaceProfile,
  ConnectionProfile,
  RecycleBinEntry,
  SshKeyProfile,
} from "@nextshell/core";
import {
  CloudSyncRuntime,
  type CloudSyncRuntimeDeps,
  type CloudSyncRuntimeStatus,
} from "./cloud-sync-runtime";

// ── Public types used by IPC ────────────────────────────────────────────────

export interface CloudSyncManagerStatus {
  workspaces: CloudSyncRuntimeStatus[];
}

export interface CloudSyncWorkspaceInput {
  id?: string;
  apiBaseUrl: string;
  workspaceName: string;
  displayName?: string;
  workspacePassword?: string;
  pullIntervalSec?: number;
  ignoreTlsErrors?: boolean;
  enabled?: boolean;
}

export interface CloudSyncConflictItemV2 {
  workspaceId: string;
  workspaceName: string;
  resourceType: "server" | "sshKey";
  resourceId: string;
  displayName: string;
  serverRevision: number;
  conflictRemoteRevision: number;
  conflictRemotePayloadJson: string | undefined;
  conflictRemoteUpdatedAt: string | undefined;
  conflictRemoteDeleted: boolean;
  conflictDetectedAt: string;
}

// ── Dependency contract ─────────────────────────────────────────────────────

export interface CloudSyncManagerDeps {
  /** Connection repository */
  listConnections: () => ConnectionProfile[];
  saveConnection: (conn: ConnectionProfile) => void;
  removeConnection: (id: string) => void;

  /** SSH key repository */
  listSshKeys: () => SshKeyProfile[];
  saveSshKey: (key: SshKeyProfile) => void;
  removeSshKey: (id: string) => void;

  /** Credential access */
  readCredential: (ref: string) => Promise<string | undefined>;

  /** Workspace config persistence */
  listWorkspaces: () => CloudSyncWorkspaceProfile[];
  saveWorkspace: (ws: CloudSyncWorkspaceProfile) => void;
  removeWorkspace: (id: string) => void;

  /** Pending ops persistence */
  listPendingOps: (workspaceId: string) => CloudSyncPendingOp[];
  savePendingOp: (op: CloudSyncPendingOp) => number;
  upsertPendingOp: (op: CloudSyncPendingOp) => number;
  updatePendingOp: (op: CloudSyncPendingOp) => void;
  removePendingOp: (id: number) => void;
  clearPendingOps: (workspaceId: string) => void;

  /** Resource state persistence */
  listResourceStates: (workspaceId: string) => CloudSyncResourceStateV2[];
  getResourceState: (workspaceId: string, resourceType: string, resourceId: string) => CloudSyncResourceStateV2 | undefined;
  saveResourceState: (state: CloudSyncResourceStateV2) => void;
  removeResourceState: (workspaceId: string, resourceType: string, resourceId: string) => void;
  clearResourceStates: (workspaceId: string) => void;

  /** Recycle bin */
  saveRecycleBinEntry: (entry: RecycleBinEntry) => void;
  listRecycleBinEntries: () => RecycleBinEntry[];
  removeRecycleBinEntry: (id: string) => void;

  /** Credential management */
  storeWorkspacePassword: (workspaceId: string, password: string) => Promise<void>;
  getWorkspacePassword: (workspaceId: string) => Promise<string | undefined>;
  deleteWorkspacePassword: (workspaceId: string) => Promise<void>;

  /** Version persistence */
  getRuntimeCurrentVersion: (workspaceId: string) => number | null;
  saveRuntimeCurrentVersion: (workspaceId: string, currentVersion: number) => void;

  /** Event broadcast */
  broadcastStatus: (status: CloudSyncManagerStatus) => void;
  broadcastApplied: (workspaceId: string) => void;
}

// ── Manager implementation ──────────────────────────────────────────────────

export class CloudSyncManager {
  private readonly runtimes = new Map<string, CloudSyncRuntime>();
  private disposed = false;

  constructor(private readonly deps: CloudSyncManagerDeps) {}

  // ── Lifecycle ─────────────────────────────────────────────────────────

  /** Initialize all enabled workspaces from the database */
  initialize(): void {
    const workspaces = this.deps.listWorkspaces();
    for (const ws of workspaces) {
      if (ws.enabled) {
        this.startRuntime(ws);
      }
    }
  }

  dispose(): void {
    this.disposed = true;
    for (const runtime of this.runtimes.values()) {
      runtime.dispose();
    }
    this.runtimes.clear();
  }

  // ── Workspace Registry ────────────────────────────────────────────────

  listWorkspaces(): CloudSyncWorkspaceProfile[] {
    return this.deps.listWorkspaces();
  }

  async addWorkspace(input: CloudSyncWorkspaceInput): Promise<CloudSyncWorkspaceProfile> {
    const id = input.id ?? crypto.randomUUID();
    const now = new Date().toISOString();

    const ws: CloudSyncWorkspaceProfile = {
      id,
      apiBaseUrl: input.apiBaseUrl.replace(/\/+$/, ""),
      workspaceName: input.workspaceName,
      displayName: input.displayName ?? input.workspaceName,
      pullIntervalSec: input.pullIntervalSec ?? 300,
      ignoreTlsErrors: input.ignoreTlsErrors ?? false,
      enabled: input.enabled ?? true,
      createdAt: now,
      updatedAt: now,
      lastSyncAt: null,
      lastError: null,
    };

    // Store password securely
    if (!input.workspacePassword) {
      throw new Error("工作区密码不能为空");
    }
    await this.deps.storeWorkspacePassword(id, input.workspacePassword);

    // Persist workspace config
    this.deps.saveWorkspace(ws);

    // Start runtime if enabled
    if (ws.enabled) {
      this.startRuntime(ws);
    }

    this.broadcastManagerStatus();
    return ws;
  }

  async updateWorkspace(input: CloudSyncWorkspaceInput & { id: string }): Promise<CloudSyncWorkspaceProfile> {
    const existing = this.deps.listWorkspaces().find((w) => w.id === input.id);
    if (!existing) throw new Error(`Workspace not found: ${input.id}`);

    const ws: CloudSyncWorkspaceProfile = {
      ...existing,
      apiBaseUrl: input.apiBaseUrl.replace(/\/+$/, ""),
      workspaceName: input.workspaceName,
      displayName: input.displayName ?? existing.displayName,
      pullIntervalSec: input.pullIntervalSec ?? existing.pullIntervalSec,
      ignoreTlsErrors: input.ignoreTlsErrors ?? existing.ignoreTlsErrors,
      enabled: input.enabled ?? existing.enabled,
      updatedAt: new Date().toISOString(),
    };

    // Update password if provided
    if (input.workspacePassword) {
      await this.deps.storeWorkspacePassword(input.id, input.workspacePassword);
    }

    this.deps.saveWorkspace(ws);

    // Update or start/stop runtime
    const runtime = this.runtimes.get(input.id);
    if (ws.enabled) {
      if (runtime) {
        runtime.updateConfig(ws);
      } else {
        this.startRuntime(ws);
      }
    } else {
      if (runtime) {
        runtime.dispose();
        this.runtimes.delete(input.id);
      }
    }

    this.broadcastManagerStatus();
    return ws;
  }

  async removeWorkspace(workspaceId: string): Promise<void> {
    // Stop runtime
    const runtime = this.runtimes.get(workspaceId);
    if (runtime) {
      runtime.dispose();
      this.runtimes.delete(workspaceId);
    }

    // Clean up associated data
    this.deps.clearPendingOps(workspaceId);
    this.deps.clearResourceStates(workspaceId);
    await this.deps.deleteWorkspacePassword(workspaceId);
    this.deps.removeWorkspace(workspaceId);

    this.broadcastManagerStatus();
  }

  // ── Sync Operations ───────────────────────────────────────────────────

  async syncNow(workspaceId?: string): Promise<void> {
    if (workspaceId) {
      const runtime = this.runtimes.get(workspaceId);
      if (!runtime) throw new Error(`Workspace runtime not active: ${workspaceId}`);
      await runtime.syncNow();
    } else {
      // Sync all active workspaces
      const promises = [...this.runtimes.values()].map((r) => r.syncNow());
      await Promise.allSettled(promises);
    }
  }

  // ── Status ────────────────────────────────────────────────────────────

  getStatus(): CloudSyncManagerStatus {
    const workspaces: CloudSyncRuntimeStatus[] = [];

    // Include status for all registered workspaces (even disabled ones)
    const allWs = this.deps.listWorkspaces();
    for (const ws of allWs) {
      const runtime = this.runtimes.get(ws.id);
      if (runtime) {
        workspaces.push(runtime.getStatus());
      } else {
        // Disabled workspace — static status
        workspaces.push({
          workspaceId: ws.id,
          state: "disabled",
          lastSyncAt: ws.lastSyncAt,
          lastError: ws.lastError,
          pendingCount: 0,
          conflictCount: 0,
          currentVersion: null,
        });
      }
    }

    return { workspaces };
  }

  // ── Conflicts ─────────────────────────────────────────────────────────

  listConflicts(): CloudSyncConflictItemV2[] {
    const conflicts: CloudSyncConflictItemV2[] = [];
    const allWs = this.deps.listWorkspaces();
    const wsMap = new Map(allWs.map((w) => [w.id, w]));

    for (const ws of allWs) {
      const states = this.deps.listResourceStates(ws.id);
      for (const state of states) {
        if (state.conflictRemoteRevision == null) continue;

        let displayName = state.resourceId;
        if (state.resourceType === "server") {
          const conn = this.deps.listConnections().find((c) => c.id === state.resourceId);
          if (conn) displayName = conn.name || conn.host;
        } else {
          const key = this.deps.listSshKeys().find((k) => k.id === state.resourceId);
          if (key) displayName = key.name;
        }

        conflicts.push({
          workspaceId: ws.id,
          workspaceName: wsMap.get(ws.id)?.workspaceName ?? ws.id,
          resourceType: state.resourceType,
          resourceId: state.resourceId,
          displayName,
          serverRevision: state.serverRevision ?? 0,
          conflictRemoteRevision: state.conflictRemoteRevision,
          conflictRemotePayloadJson: state.conflictRemotePayloadJson,
          conflictRemoteUpdatedAt: state.conflictRemoteUpdatedAt,
          conflictRemoteDeleted: state.conflictRemoteDeleted ?? false,
          conflictDetectedAt: state.conflictDetectedAt ?? new Date().toISOString(),
        });
      }
    }

    return conflicts;
  }

  async resolveConflict(
    workspaceId: string,
    resourceType: "server" | "sshKey",
    resourceId: string,
    strategy: "keep_local" | "accept_remote",
  ): Promise<void> {
    const runtime = this.runtimes.get(workspaceId);
    if (!runtime) throw new Error(`Workspace runtime not active: ${workspaceId}`);
    await runtime.resolveConflict(resourceType, resourceId, strategy);
  }

  // ── Push-on-Mutation (called by ConnectionService) ────────────────────

  /** Push-on-mutation: ConnectionService calls this when a connection is upserted */
  pushConnectionUpsert(profile: ConnectionProfile): void {
    if (!profile.originWorkspaceId || profile.originKind !== "cloud") return;
    const runtime = this.runtimes.get(profile.originWorkspaceId);
    if (!runtime) return;
    runtime.enqueuePendingOp(
      "server",
      profile.uuidInScope ?? profile.id,
      "upsert",
    );
  }

  /** Push-on-mutation: ConnectionService calls this when a connection is deleted */
  pushConnectionDelete(profile: ConnectionProfile): void {
    if (!profile.originWorkspaceId || profile.originKind !== "cloud") return;
    const runtime = this.runtimes.get(profile.originWorkspaceId);
    if (!runtime) return;
    runtime.enqueuePendingOp(
      "server",
      profile.uuidInScope ?? profile.id,
      "delete",
    );
  }

  /** Push-on-mutation: ConnectionService calls this when an SSH key is upserted */
  pushSshKeyUpsert(profile: SshKeyProfile): void {
    if (!profile.originWorkspaceId || profile.originKind !== "cloud") return;
    const runtime = this.runtimes.get(profile.originWorkspaceId);
    if (!runtime) return;
    runtime.enqueuePendingOp(
      "sshKey",
      profile.uuidInScope ?? profile.id,
      "upsert",
    );
  }

  /** Push-on-mutation: ConnectionService calls this when an SSH key is deleted */
  pushSshKeyDelete(profile: SshKeyProfile): void {
    if (!profile.originWorkspaceId || profile.originKind !== "cloud") return;
    const runtime = this.runtimes.get(profile.originWorkspaceId);
    if (!runtime) return;
    runtime.enqueuePendingOp(
      "sshKey",
      profile.uuidInScope ?? profile.id,
      "delete",
    );
  }

  // ── Recycle Bin ───────────────────────────────────────────────────────

  listRecycleBinEntries(): RecycleBinEntry[] {
    return this.deps.listRecycleBinEntries();
  }

  removeRecycleBinEntry(id: string): void {
    this.deps.removeRecycleBinEntry(id);
  }

  // ── Private ───────────────────────────────────────────────────────────

  private startRuntime(ws: CloudSyncWorkspaceProfile): void {
    if (this.disposed || this.runtimes.has(ws.id)) return;

    const runtimeDeps: CloudSyncRuntimeDeps = {
      listConnections: () => this.deps.listConnections(),
      listSshKeys: () => this.deps.listSshKeys(),
      saveConnection: (conn) => this.deps.saveConnection(conn),
      saveSshKey: (key) => this.deps.saveSshKey(key),
      removeConnection: (id) => this.deps.removeConnection(id),
      removeSshKey: (id) => this.deps.removeSshKey(id),
      readCredential: (ref) => this.deps.readCredential(ref),

      listPendingOps: (wId) => this.deps.listPendingOps(wId),
      savePendingOp: (op) => this.deps.savePendingOp(op),
      upsertPendingOp: (op) => this.deps.upsertPendingOp(op),
      updatePendingOp: (op) => this.deps.updatePendingOp(op),
      removePendingOp: (id) => this.deps.removePendingOp(id),
      clearPendingOps: (wId) => this.deps.clearPendingOps(wId),

      listResourceStates: (wId) => this.deps.listResourceStates(wId),
      getResourceState: (wId, rType, rId) => this.deps.getResourceState(wId, rType, rId),
      saveResourceState: (s) => this.deps.saveResourceState(s),
      removeResourceState: (wId, rType, rId) => this.deps.removeResourceState(wId, rType, rId),
      clearResourceStates: (wId) => this.deps.clearResourceStates(wId),

      saveRecycleBinEntry: (entry) => this.deps.saveRecycleBinEntry(entry),
      saveWorkspace: (w) => this.deps.saveWorkspace(w),
      getRuntimeCurrentVersion: (wId) => this.deps.getRuntimeCurrentVersion(wId),
      saveRuntimeCurrentVersion: (wId, v) => this.deps.saveRuntimeCurrentVersion(wId, v),
      getWorkspacePassword: (wId) => this.deps.getWorkspacePassword(wId),

      emitStatus: () => this.broadcastManagerStatus(),
      emitApplied: (wId) => this.deps.broadcastApplied(wId),
    };

    const runtime = new CloudSyncRuntime(ws, runtimeDeps);
    this.runtimes.set(ws.id, runtime);
    runtime.start();
  }

  private broadcastManagerStatus(): void {
    this.deps.broadcastStatus(this.getStatus());
  }
}
