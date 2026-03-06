import { createRequire } from "node:module";
import { randomBytes, scrypt, createCipheriv, createDecipheriv, createHash } from "node:crypto";
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
const SALT_LENGTH = 32;
const DERIVED_KEY_CACHE_LIMIT = 32;
const IV_LENGTH = 12;
const AUTH_TAG_LENGTH = 16;
const ALGORITHM = "aes-256-gcm";

type ScryptImplementation = (
  password: string,
  salt: Buffer,
  keyLength: number,
  options: { N: number; r: number; p: number }
) => Promise<Buffer>;

const createCacheKey = (password: string, salt: Buffer, n: number, r: number, p: number): string => {
  const passwordDigest = createHash("sha256").update(password, "utf8").digest("hex");
  return `${passwordDigest}:${salt.toString("hex")}:${n}:${r}:${p}`;
};

const cloneBuffer = (value: Buffer): Buffer => Buffer.from(value);

const wipeBuffer = (value: Buffer): void => {
  value.fill(0);
};

const resolvedDerivedKeys = new Map<string, Buffer>();
const inFlightDerivedKeys = new Map<string, Promise<Buffer>>();
let cacheGeneration = 0;

const defaultScryptImplementation: ScryptImplementation = (password, salt, keyLength, options) => {
  return new Promise<Buffer>((resolve, reject) => {
    scrypt(password, salt, keyLength, options, (error, derivedKey) => {
      if (error) {
        reject(error);
        return;
      }
      resolve(Buffer.from(derivedKey));
    });
  });
};

let scryptImplementation: ScryptImplementation = defaultScryptImplementation;

const touchResolvedKey = (cacheKey: string, derivedKey: Buffer): void => {
  if (resolvedDerivedKeys.has(cacheKey)) {
    resolvedDerivedKeys.delete(cacheKey);
  }
  resolvedDerivedKeys.set(cacheKey, derivedKey);
  if (resolvedDerivedKeys.size <= DERIVED_KEY_CACHE_LIMIT) {
    return;
  }
  const oldestKey = resolvedDerivedKeys.keys().next().value;
  if (oldestKey) {
    const oldestDerivedKey = resolvedDerivedKeys.get(oldestKey);
    resolvedDerivedKeys.delete(oldestKey);
    if (oldestDerivedKey) {
      wipeBuffer(oldestDerivedKey);
    }
  }
};

export const clearDerivedKeyCache = (): void => {
  cacheGeneration += 1;
  for (const derivedKey of resolvedDerivedKeys.values()) {
    wipeBuffer(derivedKey);
  }
  resolvedDerivedKeys.clear();
  inFlightDerivedKeys.clear();
};

export const __setScryptImplForTesting = (implementation: ScryptImplementation): void => {
  scryptImplementation = implementation;
  clearDerivedKeyCache();
};

export const __resetScryptImplForTesting = (): void => {
  scryptImplementation = defaultScryptImplementation;
  clearDerivedKeyCache();
};

export const deriveKey = async (
  password: string,
  salt: Buffer,
  n = KDF_N,
  r = KDF_R,
  p = KDF_P
): Promise<Buffer> => {
  const cacheKey = createCacheKey(password, salt, n, r, p);
  const cached = resolvedDerivedKeys.get(cacheKey);
  if (cached) {
    touchResolvedKey(cacheKey, cached);
    return cloneBuffer(cached);
  }

  const inFlight = inFlightDerivedKeys.get(cacheKey);
  if (inFlight) {
    return cloneBuffer(await inFlight);
  }

  const currentGeneration = cacheGeneration;
  const derivationPromise = scryptImplementation(password, salt, KEY_LENGTH, { N: n, r, p }).then((derivedKey) => {
    const normalized = cloneBuffer(derivedKey);
    if (currentGeneration === cacheGeneration && inFlightDerivedKeys.get(cacheKey) === derivationPromise) {
      touchResolvedKey(cacheKey, normalized);
      inFlightDerivedKeys.delete(cacheKey);
    }
    return normalized;
  }).catch((error) => {
    if (inFlightDerivedKeys.get(cacheKey) === derivationPromise) {
      inFlightDerivedKeys.delete(cacheKey);
    }
    throw error;
  });

  inFlightDerivedKeys.set(cacheKey, derivationPromise);
  return cloneBuffer(await derivationPromise);
};

