import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import { afterAll, describe, expect, it } from "vitest";

import {
  discoverEndpointTargets,
  parseEndpointRecords,
  readEndpointRecords,
  resolveUserDataDirs,
  selectEndpointTargets,
  type EndpointDiscoveryDeps,
  type EndpointRecord
} from "./endpoint.js";

const tempDirs: string[] = [];

const makeTempDir = (): string => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "nsbridge-"));
  tempDirs.push(dir);
  return dir;
};

afterAll(() => {
  for (const dir of tempDirs) {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

const record = (overrides: Partial<EndpointRecord> = {}): EndpointRecord => ({
  socketPath: "/tmp/nextshell.sock",
  pid: 4242,
  updatedAt: 1000,
  source: "test",
  ...overrides
});

describe("resolveUserDataDirs", () => {
  it("uses the platform application data directory", () => {
    expect(resolveUserDataDirs({ platform: "darwin", env: { HOME: "/Users/tester" } })).toContain(
      "/Users/tester/Library/Application Support/NextShell"
    );
    expect(resolveUserDataDirs({ platform: "linux", env: { HOME: "/home/tester" } })).toContain(
      "/home/tester/.config/NextShell"
    );
    expect(resolveUserDataDirs({ platform: "linux", env: { XDG_CONFIG_HOME: "/xdg" } })).toContain(
      "/xdg/NextShell"
    );
    expect(
      resolveUserDataDirs({ platform: "win32", env: { APPDATA: "C:\\Users\\t\\AppData\\Roaming" } })
    ).toContain("C:\\Users\\t\\AppData\\Roaming\\NextShell");
  });
});

describe("parseEndpointRecords", () => {
  it("accepts a single object and a list of instances", () => {
    const single = parseEndpointRecords(
      { socketPath: "/tmp/a.sock", pid: 1, updatedAt: "2026-08-03T00:00:00.000Z" },
      "file"
    );
    expect(single).toHaveLength(1);
    expect(single[0]?.socketPath).toBe("/tmp/a.sock");
    expect(single[0]?.updatedAt).toBe(Date.parse("2026-08-03T00:00:00.000Z"));

    const many = parseEndpointRecords(
      { endpoints: [{ socketPath: "/tmp/a.sock" }, { socketPath: "/tmp/b.sock" }] },
      "file"
    );
    expect(many).toHaveLength(2);
    expect(many[1]?.socketPath).toBe("/tmp/b.sock");
  });

  it("drops entries without a socket path, including TCP-era leftovers", () => {
    expect(parseEndpointRecords({ pid: 5 }, "file")).toEqual([]);
    expect(parseEndpointRecords("nonsense", "file")).toEqual([]);
    // Written by a pre-redesign desktop build: port + token, no socket.
    expect(parseEndpointRecords({ httpPort: 7412, token: "loopback-token" }, "file")).toEqual([]);
  });
});

describe("selectEndpointTargets", () => {
  it("discards records whose process is gone and prefers the newest survivor", () => {
    const deps: EndpointDiscoveryDeps = {
      isProcessAlive: (pid) => pid === 200,
      fileExists: () => true
    };
    const targets = selectEndpointTargets(
      [
        record({ pid: 100, socketPath: "/tmp/dead.sock", updatedAt: 9000 }),
        record({ pid: 200, socketPath: "/tmp/old.sock", updatedAt: 1000 }),
        record({ pid: 200, socketPath: "/tmp/new.sock", updatedAt: 5000 })
      ],
      deps
    );
    expect(targets.map((target) => target.socketPath)).toEqual(["/tmp/new.sock", "/tmp/old.sock"]);
  });

  it("skips a socket whose file no longer exists", () => {
    const targets = selectEndpointTargets([record({ pid: null })], {
      fileExists: () => false
    });
    expect(targets).toHaveLength(0);
  });

  it("a socket target is all there is", () => {
    const targets = selectEndpointTargets([record({ pid: null })], {
      fileExists: () => true
    });
    expect(targets[0]?.transport).toBe("socket");
    expect(targets[0]?.socketPath).toBe("/tmp/nextshell.sock");
  });
});

describe("discoverEndpointTargets", () => {
  it("reads endpoint.json from the real user data directory layout", () => {
    const home = makeTempDir();
    const dir = path.join(home, "Library", "Application Support", "NextShell", "mcp");
    fs.mkdirSync(dir, { recursive: true });
    const socketPath = path.join(home, "live.sock");
    fs.writeFileSync(socketPath, "");
    fs.writeFileSync(
      path.join(dir, "endpoint.json"),
      JSON.stringify({ pid: process.pid, socketPath, updatedAt: Date.now() })
    );

    const targets = discoverEndpointTargets({
      platform: "darwin",
      env: { HOME: home }
    });
    expect(targets).toHaveLength(1);
    expect(targets[0]?.socketPath).toBe(socketPath);
  });

  it("eliminates a stale endpoint file left behind by a crashed app", () => {
    const home = makeTempDir();
    const dir = path.join(home, ".config", "NextShell", "mcp");
    fs.mkdirSync(dir, { recursive: true });
    const socketPath = path.join(home, "stale.sock");
    fs.writeFileSync(socketPath, "");
    fs.writeFileSync(
      path.join(dir, "endpoint.json"),
      JSON.stringify({ pid: 999999, socketPath, updatedAt: Date.now() })
    );

    const targets = discoverEndpointTargets({
      platform: "linux",
      env: { HOME: home },
      isProcessAlive: (pid) => pid !== 999999
    });
    expect(targets).toEqual([]);
  });

  it("collects per-instance endpoint files from the same directory", () => {
    const home = makeTempDir();
    const dir = path.join(home, ".config", "NextShell", "mcp");
    fs.mkdirSync(dir, { recursive: true });
    fs.writeFileSync(
      path.join(dir, "endpoint-2.json"),
      JSON.stringify({ pid: process.pid, socketPath: "/tmp/instance-2.sock", updatedAt: 2 })
    );
    fs.writeFileSync(path.join(dir, "unrelated.json"), "{}");

    const records = readEndpointRecords({ platform: "linux", env: { HOME: home } });
    expect(records).toHaveLength(1);
    expect(records[0]?.socketPath).toBe("/tmp/instance-2.sock");
  });

  it("honours the environment override for socket paths and files", () => {
    const socketTargets = discoverEndpointTargets({
      env: { NEXTSHELL_MCP_ENDPOINT: "/tmp/override.sock" }
    });
    expect(socketTargets[0]).toMatchObject({
      transport: "socket",
      socketPath: "/tmp/override.sock"
    });

    const dir = makeTempDir();
    const socketPath = path.join(dir, "file.sock");
    fs.writeFileSync(socketPath, "");
    const file = path.join(dir, "endpoint.json");
    fs.writeFileSync(file, JSON.stringify({ pid: process.pid, socketPath }));
    const fileTargets = discoverEndpointTargets({ env: { NEXTSHELL_MCP_ENDPOINT: file } });
    expect(fileTargets[0]).toMatchObject({ transport: "socket", socketPath });
  });

  it("ignores an override pointing at an unreadable file", () => {
    expect(
      discoverEndpointTargets({ env: { NEXTSHELL_MCP_ENDPOINT: "/nope/missing-endpoint.json" } })
    ).toEqual([]);
  });
});
