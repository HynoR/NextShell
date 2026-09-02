/**
 * ResourceOperationsService — implements copy-first semantics, danger move,
 * delete-to-recycle-bin, and recycle bin restore/purge.
 *
 * Key design principles from goal-sync docs:
 * 1. Copy is the primary operation, preserving source and creating new identity.
 * 2. Move is dangerous: copy + delete source (both sides).
 * 3. Delete always goes through recycle bin first.
 * 4. Recycle bin restore creates a new copy (never overwrite).
 * 5. Physical purge only from recycle bin secondary delete.
 */

import { randomUUID } from "node:crypto";
import type {
  ConnectionProfile,
  ProxyProfile,
  SshKeyProfile,
  RecycleBinEntry,
  OriginKind
} from "@nextshell/core";
import { buildResourceId, buildScopeKey, LOCAL_DEFAULT_SCOPE_KEY } from "@nextshell/core";
import { deriveGroupPath, resolveFolderNames } from "@nextshell/shared";
import type { ResourceCopyConnectionInput } from "@nextshell/shared";
import type { CloudSyncManager } from "./cloud-sync-manager";
import type { EncryptedSecretVault } from "@nextshell/security";
import type {
  CachedConnectionRepository,
  CachedProxyRepository,
  CachedSshKeyRepository,
  ConnectionFolderRepository
} from "@nextshell/storage";

// ── Input types ─────────────────────────────────────────────────────────────

/**
 * 复制入参 = IPC 契约本体(`resourceCopyConnectionSchema` 的 z.infer)。
 *
 * 以前这里是一份手抄的同名接口,契约加字段时它不会报错,新字段在服务里被静默丢掉。
 */
export type CopyConnectionInput = ResourceCopyConnectionInput;

export interface DeleteConnectionInput {
  id: string;
}

export interface DeleteSshKeyInput {
  id: string;
  force?: boolean;
}

export interface RestoreFromRecycleBinInput {
  recycleBinEntryId: string;
  /** 省略时按条目自身的来源范围恢复,而不是一律落到本地。 */
  targetOriginKind?: OriginKind;
  targetWorkspaceId?: string;
}

// ── Dependencies ────────────────────────────────────────────────────────────

export interface ResourceOperationsDeps {
  connections: CachedConnectionRepository;
  sshKeyRepo: CachedSshKeyRepository;
  proxyRepo: CachedProxyRepository;
  connectionFolders: ConnectionFolderRepository;
  vault: EncryptedSecretVault;
  cloudSyncManager: CloudSyncManager | undefined;
  saveRecycleBinEntry: (entry: RecycleBinEntry) => void;
  listRecycleBinEntries: () => RecycleBinEntry[];
  removeRecycleBinEntry: (id: string) => void;
}

// ── Service ─────────────────────────────────────────────────────────────────

export class ResourceOperationsService {
  constructor(private readonly deps: ResourceOperationsDeps) {}

  // ── Copy Connection ─────────────────────────────────────────────────

