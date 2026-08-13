import { createHash, generateKeyPairSync, randomBytes } from "node:crypto";
import { createRequire } from "node:module";
import type { ParsedKey } from "ssh2";

const require = createRequire(import.meta.url);

interface Ssh2Utils {
  parseKey: (data: string | Buffer, passphrase?: string) => ParsedKey | ParsedKey[] | Error;
}

/**
 * ssh2 是 CommonJS，主进程打成 ESM 后具名导入会在 Node 的 ESM loader 里失败
 * （`Named export 'utils' not found`）。与 `packages/ssh/src/index.ts` 一致地用 createRequire，
 * 模块名拼接是为了不让打包器把它静态解析掉。
 */
const loadSsh2Utils = (): Ssh2Utils => {
  const moduleName = `ssh${2}`;
  return (require(moduleName) as { utils: Ssh2Utils }).utils;
};

/**
 * 私钥的可展示元数据。保存密钥时解析出来落库,让用户能核对"服务器上装的是不是这一把",
 * 也让贴错内容在保存那一刻就失败,而不是等到连接时才报认证错误。
 */
export interface SshKeyMaterialInfo {
  /** ssh2 报告的算法名,例如 `ssh-ed25519`、`ssh-rsa`。 */
  keyType: string;
  /** RSA 为模数位数;ed25519 恒为 256;无法判定时缺省。 */
  bits?: number;
  /** 私钥里带的注释,通常是 user@host。 */
  comment?: string;
  /** OpenSSH 公钥指纹,与 `ssh-keygen -lf` 输出一致。 */
  fingerprint: string;
  /** `ssh-ed25519 AAAA… comment` 形式的公钥单行文本,可直接贴进 authorized_keys。 */
  publicKeyLine: string;
}

export type SshKeyAlgorithm = "ed25519" | "rsa-2048" | "rsa-4096";

export interface GeneratedSshKey {
  /** OpenSSH 私钥文本;调用方负责把它交给 vault,绝不落明文。 */
  privateKey: string;
  info: SshKeyMaterialInfo;
}

const u32 = (value: number): Buffer => {
  const buf = Buffer.alloc(4);
  buf.writeUInt32BE(value, 0);
  return buf;
};

const sshString = (payload: Buffer): Buffer => Buffer.concat([u32(payload.length), payload]);

const sshText = (value: string): Buffer => sshString(Buffer.from(value, "utf8"));

const fingerprintOf = (publicBlob: Buffer): string =>
  `SHA256:${createHash("sha256").update(publicBlob).digest("base64").replace(/=+$/, "")}`;

/**
 * RSA 公钥 blob 的结构是 `string "ssh-rsa" | mpint e | mpint n`,模数位数要从 n 上量。
 * mpint 最高位为 1 时前面会补一个 0x00 字节,直接用字节数 ×8 会多算 8 位。
 */
const rsaBitsFromPublicBlob = (publicBlob: Buffer): number | undefined => {
  let offset = 0;
  const readField = (): Buffer | undefined => {
    if (offset + 4 > publicBlob.length) {
      return undefined;
    }
    const length = publicBlob.readUInt32BE(offset);
    offset += 4;
    if (offset + length > publicBlob.length) {
      return undefined;
    }
    const field = publicBlob.subarray(offset, offset + length);
    offset += length;
    return field;
  };

  readField(); // algorithm name
  readField(); // public exponent
  const modulus = readField();
  if (!modulus || modulus.length === 0) {
    return undefined;
  }
  let start = 0;
  while (start < modulus.length && modulus[start] === 0) {
    start += 1;
  }
  const significant = modulus.subarray(start);
  const first = significant[0];
  if (first === undefined) {
    return undefined;
  }
  return (significant.length - 1) * 8 + (32 - Math.clz32(first));
};

/**
 * 解析私钥文本。内容不合法(贴成公钥、被截断、passphrase 不对)一律抛错——这正是"保存即校验"
 * 想要的效果。
 */
