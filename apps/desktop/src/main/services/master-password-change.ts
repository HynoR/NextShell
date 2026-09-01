import type { MasterKeyMeta } from "../../../../../packages/core/src/index";
import {
  clearDerivedKeyCache,
  createMasterKeyMeta,
  verifyMasterPassword
} from "../../../../../packages/security/src/index";

interface ChangeMasterPasswordOptions {
  oldPassword: string;
  newPassword: string;
  getMasterKeyMeta: () => MasterKeyMeta | undefined;
  saveMasterKeyMeta: (meta: MasterKeyMeta) => void;
  setMasterPassword: (password: string) => void;
}

export const changeMasterPassword = async (
  options: ChangeMasterPasswordOptions
): Promise<{ ok: true }> => {
  const meta = options.getMasterKeyMeta();
  if (!meta) {
    throw new Error("尚未设置主密码。请先设置主密码。");
  }
  if (!(await verifyMasterPassword(options.oldPassword, meta))) {
    throw new Error("原密码错误，请重试。");
  }

  clearDerivedKeyCache();
  const nextMeta = await createMasterKeyMeta(options.newPassword);
  options.saveMasterKeyMeta(nextMeta);
  options.setMasterPassword(options.newPassword);

  return { ok: true };
};