  /**
   * Copy a connection to a target origin. SSH keys are auto-copied
   * if they don't already exist in the target scope.
   */
  async copyConnection(input: CopyConnectionInput): Promise<ConnectionProfile> {
    const { connections, vault } = this.deps;

    const source = connections.getById(input.sourceId);
    if (!source) throw new Error(`Source connection not found: ${input.sourceId}`);

    const targetScope = this.resolveScope(input.targetOriginKind, input.targetWorkspaceId);
    const newUuid = randomUUID();
    const newResourceId = buildResourceId(targetScope.scopeKey, newUuid);

    // Handle SSH key dependency: auto-copy if needed
    let newSshKeyId = source.sshKeyId;
    if (source.sshKeyId && source.authType === "privateKey") {
      newSshKeyId = await this.ensureSshKeyInScope(source.sshKeyId, targetScope);
    }

    let newProxyId = source.proxyId;
    if (source.proxyId) {
      newProxyId = await this.ensureProxyInScope(source.proxyId, targetScope);
    }

    // Copy credential (password) if applicable
    let credentialRef: string | undefined;
    if (source.credentialRef) {
      const password = await vault.readCredential(source.credentialRef);
      if (password) {
        credentialRef = await vault.storeCredential(`conn-${newUuid}`, password);
      }
    }

    const target = this.resolveCopyTarget(input, targetScope);

    const now = new Date().toISOString();
    const copied: ConnectionProfile = {
      id: newUuid,
      name: source.name,
      host: source.host,
      port: source.port,
      username: source.username,
      authType: source.authType,
      credentialRef,
      strictHostKeyChecking: source.strictHostKeyChecking,
      sshKeyId: newSshKeyId,
      proxyId: newProxyId,
      groupPath: target.groupPath,
      // 源目录属于源作用域,原样带过来会指向目标域里根本不存在的目录 id。
      folderId: target.folderId,
      tags: [...(source.tags ?? [])],
      notes: source.notes,
      favorite: false,
      monitorSession: false,
      terminalEncoding: source.terminalEncoding ?? "utf-8",
      backspaceMode: source.backspaceMode ?? "ascii-backspace",
      deleteMode: source.deleteMode ?? "vt220-delete",
      createdAt: now,
      updatedAt: now,
      // Origin fields
      resourceId: newResourceId,
      uuidInScope: newUuid,
      originKind: input.targetOriginKind,
      originScopeKey: targetScope.scopeKey,
      originWorkspaceId: input.targetWorkspaceId,
      copiedFromResourceId:
        source.resourceId ??
        buildResourceId(
          source.originScopeKey ?? LOCAL_DEFAULT_SCOPE_KEY,
          source.uuidInScope ?? source.id
        )
    };

    connections.save(copied);

    // If target is cloud, notify CloudSyncManager to push
    if (input.targetOriginKind === "cloud" && this.deps.cloudSyncManager) {
      this.deps.cloudSyncManager.pushConnectionUpsert(copied);
    }

    return copied;
  }

  // ── Delete Connection ───────────────────────────────────────────────

  /**
   * Delete a connection: move to recycle bin first.
   * If cloud-origin, also send tombstone via CloudSyncManager.
   */
  async deleteConnection(
    input: DeleteConnectionInput,
    reason: RecycleBinEntry["reason"] = "delete"
  ): Promise<void> {
    const { connections, vault, cloudSyncManager } = this.deps;

    const conn = connections.getById(input.id);
    if (!conn) throw new Error(`Connection not found: ${input.id}`);

    // Step 1: Snapshot to recycle bin (with embedded credential for restore)
    const snapshotData: Record<string, unknown> = { ...conn };
    if (conn.credentialRef) {
      try {
        const password = await vault.readCredential(conn.credentialRef);
        if (password) snapshotData._savedCredential = password;
      } catch {
        /* best effort */
      }
    }

    const entry: RecycleBinEntry = {
      id: randomUUID(),
      resourceType: "server",
      displayName: conn.name || conn.host,
      originalResourceId:
        conn.resourceId ??
        buildResourceId(
          conn.originScopeKey ?? LOCAL_DEFAULT_SCOPE_KEY,
          conn.uuidInScope ?? conn.id
        ),
      originalScopeKey: conn.originScopeKey ?? LOCAL_DEFAULT_SCOPE_KEY,
      reason,
      snapshotJson: JSON.stringify(snapshotData),
      createdAt: new Date().toISOString()
    };

    this.deps.saveRecycleBinEntry(entry);

    // Step 2: Remove from active connections
    connections.remove(input.id);

    // Step 3: If cloud origin, push delete tombstone
    if (conn.originKind === "cloud" && conn.originWorkspaceId && cloudSyncManager) {
      cloudSyncManager.pushConnectionDelete(conn);
    }

    // Step 4: Clean up credential
    if (conn.credentialRef) {
      await vault.deleteCredential(conn.credentialRef).catch(() => {});
    }
  }

  // ── Delete SSH Key ──────────────────────────────────────────────────

