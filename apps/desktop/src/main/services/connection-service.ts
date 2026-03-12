import { randomUUID } from "node:crypto";
import type { WebContents } from "electron";
import type {
  AuditLogRecord,
  ConnectionListQuery,
  ConnectionProfile,
  MigrationRecord,
  ProxyProfile,
  SshKeyProfile,
} from "@nextshell/core";
import type {
  ConnectionUpsertInput,
  SessionAuthOverrideInput,
  SessionStatusEvent,
  SshKeyUpsertInput,
  SshKeyRemoveInput,
  ProxyUpsertInput,
  ProxyRemoveInput,
} from "@nextshell/shared";
import type {
  CloudSyncApplyConnectionInput,
  CloudSyncApplySshKeyInput,
  CloudSyncApplyProxyInput,
} from "./cloud-sync-service";
import type { EncryptedSecretVault } from "@nextshell/security";
import type {
  CachedConnectionRepository,
  CachedSshKeyRepository,
  CachedProxyRepository,
} from "@nextshell/storage";
import type { RemoteEditManager } from "./remote-edit-manager";
import type { ActiveSession, ActiveRemoteSession, MonitorState } from "./container-types";
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
  appendAuditLogIfEnabled: (payload: {
    action: string;
    level: "info" | "warn" | "error";
    connectionId?: string;
    message: string;
    metadata?: Record<string, unknown>;
  }) => void;
  sendSessionStatus: (sender: WebContents, payload: SessionStatusEvent) => void;
  getCloudSyncService: () =>
    | {
        pushConnectionUpsert: (profile: ConnectionProfile) => void;
        pushConnectionDelete: (id: string) => void;
        pushSshKeyUpsert: (profile: SshKeyProfile) => void;
        pushSshKeyDelete: (id: string) => void;
        pushProxyUpsert: (profile: ProxyProfile) => void;
        pushProxyDelete: (id: string) => void;
      }
    | undefined;
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
      getCloudSyncService,
    } = this.options;

    const now = new Date().toISOString();
    const id = input.id ?? randomUUID();
    const current = connections.getById(id);
    const isNew = !current;
    const authTypeChanged = Boolean(current && current.authType !== input.authType);
    const needsPasswordCredential = input.authType === "password" || input.authType === "interactive";
    const shouldDropPreviousCredential = input.authType === "agent" || authTypeChanged;

    if (input.authType === "privateKey" && !input.sshKeyId) {
      throw new Error("Private key auth requires selecting an SSH key.");
    }
    if (input.sshKeyId) {
      const keyProfile = sshKeyRepo.getById(input.sshKeyId);
      if (!keyProfile) {
        throw new Error("Referenced SSH key not found.");
      }
    }
    if (input.proxyId) {
      const proxyProfile = proxyRepo.getById(input.proxyId);
      if (!proxyProfile) {
        throw new Error("Referenced proxy not found.");
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
      groupPath: input.groupPath,
      tags: input.tags,
      notes: input.notes,
      favorite: input.favorite,
      monitorSession: input.monitorSession,
      createdAt: current?.createdAt ?? now,
      updatedAt: now,
      lastConnectedAt: current?.lastConnectedAt,
    };

    connections.save(profile);
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
    void getCloudSyncService()?.pushConnectionUpsert(profile);
    return profile;
  }

  async applyConnectionFromCloudSync(input: CloudSyncApplyConnectionInput): Promise<void> {
    const { connections, sshKeyRepo, vault, disposeAllMonitorSessions } = this.options;

    const current = connections.getById(input.id);
    const needsPasswordCredential = input.authType === "password" || input.authType === "interactive";

    if (input.authType === "privateKey" && !input.sshKeyId) {
      throw new Error("Cloud sync connection is missing sshKeyId for private key auth.");
    }
    if (input.sshKeyId && !sshKeyRepo.getById(input.sshKeyId)) {
      throw new Error(`Cloud sync referenced SSH key not found: ${input.sshKeyId}`);
    }

    let credentialRef = current?.credentialRef;
    if (!needsPasswordCredential && credentialRef) {
      await vault.deleteCredential(credentialRef);
      credentialRef = undefined;
    }
    if (needsPasswordCredential) {
      if (!input.password) {
        throw new Error(`Cloud sync connection ${input.name} is missing password content.`);
      }
      credentialRef = await vault.storeCredential(`conn-${input.id}`, input.password);
    }

    const now = new Date().toISOString();
    const profile: ConnectionProfile = {
      id: input.id,
      name: input.name,
      host: input.host,
      port: input.port,
      username: input.username.trim(),
      authType: input.authType,
      credentialRef: needsPasswordCredential ? credentialRef : undefined,
      sshKeyId: input.authType === "privateKey" ? input.sshKeyId : undefined,
      hostFingerprint: input.hostFingerprint,
      strictHostKeyChecking: input.strictHostKeyChecking,
      proxyId: undefined,
      keepAliveEnabled: input.keepAliveEnabled,
      keepAliveIntervalSec: input.keepAliveIntervalSec,
      terminalEncoding: current?.terminalEncoding ?? "utf-8",
      backspaceMode: current?.backspaceMode ?? "ascii-backspace",
      deleteMode: current?.deleteMode ?? "vt220-delete",
      groupPath: input.groupPath,
      tags: input.tags,
      notes: input.notes,
      favorite: input.favorite,
      monitorSession: current?.monitorSession ?? false,
      createdAt: current?.createdAt ?? now,
      updatedAt: input.updatedAt,
      lastConnectedAt: current?.lastConnectedAt,
    };

    connections.save(profile);
    if (!profile.monitorSession) {
      await disposeAllMonitorSessions(profile.id);
    }
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
    const result = await this.removeConnectionRecord(id);
    void this.options.getCloudSyncService()?.pushConnectionDelete(id);
    return { ok: true };
  }

  // ── SSH Key CRUD ──────────────────────────────────────────────────

  listSshKeys(): SshKeyProfile[] {
    return this.options.sshKeyRepo.list();
  }

  async upsertSshKey(input: SshKeyUpsertInput): Promise<SshKeyProfile> {
    const { sshKeyRepo, vault, getCloudSyncService } = this.options;

    const now = new Date().toISOString();
    const id = input.id ?? randomUUID();
    const current = sshKeyRepo.getById(id);

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
    };

    sshKeyRepo.save(profile);
    void getCloudSyncService()?.pushSshKeyUpsert(profile);
    return profile;
  }

  async applySshKeyFromCloudSync(input: CloudSyncApplySshKeyInput): Promise<void> {
    const { sshKeyRepo, vault } = this.options;

    const current = sshKeyRepo.getById(input.id);
    const keyContentRef = await vault.storeCredential(`sshkey-${input.id}`, input.keyContent);
    let passphraseRef = current?.passphraseRef;

    if (input.passphrase) {
      passphraseRef = await vault.storeCredential(`sshkey-${input.id}-pass`, input.passphrase);
    } else if (current?.passphraseRef) {
      await vault.deleteCredential(current.passphraseRef);
      passphraseRef = undefined;
    }

    sshKeyRepo.save({
      id: input.id,
      name: input.name,
      keyContentRef,
      passphraseRef,
      createdAt: current?.createdAt ?? input.updatedAt,
      updatedAt: input.updatedAt,
    });
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
    const result = await this.removeSshKeyRecord(input);
    void this.options.getCloudSyncService()?.pushSshKeyDelete(input.id);
    return result;
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
    };

    proxyRepo.save(profile);
    return profile;
  }

  async applyProxyFromCloudSync(input: CloudSyncApplyProxyInput): Promise<void> {
    const { proxyRepo, vault } = this.options;

    const current = proxyRepo.getById(input.id);
    let credentialRef = current?.credentialRef;

    if (input.password) {
      credentialRef = await vault.storeCredential(`proxy-${input.id}`, input.password);
    } else if (current?.credentialRef) {
      await vault.deleteCredential(current.credentialRef);
      credentialRef = undefined;
    }

    proxyRepo.save({
      id: input.id,
      name: input.name,
      proxyType: input.proxyType,
      host: input.host,
      port: input.port,
      username: input.username,
      credentialRef,
      createdAt: current?.createdAt ?? input.updatedAt,
      updatedAt: input.updatedAt,
    });
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
