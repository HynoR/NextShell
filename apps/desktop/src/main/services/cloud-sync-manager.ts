import { createHash, randomUUID } from "node:crypto";
import type { CloudSyncWorkspaceTokenDraft } from "@nextshell/shared";
import type {
  CloudSyncWorkspaceProfile,
  ConnectionProfile,
  ProxyProfile,
  RecycleBinEntry,
  SshKeyProfile,
  WorkspaceCommandItem,
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

export type CloudSyncSyncMode = "cloud-wins" | "local-wins";

type WorkspaceRuntime = {
  timer?: ReturnType<typeof setTimeout>;
  syncing: boolean;
  lastManualSyncAt: number;
  diverged: boolean;
};

type WorkspaceSyncResult = WorkspaceRepoLocalState & {
  syncState: "synced" | "diverged";
};

type ConnectionSnapshotItem = WorkspaceRepoSnapshot["connections"][number];
type SshKeySnapshotItem = WorkspaceRepoSnapshot["sshKeys"][number];
type ProxySnapshotItem = WorkspaceRepoSnapshot["proxies"][number];

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

  listWorkspaceCommands: (workspaceId: string) => WorkspaceCommandItem[];
  replaceWorkspaceCommands: (workspaceId: string, commands: WorkspaceCommandItem[]) => void;

  saveRecycleBinEntry: (entry: RecycleBinEntry) => void;

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

const compareFingerprintItems = (
  left: [string, string, string],
  right: [string, string, string]
): number =>
  left[0].localeCompare(right[0]) ||
  left[1].localeCompare(right[1]) ||
  left[2].localeCompare(right[2]);

const fingerprintItem = (type: string, id: string, updatedAt: string): [string, string, string] => [
  type,
  id,
  updatedAt
];

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

const makeDefaultLocalState = (workspaceId: string): WorkspaceRepoLocalState => ({ workspaceId });

const persistableLocalState = ({
  syncState: _syncState,
  ...localState
}: WorkspaceSyncResult): WorkspaceRepoLocalState => localState;

const toStatusState = (
  localState: WorkspaceRepoLocalState | undefined,
  syncing: boolean,
  enabled: boolean,
  diverged: boolean,
  lastError?: string
): WorkspaceRepoStatus["state"] => {
  if (!enabled) return "disabled";
  if (syncing) return "syncing";
  if (lastError) return "error";
  if (diverged) return "diverged";
  if (!localState || !localState.lastSyncAt) return "idle";
  return "synced";
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
    this.deps.saveWorkspaceRepoLocalState(makeDefaultLocalState(id));
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
      this.deps.getWorkspaceRepoLocalState(workspace.id) ?? makeDefaultLocalState(workspace.id);
    this.deps.saveWorkspaceRepoLocalState(localState);

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

  async syncNow(workspaceId?: string, mode?: CloudSyncSyncMode): Promise<void> {
    if (workspaceId) {
      const workspace = this.getWorkspaceOrThrow(workspaceId);
      await this.syncWorkspace(workspace, mode);
      return;
    }

    const workspaces = this.deps.listWorkspaces().filter((workspace) => workspace.enabled);
    await Promise.allSettled(workspaces.map((workspace) => this.syncWorkspace(workspace, mode)));
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
    void this.syncNow(workspaceId).catch(() => undefined);
  }

  private startRuntime(workspace: CloudSyncWorkspaceProfile): void {
    if (this.disposed) {
      return;
    }

    const runtime = this.runtimes.get(workspace.id) ?? {
      syncing: false,
      lastManualSyncAt: 0,
      diverged: false
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
    return {
      workspaceId: workspace.id,
      state: toStatusState(
        localState,
        runtime?.syncing ?? false,
        workspace.enabled,
        runtime?.diverged ?? false,
        localState?.lastError ?? workspace.lastError ?? undefined
      ),
      lastSyncAt: localState?.lastSyncAt ?? workspace.lastSyncAt ?? undefined,
      lastError: localState?.lastError ?? workspace.lastError ?? undefined,
      commandsVersion: localState?.remoteCommandsVersion
    };
  }

  private ensureWorkspaceBootstrapped(workspaceId: string): void {
    const workspace = this.deps.listWorkspaces().find((item) => item.id === workspaceId);
    if (!workspace) {
      return;
    }
    if (!this.deps.getWorkspaceRepoLocalState(workspaceId)) {
      this.deps.saveWorkspaceRepoLocalState(makeDefaultLocalState(workspaceId));
    }
  }

  private async syncWorkspace(
    workspace: CloudSyncWorkspaceProfile,
    mode?: CloudSyncSyncMode
  ): Promise<void> {
    if (!workspace.enabled) {
      return;
    }

    const runtime = this.runtimes.get(workspace.id) ?? {
      syncing: false,
      lastManualSyncAt: 0,
      diverged: false
    };
    this.runtimes.set(workspace.id, runtime);
    if (runtime.syncing) {
      return;
    }
    if (!mode && Date.now() - runtime.lastManualSyncAt < SYNC_NOW_MIN_INTERVAL_MS) {
      return;
    }

    runtime.syncing = true;
    runtime.lastManualSyncAt = Date.now();
    this.broadcastManagerStatus();

    try {
      this.ensureWorkspaceBootstrapped(workspace.id);
      const credentials = await this.getCredentials(workspace);
      const localState =
        this.deps.getWorkspaceRepoLocalState(workspace.id) ?? makeDefaultLocalState(workspace.id);
      const resolve = await this.api.resolve(credentials);
      const remoteVersion = resolve.headCommitId ?? undefined;
      const commandsVersion = resolve.commandsVersion ?? undefined;
      if (
        !mode &&
        (this.hasResourceDivergence(workspace.id, localState, remoteVersion) ||
          this.hasCommandDivergence(workspace.id, localState, commandsVersion))
      ) {
        runtime.diverged = true;
        this.deps.saveWorkspaceRepoLocalState({
          ...localState,
          lastError: undefined
        });
        this.deps.saveWorkspace({ ...workspace, lastError: null });
        return;
      }
      const repoState = await this.syncWorkspaceRepo(
        workspace,
        credentials,
        localState,
        remoteVersion,
        mode
      );
      if (repoState.syncState === "diverged") {
        runtime.diverged = true;
        this.deps.saveWorkspaceRepoLocalState({
          ...persistableLocalState(repoState),
          lastError: undefined
        });
        this.deps.saveWorkspace({ ...workspace, lastError: null });
        return;
      }
      const commandState = await this.syncWorkspaceCommands(
        workspace,
        credentials,
        repoState,
        commandsVersion,
        mode
      );
      if (commandState.syncState === "diverged") {
        runtime.diverged = true;
        this.deps.saveWorkspaceRepoLocalState({
          ...persistableLocalState(commandState),
          lastError: undefined
        });
        this.deps.saveWorkspace({ ...workspace, lastError: null });
        return;
      }

      const syncedAt = new Date().toISOString();
      const nextLocalState: WorkspaceRepoLocalState = {
        ...persistableLocalState(commandState),
        lastSyncAt: syncedAt,
        lastError: undefined
      };
      runtime.diverged = false;
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
        this.deps.getWorkspaceRepoLocalState(workspace.id) ?? makeDefaultLocalState(workspace.id);
      this.deps.saveWorkspaceRepoLocalState({
        ...localState,
        lastError: message
      });
      runtime.diverged = false;
      this.deps.saveWorkspace({
        ...workspace,
        lastError: message
      });
    } finally {
      runtime.syncing = false;
      this.broadcastManagerStatus();
    }
  }

  private hasResourceDivergence(
    workspaceId: string,
    localState: WorkspaceRepoLocalState,
    remoteVersion?: string
  ): boolean {
    const hasBaseline =
      localState.localFingerprint !== undefined || localState.remoteVersion !== undefined;
    return (
      hasBaseline &&
      this.workspaceFingerprint(workspaceId) !== localState.localFingerprint &&
      (remoteVersion ?? undefined) !== (localState.remoteVersion ?? undefined)
    );
  }

  private hasCommandDivergence(
    workspaceId: string,
    localState: WorkspaceRepoLocalState,
    remoteVersion?: string
  ): boolean {
    const hasBaseline =
      localState.localCommandsFingerprint !== undefined ||
      localState.remoteCommandsVersion !== undefined;
    return (
      hasBaseline &&
      this.workspaceCommandsFingerprint(workspaceId) !== localState.localCommandsFingerprint &&
      (remoteVersion ?? undefined) !== (localState.remoteCommandsVersion ?? undefined)
    );
  }

  private async syncWorkspaceRepo(
    workspace: CloudSyncWorkspaceProfile,
    credentials: CloudSyncApiV3Credentials,
    localState: WorkspaceRepoLocalState,
    remoteVersion?: string,
    mode?: CloudSyncSyncMode
  ): Promise<WorkspaceSyncResult> {
    const localFingerprint = this.workspaceFingerprint(workspace.id);
    const hasBaseline =
      localState.localFingerprint !== undefined || localState.remoteVersion !== undefined;
    const localChanged = hasBaseline && localFingerprint !== localState.localFingerprint;
    const remoteChanged =
      !hasBaseline || (remoteVersion ?? undefined) !== (localState.remoteVersion ?? undefined);

    if (!localChanged && !remoteChanged) {
      return { ...localState, localFingerprint, syncState: "synced" };
    }

    if (localChanged && remoteChanged) {
      if (!mode) {
        return { ...localState, syncState: "diverged" };
      }
      if (mode === "cloud-wins") {
        return this.pullRemoteHead(workspace, credentials, localState, remoteVersion);
      }
    }

    if (remoteChanged && !localChanged) {
      return this.pullRemoteHead(workspace, credentials, localState, remoteVersion);
    }

    const localSnapshot = await this.buildWorkspaceSnapshot(
      workspace,
      credentials.workspacePassword
    );
    return this.pushLocalHead(
      workspace,
      credentials,
      localState,
      localSnapshot,
      localFingerprint,
      mode
    );
  }

  private async pullRemoteHead(
    workspace: CloudSyncWorkspaceProfile,
    credentials: CloudSyncApiV3Credentials,
    localState: WorkspaceRepoLocalState,
    remoteVersion?: string
  ): Promise<WorkspaceSyncResult> {
    const response = await this.api.pull(credentials, localState.remoteVersion);
    const nextRemoteVersion = response.headCommitId ?? remoteVersion;
    if (response.unchanged || !response.snapshot) {
      return {
        ...localState,
        remoteVersion: nextRemoteVersion,
        localFingerprint: this.workspaceFingerprint(workspace.id),
        syncState: "synced"
      };
    }
    const remoteSnapshot: WorkspaceRepoSnapshot = {
      ...response.snapshot,
      workspaceId: workspace.id
    };
    await this.applyWorkspaceSnapshot(workspace, credentials.workspacePassword, remoteSnapshot);
    return {
      ...localState,
      remoteVersion: nextRemoteVersion,
      localFingerprint: this.workspaceFingerprint(workspace.id),
      syncState: "synced"
    };
  }

  private async pushLocalHead(
    workspace: CloudSyncWorkspaceProfile,
    credentials: CloudSyncApiV3Credentials,
    localState: WorkspaceRepoLocalState,
    localSnapshot: WorkspaceRepoSnapshot,
    localFingerprint: string,
    mode?: CloudSyncSyncMode
  ): Promise<WorkspaceSyncResult> {
    let baseHeadCommitId = localState.remoteVersion ?? null;
    for (let attempt = 0; attempt < 3; attempt += 1) {
      const response = await this.api.push(credentials, {
        baseHeadCommitId,
        snapshot: localSnapshot
      });
      if (response.status === "accepted") {
        return {
          ...localState,
          remoteVersion: response.headCommitId,
          localFingerprint,
          syncState: "synced"
        };
      }

      if (mode === "cloud-wins") {
        await this.applyWorkspaceSnapshot(workspace, credentials.workspacePassword, {
          ...response.snapshot,
          workspaceId: workspace.id
        });
        return {
          ...localState,
          remoteVersion: response.headCommitId ?? undefined,
          localFingerprint: this.workspaceFingerprint(workspace.id),
          syncState: "synced"
        };
      }
      if (mode !== "local-wins") {
        return { ...localState, syncState: "diverged" };
      }
      baseHeadCommitId = response.headCommitId ?? null;
    }

    throw new Error("云端持续发生变化，已停止本地优先重试");
  }

  private async syncWorkspaceCommands(
    workspace: CloudSyncWorkspaceProfile,
    credentials: CloudSyncApiV3Credentials,
    localState: WorkspaceRepoLocalState,
    resolvedRemoteVersion?: string,
    mode?: CloudSyncSyncMode
  ): Promise<WorkspaceSyncResult> {
    const localCommandsFingerprint = this.workspaceCommandsFingerprint(workspace.id);
    const hasBaseline =
      localState.localCommandsFingerprint !== undefined ||
      localState.remoteCommandsVersion !== undefined;
    const localChanged =
      hasBaseline && localCommandsFingerprint !== localState.localCommandsFingerprint;
    const remoteChanged =
      !hasBaseline ||
      (resolvedRemoteVersion ?? undefined) !== (localState.remoteCommandsVersion ?? undefined);

    if (!localChanged && !remoteChanged) {
      return { ...localState, localCommandsFingerprint, syncState: "synced" };
    }
    if (localChanged && remoteChanged && !mode) {
      return { ...localState, syncState: "diverged" };
    }

    if (remoteChanged && (!localChanged || mode === "cloud-wins")) {
      const response = await this.api.pullCommands(
        credentials,
        localState.remoteCommandsVersion ?? null
      );
      if (response.status === "changed") {
        this.deps.replaceWorkspaceCommands(
          workspace.id,
          response.commands.map((command) => ({ ...command, workspaceId: workspace.id }))
        );
      }
      return {
        ...localState,
        remoteCommandsVersion: response.version,
        localCommandsFingerprint: this.workspaceCommandsFingerprint(workspace.id),
        syncState: "synced"
      };
    }

    const response = await this.api.pushCommands(
      credentials,
      this.deps.listWorkspaceCommands(workspace.id).map((command) => ({
        ...command,
        workspaceId: workspace.id
      }))
    );
    return {
      ...localState,
      remoteCommandsVersion: response.version,
      localCommandsFingerprint,
      syncState: "synced"
    };
  }

  private workspaceFingerprint(workspaceId: string): string {
    const items: Array<[string, string, string]> = [
      ...this.listWorkspaceConnections(workspaceId).map((item) =>
        fingerprintItem("connection", item.uuidInScope ?? item.id, item.updatedAt)
      ),
      ...this.listWorkspaceSshKeys(workspaceId).map((item) =>
        fingerprintItem("sshKey", item.uuidInScope ?? item.id, item.updatedAt)
      ),
      ...this.listWorkspaceProxies(workspaceId).map((item) =>
        fingerprintItem("proxy", item.uuidInScope ?? item.id, item.updatedAt)
      )
    ];
    return hashValue(items.sort(compareFingerprintItems));
  }

  private workspaceCommandsFingerprint(workspaceId: string): string {
    const items: Array<[string, string, string]> = this.deps
      .listWorkspaceCommands(workspaceId)
      .map((command) => fingerprintItem("command", command.id, command.updatedAt));
    return hashValue(items.sort(compareFingerprintItems));
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
        // folderId 不在线协议里,是本地的目录归属。`saveConnection` 是全行 upsert
        // (`folder_id = connection.folderId ?? null`),这里不兜底的话每次 pull 都会把云连接
        // 的目录清空,连接在树上掉回根,而 groupPath 还写着目录名——两边直接分叉。
        folderId: existing?.folderId,
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
      await this.saveRemoteDeletedConnection(connection);
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
      await this.saveRemoteDeletedSshKey(key);
      await this.clearCredential(key.keyContentRef);
      await this.clearCredential(key.passphraseRef);
      this.deps.removeSshKey(key.id);
    }
  }

  private async saveRemoteDeletedConnection(connection: ConnectionProfile): Promise<void> {
    const snapshotData: Record<string, unknown> = { ...connection };
    if (connection.credentialRef) {
      const password = await this.deps
        .readCredential(connection.credentialRef)
        .catch(() => undefined);
      if (password) snapshotData._savedCredential = password;
    }
    this.deps.saveRecycleBinEntry({
      id: randomUUID(),
      resourceType: "server",
      displayName: connection.name || connection.host,
      originalResourceId:
        connection.resourceId ??
        buildResourceId(
          connection.originScopeKey ?? LOCAL_DEFAULT_SCOPE_KEY,
          connection.uuidInScope ?? connection.id
        ),
      originalScopeKey: connection.originScopeKey ?? LOCAL_DEFAULT_SCOPE_KEY,
      reason: "delete",
      snapshotJson: JSON.stringify(snapshotData),
      createdAt: new Date().toISOString()
    });
  }

  private async saveRemoteDeletedSshKey(key: SshKeyProfile): Promise<void> {
    const snapshotData: Record<string, unknown> = { ...key };
    if (key.keyContentRef) {
      const content = await this.deps.readCredential(key.keyContentRef).catch(() => undefined);
      if (content) snapshotData._savedKeyContent = content;
    }
    if (key.passphraseRef) {
      const passphrase = await this.deps.readCredential(key.passphraseRef).catch(() => undefined);
      if (passphrase) snapshotData._savedPassphrase = passphrase;
    }
    this.deps.saveRecycleBinEntry({
      id: randomUUID(),
      resourceType: "sshKey",
      displayName: key.name,
      originalResourceId:
        key.resourceId ??
        buildResourceId(key.originScopeKey ?? LOCAL_DEFAULT_SCOPE_KEY, key.uuidInScope ?? key.id),
      originalScopeKey: key.originScopeKey ?? LOCAL_DEFAULT_SCOPE_KEY,
      reason: "delete",
      snapshotJson: JSON.stringify(snapshotData),
      createdAt: new Date().toISOString()
    });
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
