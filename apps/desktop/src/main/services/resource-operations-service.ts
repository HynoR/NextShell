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
  OriginKind,
} from "@nextshell/core";
import { buildResourceId, buildScopeKey, LOCAL_DEFAULT_SCOPE_KEY } from "@nextshell/core";
import type { CloudSyncManager } from "./cloud-sync-manager";
import type { EncryptedSecretVault } from "@nextshell/security";
import type {
  CachedConnectionRepository,
  CachedProxyRepository,
  CachedSshKeyRepository,
} from "@nextshell/storage";

// ── Input types ─────────────────────────────────────────────────────────────

export interface CopyConnectionInput {
  /** ID of the source connection to copy */
  sourceId: string;
  /** Target origin kind */
  targetOriginKind: OriginKind;
  /** Target workspace ID (required if targetOriginKind is "cloud") */
  targetWorkspaceId?: string;
  /** Optional new group sub-path within the target zone */
  targetGroupSubPath?: string;
}

export interface DangerMoveConnectionInput {
  sourceId: string;
  targetOriginKind: OriginKind;
  targetWorkspaceId?: string;
  targetGroupSubPath?: string;
}

export interface DeleteConnectionInput {
  id: string;
}

export interface DeleteSshKeyInput {
  id: string;
  force?: boolean;
}

export interface RestoreFromRecycleBinInput {
  recycleBinEntryId: string;
  targetOriginKind: OriginKind;
  targetWorkspaceId?: string;
}

// ── Dependencies ────────────────────────────────────────────────────────────

export interface ResourceOperationsDeps {
  connections: CachedConnectionRepository;
  sshKeyRepo: CachedSshKeyRepository;
  proxyRepo: CachedProxyRepository;
  vault: EncryptedSecretVault;
  cloudSyncManager: CloudSyncManager | undefined;
  saveRecycleBinEntry: (entry: RecycleBinEntry) => void;
  listRecycleBinEntries: () => RecycleBinEntry[];
  removeRecycleBinEntry: (id: string) => void;
  appendAuditLog: (payload: {
    action: string;
    level: "info" | "warn" | "error";
    connectionId?: string;
    message: string;
    metadata?: Record<string, unknown>;
  }) => void;
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
      newSshKeyId = await this.ensureSshKeyInScope(
        source.sshKeyId,
        targetScope,
      );
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