export const createMasterKeyMeta = async (password: string): Promise<MasterKeyMeta> => {
  const salt = randomBytes(SALT_LENGTH);
  const key = await deriveKey(password, salt);
  const verifier = createHash("sha256").update(key).digest("hex");

  return {
    salt: salt.toString("hex"),
    n: KDF_N,
    r: KDF_R,
    p: KDF_P,
    verifier
  };
};

export const verifyMasterPassword = async (password: string, meta: MasterKeyMeta): Promise<boolean> => {
  const salt = Buffer.from(meta.salt, "hex");
  const key = await deriveKey(password, salt, meta.n, meta.r, meta.p);
  const computedVerifier = createHash("sha256").update(key).digest("hex");
  return computedVerifier === meta.verifier;
};

export const deriveMasterKey = async (password: string, meta: MasterKeyMeta): Promise<Buffer> => {
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

export const encryptBackupPayload = async (data: Buffer, password: string): Promise<Buffer> => {
  const salt = randomBytes(SALT_LENGTH);
  const key = await deriveKey(password, salt);
  const iv = randomBytes(IV_LENGTH);
  const cipher = createCipheriv(ALGORITHM, key, iv, { authTagLength: AUTH_TAG_LENGTH });
  cipher.setAAD(Buffer.from("nextshell-backup", "utf8"));
  const encrypted = Buffer.concat([cipher.update(data), cipher.final()]);
  const tag = cipher.getAuthTag();

  // Format: [salt(32)] [iv(12)] [tag(16)] [ciphertext(...)]
  return Buffer.concat([salt, iv, tag, encrypted]);
};

export const decryptBackupPayload = async (data: Buffer, password: string): Promise<Buffer> => {
  if (data.length < SALT_LENGTH + IV_LENGTH + AUTH_TAG_LENGTH) {
    throw new Error("Backup data too short");
  }

  const salt = data.subarray(0, SALT_LENGTH);
  const iv = data.subarray(SALT_LENGTH, SALT_LENGTH + IV_LENGTH);
  const tag = data.subarray(SALT_LENGTH + IV_LENGTH, SALT_LENGTH + IV_LENGTH + AUTH_TAG_LENGTH);
  const ciphertext = data.subarray(SALT_LENGTH + IV_LENGTH + AUTH_TAG_LENGTH);

  const key = await deriveKey(password, salt);
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

export const generateDeviceKey = (): string => {
  return randomBytes(32).toString("hex");
};

export class EncryptedSecretVault implements CredentialVault {
  constructor(
    private readonly store: SecretStoreDB,
    private readonly deviceKey: Buffer
  ) {}

  async storeCredential(key: string, secret: string): Promise<string> {
    const id = key;
    const aad = `nextshell-secret:${id}`;
    const { ciphertextB64, ivB64, tagB64 } = encryptAesGcm(secret, this.deviceKey, aad);
    this.store.putSecret(id, "credential", ciphertextB64, ivB64, tagB64, aad);
    return `${SECRET_REF_PREFIX}${id}`;
  }

  async readCredential(ref: string): Promise<string | undefined> {
    const id = parseSecretRef(ref);
    if (!id) return undefined;
    const row = this.store.getSecret(id);
    if (!row) return undefined;
    try {
      return decryptAesGcm(row.ciphertext_b64, row.iv_b64, row.tag_b64, this.deviceKey, row.aad);
    } catch {
      return undefined;
    }
  }

  async deleteCredential(ref: string): Promise<void> {
    const id = parseSecretRef(ref);
    if (!id) return;
    this.store.deleteSecret(id);
  }
}

// ─── Keytar Password Cache ──────────────────────────────────────────────────

const KEYTAR_SERVICE = "NextShell";
const KEYTAR_ACCOUNT = "backup-password";

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
