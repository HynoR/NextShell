/**
 * One-click client onboarding (plan §八 B): Cursor deeplink install, writing
 * `claude_desktop_config.json` in place, and exporting a `.mcpb` bundle for
 * Claude Desktop. Pure config builders live here so they stay unit-testable;
 * the only filesystem touch-point takes an injectable fs facade.
 */
import * as nodeFs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";

import { createZipArchive } from "./zip";

/** Server key MCP clients index the NextShell endpoint under. */
export const CURSOR_DEEPLINK_BASE = "cursor://anysphere.cursor-deeplink/mcp/install";

export const buildCursorDeeplink = (
  serverKey: string,
  serverConfig: Record<string, unknown>
): string => {
  const config = Buffer.from(JSON.stringify(serverConfig), "utf8").toString("base64");
  return `${CURSOR_DEEPLINK_BASE}?name=${encodeURIComponent(serverKey)}&config=${encodeURIComponent(config)}`;
};

export const resolveClaudeDesktopConfigPath = (
  platform: NodeJS.Platform = process.platform,
  homeDir: string = os.homedir(),
  appDataDir: string | undefined = process.env.APPDATA
): string => {
  switch (platform) {
    case "darwin":
      return path.join(
        homeDir,
        "Library",
        "Application Support",
        "Claude",
        "claude_desktop_config.json"
      );
    case "win32":
      return path.join(
        appDataDir ?? path.join(homeDir, "AppData", "Roaming"),
        "Claude",
        "claude_desktop_config.json"
      );
    default:
      return path.join(homeDir, ".config", "Claude", "claude_desktop_config.json");
  }
};

/**
 * Merges the NextShell server entry into an existing config document without
 * touching any other key. Throws instead of overwriting when the existing
 * file is not valid JSON — clobbering a config the user hand-edited is worse
 * than failing the one-click path.
 */
export const mergeClaudeDesktopConfig = (
  existingText: string | null,
  serverKey: string,
  serverConfig: Record<string, unknown>
): string => {
  let root: Record<string, unknown> = {};
  if (existingText && existingText.trim().length > 0) {
    let parsed: unknown;
    try {
      parsed = JSON.parse(existingText);
    } catch {
      throw new Error("claude_desktop_config.json 不是有效的 JSON，已中止写入；请手动修复后重试");
    }
    if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
      throw new Error("claude_desktop_config.json 的顶层不是对象，已中止写入");
    }
    root = parsed as Record<string, unknown>;
  }

  const servers =
    typeof root.mcpServers === "object" &&
    root.mcpServers !== null &&
    !Array.isArray(root.mcpServers)
      ? (root.mcpServers as Record<string, unknown>)
      : {};
  root.mcpServers = { ...servers, [serverKey]: serverConfig };
  return `${JSON.stringify(root, null, 2)}\n`;
};

export interface ClaudeDesktopFs {
  exists: (filePath: string) => boolean;
  readText: (filePath: string) => string;
  writeText: (filePath: string, content: string) => void;
}

const defaultFs: ClaudeDesktopFs = {
  exists: (filePath) => nodeFs.existsSync(filePath),
  readText: (filePath) => nodeFs.readFileSync(filePath, "utf8"),
  writeText: (filePath, content) => nodeFs.writeFileSync(filePath, content, "utf8")
};

/**
 * Writes the merged config. The Claude directory is required to already exist:
 * creating it on a machine without Claude Desktop would leave the bridge
 * config stranded and tell the user the install "worked".
 */
export const installClaudeDesktopConfig = (
  serverKey: string,
  serverConfig: Record<string, unknown>,
  options: {
    configPath?: string;
    fs?: ClaudeDesktopFs;
  } = {}
): { configPath: string } => {
  const configPath = options.configPath ?? resolveClaudeDesktopConfigPath();
  const fs = options.fs ?? defaultFs;
  if (!fs.exists(path.dirname(configPath))) {
    throw new Error("未检测到 Claude Desktop（配置目录不存在），请先安装并运行一次 Claude Desktop");
  }
  const existing = fs.exists(configPath) ? fs.readText(configPath) : null;
  fs.writeText(configPath, mergeClaudeDesktopConfig(existing, serverKey, serverConfig));
  return { configPath };
};

export interface McpbBundleOptions {
  appVersion: string;
  /** Path to `<userData>/mcp/endpoint.json`, baked in as a discovery fallback. */
  endpointFilePath: string;
  /** The self-contained bridge bundle (`apps/mcp-bridge` build output). */
  bridgeCode: Buffer;
}

/**
 * A `.mcpb` bundle is a ZIP holding `manifest.json` plus the server files.
 * Claude Desktop runs the entry with its managed Node runtime, so the bridge
 * needs no `ELECTRON_RUN_AS_NODE` here — that env is only for the config that
 * points at the Electron binary.
 */
export const buildMcpbArchive = (options: McpbBundleOptions): Buffer => {
  const manifest = {
    manifest_version: "0.2",
    name: "nextshell",
    display_name: "NextShell",
    version: options.appVersion,
    description:
      "Zero-credential bridge to a running NextShell desktop app. Operates only hosts the user explicitly granted; credentials never leave the app.",
    author: { name: "NextShell" },
    server: {
      type: "node",
      entry_point: "server/index.js",
      mcp_config: {
        command: "node",
        args: ["${__dirname}/server/index.js"],
        env: { NEXTSHELL_MCP_ENDPOINT: options.endpointFilePath }
      }
    }
  };
  return createZipArchive([
    { name: "manifest.json", data: Buffer.from(`${JSON.stringify(manifest, null, 2)}\n`, "utf8") },
    { name: "server/index.js", data: options.bridgeCode, mode: 0o755 }
  ]);
};
