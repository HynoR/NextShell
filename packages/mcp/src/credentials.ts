import { readFile } from "node:fs/promises";
import { homedir } from "node:os";
import path from "node:path";

export const DEFAULT_PORT = 41777;
export const TOKEN_ENV = "NEXTSHELL_MCP_TOKEN";
export const SETUP_HINT = "在 NextShell 设置 → Agent 中开启 MCP";

export interface Credentials {
  token: string;
  port: number;
}

/** What the user gave on the command line; every field is optional. */
export interface CredentialOptions {
  token?: string;
  config?: string;
  port?: number;
}

export class MissingTokenError extends Error {
  constructor(detail: string) {
    super(
      `未找到 NextShell MCP token。${SETUP_HINT}（应用会自动写入 token 文件），或用 --token、${TOKEN_ENV}、--config 指定。${detail}`
    );
    this.name = "MissingTokenError";
  }
}

interface Host {
  platform: NodeJS.Platform;
  env: Record<string, string | undefined>;
  home: string;
}
const hostDefaults = (): Host => ({
  platform: process.platform,
  env: process.env,
  home: homedir()
});

/** Mirrors Go's os.UserConfigDir joined with NextShell-v2/mcp.json, the file the app writes. */
export function defaultConfigPath(host: Host = hostDefaults()): string {
  const p = host.platform === "win32" ? path.win32 : path.posix;
  let dir: string | undefined;
  if (host.platform === "win32") dir = host.env.AppData || undefined;
  else if (host.platform === "darwin")
    dir = host.home ? p.join(host.home, "Library", "Application Support") : undefined;
  else {
    const xdg = host.env.XDG_CONFIG_HOME;
    // Go rejects a relative XDG_CONFIG_HOME, and so does the app; ignore it here too.
    dir = xdg && p.isAbsolute(xdg) ? xdg : host.home ? p.join(host.home, ".config") : undefined;
  }
  if (!dir) throw new MissingTokenError("无法确定系统配置目录。");
  return p.join(dir, "NextShell-v2", "mcp.json");
}

async function readDiscovery(file: string): Promise<Credentials> {
  let raw: string;
  try {
    raw = await readFile(file, "utf8");
  } catch {
    throw new MissingTokenError(`未能读取 ${file}。`);
  }
  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch {
    throw new MissingTokenError(`${file} 不是有效的 JSON。`);
  }
  const { port, token } = (value ?? {}) as { port?: unknown; token?: unknown };
  if (typeof token !== "string" || token === "" || !isPort(port))
    throw new MissingTokenError(`${file} 缺少有效的 port/token。`);
  return { token, port };
}
const isPort = (v: unknown): v is number =>
  typeof v === "number" && Number.isInteger(v) && v >= 1 && v <= 65535;

/**
 * Token: --token > NEXTSHELL_MCP_TOKEN > --config file > default mcp.json.
 * Port: --port > the file's port (when a file is readable) > 41777.
 * Called on every (re)connect, never at startup: the app may start, restart or rotate its
 * token after the harness launched this process.
 */
export async function resolveCredentials(
  options: CredentialOptions,
  host: Host = hostDefaults()
): Promise<Credentials> {
  const explicit = options.token || host.env[TOKEN_ENV] || undefined;
  if (explicit && options.port !== undefined) return { token: explicit, port: options.port };
  const locate = () => options.config ?? defaultConfigPath(host);
  if (explicit) {
    // The token is settled; the file is only a port hint and its absence is not an error.
    const found = await Promise.resolve()
      .then(() => readDiscovery(locate()))
      .catch(() => undefined);
    return { token: explicit, port: found?.port ?? DEFAULT_PORT };
  }
  const found = await readDiscovery(locate());
  return { token: found.token, port: options.port ?? found.port };
}
