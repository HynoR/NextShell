import { createHash, randomUUID } from "node:crypto";
import type { CloudSyncWorkspaceTokenDraft } from "@nextshell/shared";
import type {
  CloudSyncWorkspaceProfile,
  ConnectionProfile,
  ProxyProfile,
  RecycleBinEntry,
  SshKeyProfile,
  WorkspaceCommandItem,
  WorkspaceRepoConflict,
  WorkspaceRepoLocalState,
  WorkspaceRepoSnapshot,
  WorkspaceRepoStatus
} from "@nextshell/core";
import { buildResourceId, buildScopeKey, LOCAL_DEFAULT_SCOPE_KEY } from "@nextshell/core";
import { decryptWorkspaceSecret, encryptWorkspaceSecret } from "@nextshell/security";
import { CloudSyncApiV3Client, type CloudSyncApiV3Credentials } from "./cloud-sync-api-v3";
import {
  encodeCloudSyncWorkspaceToken,
  parseCloudSyncWorkspaceToken
} from "./cloud-sync-workspace-token";

export interface CloudSyncManagerStatus {
  workspaces: WorkspaceRepoStatus[];
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

export type CloudSyncConflictItemV3 = WorkspaceRepoConflict & {
  workspaceName: string;
};

type ResourceType = WorkspaceRepoConflict["resourceType"];

type WorkspaceRuntime = {
  timer?: ReturnType<typeof setTimeout>;
  syncing: boolean;
  lastManualSyncAt: number;
};

type ConnectionSnapshotItem = WorkspaceRepoSnapshot["connections"][number];
type SshKeySnapshotItem = WorkspaceRepoSnapshot["sshKeys"][number];
type ProxySnapshotItem = WorkspaceRepoSnapshot["proxies"][number];
type ResourceSnapshotItem = ConnectionSnapshotItem | SshKeySnapshotItem | ProxySnapshotItem;

export interface CloudSyncManagerDeps {
  listConnections: () => ConnectionProfile[];
  saveConnection: (conn: ConnectionProfile) => void;
  removeConnection: (id: string) => void;

  listSshKeys: () => SshKeyProfile[];
  saveSshKey: (key: SshKeyProfile) => void;
  removeSshKey: (id: string) => void;

  listProxies: () => ProxyProfile[];
  saveProxy: (proxy: ProxyProfile) => void;
  removeProxy: (id: string) => void;

  readCredential: (ref: string) => Promise<string | undefined>;
  storeCredential: (name: string, secret: string) => Promise<string>;
  deleteCredential: (ref: string) => Promise<void>;

  listWorkspaces: () => CloudSyncWorkspaceProfile[];
  saveWorkspace: (ws: CloudSyncWorkspaceProfile) => void;
  removeWorkspace: (id: string) => void;

  getWorkspaceRepoLocalState: (workspaceId: string) => WorkspaceRepoLocalState | undefined;
  saveWorkspaceRepoLocalState: (state: WorkspaceRepoLocalState) => void;
  listWorkspaceRepoConflicts: (workspaceId: string) => WorkspaceRepoConflict[];
  saveWorkspaceRepoConflict: (conflict: WorkspaceRepoConflict) => void;
  removeWorkspaceRepoConflict: (
    workspaceId: string,
    resourceType: string,
    resourceId: string
  ) => void;
  clearWorkspaceRepoConflicts: (workspaceId: string) => void;

  listWorkspaceCommands: (workspaceId: string) => WorkspaceCommandItem[];
  replaceWorkspaceCommands: (workspaceId: string, commands: WorkspaceCommandItem[]) => void;
  getWorkspaceCommandsVersion: (workspaceId: string) => string | undefined;
  saveWorkspaceCommandsVersion: (workspaceId: string, version: string) => void;

  saveRecycleBinEntry: (entry: RecycleBinEntry) => void;
  listRecycleBinEntries: () => RecycleBinEntry[];
  removeRecycleBinEntry: (id: string) => void;

  storeWorkspacePassword: (workspaceId: string, password: string) => Promise<void>;
  getWorkspacePassword: (workspaceId: string) => Promise<string | undefined>;
  deleteWorkspacePassword: (workspaceId: string) => Promise<void>;

  getJsonSetting: <T = unknown>(key: string) => T | undefined;
  saveJsonSetting: (key: string, value: unknown) => void;

  broadcastStatus: (status: CloudSyncManagerStatus) => void;
  broadcastApplied: (workspaceId: string) => void;
}

const CLIENT_ID_SETTING_KEY = "cloud_sync_client_id";
const SYNC_NOW_MIN_INTERVAL_MS = 5_000;

const stableSerialize = (value: unknown): string => {
  if (value === null || typeof value !== "object") {
    return JSON.stringify(value);
  }

  if (Array.isArray(value)) {
    return `[${value.map((item) => stableSerialize(item)).join(",")}]`;
  }

  const entries = Object.entries(value as Record<string, unknown>)
    .filter(([, item]) => item !== undefined)
    .sort(([left], [right]) => left.localeCompare(right));
  return `{${entries.map(([key, item]) => `${JSON.stringify(key)}:${stableSerialize(item)}`).join(",")}}`;
};

const hashValue = (value: unknown): string =>
  createHash("sha256").update(stableSerialize(value), "utf8").digest("hex");

const workspaceRootSlug = (workspaceName: string): string => {
  const normalized = workspaceName
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9._-]+/g, "-")
    .replace(/-+/g, "-")
    .replace(/^-|-$/g, "");
  return normalized || "workspace";
};

const normalizeWorkspaceGroupPath = (workspaceName: string, groupPath: string): string => {
  const slug = workspaceRootSlug(workspaceName);
  const segments = groupPath.split("/").filter((segment) => segment.length > 0);
  if (segments[0] === "workspace") {
    const suffix = segments.slice(2).join("/");
    return suffix ? `/workspace/${slug}/${suffix}` : `/workspace/${slug}`;
  }
  const suffix = segments.join("/");
  return suffix ? `/workspace/${slug}/${suffix}` : `/workspace/${slug}`;
};

const buildEmptySnapshot = (workspaceId: string): WorkspaceRepoSnapshot => ({
  workspaceId,
  snapshotId: hashValue({ connections: [], sshKeys: [], proxies: [] }),
  createdAt: new Date().toISOString(),
  connections: [],
  sshKeys: [],
  proxies: []
});

const makeDefaultLocalState = (workspaceId: string, enabled: boolean): WorkspaceRepoLocalState => ({
  workspaceId,
  syncState: enabled ? "idle" : "disabled"
});

const toStatusState = (
  localState: WorkspaceRepoLocalState | undefined,
  syncing: boolean,
  enabled: boolean
): WorkspaceRepoStatus["state"] => {
  if (!enabled) return "disabled";
  if (syncing) return "syncing";
  if (localState?.syncState === "error") return "error";
  if (localState?.syncState === "diverged") return "diverged";
  if (!localState || localState.syncState === "idle") return "idle";
  return "synced";
};

const parseSnapshotJson = (
  workspaceId: string,
  json: string | undefined
): WorkspaceRepoSnapshot => {
  if (json) {
    try {
      return JSON.parse(json) as WorkspaceRepoSnapshot;
    } catch {
      // fall through to empty snapshot
    }
  }
  return buildEmptySnapshot(workspaceId);
};

