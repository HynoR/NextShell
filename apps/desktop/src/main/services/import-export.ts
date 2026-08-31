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

interface ImportParseOptions {
  groupPathOverride?: string;
}

export interface SshKeyMatchCandidate {
  id: string;
  name: string;
  fingerprint?: string;
}

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

// ─── Import folder materialization ───────────────────────────────────────────

export interface ImportFolderNode {
  id: string;
  name: string;
  parentId?: string;
}

/**
 * 物化目录链需要的最小仓储面。抽成端口是为了让"同名复用"这条逻辑脱离 SQLite 测试
 * (better-sqlite3 是 Electron ABI,单测里跑不起来)。
 */
export interface ImportFolderStore {
  /** 目标作用域下已知的全部目录,含本批刚建出来的。 */
  list: () => readonly ImportFolderNode[];
  create: (name: string, parentId: string | undefined) => ImportFolderNode;
  /**
   * 丢掉缓存、重新从库里读该作用域的目录。只在 create 撞唯一索引后调用:那说明缓存已经
   * 落后于库,`list()` 再问一遍也还是那份旧快照。没实现时退回 `list()`。
   */
  refresh?: () => readonly ImportFolderNode[];
}

const findSibling = (
  folders: readonly ImportFolderNode[],
  parentId: string | undefined,
  name: string
): ImportFolderNode | undefined =>
  folders.find((folder) => (folder.parentId ?? undefined) === parentId && folder.name === name);

/**
 * 把目录名链物化成真实目录,返回叶目录 id;链为空时原样返回 `rootFolderId`。
 *
 * 同一个 parent 下同名即复用——否则每导入一次同一份文件,树里就多出一整套同名目录,
 * 而库里 `(scope_key, parent_id, name)` 上的唯一索引会让第二条直接报错。
 *
 * 缓存有可能已经落后于库(并发导入、或别处刚建了同名目录):create 撞唯一索引时重读一次,
 * 找到同 parent 同名的就复用;还是不行才把错误换成用户看得懂的文案往上抛。
 */
export const materializeFolderChain = (
  segments: readonly string[],
  rootFolderId: string | undefined,
  store: ImportFolderStore
): string | undefined => {
  let parentId = rootFolderId;
  for (const segment of segments) {
    const name = segment.trim();
    if (!name) {
      continue;
    }
    const existing = findSibling(store.list(), parentId, name);
    if (existing) {
      parentId = existing.id;
      continue;
    }
    try {
      parentId = store.create(name, parentId).id;
    } catch (error) {
      const refreshed = store.refresh ? store.refresh() : store.list();
      const reused = findSibling(refreshed, parentId, name);
      if (!reused) {
        // 用户看到的是这句;原始错误挂在 cause 上,不然唯一索引之外的失败原因会整条丢掉。
        throw new Error(`目录「${name}」创建冲突，请重试导入`, { cause: error });
      }
      parentId = reused.id;
    }
  }
  return parentId;
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
      // 导入不再改写来源路径:归属由目标目录(folderId)决定,groupPath 只是投影。
      groupPath: options.groupPathOverride ?? conn.groupPath,
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
