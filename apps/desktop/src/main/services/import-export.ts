import { createHash } from "node:crypto";
import type {
  AuthType,
  BackspaceMode,
  ConnectionExportFile,
  ConnectionImportEntry,
  DeleteMode,
  ExportedSshKeyRef,
  TerminalEncoding
} from "../../../../../packages/core/src/index";
import { deobfuscatePassword } from "./connection-export-crypto";
import { decryptFinalShellPassword } from "./finalshell/decrypt-password";
import {
  CONNECTION_ZONES,
  extractZone,
  isValidZone,
  getSubPath
} from "../../../../../packages/shared/src/constants";

/**
 * Remap an imported connection's groupPath into the /import zone.
 * Strips any existing zone prefix so the user-visible sub-path is preserved.
 * @example remapToImportZone("/server/hk")        → "/import/hk"
 * @example remapToImportZone("/workspace/team/prod") → "/import/team/prod"
 * @example remapToImportZone("/import/old")        → "/import/old"
 * @example remapToImportZone("/mygroup/foo")       → "/import/mygroup/foo"
 */
const remapToImportZone = (groupPath: string): string => {
  if (!groupPath) return `/${CONNECTION_ZONES.IMPORT}`;
  const zone = extractZone(groupPath);
  if (isValidZone(zone)) {
    // Strip the zone prefix, keep sub-path
    const sub = getSubPath(groupPath);
    return sub ? `/${CONNECTION_ZONES.IMPORT}${sub}` : `/${CONNECTION_ZONES.IMPORT}`;
  }
  // Not a valid zone — treat entire path (after leading /) as sub-path
  const normalized = groupPath.startsWith("/") ? groupPath : "/" + groupPath;
  return `/${CONNECTION_ZONES.IMPORT}${normalized}`;
};

interface ImportParseOptions {
  groupPathOverride?: string;
}

export interface SshKeyMatchCandidate {
  id: string;
  name: string;
  fingerprint?: string;
}

/**
 * 私钥**文本**的摘要,不是 OpenSSH 的公钥指纹。前缀刻意写成 `sha256-content:` 而非 `SHA256:`:
 * 后者是 `ssh-keygen -lf` 的格式,等密钥子系统能真正解析私钥后 `sshKeyRef.fingerprint` 会换成
 * 那个值。两种前缀不会互相误判——旧导出文件届时匹配不上,自动退回按名称匹配并提示重新绑定,
 * 而不是悄悄绑到一把不相干的密钥上。
 */
export const hashSshKeyContent = (keyContent: string): string => {
  const sha256Base64 = createHash("sha256").update(keyContent.trim()).digest("base64");
  return `sha256-content:${sha256Base64.replace(/=+$/, "")}`;
};

export const matchSshKeyRef = (
  ref: ExportedSshKeyRef | undefined,
  keys: readonly SshKeyMatchCandidate[]
): string | undefined => {
  if (!ref) {
    return undefined;
  }
  if (ref.fingerprint) {
    const byFingerprint = keys.find(
      (key) => key.fingerprint && key.fingerprint === ref.fingerprint
    );
    if (byFingerprint) {
      return byFingerprint.id;
    }
  }
  const byName = keys.filter((key) => key.name === ref.name);
  return byName.length === 1 ? byName[0]?.id : undefined;
};

export const resolveImportedAuth = (
  entry: {
    authType: AuthType;
    sshKeyId?: string;
    sshKeyRef?: ExportedSshKeyRef;
    originalAuth?: AuthType;
  },
  keys: readonly SshKeyMatchCandidate[]
): {
  authType: AuthType;
  sshKeyId?: string;
  needsKeyRebind: boolean;
  originalAuth?: AuthType;
} => {
  if (entry.sshKeyId) {
    return {
      authType: "privateKey",
      sshKeyId: entry.sshKeyId,
      needsKeyRebind: false
    };
  }

  const matchedId = matchSshKeyRef(entry.sshKeyRef, keys);
  if (matchedId) {
    return {
      authType: "privateKey",
      sshKeyId: matchedId,
      needsKeyRebind: false
    };
  }

  if (entry.authType === "privateKey" || entry.originalAuth === "privateKey") {
    return {
      authType: "interactive",
      needsKeyRebind: true,
      originalAuth: "privateKey"
    };
  }

  return {
    authType: entry.authType,
    needsKeyRebind: false,
    originalAuth: entry.originalAuth
  };
};

export const enrichImportEntry = (
  entry: ConnectionImportEntry,
  keys: readonly SshKeyMatchCandidate[]
): ConnectionImportEntry => {
  const resolved = resolveImportedAuth(entry, keys);
  if (resolved.sshKeyId) {
    return {
      ...entry,
      authType: "privateKey",
      sshKeyId: resolved.sshKeyId,
      needsKeyRebind: false,
      originalAuth: undefined
    };
  }
  if (entry.authType === "privateKey" || entry.originalAuth === "privateKey") {
    return {
      ...entry,
      sshKeyId: undefined,
      needsKeyRebind: true,
      originalAuth: "privateKey"
    };
  }
  return {
    ...entry,
    needsKeyRebind: false
  };
};

// ─── Format detection ────────────────────────────────────────────────────────

export const isNextShellFormat = (data: unknown): data is ConnectionExportFile => {
  if (typeof data !== "object" || data === null) return false;
  return (data as Record<string, unknown>)["format"] === "nextshell-connections";
};

interface FinalShellEntry {
  name?: string;
  host?: string;
  port?: number;
  user_name?: string;
  authentication_type?: number;
  password?: string;
  secret_key_id?: string;
  parent_id?: string;
  terminal_encoding?: string;
  backspace_key_sequence?: number;
}

