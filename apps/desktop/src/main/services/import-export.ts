import type {
  AuthType,
  BackspaceMode,
  ConnectionExportFile,
  ConnectionImportEntry,
  DeleteMode,
  TerminalEncoding
} from "../../../../../packages/core/src/index";

// ─── Format detection ────────────────────────────────────────────────────────

export const isNextShellFormat = (data: unknown): data is ConnectionExportFile => {
  if (typeof data !== "object" || data === null) return false;
  return (data as Record<string, unknown>)["format"] === "nextshell-connections";
};

interface CompetitorEntry {
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

export const isCompetitorFormat = (data: unknown): boolean => {
  if (Array.isArray(data)) {
    return data.length > 0 && isCompetitorEntry(data[0]);
  }
  return isCompetitorEntry(data);
};

const isCompetitorEntry = (data: unknown): boolean => {
  if (typeof data !== "object" || data === null) return false;
  const record = data as Record<string, unknown>;
  return "authentication_type" in record && "user_name" in record;
};

// ─── NextShell format parser ─────────────────────────────────────────────────

export const parseNextShellImport = (data: ConnectionExportFile): ConnectionImportEntry[] => {
  return data.connections.map((conn) => ({
    name: conn.name,
    host: conn.host,
    port: conn.port,
    username: conn.username,
    authType: conn.authType,
    password: conn.password,
    groupPath: conn.groupPath,
    tags: conn.tags,
    notes: conn.notes,
    favorite: conn.favorite,
    terminalEncoding: conn.terminalEncoding,
    backspaceMode: conn.backspaceMode,
    deleteMode: conn.deleteMode,
    monitorSession: conn.monitorSession,
    sourceFormat: "nextshell"
  }));
};

// ─── Competitor format parser ────────────────────────────────────────────────

export const mapCompetitorEncoding = (encoding: string | undefined): TerminalEncoding => {
  if (!encoding) return "utf-8";
  const lower = encoding.toLowerCase().replace(/[-_\s]/g, "");
  if (lower === "utf8") return "utf-8";
  if (lower === "gb18030") return "gb18030";
  if (lower === "gbk") return "gbk";
  if (lower === "big5") return "big5";
  return "utf-8";
};

const mapCompetitorBackspace = (seq: number | undefined): BackspaceMode => {
  return seq === 2 ? "ascii-delete" : "ascii-backspace";
};

const mapCompetitorAuthType = (_authType: number | undefined): AuthType => {
  // authentication_type 2 → password; others default to password as well
  // since we can't decrypt their keys either
  return "password";
};

const parseOneCompetitorEntry = (entry: CompetitorEntry): ConnectionImportEntry => {
  const host = entry.host ?? "unknown";
  const port = entry.port ?? 22;
  const name = entry.name || `${host}:${port}`;

  return {
    name,
    host,
    port,
    username: entry.user_name ?? "",
    authType: mapCompetitorAuthType(entry.authentication_type),
    // Competitor passwords are encrypted with their own scheme — always ignore
    passwordUnavailable: true,
    groupPath: ["导入"],
    tags: [],
    favorite: false,
    terminalEncoding: mapCompetitorEncoding(entry.terminal_encoding),
    backspaceMode: mapCompetitorBackspace(entry.backspace_key_sequence),
    deleteMode: "vt220-delete" as DeleteMode,
    monitorSession: false,
    sourceFormat: "competitor"
  };
};

export const parseCompetitorImport = (data: unknown): ConnectionImportEntry[] => {
  const entries: CompetitorEntry[] = Array.isArray(data) ? data : [data as CompetitorEntry];
  return entries.filter(isCompetitorEntry).map(parseOneCompetitorEntry);
};