export class CloudSyncManager {
  private readonly api = new CloudSyncApiV3Client();
  private readonly runtimes = new Map<string, WorkspaceRuntime>();
  private readonly clientId: string;
  private readonly clientVersion: string;
  private disposed = false;

  constructor(private readonly deps: CloudSyncManagerDeps) {
    const persistedClientId =
      this.deps.getJsonSetting<string>(CLIENT_ID_SETTING_KEY) ?? randomUUID();
    this.clientId = persistedClientId;
    this.deps.saveJsonSetting(CLIENT_ID_SETTING_KEY, persistedClientId);
    this.clientVersion = process.env.npm_package_version ?? "dev";
  }

  initialize(): void {
    const workspaces = this.deps.listWorkspaces();
    for (const workspace of workspaces) {
      void this.ensureWorkspaceBootstrapped(workspace.id);
      if (workspace.enabled) {
        this.startRuntime(workspace);
      }
    }
    this.broadcastManagerStatus();
  }

  dispose(): void {
    this.disposed = true;
    for (const runtime of this.runtimes.values()) {
      if (runtime.timer) {
        clearTimeout(runtime.timer);
      }
    }
    this.runtimes.clear();
  }

  listWorkspaces(): CloudSyncWorkspaceProfile[] {
    return this.deps.listWorkspaces();
  }

  async exportWorkspaceToken(workspaceId: string): Promise<{ token: string }> {
    const workspace = this.deps.listWorkspaces().find((item) => item.id === workspaceId);
    if (!workspace) {
      throw new Error(`Workspace not found: ${workspaceId}`);
    }

    const workspacePassword = await this.deps.getWorkspacePassword(workspaceId);
    if (!workspacePassword) {
      throw new Error("该工作区缺少可导出的完整配置");
    }

    const draft: CloudSyncWorkspaceTokenDraft = {
      apiBaseUrl: workspace.apiBaseUrl,
      workspaceName: workspace.workspaceName,
      displayName: workspace.displayName,
      workspacePassword,
      pullIntervalSec: workspace.pullIntervalSec,
      ignoreTlsErrors: workspace.ignoreTlsErrors,
      enabled: workspace.enabled
    };

    return { token: encodeCloudSyncWorkspaceToken(draft) };
  }

  async parseWorkspaceToken(token: string): Promise<CloudSyncWorkspaceTokenDraft> {
    return parseCloudSyncWorkspaceToken(token);
  }

  async addWorkspace(input: CloudSyncWorkspaceInput): Promise<CloudSyncWorkspaceProfile> {
    const id = input.id ?? randomUUID();
    const now = new Date().toISOString();
    const workspace: CloudSyncWorkspaceProfile = {
      id,
      apiBaseUrl: input.apiBaseUrl.replace(/\/+$/, ""),
      workspaceName: input.workspaceName.trim(),
      displayName: input.displayName?.trim() || input.workspaceName.trim(),
      pullIntervalSec: input.pullIntervalSec ?? 300,
      ignoreTlsErrors: input.ignoreTlsErrors ?? false,
      enabled: input.enabled ?? true,
      createdAt: now,
      updatedAt: now,
      lastSyncAt: null,
      lastError: null
    };

    if (!input.workspacePassword) {
      throw new Error("工作区密码不能为空");
    }

    await this.deps.storeWorkspacePassword(id, input.workspacePassword);
    this.deps.saveWorkspace(workspace);
    this.deps.saveWorkspaceRepoLocalState(makeDefaultLocalState(id, workspace.enabled));
    this.updateLocalCommandsVersion(id);
    if (workspace.enabled) {
      this.startRuntime(workspace);
      await this.syncNow(id);
    }

    this.broadcastManagerStatus();
    return workspace;
  }

  async updateWorkspace(
    input: CloudSyncWorkspaceInput & { id: string }
  ): Promise<CloudSyncWorkspaceProfile> {
    const existing = this.deps.listWorkspaces().find((workspace) => workspace.id === input.id);
    if (!existing) {
      throw new Error(`Workspace not found: ${input.id}`);
    }

    const workspace: CloudSyncWorkspaceProfile = {
      ...existing,
      apiBaseUrl: input.apiBaseUrl.replace(/\/+$/, ""),
      workspaceName: input.workspaceName.trim(),
      displayName: input.displayName?.trim() || existing.displayName,
      pullIntervalSec: input.pullIntervalSec ?? existing.pullIntervalSec,
      ignoreTlsErrors: input.ignoreTlsErrors ?? existing.ignoreTlsErrors,
      enabled: input.enabled ?? existing.enabled,
      updatedAt: new Date().toISOString()
    };

    if (input.workspacePassword) {
      await this.deps.storeWorkspacePassword(workspace.id, input.workspacePassword);
    }

    this.deps.saveWorkspace(workspace);
    const localState =
      this.deps.getWorkspaceRepoLocalState(workspace.id) ??
      makeDefaultLocalState(workspace.id, workspace.enabled);
    this.deps.saveWorkspaceRepoLocalState({
      ...localState,
      syncState: workspace.enabled
        ? localState.syncState === "disabled"
          ? "idle"
          : localState.syncState
        : "disabled"
    });

    if (workspace.enabled) {
      this.startRuntime(workspace);
    } else {
      this.stopRuntime(workspace.id);
    }

    this.broadcastManagerStatus();
    return workspace;
  }

  async removeWorkspace(workspaceId: string): Promise<void> {
    const workspace = this.deps.listWorkspaces().find((item) => item.id === workspaceId);
    if (!workspace) {
      return;
    }

    this.stopRuntime(workspaceId);
    await this.clearWorkspaceMaterializedData(workspaceId);
    await this.deps.deleteWorkspacePassword(workspaceId);
    this.deps.removeWorkspace(workspaceId);
    this.broadcastManagerStatus();
  }

  getStatus(): CloudSyncManagerStatus {
    return {
      workspaces: this.deps.listWorkspaces().map((workspace) => this.getWorkspaceStatus(workspace))
    };
  }

  listConflicts(): CloudSyncConflictItemV3[] {
    const workspaceNameById = new Map(
      this.deps
        .listWorkspaces()
        .map((workspace) => [workspace.id, workspace.displayName || workspace.workspaceName])
    );
    return this.deps
      .listWorkspaces()
      .flatMap((workspace) =>
        this.deps.listWorkspaceRepoConflicts(workspace.id).map((conflict) => ({
          ...conflict,
          workspaceName: workspaceNameById.get(workspace.id) ?? workspace.id
        }))
      )
      .sort((left, right) => right.detectedAt.localeCompare(left.detectedAt));
  }

  async testConnection(input: {
    apiBaseUrl: string;
    workspaceName: string;
    workspacePassword: string;
    ignoreTlsErrors?: boolean;
  }): Promise<{ ok: true; displayName?: string }> {
    const result = await this.api.resolve({
      apiBaseUrl: input.apiBaseUrl.replace(/\/+$/, ""),
      workspaceName: input.workspaceName.trim(),
      workspacePassword: input.workspacePassword,
      ignoreTlsErrors: input.ignoreTlsErrors ?? false,
      clientId: this.clientId,
      clientVersion: this.clientVersion
    });
    return { ok: true, displayName: result.displayName };
  }

