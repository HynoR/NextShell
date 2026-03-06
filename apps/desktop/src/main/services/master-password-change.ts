import type { MasterKeyMeta } from "../../../../../packages/core/src/index";
import { createMasterKeyMeta, verifyMasterPassword } from "../../../../../packages/security/src/index";

type PasswordChangePhase = "set" | "unlock" | "change";

interface AuditLogPayload {
  action: string;
  level: "info" | "warn" | "error";
  message: string;
  metadata?: Record<string, unknown>;
}

interface ChangeMasterPasswordOptions {
  oldPassword: string;
  newPassword: string;
  getMasterKeyMeta: () => MasterKeyMeta | undefined;
  saveMasterKeyMeta: (meta: MasterKeyMeta) => void;
  setMasterPassword: (password: string) => void;
  rememberPasswordBestEffort: (password: string, phase: PasswordChangePhase) => Promise<void>;
  appendAuditLog: (payload: AuditLogPayload) => void;
}

export const changeMasterPassword = async (options: ChangeMasterPasswordOptions): Promise<{ ok: true }> => {
  const meta = options.getMasterKeyMeta();
  if (!meta) {
    throw new Error("尚未设置主密码。请先设置主密码。");
  }
  if (!verifyMasterPassword(options.oldPassword, meta)) {
    throw new Error("原密码错误，请重试。");
  }

  const sameAsOld = options.oldPassword === options.newPassword;
  const nextMeta = createMasterKeyMeta(options.newPassword);
  options.saveMasterKeyMeta(nextMeta);
  options.setMasterPassword(options.newPassword);
  await options.rememberPasswordBestEffort(options.newPassword, "change");
  options.appendAuditLog({
    action: "master_password.change",
    level: "warn",
    message: "Master password changed",
    metadata: { sameAsOld }
  });

  return { ok: true };
};