export const isFinalShellFormat = (data: unknown): boolean => {
  if (Array.isArray(data)) {
    return data.length > 0 && isFinalShellEntry(data[0]);
  }
  return isFinalShellEntry(data);
};

const isFinalShellEntry = (data: unknown): boolean => {
  if (typeof data !== "object" || data === null) return false;
  const record = data as Record<string, unknown>;
  return "authentication_type" in record && "user_name" in record;
};

const parseExportedSshKeyRef = (value: unknown): ExportedSshKeyRef | undefined => {
  if (typeof value !== "object" || value === null) {
    return undefined;
  }
  const record = value as Record<string, unknown>;
  if (typeof record.name !== "string" || record.name.trim().length === 0) {
    return undefined;
  }
  const fingerprint =
    typeof record.fingerprint === "string" && record.fingerprint.trim().length > 0
      ? record.fingerprint.trim()
      : undefined;
  return { name: record.name.trim(), fingerprint };
};

// ─── NextShell format parser ─────────────────────────────────────────────────

export const parseNextShellImport = (
  data: ConnectionExportFile,
  options: ImportParseOptions = {}
): ConnectionImportEntry[] => {
  const deobfuscate = data.passwordsObfuscated === true;
  return data.connections.map((conn) => {
    const password =
      conn.password !== undefined && deobfuscate
        ? deobfuscatePassword(conn.password, conn.name, conn.host, conn.port)
        : conn.password;
    const sshKeyRef = parseExportedSshKeyRef(conn.sshKeyRef);
    return {
      name: conn.name,
      host: conn.host,
      port: conn.port,
      username: conn.username,
      authType: conn.authType,
      password,
      keepAliveEnabled: conn.keepAliveEnabled,
      keepAliveIntervalSec: conn.keepAliveIntervalSec,
      groupPath: options.groupPathOverride ?? remapToImportZone(conn.groupPath),
      tags: conn.tags,
      notes: conn.notes,
      favorite: conn.favorite,
      terminalEncoding: conn.terminalEncoding,
      backspaceMode: conn.backspaceMode,
      deleteMode: conn.deleteMode,
      monitorSession: conn.monitorSession,
      sshKeyRef,
      sourceFormat: "nextshell"
    };
  });
};

// ─── FinalShell compatibility parser ─────────────────────────────────────────

export const mapFinalShellEncoding = (encoding: string | undefined): TerminalEncoding => {
  if (!encoding) return "utf-8";
  const lower = encoding.toLowerCase().replace(/[-_\s]/g, "");
  if (lower === "utf8") return "utf-8";
  if (lower === "gb18030") return "gb18030";
  if (lower === "gbk") return "gbk";
  if (lower === "big5") return "big5";
  return "utf-8";
};

const mapFinalShellBackspace = (seq: number | undefined): BackspaceMode => {
  return seq === 2 ? "ascii-delete" : "ascii-backspace";
};

/**
 * 只按 `secret_key_id` 判定密钥认证,**不解释 `authentication_type` 的数值**。
 *
 * 该字段的数值编码没有公开文档,本仓库也没有真实 FinalShell 配置样本可以佐证:历史注释断言
 * “2 → 密码”,后来的改动又按“1 → 密码”实现,两种说法都无法验证。猜错的代价不对称——把密码
 * 主机误判成密钥认证会让最常见的一批条目全部挂上“需重新绑定密钥”的红标;而漏判只是退回旧
 * 行为(按密码导入),失败方向是安全的。所以这里只用“条目明确引用了一把密钥”这个自解释信号。
 *
 * 拿到真实配置样本后,可以在此补上数值分支并附上样本来源。
 */
export const mapFinalShellAuth = (
  secretKeyId?: string
): {
  authType: AuthType;
  originalAuth?: AuthType;
  needsKeyRebind?: boolean;
} => {
  if (!secretKeyId || secretKeyId.trim().length === 0) {
    return { authType: "password" };
  }
  return {
    authType: "interactive",
    originalAuth: "privateKey",
    needsKeyRebind: true
  };
};

const parseOneFinalShellEntry = (
  entry: FinalShellEntry,
  options: ImportParseOptions = {}
): ConnectionImportEntry => {
  const host = entry.host ?? "unknown";
  const port = entry.port ?? 22;
  const name = entry.name || `${host}:${port}`;
  const decryptedPassword = decryptFinalShellPassword(entry.password);
  const mappedAuth = mapFinalShellAuth(entry.secret_key_id);

  return {
    name,
    host,
    port,
    username: entry.user_name ?? "",
    authType: mappedAuth.authType,
    originalAuth: mappedAuth.originalAuth,
    needsKeyRebind: mappedAuth.needsKeyRebind,
    ...(decryptedPassword !== undefined
      ? { password: decryptedPassword }
      : { passwordUnavailable: true }),
    groupPath: options.groupPathOverride ?? "/import/finalshell",
    tags: [],
    favorite: false,
    terminalEncoding: mapFinalShellEncoding(entry.terminal_encoding),
    backspaceMode: mapFinalShellBackspace(entry.backspace_key_sequence),
    deleteMode: "vt220-delete" as DeleteMode,
    monitorSession: false,
    sourceFormat: "finalshell"
  };
};

export const parseFinalShellImport = (
  data: unknown,
  options: ImportParseOptions = {}
): ConnectionImportEntry[] => {
  const entries: FinalShellEntry[] = Array.isArray(data) ? data : [data as FinalShellEntry];
  return entries.filter(isFinalShellEntry).map((entry) => parseOneFinalShellEntry(entry, options));
};