  async syncNow(workspaceId?: string, force = false): Promise<void> {
    if (workspaceId) {
      const workspace = this.getWorkspaceOrThrow(workspaceId);
      await this.syncWorkspace(workspace, force);
      return;
    }

    const workspaces = this.deps.listWorkspaces().filter((workspace) => workspace.enabled);
    await Promise.allSettled(workspaces.map((workspace) => this.syncWorkspace(workspace, force)));
  }

  async resolveConflict(
    workspaceId: string,
    resourceType: ResourceType,
    resourceId: string,
    strategy: "keep_local" | "accept_remote"
  ): Promise<void> {
    const workspace = this.getWorkspaceOrThrow(workspaceId);
    const password = await this.getWorkspacePassword(workspaceId);
    if (!password) {
      throw new Error("Workspace password not available");
    }

    const conflicts = this.deps.listWorkspaceRepoConflicts(workspaceId);
    const target = conflicts.find(
      (conflict) => conflict.resourceType === resourceType && conflict.resourceId === resourceId
    );
    if (!target) {
      return;
    }

    if (strategy === "accept_remote") {
      const currentSnapshot = await this.buildWorkspaceSnapshot(workspace, password);
      const patchedSnapshot = this.patchSnapshotWithConflictResolution(
        currentSnapshot,
        target,
        "accept_remote"
      );
      await this.applyWorkspaceSnapshot(workspace, password, patchedSnapshot);
    }

    this.deps.removeWorkspaceRepoConflict(workspaceId, resourceType, resourceId);
    const remaining = this.deps.listWorkspaceRepoConflicts(workspaceId);
    if (remaining.length === 0) {
      await this.syncNow(workspaceId, true);
    } else {
      const localState =
        this.deps.getWorkspaceRepoLocalState(workspaceId) ??
        makeDefaultLocalState(workspaceId, workspace.enabled);
      this.deps.saveWorkspaceRepoLocalState({
        ...localState,
        syncState: "diverged"
      });
    }

    this.broadcastManagerStatus();
    this.deps.broadcastApplied(workspaceId);
  }

  pushConnectionUpsert(profile: ConnectionProfile): void {
    if (profile.originKind !== "cloud" || !profile.originWorkspaceId) {
      return;
    }
    this.recordWorkspaceMutation(profile.originWorkspaceId);
  }

  pushConnectionDelete(profile: ConnectionProfile): void {
    if (profile.originKind !== "cloud" || !profile.originWorkspaceId) {
      return;
    }
    this.recordWorkspaceMutation(profile.originWorkspaceId);
  }

  pushSshKeyUpsert(profile: SshKeyProfile): void {
    if (profile.originKind !== "cloud" || !profile.originWorkspaceId) {
      return;
    }
    this.recordWorkspaceMutation(profile.originWorkspaceId);
  }

  pushSshKeyDelete(profile: SshKeyProfile): void {
    if (profile.originKind !== "cloud" || !profile.originWorkspaceId) {
      return;
    }
    this.recordWorkspaceMutation(profile.originWorkspaceId);
  }

  pushProxyUpsert(profile: ProxyProfile): void {
    if (profile.originKind !== "cloud" || !profile.originWorkspaceId) {
      return;
    }
    this.recordWorkspaceMutation(profile.originWorkspaceId);
  }

  pushProxyDelete(profile: ProxyProfile): void {
    if (profile.originKind !== "cloud" || !profile.originWorkspaceId) {
      return;
    }
    this.recordWorkspaceMutation(profile.originWorkspaceId);
  }

  markWorkspaceCommandsDirty(workspaceId: string): void {
    this.updateLocalCommandsVersion(workspaceId);
    void this.syncNow(workspaceId).catch(() => undefined);
  }

  private startRuntime(workspace: CloudSyncWorkspaceProfile): void {
    if (this.disposed) {
      return;
    }

    const runtime = this.runtimes.get(workspace.id) ?? {
      syncing: false,
      lastManualSyncAt: 0
    };
    this.runtimes.set(workspace.id, runtime);
    this.scheduleWorkspaceSync(workspace);
  }

  private stopRuntime(workspaceId: string): void {
    const runtime = this.runtimes.get(workspaceId);
    if (runtime?.timer) {
      clearTimeout(runtime.timer);
    }
    this.runtimes.delete(workspaceId);
  }

  private scheduleWorkspaceSync(workspace: CloudSyncWorkspaceProfile): void {
    const runtime = this.runtimes.get(workspace.id);
    if (!runtime || this.disposed || !workspace.enabled) {
      return;
    }
    if (runtime.timer) {
      clearTimeout(runtime.timer);
    }

    runtime.timer = setTimeout(
      () => {
        void this.syncNow(workspace.id).finally(() => {
          const refreshed = this.deps.listWorkspaces().find((item) => item.id === workspace.id);
          if (refreshed) {
            this.scheduleWorkspaceSync(refreshed);
          }
        });
      },
      Math.max(10, workspace.pullIntervalSec) * 1000
    );
  }

  private getWorkspaceStatus(workspace: CloudSyncWorkspaceProfile): WorkspaceRepoStatus {
    const runtime = this.runtimes.get(workspace.id);
    const localState = this.deps.getWorkspaceRepoLocalState(workspace.id);
    const commandsVersion =
      this.deps.getWorkspaceCommandsVersion(workspace.id) ?? localState?.remoteCommandsVersion;
    return {
      workspaceId: workspace.id,
      state: toStatusState(localState, runtime?.syncing ?? false, workspace.enabled),
      lastSyncAt: localState?.lastSyncAt ?? workspace.lastSyncAt ?? undefined,
      lastError: localState?.lastError ?? workspace.lastError ?? undefined,
      conflictCount: this.deps.listWorkspaceRepoConflicts(workspace.id).length,
      commandsVersion
    };
  }

  private ensureWorkspaceBootstrapped(workspaceId: string): void {
    const workspace = this.deps.listWorkspaces().find((item) => item.id === workspaceId);
    if (!workspace) {
      return;
    }
    if (!this.deps.getWorkspaceRepoLocalState(workspaceId)) {
      this.deps.saveWorkspaceRepoLocalState(makeDefaultLocalState(workspaceId, workspace.enabled));
    }
    this.updateLocalCommandsVersion(workspaceId);
  }

