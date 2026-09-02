import { createRequire } from "node:module";
import { randomBytes, scrypt, createCipheriv, createDecipheriv } from "node:crypto";

const require = createRequire(import.meta.url);

// ─── Secret Ref Prefix ──────────────────────────────────────────────────────

const SECRET_REF_PREFIX = "secret://";
// ─── Keytar (Optional) ──────────────────────────────────────────────────────

interface KeytarModule {
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
const IV_LENGTH = 12;
const AUTH_TAG_LENGTH = 16;
const ALGORITHM = "aes-256-gcm";

type ScryptImplementation = (
  password: string,
  salt: Buffer,
  keyLength: number,
  options: { N: number; r: number; p: number }
) => Promise<Buffer>;

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

export const __setScryptImplForTesting = (implementation: ScryptImplementation): void => {
  scryptImplementation = implementation;
};

export const __resetScryptImplForTesting = (): void => {
  scryptImplementation = defaultScryptImplementation;
};

export const deriveKey = async (
  password: string,
  salt: Buffer,
  n = KDF_N,
  r = KDF_R,
  p = KDF_P
): Promise<Buffer> => {
  return scryptImplementation(password, salt, KEY_LENGTH, { N: n, r, p });
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
  clear: () => Promise<void>;
}

/** Device-key storage in the local database. */
export interface DeviceKeyDbAccess {
  getLegacy: () => string | undefined;
  saveLegacy: (key: string) => void;
}

export interface ResolveDeviceKeyResult {
  deviceKeyHex: string;
  storedIn: "database";
  /**
   * Set only when a legacy keychain item could not be read (denied / locked /
   * backend error) and a fresh key was generated instead: every credential
   * encrypted with the old key is now undecryptable and must be re-entered.
   */
  credentialsUnrecoverable?: true;
}

/**
 * Resolve the device key from the database first. A legacy keychain item is
 * drained into the database once, then best-effort deleted; a failed legacy
 * read starts a new database key so a fresh profile never prompts at startup.
 */
export const resolveDeviceKey = async (
  store: DeviceKeyStore,
  db: DeviceKeyDbAccess,
  generate: () => string = generateDeviceKey
): Promise<ResolveDeviceKeyResult> => {
  const legacy = db.getLegacy();
  if (legacy) {
    return { deviceKeyHex: legacy, storedIn: "database" };
  }

  if (store.isAvailable()) {
    try {
      const keychainKey = await store.recall();
      if (keychainKey) {
        db.saveLegacy(keychainKey);
        try {
          await store.clear();
        } catch {
          // The database copy is authoritative even if cleanup is unavailable.
        }
        return { deviceKeyHex: keychainKey, storedIn: "database" };
      }
    } catch (error) {
      console.warn("[Security] legacy device keychain read failed; generating a new device key", error);
      const deviceKeyHex = generate();
      db.saveLegacy(deviceKeyHex);
      return { deviceKeyHex, storedIn: "database", credentialsUnrecoverable: true };
    }
  }

  const deviceKeyHex = generate();
  db.saveLegacy(deviceKeyHex);
  return { deviceKeyHex, storedIn: "database" };
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

// 过渡代码:仅用于设备密钥一次性 drain,下一版连同 keytar 依赖删除
const KEYTAR_SERVICE = "NextShell";
const KEYTAR_ACCOUNT = "device-key";

export interface KeytarPasswordCacheOptions {
  /** Older service name to check when the primary item is missing. */
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
  private resolvedService: string | undefined;

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
    try {
      await this.keytar.deletePassword(this.resolvedService ?? this.service, this.account);
    } catch {
      // ignore if not found
    }
    this.resolved = { value: undefined };
    this.resolvedService = undefined;
  }

  private async readThrough(): Promise<string | undefined> {
    const keytar = this.keytar;
    if (!keytar) {
      return undefined;
    }

    const primary = (await keytar.getPassword(this.service, this.account)) ?? undefined;
    if (primary) {
      this.resolvedService = this.service;
      return primary;
    }
    if (!this.fallbackService) {
      return undefined;
    }
    const fallback = (await keytar.getPassword(this.fallbackService, this.account)) ?? undefined;
    if (fallback) {
      this.resolvedService = this.fallbackService;
    }
    return fallback;
  }
}
