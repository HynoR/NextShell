import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import { isRecord } from "./json-rpc.js";

/** Overrides endpoint discovery entirely (socket path or an endpoint.json path). */
export const ENDPOINT_ENV_VAR = "NEXTSHELL_MCP_ENDPOINT";
export const ENDPOINT_DIRECTORY_NAME = "mcp";
export const ENDPOINT_FILE_NAME = "endpoint.json";

const APP_DIRECTORY_CANDIDATES = ["NextShell", "nextshell"] as const;
const WINDOWS_PIPE_PREFIX = "\\\\.\\pipe\\";

export interface EndpointRecord {
  socketPath: string;
  pid: number | null;
  /** Epoch milliseconds; 0 when the file carries no usable timestamp. */
  updatedAt: number;
  source: string;
}

/**
 * The desktop endpoint is a 0600 Unix socket / named pipe: the OS file mode is
 * the authorization, so a dial target is exactly a socket path — there is no
 * port or token to carry.
 */
export interface EndpointTarget {
  transport: "socket";
  socketPath: string;
  source: string;
}

export interface EndpointDiscoveryDeps {
  env?: NodeJS.ProcessEnv;
  platform?: NodeJS.Platform;
  homeDir?: () => string;
  readFile?: (file: string) => string;
  readDir?: (dir: string) => string[];
  fileExists?: (file: string) => boolean;
  isProcessAlive?: (pid: number) => boolean;
}

export const isNamedPipe = (value: string): boolean =>
  value.startsWith(WINDOWS_PIPE_PREFIX) || value.startsWith("\\\\?\\pipe\\");

export const isProcessAlive = (pid: number): boolean => {
  if (!Number.isInteger(pid) || pid <= 0) {
    return false;
  }
  try {
    process.kill(pid, 0);
    return true;
  } catch (error) {
    // EPERM means the pid exists but belongs to another user.
    return isRecord(error) && error.code === "EPERM";
  }
};

const defaultFileExists = (file: string): boolean => {
  try {
    fs.statSync(file);
    return true;
  } catch {
    return false;
  }
};

const defaultReadDir = (dir: string): string[] => {
  try {
    return fs.readdirSync(dir);
  } catch {
    return [];
  }
};

const defaultReadFile = (file: string): string => fs.readFileSync(file, "utf8");

const readString = (source: Record<string, unknown>, keys: string[]): string | null => {
  for (const key of keys) {
    const value = source[key];
    if (typeof value === "string" && value.trim().length > 0) {
      return value.trim();
    }
  }
  return null;
};

const readTimestamp = (source: Record<string, unknown>, keys: string[]): number => {
  for (const key of keys) {
    const value = source[key];
    if (typeof value === "number" && Number.isFinite(value) && value > 0) {
      return value;
    }
    if (typeof value === "string") {
      const parsed = Date.parse(value);
      if (Number.isFinite(parsed)) {
        return parsed;
      }
    }
  }
  return 0;
};

/**
 * The desktop side owns the endpoint file format; accept both a single object
 * and a list of instances so a schema tweak there cannot brick the bridge.
 * Records without a socket path (e.g. written by a long-dead TCP-era build)
 * are simply skipped.
 */
export const parseEndpointRecords = (raw: unknown, source: string): EndpointRecord[] => {
  let entries: unknown[];
  if (Array.isArray(raw)) {
    entries = raw;
  } else if (isRecord(raw)) {
    const nested = raw.endpoints ?? raw.instances;
    entries = Array.isArray(nested) ? nested : [raw];
  } else {
    entries = [];
  }

  const records: EndpointRecord[] = [];
  for (const entry of entries) {
    if (!isRecord(entry)) {
      continue;
    }
    const socketPath = readString(entry, ["socketPath", "socket", "pipePath", "pipe"]);
    if (socketPath === null) {
      continue;
    }
    const pidValue = entry.pid;
    records.push({
      socketPath,
      pid:
        typeof pidValue === "number" && Number.isInteger(pidValue) && pidValue > 0
          ? pidValue
          : null,
      updatedAt: readTimestamp(entry, ["updatedAt", "startedAt", "timestamp", "mtime"]),
      source
    });
  }
  return records;
};

const pathFor = (platform: NodeJS.Platform): path.PlatformPath =>
  platform === "win32" ? path.win32 : path.posix;