  private async syncWorkspace(workspace: CloudSyncWorkspaceProfile, force = false): Promise<void> {
    if (!workspace.enabled) {
      return;
    }

    const runtime = this.runtimes.get(workspace.id) ?? {
      syncing: false,
      lastManualSyncAt: 0
    };
    this.runtimes.set(workspace.id, runtime);
    if (runtime.syncing) {
      return;
    }
    if (!force && Date.now() - runtime.lastManualSyncAt < SYNC_NOW_MIN_INTERVAL_MS) {
      return;
    }

    runtime.syncing = true;
    runtime.lastManualSyncAt = Date.now();
    this.broadcastManagerStatus();

    try {
      this.ensureWorkspaceBootstrapped(workspace.id);
      const credentials = await this.getCredentials(workspace);
      const localState =
        this.deps.getWorkspaceRepoLocalState(workspace.id) ??
        makeDefaultLocalState(workspace.id, workspace.enabled);
      const resolve = await this.api.resolve(credentials);
      const remoteVersion = resolve.headCommitId ?? undefined;
      const normalizedState = await this.syncWorkspaceRepo(
        workspace,
        credentials,
        localState,
        remoteVersion
      );
      const commandState = await this.syncWorkspaceCommands(
        workspace,
        credentials,
        { ...normalizedState, remoteCommandsVersion: localState.remoteCommandsVersion },
        resolve.commandsVersion ?? undefined
      );

      const syncedAt = new Date().toISOString();
      const nextLocalState: WorkspaceRepoLocalState = {
        ...commandState,
        syncState:
          this.deps.listWorkspaceRepoConflicts(workspace.id).length > 0 ? "diverged" : "synced",
        lastSyncAt: syncedAt,
        lastError: undefined
      };
      this.deps.saveWorkspaceRepoLocalState(nextLocalState);
      this.deps.saveWorkspace({
        ...workspace,
        lastSyncAt: syncedAt,
        lastError: null
      });
      this.deps.broadcastApplied(workspace.id);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      const localState =
        this.deps.getWorkspaceRepoLocalState(workspace.id) ??
        makeDefaultLocalState(workspace.id, workspace.enabled);
      this.deps.saveWorkspaceRepoLocalState({
        ...localState,
        lastError: message,
        syncState: "error"
      });
      this.deps.saveWorkspace({
        ...workspace,
        lastError: message
      });
    } finally {
      runtime.syncing = false;
      this.broadcastManagerStatus();
    }
  }

  private async syncWorkspaceRepo(
    workspace: CloudSyncWorkspaceProfile,
    credentials: CloudSyncApiV3Credentials,
    localState: WorkspaceRepoLocalState,
    remoteVersion?: string
  ): Promise<WorkspaceRepoLocalState> {
    // Pending conflicts gate all repo sync until the user resolves them.
    if (this.deps.listWorkspaceRepoConflicts(workspace.id).length > 0) {
      return {
        ...localState,
        remoteVersion: remoteVersion ?? localState.remoteVersion,
        syncState: "diverged"
      };
    }

    const base = parseSnapshotJson(workspace.id, localState.baseSnapshotJson);
    const localSnapshot = await this.buildWorkspaceSnapshot(
      workspace,
      credentials.workspacePassword
    );
    const knownRemoteVersion = localState.remoteVersion;

    const localDirty = localSnapshot.snapshotId !== base.snapshotId;
    const remoteChanged = (remoteVersion ?? undefined) !== (knownRemoteVersion ?? undefined);

    if (!localDirty && !remoteChanged) {
      return { ...localState, syncState: "synced" };
    }
    if (!localDirty && remoteChanged) {
      return this.pullRemoteHead(workspace, credentials, localState);
    }
    if (localDirty && !remoteChanged) {
      return this.pushLocalHead(workspace, credentials, localState, localSnapshot);
    }
    // Both sides moved → reconcile via three-way merge against the common ancestor.
    return this.reconcileDivergence(workspace, credentials, localState, base, localSnapshot);
  }

  private async pullRemoteHead(
    workspace: CloudSyncWorkspaceProfile,
    credentials: CloudSyncApiV3Credentials,
    localState: WorkspaceRepoLocalState
  ): Promise<WorkspaceRepoLocalState> {
    const response = await this.api.pull(credentials, localState.remoteVersion);
    const remoteVersion = response.headCommitId ?? undefined;
    if (response.unchanged || !response.snapshot) {
      return { ...localState, remoteVersion, syncState: "synced" };
    }
    const remoteSnapshot: WorkspaceRepoSnapshot = {
      ...response.snapshot,
      workspaceId: workspace.id
    };
    await this.applyWorkspaceSnapshot(workspace, credentials.workspacePassword, remoteSnapshot);
    this.deps.clearWorkspaceRepoConflicts(workspace.id);
    return {
      ...localState,
      baseSnapshotJson: JSON.stringify(remoteSnapshot),
      remoteVersion,
      syncState: "synced"
    };
  }

  private async pushLocalHead(
    workspace: CloudSyncWorkspaceProfile,
    credentials: CloudSyncApiV3Credentials,
    localState: WorkspaceRepoLocalState,
    localSnapshot: WorkspaceRepoSnapshot
  ): Promise<WorkspaceRepoLocalState> {
    const response = await this.api.push(credentials, {
      baseHeadCommitId: localState.remoteVersion ?? null,
      snapshot: localSnapshot
    });

    if (response.status === "accepted") {
      this.deps.clearWorkspaceRepoConflicts(workspace.id);
      return {
        ...localState,
        baseSnapshotJson: JSON.stringify(localSnapshot),
        remoteVersion: response.headCommitId,
        syncState: "synced"
      };
    }

    // Remote advanced since our base; merge against the snapshot the server returned.
    const base = parseSnapshotJson(workspace.id, localState.baseSnapshotJson);
    const remoteSnapshot: WorkspaceRepoSnapshot = {
      ...response.snapshot,
      workspaceId: workspace.id
    };
    return this.mergeAndSettle(
      workspace,
      credentials,
      localState,
      base,
      localSnapshot,
      remoteSnapshot,
      response.headCommitId ?? undefined
    );
  }

  private async reconcileDivergence(
    workspace: CloudSyncWorkspaceProfile,
    credentials: CloudSyncApiV3Credentials,
    localState: WorkspaceRepoLocalState,
    base: WorkspaceRepoSnapshot,
    localSnapshot: WorkspaceRepoSnapshot
  ): Promise<WorkspaceRepoLocalState> {
    const response = await this.api.pull(credentials, localState.remoteVersion);
    const remoteVersion = response.headCommitId ?? undefined;
    if (response.unchanged || !response.snapshot) {
      // Remote token advanced without a snapshot delta; push local as the new head.
      return this.pushLocalHead(
        workspace,
        credentials,
        { ...localState, remoteVersion },
        localSnapshot
      );
    }
    const remoteSnapshot: WorkspaceRepoSnapshot = {
      ...response.snapshot,
      workspaceId: workspace.id
    };
    return this.mergeAndSettle(
      workspace,
      credentials,
      localState,
      base,
      localSnapshot,
      remoteSnapshot,
      remoteVersion
    );
  }