  /**
   * Delete an SSH key: move to recycle bin first.
   * If cloud-origin, also send tombstone via CloudSyncManager.
   */
  async deleteSshKey(input: DeleteSshKeyInput): Promise<void> {
    const { sshKeyRepo, vault, cloudSyncManager } = this.deps;

    const key = sshKeyRepo.getById(input.id);
    if (!key) throw new Error(`SSH key not found: ${input.id}`);

    // Check references (skip if forced)
    if (!input.force) {
      const refs = sshKeyRepo.getReferencingConnectionIds(input.id);
      if (refs.length > 0) {
        throw new Error(
          `SSH key "${key.name}" is still referenced by ${refs.length} connection(s). Use force=true to delete anyway.`
        );
      }
    }

    // Step 1: Snapshot to recycle bin (with embedded credentials for restore)
    const snapshotData: Record<string, unknown> = { ...key };
    if (key.keyContentRef) {
      try {
        const content = await vault.readCredential(key.keyContentRef);
        if (content) snapshotData._savedKeyContent = content;
      } catch {
        /* best effort */
      }
    }
    if (key.passphraseRef) {
      try {
        const pass = await vault.readCredential(key.passphraseRef);
        if (pass) snapshotData._savedPassphrase = pass;
      } catch {
        /* best effort */
      }
    }

    const entry: RecycleBinEntry = {
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
    };

    this.deps.saveRecycleBinEntry(entry);

    // Step 2: Remove from active keys
    sshKeyRepo.remove(input.id);

    // Step 3: If cloud origin, push delete tombstone
    if (key.originKind === "cloud" && key.originWorkspaceId && cloudSyncManager) {
      cloudSyncManager.pushSshKeyDelete(key);
    }

    // Step 4: Clean up credentials
    if (key.keyContentRef) await vault.deleteCredential(key.keyContentRef).catch(() => {});
    if (key.passphraseRef) await vault.deleteCredential(key.passphraseRef).catch(() => {});
  }

  // ── Recycle Bin Operations ──────────────────────────────────────────

  /**
   * Restore from recycle bin as a NEW copy (never overwrite existing).
   */
  async restoreFromRecycleBin(
    input: RestoreFromRecycleBinInput
  ): Promise<ConnectionProfile | SshKeyProfile> {
    const entries = this.deps.listRecycleBinEntries();
    const entry = entries.find((e) => e.id === input.recycleBinEntryId);
    if (!entry) throw new Error(`Recycle bin entry not found: ${input.recycleBinEntryId}`);

    const snapshot = JSON.parse(entry.snapshotJson) as Record<string, unknown>;
    const targetScope = input.targetOriginKind
      ? this.resolveScope(input.targetOriginKind, input.targetWorkspaceId)
      : this.resolveOriginalScope(entry.originalScopeKey);
    const newUuid = randomUUID();
    const newResourceId = buildResourceId(targetScope.scopeKey, newUuid);
    const now = new Date().toISOString();

    if (entry.resourceType === "server") {
      const restoreRoot = this.restoreRootPath(targetScope);

      // Restore credential from snapshot if available
      let credentialRef: string | undefined;
      if (typeof snapshot._savedCredential === "string" && snapshot._savedCredential) {
        credentialRef = await this.deps.vault.storeCredential(
          `conn-${newUuid}`,
          snapshot._savedCredential
        );
      }

      // Check SSH key dependency
      let sshKeyId: string | undefined;
      if (typeof snapshot.sshKeyId === "string" && snapshot.sshKeyId) {
        const keyExists = this.deps.sshKeyRepo.getById(snapshot.sshKeyId);
        if (keyExists) {
          sshKeyId = snapshot.sshKeyId;
        }
        // If key doesn't exist, leave sshKeyId undefined (user must re-attach)
      }

      let proxyId: string | undefined;
      if (typeof snapshot.proxyId === "string" && snapshot.proxyId) {
        const proxyExists = this.deps.proxyRepo.getById(snapshot.proxyId);
        if (proxyExists) {
          proxyId = snapshot.proxyId;
        }
      }

      const restored: ConnectionProfile = {
        id: newUuid,
        name: String(snapshot.name ?? ""),
        host: String(snapshot.host ?? ""),
        port: Number(snapshot.port ?? 22),
        username: String(snapshot.username ?? "root"),
        authType: (snapshot.authType as ConnectionProfile["authType"]) ?? "password",
        credentialRef,
        sshKeyId,
        proxyId,
        strictHostKeyChecking: Boolean(snapshot.strictHostKeyChecking),
        groupPath: restoreRoot,
        tags: Array.isArray(snapshot.tags)
          ? snapshot.tags.filter((t): t is string => typeof t === "string")
          : [],
        notes: typeof snapshot.notes === "string" ? snapshot.notes : undefined,
        favorite: false,
        monitorSession: false,
        terminalEncoding: "utf-8",
        backspaceMode: "ascii-backspace",
        deleteMode: "vt220-delete",
        createdAt: now,
        updatedAt: now,
        resourceId: newResourceId,
        uuidInScope: newUuid,
        originKind: targetScope.originKind,
        originScopeKey: targetScope.scopeKey,
        originWorkspaceId: input.targetWorkspaceId,
        copiedFromResourceId: entry.originalResourceId
      };

      this.deps.connections.save(restored);

      if (input.targetOriginKind === "cloud" && this.deps.cloudSyncManager) {
        this.deps.cloudSyncManager.pushConnectionUpsert(restored);
      }

      // Remove from recycle bin
      this.deps.removeRecycleBinEntry(entry.id);

      return restored;
    } else {
      // SSH key restore — re-store credentials from snapshot
      let keyContentRef = String(snapshot.keyContentRef ?? "");
      if (typeof snapshot._savedKeyContent === "string" && snapshot._savedKeyContent) {
        keyContentRef = await this.deps.vault.storeCredential(
          `sshkey-${newUuid}`,
          snapshot._savedKeyContent
        );
      }

      let passphraseRef: string | undefined;
      if (typeof snapshot._savedPassphrase === "string" && snapshot._savedPassphrase) {
        passphraseRef = await this.deps.vault.storeCredential(
          `sshkey-${newUuid}-pass`,
          snapshot._savedPassphrase
        );
      } else if (typeof snapshot.passphraseRef === "string") {
        passphraseRef = snapshot.passphraseRef;
      }

      const restored: SshKeyProfile = {
        id: newUuid,
        name: String(snapshot.name ?? ""),
        keyContentRef,
        passphraseRef,
        createdAt: now,
        updatedAt: now,
        resourceId: newResourceId,
        uuidInScope: newUuid,
        originKind: input.targetOriginKind,
        originScopeKey: targetScope.scopeKey,
        originWorkspaceId: input.targetWorkspaceId,
        copiedFromResourceId: entry.originalResourceId
      };

      this.deps.sshKeyRepo.save(restored);

      if (input.targetOriginKind === "cloud" && this.deps.cloudSyncManager) {
        this.deps.cloudSyncManager.pushSshKeyUpsert(restored);
      }

      // Remove from recycle bin
      this.deps.removeRecycleBinEntry(entry.id);

      return restored;
    }
  }