export const parseSshKeyMaterial = (
  privateKey: string,
  passphrase?: string
): SshKeyMaterialInfo => {
  const utils = loadSsh2Utils();
  const parsed = passphrase
    ? utils.parseKey(privateKey, passphrase)
    : utils.parseKey(privateKey);
  if (parsed instanceof Error) {
    throw new Error(`无法解析私钥：${parsed.message}`);
  }
  const key = Array.isArray(parsed) ? parsed[0] : parsed;
  if (!key) {
    throw new Error("无法解析私钥：文件中没有可用的密钥");
  }
  if (!key.isPrivateKey()) {
    throw new Error("这看起来是一个公钥，请提供私钥内容");
  }

  const publicBlob = key.getPublicSSH();
  const comment = key.comment?.trim() || undefined;
  const bits =
    key.type === "ssh-ed25519"
      ? 256
      : key.type === "ssh-rsa"
        ? rsaBitsFromPublicBlob(publicBlob)
        : undefined;

  return {
    keyType: key.type,
    bits,
    comment,
    fingerprint: fingerprintOf(publicBlob),
    publicKeyLine: [key.type, publicBlob.toString("base64"), comment]
      .filter((part): part is string => Boolean(part))
      .join(" ")
  };
};

/**
 * 编码为 OpenSSH 私钥格式而不是 PKCS8 PEM:ssh2 解析不了 PKCS8 的 ed25519
 * (`Unsupported key format`),而 OpenSSH 格式两边都认。
 */
const encodeOpenSshEd25519 = (
  rawPrivate: Buffer,
  rawPublic: Buffer,
  comment: string
): string => {
  const publicBlob = Buffer.concat([sshText("ssh-ed25519"), sshString(rawPublic)]);
  // 两个 checkint 必须相等,解密方靠它判断 passphrase 是否正确;这里不加密,随机值即可。
  const checkInt = randomBytes(4);
  let privateSection = Buffer.concat([
    checkInt,
    checkInt,
    sshText("ssh-ed25519"),
    sshString(rawPublic),
    sshString(Buffer.concat([rawPrivate, rawPublic])),
    sshText(comment)
  ]);
  let padByte = 1;
  while (privateSection.length % 8 !== 0) {
    privateSection = Buffer.concat([privateSection, Buffer.from([padByte])]);
    padByte += 1;
  }

  const body = Buffer.concat([
    Buffer.from("openssh-key-v1\0", "binary"),
    sshText("none"), // ciphername
    sshText("none"), // kdfname
    sshText(""), // kdfoptions
    u32(1), // key count
    sshString(publicBlob),
    sshString(privateSection)
  ]);

  const base64 = body.toString("base64").replace(/(.{70})/g, "$1\n");
  return `-----BEGIN OPENSSH PRIVATE KEY-----\n${base64}\n-----END OPENSSH PRIVATE KEY-----\n`;
};

/**
 * 生成一把新密钥。ed25519 走 OpenSSH 格式手工编码,RSA 直接用 PKCS#1 PEM(ssh2 原生支持)。
 * 不加 passphrase:私钥立即进 vault,再叠一层用户口令只会让"生成→绑定"多一道坎。
 */
export const generateSshKeyPair = (
  algorithm: SshKeyAlgorithm,
  comment = ""
): GeneratedSshKey => {
  if (algorithm === "ed25519") {
    const { privateKey, publicKey } = generateKeyPairSync("ed25519");
    // 原始 32 字节标量/公钥分别位于 PKCS8 与 SPKI DER 的尾部,固定长度所以可以直接截取。
    const privateDer = privateKey.export({ type: "pkcs8", format: "der" });
    const publicDer = publicKey.export({ type: "spki", format: "der" });
    const pem = encodeOpenSshEd25519(
      privateDer.subarray(privateDer.length - 32),
      publicDer.subarray(publicDer.length - 32),
      comment
    );
    return { privateKey: pem, info: parseSshKeyMaterial(pem) };
  }

  const modulusLength = algorithm === "rsa-4096" ? 4096 : 2048;
  const { privateKey } = generateKeyPairSync("rsa", {
    modulusLength,
    privateKeyEncoding: { type: "pkcs1", format: "pem" },
    publicKeyEncoding: { type: "pkcs1", format: "pem" }
  });
  return { privateKey, info: parseSshKeyMaterial(privateKey) };
};