  private async mergeAndSettle(
    workspace: CloudSyncWorkspaceProfile,
    credentials: CloudSyncApiV3Credentials,
    localState: WorkspaceRepoLocalState,
    base: WorkspaceRepoSnapshot,
    localSnapshot: WorkspaceRepoSnapshot,
    remoteSnapshot: WorkspaceRepoSnapshot,
    remoteVersion: string | undefined
  ): Promise<WorkspaceRepoLocalState> {
    const mergeResult = this.mergeSnapshots(base, localSnapshot, remoteSnapshot);
    this.deps.clearWorkspaceRepoConflicts(workspace.id);
    await this.applyWorkspaceSnapshot(
      workspace,
      credentials.workspacePassword,
      mergeResult.snapshot
    );

    if (mergeResult.conflicts.length > 0) {
      for (const conflict of mergeResult.conflicts) {
        this.deps.saveWorkspaceRepoConflict(conflict);
      }
      // Adopt remote as the new common ancestor; conflicts gate sync until resolved.
      return {
        ...localState,
        baseSnapshotJson: JSON.stringify(remoteSnapshot),
        remoteVersion,
        syncState: "diverged"
      };
    }

    // Merge already equals remote (local had no unique changes) → nothing to push.
    if (mergeResult.snapshot.snapshotId === remoteSnapshot.snapshotId) {
      return {
        ...localState,
        baseSnapshotJson: JSON.stringify(remoteSnapshot),
        remoteVersion,
        syncState: "synced"
      };
    }

    // Clean auto-merge: push the merged result back to the server.
    const pushResponse = await this.api.push(credentials, {
      baseHeadCommitId: remoteVersion ?? null,
      snapshot: mergeResult.snapshot
    });
    if (pushResponse.status === "accepted") {
      return {
        ...localState,
        baseSnapshotJson: JSON.stringify(mergeResult.snapshot),
        remoteVersion: pushResponse.headCommitId,
        syncState: "synced"
      };
    }

    // Remote moved again mid-merge; adopt the newest snapshot and retry next tick.
    const newerRemote: WorkspaceRepoSnapshot = {
      ...pushResponse.snapshot,
      workspaceId: workspace.id
    };
    await this.applyWorkspaceSnapshot(workspace, credentials.workspacePassword, newerRemote);
    return {
      ...localState,
      baseSnapshotJson: JSON.stringify(newerRemote),
      remoteVersion: pushResponse.headCommitId ?? remoteVersion,
      syncState: "idle"
    };
  }

  private async syncWorkspaceCommands(
    workspace: CloudSyncWorkspaceProfile,
    credentials: CloudSyncApiV3Credentials,
    localState: WorkspaceRepoLocalState,
    resolvedRemoteVersion?: string
  ): Promise<WorkspaceRepoLocalState> {
    const localVersion = this.updateLocalCommandsVersion(workspace.id);
    const lastRemoteVersion = localState.remoteCommandsVersion;
    const remoteVersion = resolvedRemoteVersion;

    if (!remoteVersion && !localVersion) {
      return {
        ...localState,
        remoteCommandsVersion: undefined
      };
    }

    const localDirty = (localVersion ?? undefined) !== (lastRemoteVersion ?? undefined);
    const remoteChanged = (remoteVersion ?? undefined) !== (lastRemoteVersion ?? undefined);

    if (localDirty && remoteChanged) {
      const response = await this.api.pullCommands(credentials, lastRemoteVersion ?? null);
      if (response.status === "changed") {
        const mergedCommands = this.mergeWorkspaceCommands(
          this.deps.listWorkspaceCommands(workspace.id),
          response.commands.map((command) => ({
            ...command,
            workspaceId: workspace.id
          })),
          workspace.id
        );
        const pushResponse = await this.api.pushCommands(
          credentials,
          mergedCommands.map((command) => ({
            ...command,
            workspaceId: workspace.id
          }))
        );
        this.deps.replaceWorkspaceCommands(workspace.id, mergedCommands);
        this.deps.saveWorkspaceCommandsVersion(workspace.id, pushResponse.version);
        return {
          ...localState,
          remoteCommandsVersion: pushResponse.version
        };
      }
      const pushResponse = await this.api.pushCommands(
        credentials,
        this.deps.listWorkspaceCommands(workspace.id).map((command) => ({
          ...command,
          workspaceId: workspace.id
        }))
      );
      this.deps.saveWorkspaceCommandsVersion(workspace.id, pushResponse.version);
      return {
        ...localState,
        remoteCommandsVersion: pushResponse.version
      };
    }

    if (localDirty || (!remoteVersion && localVersion)) {
      const response = await this.api.pushCommands(
        credentials,
        this.deps.listWorkspaceCommands(workspace.id).map((command) => ({
          ...command,
          workspaceId: workspace.id
        }))
      );
      this.deps.saveWorkspaceCommandsVersion(workspace.id, response.version);
      return {
        ...localState,
        remoteCommandsVersion: response.version
      };
    }

    if (remoteChanged && remoteVersion) {
      const response = await this.api.pullCommands(credentials, lastRemoteVersion ?? null);
      if (response.status === "changed") {
        this.deps.replaceWorkspaceCommands(
          workspace.id,
          response.commands.map((command) => ({
            ...command,
            workspaceId: workspace.id
          }))
        );
        this.deps.saveWorkspaceCommandsVersion(workspace.id, response.version);
        return {
          ...localState,
          remoteCommandsVersion: response.version
        };
      }
      this.deps.saveWorkspaceCommandsVersion(workspace.id, response.version);
      return {
        ...localState,
        remoteCommandsVersion: response.version
      };
    }

    if (remoteVersion) {
      this.deps.saveWorkspaceCommandsVersion(workspace.id, remoteVersion);
    }
    return {
      ...localState,
      remoteCommandsVersion: remoteVersion ?? localVersion
    };
  }

  private mergeWorkspaceCommands(
    localCommands: WorkspaceCommandItem[],
    remoteCommands: WorkspaceCommandItem[],
    workspaceId: string
  ): WorkspaceCommandItem[] {
    const now = new Date().toISOString();
    const merged = new Map<string, WorkspaceCommandItem>();

    for (const command of localCommands) {
      merged.set(command.id, { ...command, workspaceId });
    }

    for (const remoteCommand of remoteCommands) {
      const normalizedRemote = { ...remoteCommand, workspaceId };
      const localCommand = merged.get(remoteCommand.id);
      if (!localCommand) {
        merged.set(remoteCommand.id, normalizedRemote);
        continue;
      }
      if (
        hashValue(this.toCommandVersionItem(localCommand)) ===
        hashValue(this.toCommandVersionItem(normalizedRemote))
      ) {
        continue;
      }
      const conflictCopyId = randomUUID();
      merged.set(conflictCopyId, {
        ...normalizedRemote,
        id: conflictCopyId,
        name: `${normalizedRemote.name} (云端版本)`,
        createdAt: now,
        updatedAt: now
      });
    }

    return [...merged.values()].sort((left, right) => {
      const groupCompare = left.group.localeCompare(right.group);
      if (groupCompare !== 0) {
        return groupCompare;
      }
      return left.name.localeCompare(right.name);
    });
  }

  private toCommandVersionItem(
    command: WorkspaceCommandItem
  ): Omit<WorkspaceCommandItem, "workspaceId"> {
    return {
      id: command.id,
      name: command.name,
      description: command.description,
      group: command.group,
      command: command.command,
      isTemplate: command.isTemplate,
      createdAt: command.createdAt,
      updatedAt: command.updatedAt
    };
  }

  private updateLocalCommandsVersion(workspaceId: string): string | undefined {
    const commands = this.deps
      .listWorkspaceCommands(workspaceId)
      .map((command) => this.toCommandVersionItem(command))
      .sort((left, right) => left.id.localeCompare(right.id));

    if (commands.length === 0) {
      this.deps.saveWorkspaceCommandsVersion(workspaceId, "");
      return undefined;
    }

    const version = hashValue(commands);
    this.deps.saveWorkspaceCommandsVersion(workspaceId, version);
    return version;
  }

