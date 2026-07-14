import type {
  AuthType,
  BackspaceMode,
  ConnectionExportFile,
  ConnectionImportEntry,
  DeleteMode,
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

const mapFinalShellAuthType = (_authType: number | undefined): AuthType => {
  // authentication_type 2 → password; others default to password as well
  // since we can't decrypt their keys either
  return "password";
};

const parseOneFinalShellEntry = (
  entry: FinalShellEntry,
  options: ImportParseOptions = {}
): ConnectionImportEntry => {
  const host = entry.host ?? "unknown";
  const port = entry.port ?? 22;
  const name = entry.name || `${host}:${port}`;
  const decryptedPassword = decryptFinalShellPassword(entry.password);

  return {
    name,
    host,
    port,
    username: entry.user_name ?? "",
    authType: mapFinalShellAuthType(entry.authentication_type),
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
