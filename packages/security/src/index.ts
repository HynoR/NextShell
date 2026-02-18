import { createRequire } from "node:module";
import { randomBytes, scryptSync, createCipheriv, createDecipheriv, createHash } from "node:crypto";
import type { MasterKeyMeta } from "../../core/src/index";

const require = createRequire(import.meta.url);

// ─── Secret Ref Prefix ──────────────────────────────────────────────────────

const SECRET_REF_PREFIX = "secret://";

// ─── Keytar (Optional) ──────────────────────────────────────────────────────

interface KeytarModule {
  setPassword: (service: string, account: string, password: string) => Promise<void>;
  getPassword: (service: string, account: string) => Promise<string | null>;
  deletePassword: (service: string, account: string) => Promise<boolean>;
}

const loadKeytar = (): KeytarModule | undefined => {
  try {
    const moduleName = `key${"tar"}`;
    return require(moduleName) as KeytarModule;
  } catch {
    return undefined;
  }
};

// ─── Crypto Primitives ──────────────────────────────────────────────────────

const KDF_N = 16384;
const KDF_R = 8;
const KDF_P = 1;
const KEY_LENGTH = 32;
const IV_LENGTH = 12;
const AUTH_TAG_LENGTH = 16;
const ALGORITHM = "aes-256-gcm";

export const deriveKey = (
  password: string,
  salt: Buffer,
  n = KDF_N,
  r = KDF_R,
  p = KDF_P
): Buffer => {
  return scryptSync(password, salt, KEY_LENGTH, { N: n, r, p });
};

export const createMasterKeyMeta = (password: string): MasterKeyMeta => {
  const salt = randomBytes(32);
  const key = deriveKey(password, salt);
  const verifier = createHash("sha256").update(key).digest("hex");

  return {
    salt: salt.toString("hex"),
    n: KDF_N,
    r: KDF_R,
    p: KDF_P,
    verifier
  };
};

export const verifyMasterPassword = (password: string, meta: MasterKeyMeta): boolean => {
  const salt = Buffer.from(meta.salt, "hex");
  const key = deriveKey(password, salt, meta.n, meta.r, meta.p);
  const computedVerifier = createHash("sha256").update(key).digest("hex");
  return computedVerifier === meta.verifier;
};

export const deriveMasterKey = (password: string, meta: MasterKeyMeta): Buffer => {
  const salt = Buffer.from(meta.salt, "hex");
  return deriveKey(password, salt, meta.n, meta.r, meta.p);
};

export interface EncryptResult {
  ciphertextB64: string;
  ivB64: string;
  tagB64: string;
}

export const encryptAesGcm = (
  plaintext: string,
  key: Buffer,
  aad?: string
): EncryptResult => {
  const iv = randomBytes(IV_LENGTH);
  const cipher = createCipheriv(ALGORITHM, key, iv, { authTagLength: AUTH_TAG_LENGTH });
  if (aad) {
    cipher.setAAD(Buffer.from(aad, "utf8"));
  }
  const encrypted = Buffer.concat([cipher.update(plaintext, "utf8"), cipher.final()]);
  const tag = cipher.getAuthTag();

  return {
    ciphertextB64: encrypted.toString("base64"),
    ivB64: iv.toString("base64"),
    tagB64: tag.toString("base64")
  };
};

export const decryptAesGcm = (
  ciphertextB64: string,
  ivB64: string,
  tagB64: string,
  key: Buffer,
  aad?: string
): string => {
  const iv = Buffer.from(ivB64, "base64");
  const tag = Buffer.from(tagB64, "base64");
  const ciphertext = Buffer.from(ciphertextB64, "base64");

  const decipher = createDecipheriv(ALGORITHM, key, iv, { authTagLength: AUTH_TAG_LENGTH });
  if (aad) {
    decipher.setAAD(Buffer.from(aad, "utf8"));
  }
  decipher.setAuthTag(tag);
  const decrypted = Buffer.concat([decipher.update(ciphertext), decipher.final()]);
  return decrypted.toString("utf8");
};

export const encryptBackupPayload = (data: Buffer, password: string): Buffer => {
  const salt = randomBytes(32);
  const key = deriveKey(password, salt);
  const iv = randomBytes(IV_LENGTH);
  const cipher = createCipheriv(ALGORITHM, key, iv, { authTagLength: AUTH_TAG_LENGTH });
  cipher.setAAD(Buffer.from("nextshell-backup", "utf8"));
  const encrypted = Buffer.concat([cipher.update(data), cipher.final()]);
  const tag = cipher.getAuthTag();

  // Format: [salt(32)] [iv(12)] [tag(16)] [ciphertext(...)]
  return Buffer.concat([salt, iv, tag, encrypted]);
};