  private async buildWorkspaceSnapshot(
    workspace: CloudSyncWorkspaceProfile,
    workspacePassword: string
  ): Promise<WorkspaceRepoSnapshot> {
    const workspaceId = workspace.id;
    const scopeKey = buildScopeKey({
      kind: "cloud",
      apiBaseUrl: workspace.apiBaseUrl,
      workspaceName: workspace.workspaceName
    });
    const sshKeys = this.listWorkspaceSshKeys(workspaceId);
    const proxies = this.listWorkspaceProxies(workspaceId);
    const connections = this.listWorkspaceConnections(workspaceId);
    const sshKeyById = new Map(sshKeys.map((key) => [key.id, key]));
    const proxyById = new Map(proxies.map((proxy) => [proxy.id, proxy]));

    const snapshot: WorkspaceRepoSnapshot = {
      workspaceId,
      snapshotId: "",
      createdAt: new Date().toISOString(),
      sshKeys: (
        await Promise.all(
          sshKeys.map(async (key) => {
            const uuid = key.uuidInScope ?? key.id;
            const privateKey = await this.mustEncryptCredential(
              key.keyContentRef,
              workspacePassword,
              `${scopeKey}:sshKey:${uuid}:privateKey`
            );
            const passphrase = key.passphraseRef
              ? await this.encryptCredential(
                  key.passphraseRef,
                  workspacePassword,
                  `${scopeKey}:sshKey:${uuid}:passphrase`
                )
              : undefined;
            return {
              uuid,
              name: key.name,
              privateKey,
              passphrase,
              createdAt: key.createdAt,
              updatedAt: key.updatedAt
            } satisfies SshKeySnapshotItem;
          })
        )
      ).sort((left, right) => left.uuid.localeCompare(right.uuid)),
      proxies: (
        await Promise.all(
          proxies.map(async (proxy) => {
            const uuid = proxy.uuidInScope ?? proxy.id;
            const password = proxy.credentialRef
              ? await this.encryptCredential(
                  proxy.credentialRef,
                  workspacePassword,
                  `${scopeKey}:proxy:${uuid}:password`
                )
              : undefined;
            return {
              uuid,
              name: proxy.name,
              proxyType: proxy.proxyType,
              host: proxy.host,
              port: proxy.port,
              username: proxy.username,
              password,
              createdAt: proxy.createdAt,
              updatedAt: proxy.updatedAt
            } satisfies ProxySnapshotItem;
          })
        )
      ).sort((left, right) => left.uuid.localeCompare(right.uuid)),
      connections: (
        await Promise.all(
          connections.map(async (connection) => {
            const uuid = connection.uuidInScope ?? connection.id;
            const sshKey = connection.sshKeyId ? sshKeyById.get(connection.sshKeyId) : undefined;
            const proxy = connection.proxyId ? proxyById.get(connection.proxyId) : undefined;
            const password =
              connection.authType === "password" || connection.authType === "interactive"
                ? await this.encryptCredential(
                    connection.credentialRef,
                    workspacePassword,
                    `${scopeKey}:connection:${uuid}:password`
                  )
                : undefined;

            if (connection.authType === "privateKey" && connection.sshKeyId && !sshKey) {
              throw new Error(
                `Connection ${connection.name} references an SSH key outside workspace ${workspace.workspaceName}`
              );
            }
            if (connection.proxyId && !proxy) {
              throw new Error(
                `Connection ${connection.name} references a proxy outside workspace ${workspace.workspaceName}`
              );
            }

            return {
              uuid,
              name: connection.name,
              host: connection.host,
              port: connection.port,
              username: connection.username,
              authType: connection.authType,
              password,
              sshKeyUuid: sshKey?.uuidInScope ?? sshKey?.id,
              hostFingerprint: connection.hostFingerprint,
              strictHostKeyChecking: connection.strictHostKeyChecking,
              proxyUuid: proxy?.uuidInScope ?? proxy?.id,
              keepAliveEnabled: connection.keepAliveEnabled,
              keepAliveIntervalSec: connection.keepAliveIntervalSec,
              terminalEncoding: connection.terminalEncoding,
              backspaceMode: connection.backspaceMode,
              deleteMode: connection.deleteMode,
              groupPath: normalizeWorkspaceGroupPath(workspace.workspaceName, connection.groupPath),
              tags: [...connection.tags],
              notes: connection.notes,
              favorite: connection.favorite,
              createdAt: connection.createdAt,
              updatedAt: connection.updatedAt
            } satisfies ConnectionSnapshotItem;
          })
        )
      ).sort((left, right) => left.uuid.localeCompare(right.uuid))
    };

    snapshot.snapshotId = hashValue({
      connections: snapshot.connections,
      sshKeys: snapshot.sshKeys,
      proxies: snapshot.proxies
    });
    return snapshot;
  }

