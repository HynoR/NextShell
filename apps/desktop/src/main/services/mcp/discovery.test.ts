import { afterEach, describe, expect, test } from "vitest";
import { mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";

import { EndpointDiscoveryFile, type EndpointDiscoveryRecord } from "./discovery";

const tempDirs: string[] = [];

const createTempDir = async (): Promise<string> => {
  const dir = await mkdtemp(path.join(os.tmpdir(), "nsmcp-discovery-"));
  tempDirs.push(dir);
  return dir;
};

afterEach(async () => {
  while (tempDirs.length > 0) {
    const dir = tempDirs.pop();
    if (dir) {
      await rm(dir, { recursive: true, force: true });
    }
  }
});

describe("endpoint discovery file", () => {
  test("writes both the instance file and endpoint.json with 0600 permissions", async () => {
    const userDataDir = await createTempDir();
    const discovery = new EndpointDiscoveryFile({ userDataDir, appVersion: "1.2.3", pid: 4242 });

    const record = await discovery.write({ socketPath: "/tmp/nsmcp/s" });

    expect(record).toMatchObject({
      version: 1,
      pid: 4242,
      socketPath: "/tmp/nsmcp/s",
      appVersion: "1.2.3"
    });

    const primary = JSON.parse(
      await readFile(discovery.primaryPath, "utf8")
    ) as EndpointDiscoveryRecord;
    expect(primary.pid).toBe(4242);

    if (process.platform !== "win32") {
      const mode = (await stat(discovery.instancePath)).mode & 0o777;
      expect(mode).toBe(0o600);
      expect((await stat(discovery.primaryPath)).mode & 0o777).toBe(0o600);
    }
  });

  test("the record carries no token or port fields at all", async () => {
    const userDataDir = await createTempDir();
    const discovery = new EndpointDiscoveryFile({ userDataDir, appVersion: "1.0.0", pid: 11 });

    await discovery.write({ socketPath: "/tmp/nsmcp/s" });

    const written = await readFile(discovery.primaryPath, "utf8");
    expect(written).not.toContain("token");
    expect(written).not.toContain("httpPort");
  });

  test("prunes dead instances and repoints endpoint.json at the newest survivor", async () => {
    const userDataDir = await createTempDir();
    const alive = new Set([200]);
    const options = {
      userDataDir,
      appVersion: "1.0.0",
      isProcessAlive: (pid: number) => alive.has(pid)
    };

    const dead = new EndpointDiscoveryFile({ ...options, pid: 100, isProcessAlive: () => true });
    await dead.write({ socketPath: "/tmp/dead/s", startedAt: "2026-08-01T00:00:00.000Z" });

    const live = new EndpointDiscoveryFile(options);
    const liveInstance = new EndpointDiscoveryFile({ ...options, pid: 200 });
    await liveInstance.write({ socketPath: "/tmp/live/s", startedAt: "2026-08-02T00:00:00.000Z" });

    const survivor = await live.pruneStale();

    expect(survivor?.pid).toBe(200);
    await expect(stat(path.join(userDataDir, "mcp", "endpoint-100.json"))).rejects.toThrow();
    const primary = await live.readPrimary();
    expect(primary?.socketPath).toBe("/tmp/live/s");
  });

  test("remove clears endpoint.json only when it points at this instance", async () => {
    const userDataDir = await createTempDir();
    const other = new EndpointDiscoveryFile({
      userDataDir,
      appVersion: "1.0.0",
      pid: 300,
      isProcessAlive: () => true
    });
    await other.write({ socketPath: "/tmp/other/s" });

    const mine = new EndpointDiscoveryFile({
      userDataDir,
      appVersion: "1.0.0",
      pid: 301,
      isProcessAlive: (pid) => pid === 300
    });
    await mine.remove();

    expect((await mine.readPrimary())?.pid).toBe(300);

    const owner = new EndpointDiscoveryFile({
      userDataDir,
      appVersion: "1.0.0",
      pid: 300,
      isProcessAlive: () => false
    });
    await owner.remove();

    expect(await owner.readPrimary()).toBeNull();
  });

  test("a corrupt discovery file is treated as stale rather than crashing", async () => {
    const userDataDir = await createTempDir();
    const discovery = new EndpointDiscoveryFile({
      userDataDir,
      appVersion: "1.0.0",
      pid: 400,
      isProcessAlive: () => true
    });
    await discovery.write({ socketPath: "/tmp/x/s" });
    await writeFile(path.join(userDataDir, "mcp", "endpoint-999.json"), "{not json");

    await expect(discovery.pruneStale()).resolves.not.toBeNull();
    await expect(stat(path.join(userDataDir, "mcp", "endpoint-999.json"))).rejects.toThrow();
  });
});
