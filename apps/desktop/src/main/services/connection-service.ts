import { randomUUID } from "node:crypto";
import type { WebContents } from "electron";
import type {
  CloudSyncWorkspaceProfile,
  AuditLogRecord,
  ConnectionListQuery,
  ConnectionProfile,
  OriginKind,
  MigrationRecord,
  ProxyProfile,
  SshKeyProfile,
} from "@nextshell/core";
import { buildResourceId, buildScopeKey, LOCAL_DEFAULT_SCOPE_KEY } from "@nextshell/core";
import type {
  ConnectionUpsertInput,
  SessionAuthOverrideInput,
  SessionStatusEvent,
  SshKeyUpsertInput,
  SshKeyRemoveInput,
  ProxyUpsertInput,
  ProxyRemoveInput,
} from "@nextshell/shared";
import type { EncryptedSecretVault } from "@nextshell/security";
import type {
  CachedConnectionRepository,
  CachedSshKeyRepository,
  CachedProxyRepository,
} from "@nextshell/storage";
import type { RemoteEditManager } from "./remote-edit-manager";
import type { ActiveSession, ActiveRemoteSession, MonitorState } from "./container-types";
import type { CloudSyncManager } from "./cloud-sync-manager";
import { enforceZonePrefix } from "../../../../../packages/shared/src/constants";
import { normalizeError } from "./container-utils";
import { logger } from "../logger";

export interface ConnectionServiceOptions {
  connections: CachedConnectionRepository;
  sshKeyRepo: CachedSshKeyRepository;
  proxyRepo: CachedProxyRepository;
  vault: EncryptedSecretVault;
  activeSessions: Map<string, ActiveSession>;
  disposeAllMonitorSessions: (connectionId: string) => Promise<void>;
  closeConnectionIfIdle: (connectionId: string) => Promise<void>;
  remoteEditManager: RemoteEditManager;
  monitorStates: Map<string, MonitorState>;
  getCloudSyncManager?: () => CloudSyncManager | undefined;
  appendAuditLogIfEnabled: (payload: {
    action: string;
    level: "info" | "warn" | "error";
    connectionId?: string;
    message: string;
    metadata?: Record<string, unknown>;
  }) => void;
  sendSessionStatus: (sender: WebContents, payload: SessionStatusEvent) => void;
}

export class ConnectionService {
  constructor(private readonly options: ConnectionServiceOptions) {}

  // ── Private helpers ───────────────────────────────────────────────

  private getConnectionOrThrow(id: string): ConnectionProfile {
    const connection = this.options.connections.getById(id);
    if (!connection) {
      throw new Error("Connection not found");
    }
    return connection;
  }

  private getCloudWorkspace(workspaceId: string | undefined): CloudSyncWorkspaceProfile | undefined {
    if (!workspaceId) {
      return undefined;
    }
    return this.options.getCloudSyncManager?.()
      ?.listWorkspaces()
      .find((workspace) => workspace.id === workspaceId);
  }

  private resolveResourceOrigin(
    current: { originKind?: OriginKind; originScopeKey?: string; originWorkspaceId?: string; uuidInScope?: string; resourceId?: string; copiedFromResourceId?: string } | undefined,
    workspaceId: string | undefined,
  ): {
    originKind: OriginKind;
    originScopeKey: string;
    originWorkspaceId?: string;
    uuidInScope?: string;
    resourceId?: string;
    copiedFromResourceId?: string;
  } {
    const targetWorkspaceId = workspaceId ?? (current?.originKind === "cloud" ? current.originWorkspaceId : undefined);
    if (!targetWorkspaceId) {
      return {
        originKind: "local",
        originScopeKey: current?.originScopeKey ?? LOCAL_DEFAULT_SCOPE_KEY,
        originWorkspaceId: undefined,
        uuidInScope: current?.uuidInScope,
        resourceId: current?.resourceId,
        copiedFromResourceId: current?.copiedFromResourceId,
      };
    }

    const workspace = this.getCloudWorkspace(targetWorkspaceId);
    if (!workspace) {
      throw new Error(`Workspace not found: ${targetWorkspaceId}`);
    }

    const scopeKey = buildScopeKey({
      kind: "cloud",
      apiBaseUrl: workspace.apiBaseUrl,
      workspaceName: workspace.workspaceName,
    });
    const uuidInScope = current?.uuidInScope ?? randomUUID();
    return {
      originKind: "cloud",
      originScopeKey: scopeKey,
      originWorkspaceId: targetWorkspaceId,
      uuidInScope,
      resourceId: buildResourceId(scopeKey, uuidInScope),
      copiedFromResourceId: current?.copiedFromResourceId,
    };
  }