  private async applyWorkspaceSnapshot(
    workspace: CloudSyncWorkspaceProfile,
    workspacePassword: string,
    snapshot: WorkspaceRepoSnapshot
  ): Promise<void> {
    const scopeKey = buildScopeKey({
      kind: "cloud",
      apiBaseUrl: workspace.apiBaseUrl,
      workspaceName: workspace.workspaceName
    });
    const existingKeys = this.listWorkspaceSshKeys(workspace.id);
    const existingProxies = this.listWorkspaceProxies(workspace.id);
    const existingConnections = this.listWorkspaceConnections(workspace.id);
    const keyByUuid = new Map(existingKeys.map((key) => [key.uuidInScope ?? key.id, key]));
    const proxyByUuid = new Map(
      existingProxies.map((proxy) => [proxy.uuidInScope ?? proxy.id, proxy])
    );
    const connectionByUuid = new Map(
      existingConnections.map((connection) => [connection.uuidInScope ?? connection.id, connection])
    );
    const keyIdByUuid = new Map<string, string>();
    const proxyIdByUuid = new Map<string, string>();
    const retainedKeyIds = new Set<string>();
    const retainedProxyIds = new Set<string>();
    const retainedConnectionIds = new Set<string>();

    for (const key of snapshot.sshKeys) {
      const existing = keyByUuid.get(key.uuid);
      const localId = existing?.id ?? randomUUID();
      const privateKey = await decryptWorkspaceSecret(
        key.privateKey as Parameters<typeof decryptWorkspaceSecret>[0],
        workspacePassword
      );
      const passphrase = key.passphrase
        ? await decryptWorkspaceSecret(
            key.passphrase as Parameters<typeof decryptWorkspaceSecret>[0],
            workspacePassword
          )
        : undefined;
      const keyContentRef = await this.replaceCredential(
        existing?.keyContentRef,
        `sshkey-${localId}`,
        privateKey
      );
      const passphraseRef = passphrase
        ? await this.replaceCredential(
            existing?.passphraseRef,
            `sshkey-${localId}-pass`,
            passphrase
          )
        : await this.clearCredential(existing?.passphraseRef);
      const profile: SshKeyProfile = {
        id: localId,
        name: key.name,
        keyContentRef,
        passphraseRef,
        createdAt: existing?.createdAt ?? key.createdAt,
        updatedAt: key.updatedAt,
        resourceId: buildResourceId(scopeKey, key.uuid),
        uuidInScope: key.uuid,
        originKind: "cloud",
        originScopeKey: scopeKey,
        originWorkspaceId: workspace.id,
        copiedFromResourceId: existing?.copiedFromResourceId
      };
      this.deps.saveSshKey(profile);
      keyIdByUuid.set(key.uuid, localId);
      retainedKeyIds.add(localId);
    }

    for (const proxy of snapshot.proxies) {
      const existing = proxyByUuid.get(proxy.uuid);
      const localId = existing?.id ?? randomUUID();
      const password = proxy.password
        ? await decryptWorkspaceSecret(
            proxy.password as Parameters<typeof decryptWorkspaceSecret>[0],
            workspacePassword
          )
        : undefined;
      const credentialRef = password
        ? await this.replaceCredential(existing?.credentialRef, `proxy-${localId}`, password)
        : await this.clearCredential(existing?.credentialRef);
      const profile: ProxyProfile = {
        id: localId,
        name: proxy.name,
        proxyType: proxy.proxyType,
        host: proxy.host,
        port: proxy.port,
        username: proxy.username,
        credentialRef,
        createdAt: existing?.createdAt ?? proxy.createdAt,
        updatedAt: proxy.updatedAt,
        resourceId: buildResourceId(scopeKey, proxy.uuid),
        uuidInScope: proxy.uuid,
        originKind: "cloud",
        originScopeKey: scopeKey,
        originWorkspaceId: workspace.id,
        copiedFromResourceId: existing?.copiedFromResourceId
      };
      this.deps.saveProxy(profile);
      proxyIdByUuid.set(proxy.uuid, localId);
      retainedProxyIds.add(localId);
    }

    for (const connection of snapshot.connections) {
      const existing = connectionByUuid.get(connection.uuid);
      const localId = existing?.id ?? randomUUID();
      const password = connection.password
        ? await decryptWorkspaceSecret(
            connection.password as Parameters<typeof decryptWorkspaceSecret>[0],
            workspacePassword
          )
        : undefined;
      const credentialRef = password
        ? await this.replaceCredential(existing?.credentialRef, `conn-${localId}`, password)
        : await this.clearCredential(existing?.credentialRef);

      const profile: ConnectionProfile = {
        id: localId,
        name: connection.name,
        host: connection.host,
        port: connection.port,
        username: connection.username,
        authType: connection.authType,
        credentialRef,
        sshKeyId: connection.sshKeyUuid ? keyIdByUuid.get(connection.sshKeyUuid) : undefined,
        hostFingerprint: connection.hostFingerprint,
        strictHostKeyChecking: connection.strictHostKeyChecking,
        proxyId: connection.proxyUuid ? proxyIdByUuid.get(connection.proxyUuid) : undefined,
        keepAliveEnabled: connection.keepAliveEnabled,
        keepAliveIntervalSec: connection.keepAliveIntervalSec,
        terminalEncoding: connection.terminalEncoding,
        backspaceMode: connection.backspaceMode,
        deleteMode: connection.deleteMode,
        groupPath: normalizeWorkspaceGroupPath(workspace.workspaceName, connection.groupPath),
        tags: [...connection.tags],
        notes: connection.notes,
        favorite: connection.favorite,
        monitorSession: existing?.monitorSession ?? false,
        createdAt: existing?.createdAt ?? connection.createdAt,
        updatedAt: connection.updatedAt,
        lastConnectedAt: existing?.lastConnectedAt,
        resourceId: buildResourceId(scopeKey, connection.uuid),
        uuidInScope: connection.uuid,
        originKind: "cloud",
        originScopeKey: scopeKey,
        originWorkspaceId: workspace.id,
        sshKeyResourceId: connection.sshKeyUuid
          ? buildResourceId(scopeKey, connection.sshKeyUuid)
          : undefined,
        copiedFromResourceId: existing?.copiedFromResourceId
      };
      this.deps.saveConnection(profile);
      retainedConnectionIds.add(localId);
    }

    for (const connection of existingConnections) {
      if (retainedConnectionIds.has(connection.id)) {
        continue;
      }
      await this.clearCredential(connection.credentialRef);
      this.deps.removeConnection(connection.id);
    }
    for (const proxy of existingProxies) {
      if (retainedProxyIds.has(proxy.id)) {
        continue;
      }
      await this.clearCredential(proxy.credentialRef);
      this.deps.removeProxy(proxy.id);
    }
    for (const key of existingKeys) {
      if (retainedKeyIds.has(key.id)) {
        continue;
      }
      await this.clearCredential(key.keyContentRef);
      await this.clearCredential(key.passphraseRef);
      this.deps.removeSshKey(key.id);
    }
  }

  private mergeSnapshots(
    base: WorkspaceRepoSnapshot,
    local: WorkspaceRepoSnapshot,
    remote: WorkspaceRepoSnapshot
  ): { snapshot: WorkspaceRepoSnapshot; conflicts: WorkspaceRepoConflict[] } {
    const mergedSnapshot: WorkspaceRepoSnapshot = {
      workspaceId: local.workspaceId,
      snapshotId: "",
      createdAt: new Date().toISOString(),
      connections: [],
      sshKeys: [],
      proxies: []
    };
    const conflicts: WorkspaceRepoConflict[] = [];

    const mergeResourceType = <T extends ResourceSnapshotItem>(
      resourceType: ResourceType,
      getDisplayName: (item: T | undefined, fallbackId: string) => string,
      baseItems: T[],
      localItems: T[],
      remoteItems: T[]
    ): T[] => {
      const baseById = new Map(baseItems.map((item) => [item.uuid, item]));
      const localById = new Map(localItems.map((item) => [item.uuid, item]));
      const remoteById = new Map(remoteItems.map((item) => [item.uuid, item]));
      const ids = new Set([...baseById.keys(), ...localById.keys(), ...remoteById.keys()]);
      const merged: T[] = [];

      for (const id of [...ids].sort()) {
        const baseItem = baseById.get(id);
        const localItem = localById.get(id);
        const remoteItem = remoteById.get(id);

        if (!baseItem && localItem && !remoteItem) {
          merged.push(localItem);
          continue;
        }
        if (!baseItem && !localItem && remoteItem) {
          merged.push(remoteItem);
          continue;
        }
        if (!baseItem && localItem && remoteItem) {
          if (hashValue(localItem) === hashValue(remoteItem)) {
            merged.push(localItem);
          } else {
            merged.push(localItem);
            conflicts.push({
              workspaceId: local.workspaceId,
              resourceType,
              resourceId: id,
              displayName: getDisplayName(localItem, id),
              localSnapshotJson: JSON.stringify(localItem),
              remoteSnapshotJson: JSON.stringify(remoteItem),
              remoteDeleted: false,
              detectedAt: new Date().toISOString()
            });
          }
          continue;
        }
        if (!baseItem) {
          continue;
        }
        if (!localItem && !remoteItem) {
          continue;
        }
        if (localItem && remoteItem) {
          const localHash = hashValue(localItem);
          const remoteHash = hashValue(remoteItem);
          const baseHash = hashValue(baseItem);
          if (localHash === remoteHash) {
            merged.push(localItem);
          } else if (localHash === baseHash) {
            merged.push(remoteItem);
          } else if (remoteHash === baseHash) {
            merged.push(localItem);
          } else {
            merged.push(localItem);
            conflicts.push({
              workspaceId: local.workspaceId,
              resourceType,
              resourceId: id,
              displayName: getDisplayName(localItem, id),
              localSnapshotJson: JSON.stringify(localItem),
              remoteSnapshotJson: JSON.stringify(remoteItem),
              remoteDeleted: false,
              detectedAt: new Date().toISOString()
            });
          }
          continue;
        }
        if (!localItem && remoteItem) {
          if (hashValue(remoteItem) === hashValue(baseItem)) {
            continue;
          }
          conflicts.push({
            workspaceId: local.workspaceId,
            resourceType,
            resourceId: id,
            displayName: getDisplayName(remoteItem, id),
            localSnapshotJson: undefined,
            remoteSnapshotJson: JSON.stringify(remoteItem),
            remoteDeleted: false,
            detectedAt: new Date().toISOString()
          });
          continue;
        }
        if (localItem && !remoteItem) {
          if (hashValue(localItem) === hashValue(baseItem)) {
            continue;
          }
          merged.push(localItem);
          conflicts.push({
            workspaceId: local.workspaceId,
            resourceType,
            resourceId: id,
            displayName: getDisplayName(localItem, id),
            localSnapshotJson: JSON.stringify(localItem),
            remoteSnapshotJson: undefined,
            remoteDeleted: true,
            detectedAt: new Date().toISOString()
          });
        }
      }

      return merged;
    };

    mergedSnapshot.connections = mergeResourceType<ConnectionSnapshotItem>(
      "connection",
      (item, fallbackId) => item?.name || item?.host || fallbackId,
      base.connections,
      local.connections,
      remote.connections
    );
    mergedSnapshot.sshKeys = mergeResourceType<SshKeySnapshotItem>(
      "sshKey",
      (item, fallbackId) => item?.name || fallbackId,
      base.sshKeys,
      local.sshKeys,
      remote.sshKeys
    );
    mergedSnapshot.proxies = mergeResourceType<ProxySnapshotItem>(
      "proxy",
      (item, fallbackId) => item?.name || item?.host || fallbackId,
      base.proxies,
      local.proxies,
      remote.proxies
    );
    mergedSnapshot.snapshotId = hashValue({
      connections: mergedSnapshot.connections,
      sshKeys: mergedSnapshot.sshKeys,
      proxies: mergedSnapshot.proxies
    });
    return { snapshot: mergedSnapshot, conflicts };
  }