  /**
   * Physical purge: permanently delete a recycle bin entry.
   * Only from secondary delete in recycle bin view.
   */
  purgeRecycleBinEntry(id: string): void {
    this.deps.removeRecycleBinEntry(id);
  }

  // ── Private helpers ─────────────────────────────────────────────────

  /**
   * Ensure an SSH key exists in the target scope.
   * If it already exists (same origin), return its ID.
   * If not, copy it and return the new ID.
   */
  private async ensureSshKeyInScope(
    sourceKeyId: string,
    targetScope: { scopeKey: string; originKind: OriginKind; workspaceId?: string }
  ): Promise<string> {
    const { sshKeyRepo, vault } = this.deps;

    const sourceKey = sshKeyRepo.getById(sourceKeyId);
    if (!sourceKey) throw new Error(`Source SSH key not found: ${sourceKeyId}`);

    // If source key already belongs to target scope, reuse it
    const sourceScopeKey = sourceKey.originScopeKey ?? LOCAL_DEFAULT_SCOPE_KEY;
    if (sourceScopeKey === targetScope.scopeKey) {
      return sourceKeyId;
    }

    // Check if we already have a copy of this key in the target scope
    const allKeys = sshKeyRepo.list();
    const existingCopy = allKeys.find(
      (k) =>
        k.originScopeKey === targetScope.scopeKey &&
        k.copiedFromResourceId ===
          (sourceKey.resourceId ??
            buildResourceId(sourceScopeKey, sourceKey.uuidInScope ?? sourceKey.id))
    );
    if (existingCopy) return existingCopy.id;

    // Copy the key
    const newUuid = randomUUID();
    const newResourceId = buildResourceId(targetScope.scopeKey, newUuid);

    // Copy key content via vault
    let keyContentRef = sourceKey.keyContentRef;
    if (sourceKey.keyContentRef) {
      const content = await vault.readCredential(sourceKey.keyContentRef);
      if (content) {
        keyContentRef = await vault.storeCredential(`sshkey-${newUuid}`, content);
      }
    }

    let passphraseRef = sourceKey.passphraseRef;
    if (sourceKey.passphraseRef) {
      const pass = await vault.readCredential(sourceKey.passphraseRef);
      if (pass) {
        passphraseRef = await vault.storeCredential(`sshkey-${newUuid}-pass`, pass);
      }
    }

    const now = new Date().toISOString();
    const copiedKey: SshKeyProfile = {
      id: newUuid,
      name: sourceKey.name,
      keyContentRef,
      passphraseRef,
      createdAt: now,
      updatedAt: now,
      resourceId: newResourceId,
      uuidInScope: newUuid,
      originKind: targetScope.originKind,
      originScopeKey: targetScope.scopeKey,
      originWorkspaceId: targetScope.workspaceId,
      copiedFromResourceId:
        sourceKey.resourceId ??
        buildResourceId(sourceScopeKey, sourceKey.uuidInScope ?? sourceKey.id)
    };

    sshKeyRepo.save(copiedKey);

    // If target is cloud, push the key
    if (targetScope.originKind === "cloud" && this.deps.cloudSyncManager) {
      this.deps.cloudSyncManager.pushSshKeyUpsert(copiedKey);
    }

    return newUuid;
  }

