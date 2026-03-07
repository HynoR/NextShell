import { createCipheriv, createDecipheriv, createHash, randomBytes, scrypt } from "node:crypto";

const ALGORITHM = "aes-256-gcm";
const KEY_LENGTH = 32;
const SALT_LENGTH = 32;
const IV_LENGTH = 12;
const AUTH_TAG_LENGTH = 16;
const KDF_N = 16384;
const KDF_R = 8;
const KDF_P = 1;
const AAD = "nextshell-connection-export";

const deriveKey = (password: string, salt: Buffer): Promise<Buffer> => {
  return new Promise<Buffer>((resolve, reject) => {
    scrypt(password, salt, KEY_LENGTH, { N: KDF_N, r: KDF_R, p: KDF_P }, (error, derivedKey) => {
      if (error) {
        reject(error);
        return;
      }
      resolve(Buffer.from(derivedKey));
    });
  });
};

export const encryptConnectionExportPayload = async (plainJson: string, password: string): Promise<string> => {
  const salt = randomBytes(SALT_LENGTH);
  const key = await deriveKey(password, salt);
  const iv = randomBytes(IV_LENGTH);

  const cipher = createCipheriv(ALGORITHM, key, iv, { authTagLength: AUTH_TAG_LENGTH });
  cipher.setAAD(Buffer.from(AAD, "utf8"));

  const ciphertext = Buffer.concat([cipher.update(plainJson, "utf8"), cipher.final()]);
  const tag = cipher.getAuthTag();

  // Format: [salt(32)] [iv(12)] [tag(16)] [ciphertext(...)]
  return Buffer.concat([salt, iv, tag, ciphertext]).toString("base64");
};

export const decryptConnectionExportPayload = async (payloadB64: string, password: string): Promise<string> => {
  const payload = Buffer.from(payloadB64, "base64");
  const minLength = SALT_LENGTH + IV_LENGTH + AUTH_TAG_LENGTH;

  if (payload.length < minLength) {
    throw new Error("Encrypted payload is too short");
  }

  const salt = payload.subarray(0, SALT_LENGTH);
  const iv = payload.subarray(SALT_LENGTH, SALT_LENGTH + IV_LENGTH);
  const tag = payload.subarray(SALT_LENGTH + IV_LENGTH, SALT_LENGTH + IV_LENGTH + AUTH_TAG_LENGTH);
  const ciphertext = payload.subarray(SALT_LENGTH + IV_LENGTH + AUTH_TAG_LENGTH);

  const key = await deriveKey(password, salt);
  const decipher = createDecipheriv(ALGORITHM, key, iv, { authTagLength: AUTH_TAG_LENGTH });
  decipher.setAAD(Buffer.from(AAD, "utf8"));
  decipher.setAuthTag(tag);

  const decrypted = Buffer.concat([decipher.update(ciphertext), decipher.final()]);
  return decrypted.toString("utf8");
};

// ─── Plain-export password obfuscation (XOR + SHA-256) ────────────────────────
// Key = SHA256(`${name}\x00${host}\x00${port}`), cycled over password bytes.
// XOR is self-inverse, so the same function both obfuscates and deobfuscates.

const passwordObfuscationKey = (name: string, host: string, port: number): Buffer =>
  createHash("sha256")
    .update(`${name}\x00${host}\x00${port}`)
    .digest();

export const obfuscatePassword = (password: string, name: string, host: string, port: number): string => {
  const key = passwordObfuscationKey(name, host, port);
  const input = Buffer.from(password, "utf8");
  const output = Buffer.alloc(input.length);
  for (let i = 0; i < input.length; i++) {
    output[i] = (input[i] as number) ^ (key[i % 32] as number);
  }
  return output.toString("base64");
};

export const deobfuscatePassword = (obfuscated: string, name: string, host: string, port: number): string => {
  const key = passwordObfuscationKey(name, host, port);
  const input = Buffer.from(obfuscated, "base64");
  const output = Buffer.alloc(input.length);
  for (let i = 0; i < input.length; i++) {
    output[i] = (input[i] as number) ^ (key[i % 32] as number);
  }
  return output.toString("utf8");
};