    // Determine group path
    const zone = input.targetOriginKind === "cloud" ? "workspace" : "server";
    const subPath = input.targetGroupSubPath ?? "";
    const groupPath = subPath ? `/${zone}${subPath.startsWith("/") ? subPath : "/" + subPath}` : `/${zone}`;

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
      groupPath,
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
      copiedFromResourceId: source.resourceId ?? buildResourceId(
        source.originScopeKey ?? LOCAL_DEFAULT_SCOPE_KEY,
        source.uuidInScope ?? source.id,
      ),
    };

    connections.save(copied);

    // If target is cloud, notify CloudSyncManager to push
    if (input.targetOriginKind === "cloud" && this.deps.cloudSyncManager) {
      this.deps.cloudSyncManager.pushConnectionUpsert(copied);
    }

    this.deps.appendAuditLog({
      action: "resource.copy",
      level: "info",
      connectionId: copied.id,
      message: `Copied connection "${source.name}" to ${input.targetOriginKind}`,
      metadata: {
        sourceId: source.id,
        targetId: copied.id,
        targetOriginKind: input.targetOriginKind,
        copiedFromResourceId: copied.copiedFromResourceId,
      },
    });

    return copied;
  }

  // ── Danger Move Connection ──────────────────────────────────────────

  /**
   * Move a connection: copy to target + delete source (via recycle bin).
   * This is the "dangerous" operation — source is removed.
   */
  async dangerMoveConnection(input: DangerMoveConnectionInput): Promise<ConnectionProfile> {
    const source = this.deps.connections.getById(input.sourceId);
    if (!source) throw new Error(`Source connection not found: ${input.sourceId}`);

    // Step 1: Copy to target
    const copied = await this.copyConnection({
      sourceId: input.sourceId,
      targetOriginKind: input.targetOriginKind,
      targetWorkspaceId: input.targetWorkspaceId,
      targetGroupSubPath: input.targetGroupSubPath,
    });

    // Step 2: Delete source to recycle bin
    await this.deleteConnection({ id: input.sourceId }, "danger_move");

    this.deps.appendAuditLog({
      action: "resource.danger_move",
      level: "warn",
      connectionId: copied.id,
      message: `Danger-moved connection "${source.name}" from ${source.originKind ?? "local"} to ${input.targetOriginKind}`,
      metadata: {
        sourceId: source.id,
        targetId: copied.id,
      },
    });

    return copied;
  }

  // ── Delete Connection ───────────────────────────────────────────────

  /**
   * Delete a connection: move to recycle bin first.
   * If cloud-origin, also send tombstone via CloudSyncManager.
   */
  async deleteConnection(
    input: DeleteConnectionInput,
    reason: RecycleBinEntry["reason"] = "delete",
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
      } catch { /* best effort */ }
    }

    const entry: RecycleBinEntry = {
      id: randomUUID(),
      resourceType: "server",
      displayName: conn.name || conn.host,
      originalResourceId: conn.resourceId ?? buildResourceId(
        conn.originScopeKey ?? LOCAL_DEFAULT_SCOPE_KEY,
        conn.uuidInScope ?? conn.id,
      ),
      originalScopeKey: conn.originScopeKey ?? LOCAL_DEFAULT_SCOPE_KEY,
      reason,
      snapshotJson: JSON.stringify(snapshotData),
      createdAt: new Date().toISOString(),
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

    this.deps.appendAuditLog({
      action: "resource.delete",
      level: "warn",
      connectionId: input.id,
      message: `Deleted connection "${conn.name}" to recycle bin`,
      metadata: { reason, originKind: conn.originKind },
    });
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
        throw new Error(`SSH key "${key.name}" is still referenced by ${refs.length} connection(s). Use force=true to delete anyway.`);
      }
    }

    // Step 1: Snapshot to recycle bin (with embedded credentials for restore)
    const snapshotData: Record<string, unknown> = { ...key };
    if (key.keyContentRef) {
      try {
        const content = await vault.readCredential(key.keyContentRef);
        if (content) snapshotData._savedKeyContent = content;
      } catch { /* best effort */ }
    }
    if (key.passphraseRef) {
      try {
        const pass = await vault.readCredential(key.passphraseRef);
        if (pass) snapshotData._savedPassphrase = pass;
      } catch { /* best effort */ }
    }

    const entry: RecycleBinEntry = {
      id: randomUUID(),
      resourceType: "sshKey",
      displayName: key.name,
      originalResourceId: key.resourceId ?? buildResourceId(
        key.originScopeKey ?? LOCAL_DEFAULT_SCOPE_KEY,
        key.uuidInScope ?? key.id,
      ),
      originalScopeKey: key.originScopeKey ?? LOCAL_DEFAULT_SCOPE_KEY,
      reason: "delete",
      snapshotJson: JSON.stringify(snapshotData),
      createdAt: new Date().toISOString(),
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

    this.deps.appendAuditLog({
      action: "resource.delete_ssh_key",
      level: "warn",
      message: `Deleted SSH key "${key.name}" to recycle bin`,
      metadata: { originKind: key.originKind },
    });
  }

  // ── Recycle Bin Operations ──────────────────────────────────────────

  /**
   * Restore from recycle bin as a NEW copy (never overwrite existing).
   */
  async restoreFromRecycleBin(input: RestoreFromRecycleBinInput): Promise<ConnectionProfile | SshKeyProfile> {
    const entries = this.deps.listRecycleBinEntries();
    const entry = entries.find((e) => e.id === input.recycleBinEntryId);
    if (!entry) throw new Error(`Recycle bin entry not found: ${input.recycleBinEntryId}`);

    const snapshot = JSON.parse(entry.snapshotJson) as Record<string, unknown>;
    const targetScope = this.resolveScope(input.targetOriginKind, input.targetWorkspaceId);
    const newUuid = randomUUID();
    const newResourceId = buildResourceId(targetScope.scopeKey, newUuid);
    const now = new Date().toISOString();

    if (entry.resourceType === "server") {
      const zone = input.targetOriginKind === "cloud" ? "workspace" : "server";

      // Restore credential from snapshot if available
      let credentialRef: string | undefined;
      if (typeof snapshot._savedCredential === "string" && snapshot._savedCredential) {
        credentialRef = await this.deps.vault.storeCredential(`conn-${newUuid}`, snapshot._savedCredential);
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
        groupPath: `/${zone}`,
        tags: Array.isArray(snapshot.tags) ? snapshot.tags.filter((t): t is string => typeof t === "string") : [],
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
        originKind: input.targetOriginKind,
        originScopeKey: targetScope.scopeKey,
        originWorkspaceId: input.targetWorkspaceId,
        copiedFromResourceId: entry.originalResourceId,
      };

      this.deps.connections.save(restored);

      if (input.targetOriginKind === "cloud" && this.deps.cloudSyncManager) {
        this.deps.cloudSyncManager.pushConnectionUpsert(restored);
      }

      // Remove from recycle bin
      this.deps.removeRecycleBinEntry(entry.id);

      this.deps.appendAuditLog({
        action: "resource.restore",
        level: "info",
        connectionId: restored.id,
        message: `Restored connection "${restored.name}" from recycle bin as new copy`,
      });

      return restored;
    } else {
      // SSH key restore — re-store credentials from snapshot
      let keyContentRef = String(snapshot.keyContentRef ?? "");
      if (typeof snapshot._savedKeyContent === "string" && snapshot._savedKeyContent) {
        keyContentRef = await this.deps.vault.storeCredential(`sshkey-${newUuid}`, snapshot._savedKeyContent);
      }

      let passphraseRef: string | undefined;
      if (typeof snapshot._savedPassphrase === "string" && snapshot._savedPassphrase) {
        passphraseRef = await this.deps.vault.storeCredential(`sshkey-${newUuid}-pass`, snapshot._savedPassphrase);
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
        copiedFromResourceId: entry.originalResourceId,
      };

      this.deps.sshKeyRepo.save(restored);

      if (input.targetOriginKind === "cloud" && this.deps.cloudSyncManager) {
        this.deps.cloudSyncManager.pushSshKeyUpsert(restored);
      }

      // Remove from recycle bin
      this.deps.removeRecycleBinEntry(entry.id);

      this.deps.appendAuditLog({
        action: "resource.restore_ssh_key",
        level: "info",
        message: `Restored SSH key "${restored.name}" from recycle bin as new copy`,
      });

      return restored;
    }
  }

  /**
   * Physical purge: permanently delete a recycle bin entry.
   * Only from secondary delete in recycle bin view.
   */
  purgeRecycleBinEntry(id: string): void {
    this.deps.removeRecycleBinEntry(id);

    this.deps.appendAuditLog({
      action: "resource.purge",
      level: "warn",
      message: `Permanently purged recycle bin entry ${id}`,
    });
  }

  /**
   * Copy an SSH key to a target origin scope.
   */
  async copySshKey(input: {
    sourceId: string;
    targetOriginKind: OriginKind;
    targetWorkspaceId?: string;
  }): Promise<SshKeyProfile> {
    const targetScope = this.resolveScope(input.targetOriginKind, input.targetWorkspaceId);
    const newId = await this.ensureSshKeyInScope(input.sourceId, targetScope);
    const result = this.deps.sshKeyRepo.getById(newId);
    if (!result) throw new Error("Failed to copy SSH key");
    return result;
  }

  // ── Private helpers ─────────────────────────────────────────────────

  /**
   * Ensure an SSH key exists in the target scope.
   * If it already exists (same origin), return its ID.
   * If not, copy it and return the new ID.
   */
  private async ensureSshKeyInScope(
    sourceKeyId: string,
    targetScope: { scopeKey: string; originKind: OriginKind; workspaceId?: string },
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
      (k) => k.originScopeKey === targetScope.scopeKey &&
             k.copiedFromResourceId === (sourceKey.resourceId ?? buildResourceId(sourceScopeKey, sourceKey.uuidInScope ?? sourceKey.id))
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
      copiedFromResourceId: sourceKey.resourceId ?? buildResourceId(sourceScopeKey, sourceKey.uuidInScope ?? sourceKey.id),
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
    targetScope: { scopeKey: string; originKind: OriginKind; workspaceId?: string },
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

    const existingCopy = proxyRepo.list().find(
      (proxy) =>
        proxy.originScopeKey === targetScope.scopeKey &&
        proxy.copiedFromResourceId ===
          (sourceProxy.resourceId ?? buildResourceId(sourceScopeKey, sourceProxy.uuidInScope ?? sourceProxy.id)),
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
        sourceProxy.resourceId ?? buildResourceId(sourceScopeKey, sourceProxy.uuidInScope ?? sourceProxy.id),
    };

    proxyRepo.save(copiedProxy);
    if (targetScope.originKind === "cloud" && this.deps.cloudSyncManager) {
      this.deps.cloudSyncManager.pushProxyUpsert(copiedProxy);
    }

    return copiedProxy.id;
  }

  private resolveScope(
    originKind: OriginKind,
    workspaceId?: string,
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
      workspaceName: ws.workspaceName,
    });

    return { scopeKey, originKind: "cloud", workspaceId };
  }
}