  private async ensureProxyInScope(
    sourceProxyId: string,
    targetScope: { scopeKey: string; originKind: OriginKind; workspaceId?: string }
  ): Promise<string> {
    const { proxyRepo, vault } = this.deps;
    const sourceProxy = proxyRepo.getById(sourceProxyId);
    if (!sourceProxy) {
      throw new Error(`Source proxy not found: ${sourceProxyId}`);
    }

    const sourceScopeKey = sourceProxy.originScopeKey ?? LOCAL_DEFAULT_SCOPE_KEY;
    if (sourceScopeKey === targetScope.scopeKey) {
      return sourceProxyId;
    }

    const existingCopy = proxyRepo
      .list()
      .find(
        (proxy) =>
          proxy.originScopeKey === targetScope.scopeKey &&
          proxy.copiedFromResourceId ===
            (sourceProxy.resourceId ??
              buildResourceId(sourceScopeKey, sourceProxy.uuidInScope ?? sourceProxy.id))
      );
    if (existingCopy) {
      return existingCopy.id;
    }

    const newUuid = randomUUID();
    const newResourceId = buildResourceId(targetScope.scopeKey, newUuid);
    let credentialRef = sourceProxy.credentialRef;
    if (sourceProxy.credentialRef) {
      const password = await vault.readCredential(sourceProxy.credentialRef);
      if (password) {
        credentialRef = await vault.storeCredential(`proxy-${newUuid}`, password);
      }
    }

    const now = new Date().toISOString();
    const copiedProxy: ProxyProfile = {
      id: newUuid,
      name: sourceProxy.name,
      proxyType: sourceProxy.proxyType,
      host: sourceProxy.host,
      port: sourceProxy.port,
      username: sourceProxy.username,
      credentialRef,
      createdAt: now,
      updatedAt: now,
      resourceId: newResourceId,
      uuidInScope: newUuid,
      originKind: targetScope.originKind,
      originScopeKey: targetScope.scopeKey,
      originWorkspaceId: targetScope.workspaceId,
      copiedFromResourceId:
        sourceProxy.resourceId ??
        buildResourceId(sourceScopeKey, sourceProxy.uuidInScope ?? sourceProxy.id)
    };

    proxyRepo.save(copiedProxy);
    if (targetScope.originKind === "cloud" && this.deps.cloudSyncManager) {
      this.deps.cloudSyncManager.pushProxyUpsert(copiedProxy);
    }

    return copiedProxy.id;
  }

  /**
   * 按条目被删除时所在的来源范围恢复。目标 workspace 已被移除时退回本地——恢复不该因为
   * workspace 没了就失败,把资源还给用户比丢掉它重要。
   */
  private resolveOriginalScope(originalScopeKey: string): {
    scopeKey: string;
    originKind: OriginKind;
    workspaceId?: string;
  } {
    if (!originalScopeKey || originalScopeKey === LOCAL_DEFAULT_SCOPE_KEY) {
      return { scopeKey: LOCAL_DEFAULT_SCOPE_KEY, originKind: "local" };
    }
    const workspace = this.deps.cloudSyncManager?.listWorkspaces().find(
      (item) =>
        buildScopeKey({
          kind: "cloud",
          apiBaseUrl: item.apiBaseUrl,
          workspaceName: item.workspaceName
        }) === originalScopeKey
    );
    if (!workspace) {
      return { scopeKey: LOCAL_DEFAULT_SCOPE_KEY, originKind: "local" };
    }
    return { scopeKey: originalScopeKey, originKind: "cloud", workspaceId: workspace.id };
  }