export const resolveUserDataDirs = (deps: EndpointDiscoveryDeps = {}): string[] => {
  const env = deps.env ?? process.env;
  const platform = deps.platform ?? process.platform;
  const homeDir = deps.homeDir ?? (() => os.homedir());
  const join = pathFor(platform).join;

  const bases: string[] = [];
  if (platform === "win32") {
    const appData = env.APPDATA;
    bases.push(
      appData !== undefined && appData.length > 0
        ? appData
        : join(env.USERPROFILE ?? homeDir(), "AppData", "Roaming")
    );
  } else if (platform === "darwin") {
    bases.push(join(env.HOME ?? homeDir(), "Library", "Application Support"));
  } else {
    const xdg = env.XDG_CONFIG_HOME;
    bases.push(xdg !== undefined && xdg.length > 0 ? xdg : join(env.HOME ?? homeDir(), ".config"));
  }

  const dirs: string[] = [];
  for (const base of bases) {
    for (const appDir of APP_DIRECTORY_CANDIDATES) {
      const candidate = join(base, appDir);
      if (!dirs.includes(candidate)) {
        dirs.push(candidate);
      }
    }
  }
  return dirs;
};

const recordKey = (record: EndpointRecord): string =>
  `${record.socketPath}|${record.pid ?? 0}`;

export const readEndpointRecords = (deps: EndpointDiscoveryDeps = {}): EndpointRecord[] => {
  const readFile = deps.readFile ?? defaultReadFile;
  const readDir = deps.readDir ?? defaultReadDir;
  const join = pathFor(deps.platform ?? process.platform).join;

  const records: EndpointRecord[] = [];
  // Case-insensitive filesystems make the app-directory candidates collapse onto
  // the same folder, so identical listeners have to be folded together.
  const seen = new Set<string>();
  for (const userDataDir of resolveUserDataDirs(deps)) {
    const dir = join(userDataDir, ENDPOINT_DIRECTORY_NAME);
    const names = readDir(dir).filter((name) => /^endpoint.*\.json$/i.test(name));
    if (!names.includes(ENDPOINT_FILE_NAME)) {
      names.unshift(ENDPOINT_FILE_NAME);
    }
    for (const name of names) {
      const file = join(dir, name);
      let raw: string;
      try {
        raw = readFile(file);
      } catch {
        continue;
      }
      let parsed: EndpointRecord[];
      try {
        parsed = parseEndpointRecords(JSON.parse(raw), file);
      } catch {
        continue;
      }
      for (const record of parsed) {
        const key = recordKey(record);
        if (seen.has(key)) {
          continue;
        }
        seen.add(key);
        records.push(record);
      }
    }
  }
  return records;
};

/**
 * Drops records whose owning process is gone (a crashed app leaves the file
 * behind) or whose socket vanished, and orders the survivors newest first.
 */
export const selectEndpointTargets = (
  records: EndpointRecord[],
  deps: EndpointDiscoveryDeps = {}
): EndpointTarget[] => {
  const alive = deps.isProcessAlive ?? isProcessAlive;
  const exists = deps.fileExists ?? defaultFileExists;

  const live = records.filter((record) => record.pid === null || alive(record.pid));
  const ordered = [...live].sort((left, right) => right.updatedAt - left.updatedAt);

  const targets: EndpointTarget[] = [];
  for (const record of ordered) {
    if (isNamedPipe(record.socketPath) || exists(record.socketPath)) {
      targets.push({
        transport: "socket",
        socketPath: record.socketPath,
        source: record.source
      });
    }
  }
  return targets;
};

export const parseEndpointOverride = (
  value: string,
  deps: EndpointDiscoveryDeps = {}
): EndpointTarget[] => {
  const trimmed = value.trim();
  if (trimmed.length === 0) {
    return [];
  }

  if (trimmed.toLowerCase().endsWith(".json")) {
    const readFile = deps.readFile ?? defaultReadFile;
    try {
      const records = parseEndpointRecords(JSON.parse(readFile(trimmed)), trimmed);
      return selectEndpointTargets(records, deps);
    } catch {
      return [];
    }
  }

  return [
    {
      transport: "socket",
      socketPath: trimmed,
      source: ENDPOINT_ENV_VAR
    }
  ];
};

export const discoverEndpointTargets = (deps: EndpointDiscoveryDeps = {}): EndpointTarget[] => {
  const env = deps.env ?? process.env;
  const override = env[ENDPOINT_ENV_VAR];
  if (override !== undefined && override.trim().length > 0) {
    return parseEndpointOverride(override, deps);
  }
  return selectEndpointTargets(readEndpointRecords(deps), deps);
};
