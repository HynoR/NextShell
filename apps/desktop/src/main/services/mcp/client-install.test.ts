import * as path from "node:path";

import { describe, expect, it } from "vitest";

import {
  buildCursorDeeplink,
  buildMcpbArchive,
  installClaudeDesktopConfig,
  mergeClaudeDesktopConfig,
  resolveClaudeDesktopConfigPath,
  type ClaudeDesktopFs
} from "./client-install";

describe("buildCursorDeeplink", () => {
  it("encodes the server config as base64 in the deeplink", () => {
    const config = { command: "/bin/node", args: ["/x/bridge.mjs"] };
    const link = buildCursorDeeplink("nextshell", config);
    expect(
      link.startsWith("cursor://anysphere.cursor-deeplink/mcp/install?name=nextshell&config=")
    ).toBe(true);
    const encoded = decodeURIComponent(link.split("config=")[1]!);
    expect(JSON.parse(Buffer.from(encoded, "base64").toString("utf8"))).toEqual(config);
  });
});

describe("resolveClaudeDesktopConfigPath", () => {
  it("maps each platform to its Claude config location", () => {
    expect(resolveClaudeDesktopConfigPath("darwin", "/Users/a")).toBe(
      path.join(
        "/Users/a",
        "Library",
        "Application Support",
        "Claude",
        "claude_desktop_config.json"
      )
    );
    expect(
      resolveClaudeDesktopConfigPath("win32", "C:\\Users\\a", "C:\\Users\\a\\AppData\\Roaming")
    ).toBe(path.join("C:\\Users\\a\\AppData\\Roaming", "Claude", "claude_desktop_config.json"));
    expect(resolveClaudeDesktopConfigPath("linux", "/home/a")).toBe(
      path.join("/home/a", ".config", "Claude", "claude_desktop_config.json")
    );
  });
});

describe("mergeClaudeDesktopConfig", () => {
  const server = { command: "/bin/node", args: ["/x/bridge.mjs"] };

  it("creates the document from scratch when none exists", () => {
    const merged = JSON.parse(mergeClaudeDesktopConfig(null, "nextshell", server));
    expect(merged).toEqual({ mcpServers: { nextshell: server } });
  });

  it("preserves unrelated keys and other servers", () => {
    const existing = JSON.stringify({
      globalShortcut: "Cmd+K",
      mcpServers: { other: { command: "foo" } }
    });
    const merged = JSON.parse(mergeClaudeDesktopConfig(existing, "nextshell", server));
    expect(merged.globalShortcut).toBe("Cmd+K");
    expect(merged.mcpServers.other).toEqual({ command: "foo" });
    expect(merged.mcpServers.nextshell).toEqual(server);
  });

  it("refuses to overwrite a corrupt config file", () => {
    expect(() => mergeClaudeDesktopConfig("{ not json", "nextshell", server)).toThrow(/JSON/);
    expect(() => mergeClaudeDesktopConfig("[1,2]", "nextshell", server)).toThrow(/顶层/);
  });
});

describe("installClaudeDesktopConfig", () => {
  const server = { command: "/bin/node" };

  const memoryFs = (
    files: Record<string, string>,
    dirs: string[]
  ): ClaudeDesktopFs & {
    files: Record<string, string>;
  } => ({
    files,
    exists: (p) => p in files || dirs.includes(p),
    readText: (p) => files[p]!,
    writeText: (p, content) => {
      files[p] = content;
    }
  });

  it("fails when the Claude directory does not exist", () => {
    const fs = memoryFs({}, []);
    expect(() =>
      installClaudeDesktopConfig("nextshell", server, { configPath: "/cfg/Claude/c.json", fs })
    ).toThrow(/Claude Desktop/);
    expect(Object.keys(fs.files)).toHaveLength(0);
  });

  it("merges into the existing file in place", () => {
    const configPath = "/cfg/Claude/claude_desktop_config.json";
    const fs = memoryFs({ [configPath]: JSON.stringify({ mcpServers: { keep: {} } }) }, [
      "/cfg/Claude"
    ]);
    const result = installClaudeDesktopConfig("nextshell", server, { configPath, fs });
    expect(result.configPath).toBe(configPath);
    const written = JSON.parse(fs.files[configPath]!);
    expect(written.mcpServers.keep).toEqual({});
    expect(written.mcpServers.nextshell).toEqual(server);
  });
});

describe("buildMcpbArchive", () => {
  it("bundles a manifest and the bridge entry", () => {
    const zip = buildMcpbArchive({
      appVersion: "1.2.3",
      endpointFilePath: "/data/mcp/endpoint.json",
      bridgeCode: Buffer.from("#!/usr/bin/env node\nconsole.error('bridge')\n")
    });
    const text = zip.toString("latin1");
    expect(text).toContain("manifest.json");
    expect(text).toContain("server/index.js");
    expect(text).toContain('"manifest_version": "0.2"');
    expect(text).toContain('"version": "1.2.3"');
    expect(text).toContain("/data/mcp/endpoint.json");
  });
});