  private patchSnapshotWithConflictResolution(
    snapshot: WorkspaceRepoSnapshot,
    conflict: WorkspaceRepoConflict,
    strategy: "keep_local" | "accept_remote"
  ): WorkspaceRepoSnapshot {
    if (strategy === "keep_local") {
      return snapshot;
    }

    const patchCollection = <T extends ResourceSnapshotItem>(items: T[]): T[] => {
      const map = new Map(items.map((item) => [item.uuid, item]));
      if (conflict.remoteDeleted || !conflict.remoteSnapshotJson) {
        map.delete(conflict.resourceId);
      } else {
        map.set(conflict.resourceId, JSON.parse(conflict.remoteSnapshotJson) as T);
      }
      return [...map.values()].sort((left, right) => left.uuid.localeCompare(right.uuid));
    };

    const patched: WorkspaceRepoSnapshot = {
      ...snapshot,
      createdAt: new Date().toISOString()
    };
    if (conflict.resourceType === "connection") {
      patched.connections = patchCollection(snapshot.connections);
    } else if (conflict.resourceType === "sshKey") {
      patched.sshKeys = patchCollection(snapshot.sshKeys);
    } else {
      patched.proxies = patchCollection(snapshot.proxies);
    }
    patched.snapshotId = hashValue({
      connections: patched.connections,
      sshKeys: patched.sshKeys,
      proxies: patched.proxies
    });
    return patched;
  }

  private getWorkspaceOrThrow(workspaceId: string): CloudSyncWorkspaceProfile {
    const workspace = this.deps.listWorkspaces().find((item) => item.id === workspaceId);
    if (!workspace) {
      throw new Error(`Workspace not found: ${workspaceId}`);
    }
    return workspace;
  }

  private async getCredentials(
    workspace: CloudSyncWorkspaceProfile
  ): Promise<CloudSyncApiV3Credentials> {
    const workspacePassword = await this.getWorkspacePassword(workspace.id);
    if (!workspacePassword) {
      throw new Error("Workspace password not available");
    }
    return {
      apiBaseUrl: workspace.apiBaseUrl,
      workspaceName: workspace.workspaceName,
      workspacePassword,
      ignoreTlsErrors: workspace.ignoreTlsErrors,
      clientId: this.clientId,
      clientVersion: this.clientVersion
    };
  }

  private async getWorkspacePassword(workspaceId: string): Promise<string | undefined> {
    return this.deps.getWorkspacePassword(workspaceId);
  }

  private listWorkspaceConnections(workspaceId: string): ConnectionProfile[] {
    return this.deps
      .listConnections()
      .filter(
        (connection) =>
          connection.originKind === "cloud" && connection.originWorkspaceId === workspaceId
      );
  }

  private listWorkspaceSshKeys(workspaceId: string): SshKeyProfile[] {
    return this.deps
      .listSshKeys()
      .filter((key) => key.originKind === "cloud" && key.originWorkspaceId === workspaceId);
  }

  private listWorkspaceProxies(workspaceId: string): ProxyProfile[] {
    return this.deps
      .listProxies()
      .filter((proxy) => proxy.originKind === "cloud" && proxy.originWorkspaceId === workspaceId);
  }

  private async clearWorkspaceMaterializedData(workspaceId: string): Promise<void> {
    for (const connection of this.listWorkspaceConnections(workspaceId)) {
      await this.clearCredential(connection.credentialRef);
      this.deps.removeConnection(connection.id);
    }
    for (const proxy of this.listWorkspaceProxies(workspaceId)) {
      await this.clearCredential(proxy.credentialRef);
      this.deps.removeProxy(proxy.id);
    }
    for (const key of this.listWorkspaceSshKeys(workspaceId)) {
      await this.clearCredential(key.keyContentRef);
      await this.clearCredential(key.passphraseRef);
      this.deps.removeSshKey(key.id);
    }
    this.deps.replaceWorkspaceCommands(workspaceId, []);
    this.deps.saveWorkspaceCommandsVersion(workspaceId, "");
  }

  private async replaceCredential(
    existingRef: string | undefined,
    name: string,
    secret: string
  ): Promise<string> {
    if (existingRef) {
      await this.deps.deleteCredential(existingRef).catch(() => undefined);
    }
    return this.deps.storeCredential(name, secret);
  }

  private async clearCredential(ref: string | undefined): Promise<undefined> {
    if (ref) {
      await this.deps.deleteCredential(ref).catch(() => undefined);
    }
    return undefined;
  }

  private async encryptCredential(ref: string | undefined, workspacePassword: string, aad: string) {
    if (!ref) {
      return undefined;
    }
    const secret = await this.deps.readCredential(ref);
    if (!secret) {
      return undefined;
    }
    return encryptWorkspaceSecret(secret, workspacePassword, aad);
  }

  private async mustEncryptCredential(
    ref: string | undefined,
    workspacePassword: string,
    aad: string
  ) {
    const encrypted = await this.encryptCredential(ref, workspacePassword, aad);
    if (!encrypted) {
      throw new Error("Workspace asset is missing required credential data");
    }
    return encrypted;
  }

  private recordWorkspaceMutation(workspaceId: string): void {
    // The local snapshot is derived from the live DB at sync time, so a local
    // edit just needs to trigger a sync.
    void this.syncNow(workspaceId).catch(() => undefined);
  }

  private broadcastManagerStatus(): void {
    this.deps.broadcastStatus(this.getStatus());
  }
}