  // ── Connection CRUD ───────────────────────────────────────────────

  listConnections(query: ConnectionListQuery): ConnectionProfile[] {
    return this.options.connections.list(query);
  }

  async upsertConnection(input: ConnectionUpsertInput): Promise<ConnectionProfile> {
    const {
      connections,
      sshKeyRepo,
      proxyRepo,
      vault,
      appendAuditLogIfEnabled,
      disposeAllMonitorSessions,
    } = this.options;

    const now = new Date().toISOString();
    const id = input.id ?? randomUUID();
    const current = connections.getById(id);
    const isNew = !current;
    const authTypeChanged = Boolean(current && current.authType !== input.authType);
    const needsPasswordCredential = input.authType === "password" || input.authType === "interactive";
    const shouldDropPreviousCredential = input.authType === "agent" || authTypeChanged;
    const origin = this.resolveResourceOrigin(current, input.workspaceId);
    const targetScopeKey = origin.originScopeKey;

    if (input.authType === "privateKey" && !input.sshKeyId) {
      throw new Error("Private key auth requires selecting an SSH key.");
    }
    if (input.sshKeyId) {
      const keyProfile = sshKeyRepo.getById(input.sshKeyId);
      if (!keyProfile) {
        throw new Error("Referenced SSH key not found.");
      }
      if (input.authType === "privateKey") {
        const keyScopeKey = keyProfile.originScopeKey ?? LOCAL_DEFAULT_SCOPE_KEY;
        if (keyScopeKey !== targetScopeKey) {
          throw new Error("禁止跨来源引用 SSH 密钥");
        }
      }
    }
    const selectedProxy = input.proxyId ? proxyRepo.getById(input.proxyId) : undefined;
    if (input.proxyId) {
      if (!selectedProxy) {
        throw new Error("Referenced proxy not found.");
      }
      const proxyScopeKey = selectedProxy.originScopeKey ?? LOCAL_DEFAULT_SCOPE_KEY;
      if (proxyScopeKey !== targetScopeKey) {
        throw new Error("禁止跨来源引用代理配置");
      }
    }

    const normalizedUsername = input.username.trim();
    const keepAliveEnabled = input.keepAliveEnabled;
    const keepAliveIntervalSec =
      Number.isInteger(input.keepAliveIntervalSec) &&
      (input.keepAliveIntervalSec as number) >= 5 &&
      (input.keepAliveIntervalSec as number) <= 600
        ? input.keepAliveIntervalSec
        : undefined;

    let credentialRef = current?.credentialRef;
    if (shouldDropPreviousCredential && current?.credentialRef) {
      await vault.deleteCredential(current.credentialRef);
      credentialRef = undefined;
    }
    if (needsPasswordCredential) {
      if (input.password) {
        credentialRef = await vault.storeCredential(`conn-${id}`, input.password);
      }
    } else {
      if (credentialRef && (isNew || authTypeChanged)) {
        credentialRef = undefined;
      }
    }

    // Enforce zone prefix on groupPath to guarantee valid zone isolation
    const safeGroupPath = enforceZonePrefix(input.groupPath);

    const profile: ConnectionProfile = {
      id,
      name: input.name,
      host: input.host,
      port: input.port,
      username: normalizedUsername,
      authType: input.authType,
      credentialRef: needsPasswordCredential ? credentialRef : undefined,
      sshKeyId: input.authType === "privateKey" ? input.sshKeyId : undefined,
      hostFingerprint: input.hostFingerprint,
      strictHostKeyChecking: input.strictHostKeyChecking,
      proxyId: input.proxyId,
      keepAliveEnabled,
      keepAliveIntervalSec,
      terminalEncoding: input.terminalEncoding,
      backspaceMode: input.backspaceMode,
      deleteMode: input.deleteMode,
      groupPath: safeGroupPath,
      tags: input.tags,
      notes: input.notes,
      favorite: input.favorite,
      monitorSession: input.monitorSession,
      createdAt: current?.createdAt ?? now,
      updatedAt: now,
      lastConnectedAt: current?.lastConnectedAt,
      resourceId: origin.resourceId,
      uuidInScope: origin.uuidInScope,
      originKind: origin.originKind,
      originScopeKey: origin.originScopeKey,
      originWorkspaceId: origin.originWorkspaceId,
      sshKeyResourceId:
        input.authType === "privateKey"
          ? sshKeyRepo.getById(input.sshKeyId ?? "")?.resourceId
          : undefined,
      copiedFromResourceId: origin.copiedFromResourceId,
    };

    connections.save(profile);
    if (profile.originKind === "cloud" && profile.originWorkspaceId) {
      this.options.getCloudSyncManager?.()?.pushConnectionUpsert(profile);
    }
    if (!profile.monitorSession) {
      await disposeAllMonitorSessions(profile.id);
    }
    appendAuditLogIfEnabled({
      action: "connection.upsert",
      level: "info",
      connectionId: profile.id,
      message: current ? "Updated connection profile" : "Created connection profile",
      metadata: {
        authType: profile.authType,
        strictHostKeyChecking: profile.strictHostKeyChecking,
        hasSshKey: Boolean(profile.sshKeyId),
        hasProxy: Boolean(profile.proxyId),
        terminalEncoding: profile.terminalEncoding,
        backspaceMode: profile.backspaceMode,
        deleteMode: profile.deleteMode,
      },
    });
    return profile;
  }

