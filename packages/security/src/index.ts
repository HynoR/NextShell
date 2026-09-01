import { createRequire } from "node:module";
import { randomBytes, scrypt, createCipheriv, createDecipheriv, createHash } from "node:crypto";
import type { MasterKeyMeta } from "../../core/src/index";

const require = createRequire(import.meta.url);

// ─── Secret Ref Prefix ──────────────────────────────────────────────────────

const SECRET_REF_PREFIX = "secret://";

/**
 * Secret-store id for a remembered master password. It lives in the local
 * secret store (encrypted with the device key) rather than in its own keychain
 * item: a separate item cost a second OS authorization prompt while adding no
 * security, since it sat beside the device key and both were equally reachable.
 */
export const MASTER_PASSWORD_SECRET_ID = "master-password";
export const MASTER_PASSWORD_SECRET_REF = `${SECRET_REF_PREFIX}${MASTER_PASSWORD_SECRET_ID}`;
/** `purpose` column value that keeps app-level secrets out of credential sweeps. */
export const APP_SECRET_PURPOSE = "app";

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

const createCacheKey = (
  password: string,
  salt: Buffer,
  n: number,
  r: number,
  p: number
): string => {
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
  const derivationPromise = scryptImplementation(password, salt, KEY_LENGTH, { N: n, r, p })
    .then((derivedKey) => {
      const normalized = cloneBuffer(derivedKey);
      if (
        currentGeneration === cacheGeneration &&
        inFlightDerivedKeys.get(cacheKey) === derivationPromise
      ) {
        touchResolvedKey(cacheKey, normalized);
        inFlightDerivedKeys.delete(cacheKey);
      }
      return normalized;
    })
    .catch((error) => {
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

export const verifyMasterPassword = async (
  password: string,
  meta: MasterKeyMeta
): Promise<boolean> => {
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

export interface WorkspaceSecretEnvelope {
  v: 1;
  alg: typeof ALGORITHM;
  kdf: "scrypt";
  salt: string;
  iv: string;
  aad?: string;
  ciphertext: string;
  tag: string;
}

export const encryptAesGcm = (plaintext: string, key: Buffer, aad?: string): EncryptResult => {
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

export const encryptWorkspaceSecret = async (
  secret: string,
  workspacePassword: string,
  aad?: string
): Promise<WorkspaceSecretEnvelope> => {
  const salt = randomBytes(SALT_LENGTH);
  const key = await deriveKey(workspacePassword, salt);
  const encrypted = encryptAesGcm(secret, key, aad);

  return {
    v: 1,
    alg: ALGORITHM,
    kdf: "scrypt",
    salt: salt.toString("base64"),
    iv: encrypted.ivB64,
    aad,
    ciphertext: encrypted.ciphertextB64,
    tag: encrypted.tagB64
  };
};

export const decryptWorkspaceSecret = async (
  envelope: WorkspaceSecretEnvelope,
  workspacePassword: string
): Promise<string> => {
  if (envelope.v !== 1) {
    throw new Error(`Unsupported workspace secret version: ${String(envelope.v)}`);
  }
  if (envelope.alg !== ALGORITHM || envelope.kdf !== "scrypt") {
    throw new Error("Unsupported workspace secret envelope");
  }

  const salt = Buffer.from(envelope.salt, "base64");
  const key = await deriveKey(workspacePassword, salt);
  return decryptAesGcm(envelope.ciphertext, envelope.iv, envelope.tag, key, envelope.aad);
};

// ─── Credential Vault (interface) ───────────────────────────────────────────

export interface CredentialVault {
  storeCredential: (key: string, secret: string, purpose?: string) => Promise<string>;
  readCredential: (ref: string) => Promise<string | undefined>;
  deleteCredential: (ref: string) => Promise<void>;
}

// ─── SecretStore DB Interface ───────────────────────────────────────────────

export interface SecretStoreDB {
  putSecret: (
    id: string,
    purpose: string,
    ciphertextB64: string,
    ivB64: string,
    tagB64: string,
    aad: string
  ) => void;
  getSecret: (
    id: string
  ) => { ciphertext_b64: string; iv_b64: string; tag_b64: string; aad: string } | undefined;
  deleteSecret: (id: string) => void;
  listSecrets: () => Array<{
    id: string;
    purpose: string;
    ciphertext_b64: string;
    iv_b64: string;
    tag_b64: string;
    aad: string;
  }>;
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

// ─── Device Key Resolution ──────────────────────────────────────────────────

/** System keychain backing for the device key (satisfied by KeytarPasswordCache). */
export interface DeviceKeyStore {
  isAvailable: () => boolean;
  recall: () => Promise<string | undefined>;
  remember: (key: string) => Promise<void>;
}

/** Legacy plaintext device-key storage in the local database. */
export interface DeviceKeyDbAccess {
  getLegacy: () => string | undefined;
  saveLegacy: (key: string) => void;
  clearLegacy: () => void;
}

export interface ResolveDeviceKeyResult {
  deviceKeyHex: string;
  storedIn: "keychain" | "database";
  migratedFromDatabase: boolean;
}

/**
 * The keychain was reachable but refused to hand over the device key — the user
 * denied or cancelled the OS authorization prompt, or the read failed outright.
 * Callers must surface this instead of falling back to a fresh key: the stored
 * credentials are still encrypted with the key that could not be read.
 */
export class KeychainAccessDeniedError extends Error {
  readonly reason: unknown;

  constructor(reason?: unknown) {
    super("系统钥匙串授权被拒绝，无法读取已保存凭据的加密密钥。");
    this.name = "KeychainAccessDeniedError";
    this.reason = reason;
  }
}

/**
 * Resolve the device key that encrypts all stored credentials, preferring the OS
 * keychain over the database so the key never sits in plaintext next to the
 * ciphertext. On first run after upgrade it migrates the legacy plaintext key
 * into the keychain and purges it from the DB.
 *
 * Failure handling distinguishes two cases that must never be conflated:
 * a keychain that is *absent* (no backend on this platform) may degrade to DB
 * storage, but a keychain that is present and *refuses to answer* must not — the
 * existing ciphertext is bound to the key behind that prompt, so minting a
 * replacement would silently render every saved credential undecryptable.
 */
export const resolveDeviceKey = async (
  store: DeviceKeyStore,
  db: DeviceKeyDbAccess,
  generate: () => string = generateDeviceKey
): Promise<ResolveDeviceKeyResult> => {
  const legacy = db.getLegacy();

  if (store.isAvailable()) {
    let existing: string | undefined;
    try {
      existing = await store.recall();
    } catch (error) {
      // A pre-migration install still holds the very same key in the DB, so it
      // can carry on; everyone else has to be told, not silently re-keyed.
      if (legacy) {
        return { deviceKeyHex: legacy, storedIn: "database", migratedFromDatabase: false };
      }
      throw new KeychainAccessDeniedError(error);
    }

    if (existing) {
      // The keychain is authoritative — drop any plaintext copy from the DB.
      if (legacy) {
        db.clearLegacy();
      }
      return { deviceKeyHex: existing, storedIn: "keychain", migratedFromDatabase: false };
    }

    // No key in the keychain yet: migrate the legacy plaintext key or mint one.
    const candidate = legacy ?? generate();
    try {
      await store.remember(candidate);
      if (legacy) {
        db.clearLegacy();
      }
      return {
        deviceKeyHex: candidate,
        storedIn: "keychain",
        migratedFromDatabase: Boolean(legacy)
      };
    } catch {
      // Nothing was in the keychain to begin with, so DB storage loses nothing.
      if (!legacy) {
        db.saveLegacy(candidate);
      }
      return { deviceKeyHex: candidate, storedIn: "database", migratedFromDatabase: false };
    }
  }

  const deviceKeyHex = legacy ?? generate();
  if (!legacy) {
    db.saveLegacy(deviceKeyHex);
  }
  return { deviceKeyHex, storedIn: "database", migratedFromDatabase: false };
};

/**
 * Either the device key itself, or a resolver that produces it on demand. The
 * lazy form lets the app defer the OS keychain read (and the authorization
 * prompt that comes with it on macOS) until a secret is actually needed.
 */
export type DeviceKeyResolver = Buffer | (() => Promise<Buffer>);

export class EncryptedSecretVault implements CredentialVault {
  constructor(
    private readonly store: SecretStoreDB,
    private readonly deviceKey: DeviceKeyResolver
  ) {}

  private async resolveDeviceKey(): Promise<Buffer> {
    return typeof this.deviceKey === "function" ? await this.deviceKey() : this.deviceKey;
  }

  async storeCredential(key: string, secret: string, purpose = "credential"): Promise<string> {
    const id = key;
    const aad = `nextshell-secret:${id}`;
    const deviceKey = await this.resolveDeviceKey();
    const { ciphertextB64, ivB64, tagB64 } = encryptAesGcm(secret, deviceKey, aad);
    this.store.putSecret(id, purpose, ciphertextB64, ivB64, tagB64, aad);
    return `${SECRET_REF_PREFIX}${id}`;
  }

  async readCredential(ref: string): Promise<string | undefined> {
    const id = parseSecretRef(ref);
    if (!id) return undefined;
    const row = this.store.getSecret(id);
    if (!row) return undefined;
    // Resolved outside the try below on purpose: a keychain that is locked or
    // denied must surface as an error, not masquerade as "no secret stored".
    const deviceKey = await this.resolveDeviceKey();
    try {
      return decryptAesGcm(row.ciphertext_b64, row.iv_b64, row.tag_b64, deviceKey, row.aad);
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

export interface KeytarPasswordCacheOptions {
  /**
   * Older service name to adopt from when the primary item is missing. Lets a
   * dev build own a separate keychain item without orphaning secrets written
   * under the shared name; the value is copied into the primary item on first
   * read so later launches only touch the primary.
   */
  fallbackService?: string;
  /** Injected keytar module (tests only). */
  keytar?: KeytarModule;
}

export class KeytarPasswordCache {
  private readonly keytar: KeytarModule | undefined;
  private readonly service: string;
  private readonly account: string;
  private readonly fallbackService: string | undefined;
  /** Memoized read, including a negative result — `undefined` means "not read yet". */
  private resolved: { value: string | undefined } | undefined;
  private inFlight: Promise<string | undefined> | undefined;

  constructor(
    service = KEYTAR_SERVICE,
    account = KEYTAR_ACCOUNT,
    options: KeytarPasswordCacheOptions = {}
  ) {
    this.keytar = options.keytar ?? loadKeytar();
    this.service = service;
    this.account = account;
    this.fallbackService = options.fallbackService;
  }

  isAvailable(): boolean {
    return this.keytar !== undefined;
  }

  async remember(password: string): Promise<void> {
    if (!this.keytar) {
      return;
    }
    this.invalidate();
    await this.keytar.setPassword(this.service, this.account, password);
    this.resolved = { value: password };
  }

  /**
   * Read the secret, hitting the OS keychain at most once per process. macOS
   * prompts for authorization on every keychain access whose ACL does not match
   * the running binary, so repeated reads mean repeated password dialogs.
   */
  async recall(): Promise<string | undefined> {
    if (!this.keytar) {
      return undefined;
    }
    if (this.resolved) {
      return this.resolved.value;
    }
    if (this.inFlight) {
      return this.inFlight;
    }

    const request = this.readThrough()
      .then((value) => {
        this.resolved = { value };
        this.inFlight = undefined;
        return value;
      })
      .catch((error: unknown) => {
        // Leave the memo empty so a transient failure can be retried.
        this.inFlight = undefined;
        throw error;
      });

    this.inFlight = request;
    return request;
  }

  async clear(): Promise<void> {
    if (!this.keytar) {
      return;
    }
    this.invalidate();
    try {
      await this.keytar.deletePassword(this.service, this.account);
    } catch {
      // ignore if not found
    }
    this.resolved = { value: undefined };
  }

  /** Drop the memoized read so the next `recall()` goes back to the keychain. */
  invalidate(): void {
    this.resolved = undefined;
    this.inFlight = undefined;
  }

  private async readThrough(): Promise<string | undefined> {
    const keytar = this.keytar;
    if (!keytar) {
      return undefined;
    }

    const primary = await keytar.getPassword(this.service, this.account);
    if (primary) {
      return primary;
    }
    if (!this.fallbackService) {
      return undefined;
    }

    const adopted = await keytar.getPassword(this.fallbackService, this.account);
    if (!adopted) {
      return undefined;
    }
    try {
      await keytar.setPassword(this.service, this.account, adopted);
    } catch {
      // Adoption is best-effort; the value is still usable this run.
    }
    return adopted;
  }
}