  /**
   * 恢复目标的根路径。云资源必须落到 `/workspace/<slug>`:只写 `/workspace` 的话树会为它
   * 造出一个名为 "workspace" 的幽灵根节点。
   *
   * slug 规则只有一份:`deriveGroupPath`。workspace 查不到时它按本地根投影,与旧实现
   * 「找不到就退回 /server」一致。
   */
  private restoreRootPath(target: {
    scopeKey: string;
    originKind: OriginKind;
    workspaceId?: string;
  }): string {
    return deriveGroupPath({
      scopeKey: target.scopeKey,
      workspaceName: this.resolveWorkspaceName(target),
      folderNames: []
    });
  }

  /**
   * 复制落点。`targetFolderId` 必须属于**目标**作用域:拿本地目录去投影一条云连接会算出
   * `/server/...`,同步会把它看成换了分组;反过来则会给本地连接安上 `/workspace/<slug>`。
   * 所以这里校验目录归属并按目录链投影 groupPath;没传时才退回旧的路径字符串行为。
   */
  private resolveCopyTarget(
    input: CopyConnectionInput,
    targetScope: { scopeKey: string; originKind: OriginKind; workspaceId?: string }
  ): { groupPath: string; folderId?: string } {
    if (input.targetFolderId) {
      const folders = this.deps.connectionFolders.list(targetScope.scopeKey);
      if (!folders.some((folder) => folder.id === input.targetFolderId)) {
        throw new Error("目标目录不存在或不属于目标作用域");
      }
      return {
        folderId: input.targetFolderId,
        groupPath: deriveGroupPath({
          scopeKey: targetScope.scopeKey,
          workspaceName: this.resolveWorkspaceName(targetScope),
          folderNames: resolveFolderNames(input.targetFolderId, folders)
        })
      };
    }

    const subPath = input.targetGroupSubPath ?? "";
    if (!subPath) {
      // 复制到作用域根。云 scope 的根是 `/workspace/<slug>`,拼 `/workspace` 会造出一个
      // 名为 "workspace" 的幽灵根节点(folder-path.ts 明令禁止的形状)。
      return {
        groupPath: deriveGroupPath({
          scopeKey: targetScope.scopeKey,
          workspaceName: this.resolveWorkspaceName(targetScope),
          folderNames: []
        })
      };
    }
    const zone = input.targetOriginKind === "cloud" ? "workspace" : "server";
    return {
      groupPath: `/${zone}${subPath.startsWith("/") ? subPath : "/" + subPath}`
    };
  }

  private resolveWorkspaceName(target: {
    originKind: OriginKind;
    workspaceId?: string;
  }): string | undefined {
    if (target.originKind !== "cloud" || !target.workspaceId) {
      return undefined;
    }
    return this.deps.cloudSyncManager
      ?.listWorkspaces()
      .find((item) => item.id === target.workspaceId)?.workspaceName;
  }

  private resolveScope(
    originKind: OriginKind,
    workspaceId?: string
  ): { scopeKey: string; originKind: OriginKind; workspaceId?: string } {
    if (originKind === "local") {
      return { scopeKey: LOCAL_DEFAULT_SCOPE_KEY, originKind: "local" };
    }

    if (!workspaceId) {
      throw new Error("workspaceId is required for cloud origin");
    }

    const manager = this.deps.cloudSyncManager;
    if (!manager) throw new Error("CloudSyncManager not available for cloud operations");

    const workspaces = manager.listWorkspaces();
    const ws = workspaces.find((w) => w.id === workspaceId);
    if (!ws) throw new Error(`Workspace not found: ${workspaceId}`);

    const scopeKey = buildScopeKey({
      kind: "cloud",
      apiBaseUrl: ws.apiBaseUrl,
      workspaceName: ws.workspaceName
    });

    return { scopeKey, originKind: "cloud", workspaceId };
  }
}
