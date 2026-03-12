import type {
  BackupArchiveMeta,
  BackupConflictPolicy,
  RestoreConflictPolicy,
} from "@nextshell/core";
import type { EncryptedSecretVault, KeytarPasswordCache } from "@nextshell/security";
import {
  createMasterKeyMeta,
  clearDerivedKeyCache,
  verifyMasterPassword,
} from "@nextshell/security";
import type { CachedConnectionRepository } from "@nextshell/storage";

import type { BackupService } from "./backup-service";
import { changeMasterPassword } from "./master-password-change";
import { normalizeError } from "./container-utils";
import { logger } from "../logger";

interface BackupPasswordServiceOptions {
  connections: CachedConnectionRepository;
  vault: EncryptedSecretVault;
  keytarCache: KeytarPasswordCache;
  backupService: BackupService;
  getMasterPassword: () => string | undefined;
  setMasterPassword: (password: string | undefined) => void;
  tryRecallMasterPassword: () => Promise<void>;
  appendAuditLogIfEnabled: (payload: {
    action: string;
    level: "info" | "warn" | "error";
    connectionId?: string;
    message: string;
    metadata?: Record<string, unknown>;
  }) => void;
}

export class BackupPasswordService {
  constructor(private readonly options: BackupPasswordServiceOptions) {}

  async backupList(): Promise<BackupArchiveMeta[]> {
    return this.options.backupService.list();
  }

  async backupRun(
    conflictPolicy: BackupConflictPolicy,
  ): Promise<{ ok: true; fileName?: string }> {
    return this.options.backupService.run(conflictPolicy);
  }

  async backupRestore(
    archiveId: string,
    conflictPolicy: RestoreConflictPolicy,
  ): Promise<{ ok: true }> {
    return this.options.backupService.restore(archiveId, conflictPolicy);
  }

  private async rememberPasswordBestEffort(
    password: string,
    phase: "set" | "unlock" | "change",
  ): Promise<void> {
    const prefs = this.options.connections.getAppPreferences();
    if (!prefs.backup.rememberPassword) {
      return;
    }
    try {
      await this.options.keytarCache.remember(password);
    } catch (error) {
      const reason = normalizeError(error);
      logger.warn("[Security] failed to cache master password in keytar", {
        phase,
        reason,
      });
      this.options.appendAuditLogIfEnabled({
        action: "master_password.cache_failed",
        level: "warn",
        message: "Failed to cache master password in keytar",
        metadata: { phase, reason },
      });
    }
  }

  private getMasterKeyMetaOrThrow() {
    const meta = this.options.connections.getMasterKeyMeta();
    if (!meta) {
      throw new Error("尚未设置主密码。请先设置主密码。");
    }
    return meta;
  }

  async masterPasswordSet(password: string): Promise<{ ok: true }> {
    const meta = await createMasterKeyMeta(password);
    this.options.connections.saveMasterKeyMeta(meta);
    this.options.setMasterPassword(password);
    await this.rememberPasswordBestEffort(password, "set");
    this.options.appendAuditLogIfEnabled({
      action: "master_password.set",
      level: "info",
      message: "Master password configured",
    });
    return { ok: true };
  }

  async masterPasswordUnlock(password: string): Promise<{ ok: true }> {
    const meta = this.getMasterKeyMetaOrThrow();
    if (!(await verifyMasterPassword(password, meta))) {
      throw new Error("主密码错误。");
    }
    this.options.setMasterPassword(password);
    await this.rememberPasswordBestEffort(password, "unlock");
    return { ok: true };
  }

  async masterPasswordChange(
    oldPassword: string,
    newPassword: string,
  ): Promise<{ ok: true }> {
    return changeMasterPassword({
      oldPassword,
      newPassword,
      getMasterKeyMeta: () => this.options.connections.getMasterKeyMeta(),
      saveMasterKeyMeta: (meta) =>
        this.options.connections.saveMasterKeyMeta(meta),
      setMasterPassword: (password) => {
        this.options.setMasterPassword(password);
      },
      rememberPasswordBestEffort: (password, phase) =>
        this.rememberPasswordBestEffort(password, phase),
      appendAuditLog: (payload) => {
        this.options.appendAuditLogIfEnabled(payload);
      },
    });
  }

  async masterPasswordClearRemembered(): Promise<{ ok: true }> {
    await this.options.keytarCache.clear();
    clearDerivedKeyCache();
    return { ok: true };
  }

  async masterPasswordStatus(): Promise<{
    isSet: boolean;
    isUnlocked: boolean;
    keytarAvailable: boolean;
  }> {
    const meta = this.options.connections.getMasterKeyMeta();
    return {
      isSet: meta !== undefined,
      isUnlocked: this.options.getMasterPassword() !== undefined,
      keytarAvailable: this.options.keytarCache.isAvailable(),
    };
  }

  async masterPasswordGetCached(): Promise<{ password?: string }> {
    if (!this.options.getMasterPassword()) {
      await this.options.tryRecallMasterPassword();
    }
    return { password: this.options.getMasterPassword() };
  }

  async resolveMasterPassword(candidate?: string): Promise<string> {
    const input = candidate?.trim();
    if (input) {
      const meta = this.getMasterKeyMetaOrThrow();
      if (!(await verifyMasterPassword(input, meta))) {
        throw new Error("主密码错误。");
      }
      this.options.setMasterPassword(input);
      await this.rememberPasswordBestEffort(input, "unlock");
      return input;
    }
    const cached = this.options.getMasterPassword();
    if (cached) {
      return cached;
    }
    await this.options.tryRecallMasterPassword();
    const recalled = this.options.getMasterPassword();
    if (recalled) {
      return recalled;
    }
    throw new Error("主密码未解锁，请先输入主密码。");
  }

  async revealConnectionPassword(
    connectionId: string,
    providedMasterPassword?: string,
  ): Promise<{ password: string }> {
    const connection = this.options.connections.getById(connectionId);
    if (!connection) {
      throw new Error("连接不存在。");
    }
    if (
      connection.authType !== "password" &&
      connection.authType !== "interactive"
    ) {
      throw new Error("该连接未使用密码或交互式认证。");
    }
    if (!connection.credentialRef) {
      throw new Error("该连接未保存登录密码。");
    }
    await this.resolveMasterPassword(providedMasterPassword);
    const password = await this.options.vault.readCredential(
      connection.credentialRef,
    );
    if (!password) {
      throw new Error("该连接未保存登录密码。");
    }
    this.options.appendAuditLogIfEnabled({
      action: "connection.password_reveal",
      level: "warn",
      connectionId,
      message: "Revealed saved connection password",
      metadata: {
        via: providedMasterPassword?.trim()
          ? "master-password-input"
          : "master-password-cache",
      },
    });
    return { password };
  }

  async backupSetPassword(password: string): Promise<{ ok: true }> {
    return this.masterPasswordSet(password);
  }

  async backupUnlockPassword(password: string): Promise<{ ok: true }> {
    return this.masterPasswordUnlock(password);
  }

  async backupClearRemembered(): Promise<{ ok: true }> {
    return this.masterPasswordClearRemembered();
  }

  async backupPasswordStatus(): Promise<{
    isSet: boolean;
    isUnlocked: boolean;
    keytarAvailable: boolean;
  }> {
    return this.masterPasswordStatus();
  }
}