  async removeConnectionRecord(
    id: string,
    options?: { skipAudit?: boolean }
  ): Promise<{ ok: true }> {
    const {
      activeSessions,
      connections,
      vault,
      remoteEditManager,
      closeConnectionIfIdle,
      monitorStates,
      appendAuditLogIfEnabled,
      sendSessionStatus,
    } = this.options;

    const sessions = Array.from(activeSessions.values()).filter(
      (session): session is ActiveRemoteSession =>
        session.kind === "remote" && session.connectionId === id
    );

    for (const session of sessions) {
      session.channel.end();
      activeSessions.delete(session.descriptor.id);
      sendSessionStatus(session.sender, {
        sessionId: session.descriptor.id,
        status: "disconnected",
        reason: "Connection deleted",
      });
    }

    await remoteEditManager.cleanupByConnectionId(id);

    const connection = connections.getById(id);
    if (connection?.credentialRef) {
      await vault.deleteCredential(connection.credentialRef);
    }

    await closeConnectionIfIdle(id);
    connections.remove(id);
    monitorStates.delete(id);
    if (!options?.skipAudit) {
      appendAuditLogIfEnabled({
        action: "connection.remove",
        level: "warn",
        connectionId: id,
        message: "Connection profile deleted",
      });
    }
    return { ok: true };
  }

  async removeConnection(id: string): Promise<{ ok: true }> {
    return this.removeConnectionRecord(id);
  }

  // ── SSH Key CRUD ──────────────────────────────────────────────────

  listSshKeys(): SshKeyProfile[] {
    return this.options.sshKeyRepo.list();
  }

