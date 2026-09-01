import type {
  BackupArchiveMeta,
  BackupConflictPolicy,
  RestoreConflictPolicy
} from "@nextshell/core";
import type { EncryptedSecretVault, KeytarPasswordCache } from "@nextshell/security";
import {
  APP_SECRET_PURPOSE,
  createMasterKeyMeta,
  clearDerivedKeyCache,
  MASTER_PASSWORD_SECRET_ID,
  MASTER_PASSWORD_SECRET_REF,
  verifyMasterPassword
} from "@nextshell/security";
import type { CachedConnectionRepository } from "@nextshell/storage";

import type { BackupService } from "./backup-service";
import type { DeviceKeyStatus } from "./device-key-provider";
import { changeMasterPassword } from "./master-password-change";
import { normalizeError } from "./container-utils";
import { logger } from "../logger";

interface BackupPasswordServiceOptions {
  connections: CachedConnectionRepository;
  vault: EncryptedSecretVault;
  /** Only used to drain the pre-migration keychain item. */
  keytarCache: KeytarPasswordCache;
  getCredentialStoreStatus: () => DeviceKeyStatus;
  reauthorizeCredentialStore: () => void;
  getDeviceKeyHex: () => Promise<string>;
  backupService: BackupService;
  getMasterPassword: () => string | undefined;
  setMasterPassword: (password: string | undefined) => void;
  tryRecallMasterPassword: () => Promise<void>;
}

export class BackupPasswordService {
  constructor(private readonly options: BackupPasswordServiceOptions) {}

  async backupList(): Promise<BackupArchiveMeta[]> {
    return this.options.backupService.list();
  }

  async backupRun(conflictPolicy: BackupConflictPolicy): Promise<{ ok: true; fileName?: string }> {
    await this.ensureRecalled();
    return this.options.backupService.run(conflictPolicy);
  }

  async backupRestore(
    archiveId: string,
    conflictPolicy: RestoreConflictPolicy
  ): Promise<{ ok: true }> {
    await this.ensureRecalled();
    return this.options.backupService.restore(archiveId, conflictPolicy);
  }

  /**
   * The master password is recalled from the keychain on demand rather than at
   * startup, so anything reading it synchronously must pull it in first.
   * Failures are swallowed — callers surface a "not unlocked" error of their own.
   */
  private async ensureRecalled(): Promise<void> {
    if (this.options.getMasterPassword()) {
      return;
    }
    try {
      await this.options.tryRecallMasterPassword();
    } catch (error) {
      logger.warn("[Security] failed to recall master password from keytar", {
        reason: normalizeError(error)
      });
    }
  }

  private async rememberPasswordBestEffort(
    password: string,
    phase: "set" | "unlock" | "change"
  ): Promise<void> {
    const prefs = this.options.connections.getAppPreferences();
    if (!prefs.backup.rememberPassword) {
      return;
    }
    try {
      await this.options.vault.storeCredential(
        MASTER_PASSWORD_SECRET_ID,
        password,
        APP_SECRET_PURPOSE
      );
    } catch (error) {
      const reason = normalizeError(error);
      logger.warn("[Security] failed to remember master password", {
        phase,
        reason
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

  async masterPasswordChange(oldPassword: string, newPassword: string): Promise<{ ok: true }> {
    return changeMasterPassword({
      oldPassword,
      newPassword,
      getMasterKeyMeta: () => this.options.connections.getMasterKeyMeta(),
      saveMasterKeyMeta: (meta) => this.options.connections.saveMasterKeyMeta(meta),
      setMasterPassword: (password) => {
        this.options.setMasterPassword(password);
      },
      rememberPasswordBestEffort: (password, phase) =>
        this.rememberPasswordBestEffort(password, phase)
    });
  }

  async masterPasswordClearRemembered(): Promise<{ ok: true }> {
    await this.options.vault.deleteCredential(MASTER_PASSWORD_SECRET_REF);
    // Also drain the pre-migration keychain item for installs that never
    // reached the migration path.
    await this.options.keytarCache.clear();
    clearDerivedKeyCache();
    return { ok: true };
  }

  async masterPasswordStatus(): Promise<{
    isSet: boolean;
    isUnlocked: boolean;
    canRememberPassword: boolean;
  }> {
    const meta = this.options.connections.getMasterKeyMeta();
    if (meta) {
      await this.ensureRecalled();
    }
    return {
      isSet: meta !== undefined,
      isUnlocked: this.options.getMasterPassword() !== undefined,
      // Remembering writes through the device-key-encrypted secret store, so it
      // works everywhere except when keychain authorization was refused.
      canRememberPassword: this.options.getCredentialStoreStatus() !== "denied"
    };
  }

  async masterPasswordGetCached(): Promise<{ available: boolean }> {
    if (!this.options.getMasterPassword()) {
      await this.options.tryRecallMasterPassword();
    }
    const available = this.options.getMasterPassword() !== undefined;
    return { available };
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

  /**
   * Retry a keychain read the user previously refused. A denial is sticky for
   * the session so that every later credential access does not re-prompt, which
   * would otherwise leave an accidental "Deny" only fixable by restarting.
   */
  async credentialStoreReauthorize(): Promise<{ ok: true; authorized: boolean }> {
    this.options.reauthorizeCredentialStore();
    try {
      await this.options.getDeviceKeyHex();
      return { ok: true, authorized: true };
    } catch (error) {
      logger.warn("[Security] credential store reauthorization failed", {
        reason: normalizeError(error)
      });
      return { ok: true, authorized: false };
    }
  }

  async revealConnectionPassword(
    connectionId: string,
    providedMasterPassword?: string
  ): Promise<{ password: string }> {
    const connection = this.options.connections.getById(connectionId);
    if (!connection) {
      throw new Error("连接不存在。");
    }
    if (connection.authType !== "password" && connection.authType !== "interactive") {
      throw new Error("该连接未使用密码或交互式认证。");
    }
    if (!connection.credentialRef) {
      throw new Error("该连接未保存登录密码。");
    }
    await this.resolveMasterPassword(providedMasterPassword);
    const password = await this.options.vault.readCredential(connection.credentialRef);
    if (!password) {
      throw new Error("该连接未保存登录密码。");
    }
    return { password };
  }
}