export const decryptBackupPayload = (data: Buffer, password: string): Buffer => {
  if (data.length < 32 + IV_LENGTH + AUTH_TAG_LENGTH) {
    throw new Error("Backup data too short");
  }

  const salt = data.subarray(0, 32);
  const iv = data.subarray(32, 32 + IV_LENGTH);
  const tag = data.subarray(32 + IV_LENGTH, 32 + IV_LENGTH + AUTH_TAG_LENGTH);
  const ciphertext = data.subarray(32 + IV_LENGTH + AUTH_TAG_LENGTH);

  const key = deriveKey(password, salt);
  const decipher = createDecipheriv(ALGORITHM, key, iv, { authTagLength: AUTH_TAG_LENGTH });
  decipher.setAAD(Buffer.from("nextshell-backup", "utf8"));
  decipher.setAuthTag(tag);
  const decrypted = Buffer.concat([decipher.update(ciphertext), decipher.final()]);
  return decrypted;
};

// ─── Credential Vault (interface) ───────────────────────────────────────────

export interface CredentialVault {
  storeCredential: (key: string, secret: string) => Promise<string>;
  readCredential: (ref: string) => Promise<string | undefined>;
  deleteCredential: (ref: string) => Promise<void>;
}

// ─── SecretStore DB Interface ───────────────────────────────────────────────

export interface SecretStoreDB {
  putSecret: (id: string, purpose: string, ciphertextB64: string, ivB64: string, tagB64: string, aad: string) => void;
  getSecret: (id: string) => { ciphertext_b64: string; iv_b64: string; tag_b64: string; aad: string } | undefined;
  deleteSecret: (id: string) => void;
  listSecrets: () => Array<{ id: string; purpose: string; ciphertext_b64: string; iv_b64: string; tag_b64: string; aad: string }>;
}

// ─── EncryptedSecretVault ───────────────────────────────────────────────────

const parseSecretRef = (ref: string): string | undefined => {
  if (!ref.startsWith(SECRET_REF_PREFIX)) {
    return undefined;
  }
  return ref.slice(SECRET_REF_PREFIX.length);
};

export class EncryptedSecretVault implements CredentialVault {
  private masterKey: Buffer | undefined;

  constructor(private readonly store: SecretStoreDB) {}

  unlock(masterKey: Buffer): void {
    this.masterKey = masterKey;
  }

  lock(): void {
    this.masterKey = undefined;
  }

  isUnlocked(): boolean {
    return this.masterKey !== undefined;
  }

  private requireKey(): Buffer {
    if (!this.masterKey) {
      throw new Error("Secret vault is locked. Please unlock with your backup password first.");
    }
    return this.masterKey;
  }

  async storeCredential(key: string, secret: string): Promise<string> {
    const masterKey = this.requireKey();
    const id = key;
    const aad = `nextshell-secret:${id}`;
    const { ciphertextB64, ivB64, tagB64 } = encryptAesGcm(secret, masterKey, aad);
    this.store.putSecret(id, "credential", ciphertextB64, ivB64, tagB64, aad);
    return `${SECRET_REF_PREFIX}${id}`;
  }

  async readCredential(ref: string): Promise<string | undefined> {
    const id = parseSecretRef(ref);
    if (!id) {
      return undefined;
    }

    const row = this.store.getSecret(id);
    if (!row) {
      return undefined;
    }

    const masterKey = this.requireKey();
    try {
      return decryptAesGcm(row.ciphertext_b64, row.iv_b64, row.tag_b64, masterKey, row.aad);
    } catch {
      return undefined;
    }
  }

  async deleteCredential(ref: string): Promise<void> {
    const id = parseSecretRef(ref);
    if (!id) {
      return;
    }
    this.store.deleteSecret(id);
  }
}

// ─── Keytar Password Cache ──────────────────────────────────────────────────

const KEYTAR_SERVICE = "NextShell";
const KEYTAR_ACCOUNT = "backup-master-password";

export class KeytarPasswordCache {
  private readonly keytar: KeytarModule | undefined;

  constructor() {
    this.keytar = loadKeytar();
  }

  isAvailable(): boolean {
    return this.keytar !== undefined;
  }

  async remember(password: string): Promise<void> {
    if (!this.keytar) {
      return;
    }
    await this.keytar.setPassword(KEYTAR_SERVICE, KEYTAR_ACCOUNT, password);
  }

  async recall(): Promise<string | undefined> {
    if (!this.keytar) {
      return undefined;
    }
    const value = await this.keytar.getPassword(KEYTAR_SERVICE, KEYTAR_ACCOUNT);
    return value ?? undefined;
  }

  async clear(): Promise<void> {
    if (!this.keytar) {
      return;
    }
    try {
      await this.keytar.deletePassword(KEYTAR_SERVICE, KEYTAR_ACCOUNT);
    } catch {
      // ignore if not found
    }
  }
}
