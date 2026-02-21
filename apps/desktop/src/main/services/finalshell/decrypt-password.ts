import { createHash } from "node:crypto";
import CryptoJS from "crypto-js";

const JAVA_RANDOM_MULTIPLIER = 25214903917n;
const JAVA_RANDOM_ADDEND = 11n;
const JAVA_RANDOM_MASK = (1n << 48n) - 1n;
const JAVA_LONG_MAGIC = 3680984568597093857n;
const DES_KEY_LENGTH = 8;
const FINALSHELL_HEAD_LENGTH = 8;

const toSignedByte = (byteValue: number): number => {
  return byteValue > 127 ? byteValue - 256 : byteValue;
};

const toSignedInt32 = (value: number): number => {
  return value | 0;
};

const readHeadByte = (head: Buffer, index: number, useSignedByte: boolean): number => {
  const value = head[index] ?? 0;
  return useSignedByte ? toSignedByte(value) : value;
};

class JavaRandom {
  private seed: bigint;

  constructor(seed: bigint | number) {
    this.seed = (BigInt(seed) ^ JAVA_RANDOM_MULTIPLIER) & JAVA_RANDOM_MASK;
  }

  private next(bits: number): number {
    this.seed = (this.seed * JAVA_RANDOM_MULTIPLIER + JAVA_RANDOM_ADDEND) & JAVA_RANDOM_MASK;
    return Number(this.seed >> BigInt(48 - bits));
  }

  nextInt(bound: number): number {
    if (bound <= 0) {
      throw new RangeError("bound must be positive");
    }

    if ((bound & -bound) === bound) {
      return Math.floor((bound * this.next(31)) / 0x80000000);
    }

    while (true) {
      const bits = this.next(31);
      const value = bits % bound;
      if (bits - value + (bound - 1) >= 0) {
        return value;
      }
    }
  }

  nextLong(): bigint {
    const high = BigInt(toSignedInt32(this.next(32)));
    const low = BigInt(toSignedInt32(this.next(32)));
    return BigInt.asIntN(64, (high << 32n) + low);
  }
}

const deriveFinalShellDesKey = (head: Buffer, useSignedByte: boolean): Buffer | undefined => {
  const seedRandom = new JavaRandom(BigInt(readHeadByte(head, 5, useSignedByte)));
  const divisor = seedRandom.nextInt(127);
  if (divisor === 0) {
    return undefined;
  }

  const ks = BigInt.asIntN(64, JAVA_LONG_MAGIC / BigInt(divisor));
  const random = new JavaRandom(ks);
  const t = readHeadByte(head, 0, useSignedByte);

  for (let i = 0; i < t; i += 1) {
    random.nextLong();
  }

  const n = random.nextLong();
  const r2 = new JavaRandom(n);
  const ldValues: bigint[] = [
    BigInt(readHeadByte(head, 4, useSignedByte)),
    r2.nextLong(),
    BigInt(readHeadByte(head, 7, useSignedByte)),
    BigInt(readHeadByte(head, 3, useSignedByte)),
    r2.nextLong(),
    BigInt(readHeadByte(head, 1, useSignedByte)),
    random.nextLong(),
    BigInt(readHeadByte(head, 2, useSignedByte))
  ];

  const keyMaterial = Buffer.alloc(ldValues.length * 8);
  ldValues.forEach((value, index) => {
    keyMaterial.writeBigInt64BE(value, index * 8);
  });

  return createHash("md5").update(keyMaterial).digest().subarray(0, DES_KEY_LENGTH);
};

const decryptWithKey = (ciphertext: Buffer, key: Buffer): string | undefined => {
  try {
    const decrypted = CryptoJS.DES.decrypt(
      CryptoJS.lib.CipherParams.create({
        ciphertext: CryptoJS.enc.Hex.parse(ciphertext.toString("hex"))
      }),
      CryptoJS.enc.Hex.parse(key.toString("hex")),
      {
        mode: CryptoJS.mode.ECB,
        padding: CryptoJS.pad.Pkcs7
      }
    );
    if (decrypted.sigBytes <= 0) {
      return undefined;
    }

    const text = decrypted.toString(CryptoJS.enc.Utf8);
    return text.length > 0 ? text : undefined;
  } catch {
    return undefined;
  }
};

export const decryptFinalShellPassword = (encoded: string | undefined): string | undefined => {
  if (!encoded) {
    return undefined;
  }

  let decoded: Buffer;
  try {
    decoded = Buffer.from(encoded, "base64");
  } catch {
    return undefined;
  }

  if (decoded.length <= FINALSHELL_HEAD_LENGTH) {
    return undefined;
  }

  const head = decoded.subarray(0, FINALSHELL_HEAD_LENGTH);
  const ciphertext = decoded.subarray(FINALSHELL_HEAD_LENGTH);
  const unsignedKey = deriveFinalShellDesKey(head, false);
  const unsignedResult = unsignedKey ? decryptWithKey(ciphertext, unsignedKey) : undefined;
  if (unsignedResult !== undefined) {
    return unsignedResult;
  }

  const signedKey = deriveFinalShellDesKey(head, true);
  return signedKey ? decryptWithKey(ciphertext, signedKey) : undefined;
};