  async upsertSshKey(input: SshKeyUpsertInput): Promise<SshKeyProfile> {
    const { sshKeyRepo, vault } = this.options;

    const now = new Date().toISOString();
    const id = input.id ?? randomUUID();
    const current = sshKeyRepo.getById(id);
    const origin = this.resolveResourceOrigin(current, input.workspaceId);

    // Store key content in vault
    let keyContentRef = current?.keyContentRef;
    if (input.keyContent) {
      if (current?.keyContentRef) {
        await vault.deleteCredential(current.keyContentRef);
      }
      keyContentRef = await vault.storeCredential(`sshkey-${id}`, input.keyContent);
    }
    if (!keyContentRef) {
      throw new Error("SSH key content is required.");
    }

    // Store passphrase in vault (optional)
    let passphraseRef = current?.passphraseRef;
    if (input.passphrase !== undefined) {
      if (current?.passphraseRef) {
        await vault.deleteCredential(current.passphraseRef);
        passphraseRef = undefined;
      }
      if (input.passphrase) {
        passphraseRef = await vault.storeCredential(`sshkey-${id}-pass`, input.passphrase);
      }
    }

    const profile: SshKeyProfile = {
      id,
      name: input.name,
      keyContentRef,
      passphraseRef,
      createdAt: current?.createdAt ?? now,
      updatedAt: now,
      resourceId: origin.resourceId,
      uuidInScope: origin.uuidInScope,
      originKind: origin.originKind,
      originScopeKey: origin.originScopeKey,
      originWorkspaceId: origin.originWorkspaceId,
      copiedFromResourceId: origin.copiedFromResourceId,
    };

    sshKeyRepo.save(profile);
    if (profile.originKind === "cloud" && profile.originWorkspaceId) {
      this.options.getCloudSyncManager?.()?.pushSshKeyUpsert(profile);
    }
    return profile;
  }

  async removeSshKeyRecord(input: SshKeyRemoveInput): Promise<{ ok: true }> {
    const { sshKeyRepo, vault } = this.options;

    const profile = sshKeyRepo.getById(input.id);
    if (!profile) {
      throw new Error("SSH key not found.");
    }

    const refs = sshKeyRepo.getReferencingConnectionIds(input.id);
    if (refs.length > 0) {
      throw new Error(`该密钥仍被 ${refs.length} 个连接引用，无法删除。`);
    }

    if (profile.keyContentRef) {
      await vault.deleteCredential(profile.keyContentRef);
    }
    if (profile.passphraseRef) {
      await vault.deleteCredential(profile.passphraseRef);
    }

    sshKeyRepo.remove(input.id);
    return { ok: true };
  }

  async removeSshKey(input: SshKeyRemoveInput): Promise<{ ok: true }> {
    return this.removeSshKeyRecord(input);
  }

  // ── Proxy CRUD ────────────────────────────────────────────────────

  listProxies(): ProxyProfile[] {
    return this.options.proxyRepo.list();
  }

  async upsertProxy(input: ProxyUpsertInput): Promise<ProxyProfile> {
    const { proxyRepo, vault } = this.options;

    const now = new Date().toISOString();
    const id = input.id ?? randomUUID();
    const current = proxyRepo.getById(id);
    const origin = this.resolveResourceOrigin(current, input.workspaceId);

    // Store proxy credential in vault (optional)
    let credentialRef = current?.credentialRef;
    if (input.password !== undefined) {
      if (current?.credentialRef) {
        await vault.deleteCredential(current.credentialRef);
        credentialRef = undefined;
      }
      if (input.password) {
        credentialRef = await vault.storeCredential(`proxy-${id}`, input.password);
      }
    }

    const profile: ProxyProfile = {
      id,
      name: input.name,
      proxyType: input.proxyType,
      host: input.host,
      port: input.port,
      username: input.username,
      credentialRef,
      createdAt: current?.createdAt ?? now,
      updatedAt: now,
      resourceId: origin.resourceId,
      uuidInScope: origin.uuidInScope,
      originKind: origin.originKind,
      originScopeKey: origin.originScopeKey,
      originWorkspaceId: origin.originWorkspaceId,
      copiedFromResourceId: origin.copiedFromResourceId,
    };

    proxyRepo.save(profile);
    if (profile.originKind === "cloud" && profile.originWorkspaceId) {
      this.options.getCloudSyncManager?.()?.pushProxyUpsert(profile);
    }
    return profile;
  }

