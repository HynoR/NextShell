export const AUTH_REQUIRED_PREFIX = "AUTH_REQUIRED::";
export const CONNECTION_IMPORT_DECRYPT_PROMPT_PREFIX = "CONNECTION_IMPORT_DECRYPT_PROMPT::";
export const SUPPORTED_BACKGROUND_IMAGE_EXTENSIONS = ["png", "jpg", "jpeg", "gif", "bmp", "webp"];

// ─── Connection Zone Isolation ────────────────────────────────────────────────

/**
 * Three fixed first-level groupPath zones.
 *
 * - `server`    — 我的服务器 (local, not synced to cloud)
 * - `workspace` — 云同步 (cloud-synced servers)
 * - `import`    — 文件导入 (imported from files)
 */
export const CONNECTION_ZONES = {
  SERVER: "server",
  WORKSPACE: "workspace",
  IMPORT: "import"
} as const;

export type ConnectionZone = (typeof CONNECTION_ZONES)[keyof typeof CONNECTION_ZONES];

/** Ordered list of zone keys for UI rendering */
export const ZONE_ORDER: readonly ConnectionZone[] = [
  CONNECTION_ZONES.SERVER,
  CONNECTION_ZONES.WORKSPACE,
  CONNECTION_ZONES.IMPORT
] as const;

/** Display names (Chinese) for each zone */
export const ZONE_DISPLAY_NAMES: Record<ConnectionZone, string> = {
  [CONNECTION_ZONES.SERVER]: "我的服务器",
  [CONNECTION_ZONES.WORKSPACE]: "云同步",
  [CONNECTION_ZONES.IMPORT]: "文件导入"
} as const;

/** Icons for each zone (Remix Icon class names) */
export const ZONE_ICONS: Record<ConnectionZone, string> = {
  [CONNECTION_ZONES.SERVER]: "ri-server-line",
  [CONNECTION_ZONES.WORKSPACE]: "ri-cloud-line",
  [CONNECTION_ZONES.IMPORT]: "ri-import-line"
} as const;

const VALID_ZONES = new Set<string>(ZONE_ORDER);

/**
 * Extract the zone (first path segment) from a normalized groupPath.
 * @example extractZone("/server/hk") → "server"
 * @example extractZone("/workspace/team/prod") → "workspace"
 */
export const extractZone = (groupPath: string): string => {
  const segments = groupPath.split("/").filter((s) => s.length > 0);
  return segments[0] ?? CONNECTION_ZONES.SERVER;
};

/** Check whether a zone string is one of the three allowed zones */
export const isValidZone = (zone: string): zone is ConnectionZone => VALID_ZONES.has(zone);

/**
 * Ensure a groupPath starts with one of the three allowed zone prefixes.
 * If the first segment is not a valid zone, the path is placed under the
 * given `defaultZone` (defaults to `"server"`).
 *
 * @example enforceZonePrefix("/server/hk")         → "/server/hk"
 * @example enforceZonePrefix("/mygroup/foo")        → "/server/mygroup/foo"
 * @example enforceZonePrefix("/导入/xxx")           → "/server/导入/xxx"
 * @example enforceZonePrefix("/workspace/team")     → "/workspace/team"
 */
export const enforceZonePrefix = (
  groupPath: string,
  defaultZone: ConnectionZone = CONNECTION_ZONES.SERVER
): string => {
  const normalized = normalizeGroupPathBasic(groupPath);
  if (normalized === "/" || normalized === "") return `/${defaultZone}`;
  const zone = extractZone(normalized);
  if (isValidZone(zone)) return normalized;
  // Prepend default zone. normalized always starts with "/"
  return `/${defaultZone}${normalized}`;
};

/**
 * Extract the user-visible sub-path (everything after the zone segment).
 * @example getSubPath("/server/hk/prod") → "/hk/prod"
 * @example getSubPath("/server")         → ""
 */
export const getSubPath = (groupPath: string): string => {
  const segments = groupPath.split("/").filter((s) => s.length > 0);
  if (segments.length <= 1) return "";
  return "/" + segments.slice(1).join("/");
};

/**
 * Build a groupPath from a zone + user-entered sub-path.
 * @example buildGroupPath("server", "/hk/prod") → "/server/hk/prod"
 * @example buildGroupPath("workspace", "")       → "/workspace"
 */
export const buildGroupPath = (zone: ConnectionZone, subPath: string): string => {
  const sub = subPath.trim().replace(/\\/g, "/").replace(/\/+/g, "/");
  if (!sub || sub === "/") return `/${zone}`;
  const cleanSub = sub.startsWith("/") ? sub : `/${sub}`;
  return `/${zone}${cleanSub}`;
};

/** Basic normalizeGroupPath (no zone enforcement) */
const normalizeGroupPathBasic = (value: string): string => {
  if (!value) return "/server";
  let path = value.trim().replace(/\\/g, "/");
  if (!path.startsWith("/")) path = "/" + path;
  path = path.replace(/\/+/g, "/");
  if (path.length > 1 && path.endsWith("/")) path = path.slice(0, -1);
  return path || "/server";
};