  async removeProxyRecord(input: ProxyRemoveInput): Promise<{ ok: true }> {
    const { proxyRepo, vault } = this.options;

    const profile = proxyRepo.getById(input.id);
    if (!profile) {
      throw new Error("Proxy not found.");
    }

    const refs = proxyRepo.getReferencingConnectionIds(input.id);
    if (refs.length > 0) {
      throw new Error(`该代理仍被 ${refs.length} 个连接引用，无法删除。`);
    }

    if (profile.credentialRef) {
      await vault.deleteCredential(profile.credentialRef);
    }

    proxyRepo.remove(input.id);
    if (profile.originKind === "cloud" && profile.originWorkspaceId) {
      this.options.getCloudSyncManager?.()?.pushProxyDelete(profile);
    }
    return { ok: true };
  }

  async removeProxy(input: ProxyRemoveInput): Promise<{ ok: true }> {
    return this.removeProxyRecord(input);
  }

  // ── Auth Override Persistence ─────────────────────────────────────

  async persistSuccessfulAuthOverride(
    connectionId: string,
    authOverride: SessionAuthOverrideInput
  ): Promise<string | undefined> {
    const { sshKeyRepo, vault, appendAuditLogIfEnabled } = this.options;

    const latest = this.getConnectionOrThrow(connectionId);

    // If the override supplies a raw private key, import it as a new SshKeyProfile first
    let effectiveSshKeyId = authOverride.sshKeyId ?? latest.sshKeyId;
    if (authOverride.authType === "privateKey" && authOverride.privateKeyContent) {
      const keyId = randomUUID();
      const keyContentRef = await vault.storeCredential(`sshkey-${keyId}`, authOverride.privateKeyContent);
      let passphraseRef: string | undefined;
      if (authOverride.passphrase) {
        passphraseRef = await vault.storeCredential(`sshkey-${keyId}-pass`, authOverride.passphrase);
      }
      const now = new Date().toISOString();
      sshKeyRepo.save({
        id: keyId,
        name: `${latest.name}-retried-${now}`,
        keyContentRef,
        passphraseRef,
        createdAt: now,
        updatedAt: now,
      });
      effectiveSshKeyId = keyId;
    }

    const payload: ConnectionUpsertInput = {
      id: latest.id,
      name: latest.name,
      host: latest.host,
      port: latest.port,
      username: authOverride.username?.trim() || latest.username,
      authType: authOverride.authType,
      password:
        authOverride.authType === "password" || authOverride.authType === "interactive"
          ? authOverride.password
          : undefined,
      sshKeyId: authOverride.authType === "privateKey" ? effectiveSshKeyId : undefined,
      hostFingerprint: latest.hostFingerprint,
      strictHostKeyChecking: latest.strictHostKeyChecking,
      proxyId: latest.proxyId,
      keepAliveEnabled: latest.keepAliveEnabled,
      keepAliveIntervalSec: latest.keepAliveIntervalSec,
      terminalEncoding: latest.terminalEncoding,
      backspaceMode: latest.backspaceMode,
      deleteMode: latest.deleteMode,
      groupPath: latest.groupPath,
      tags: latest.tags,
      notes: latest.notes,
      favorite: latest.favorite,
      monitorSession: latest.monitorSession,
    };

    try {
      await this.upsertConnection(payload);
      return undefined;
    } catch (error) {
      const reason = normalizeError(error);
      logger.error("[Session] failed to persist auth override", {
        connectionId,
        reason,
      });
      appendAuditLogIfEnabled({
        action: "connection.auth_override_persist_failed",
        level: "warn",
        connectionId,
        message: "SSH auth override could not be persisted",
        metadata: {
          reason,
        },
      });
      return "认证成功，但自动保存凭据失败，请在连接管理器中手动保存。";
    }
  }

  // ── Audit Logs & Migrations ───────────────────────────────────────

  listAuditLogs(limit: number): AuditLogRecord[] {
    return this.options.connections.listAuditLogs(limit);
  }

  clearAuditLogs(): { ok: true; deleted: number } {
    return { ok: true, deleted: this.options.connections.clearAuditLogs() };
  }

  listMigrations(): MigrationRecord[] {
    return this.options.connections.listMigrations();
  }
}
