import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import { afterAll, describe, expect, it } from "vitest";

// Imported straight from the desktop app on purpose: the two sides agree on a
// directory layout and a record shape that neither package's own tests can
// verify alone, and a mismatch silently disables zero-config discovery.
import { EndpointDiscoveryFile } from "../../desktop/src/main/services/mcp/discovery.js";
import {
  discoverEndpointTargets,
  ENDPOINT_DIRECTORY_NAME,
  ENDPOINT_FILE_NAME
} from "./endpoint.js";

const tempDirs: string[] = [];

const makeUserDataDir = (): { home: string; userDataDir: string } => {
  const home = fs.mkdtempSync(path.join(os.tmpdir(), "nscontract-"));
  tempDirs.push(home);
  // Mirrors what Electron reports for `app.getPath("userData")` on Linux.
  const userDataDir = path.join(home, ".config", "NextShell");
  fs.mkdirSync(userDataDir, { recursive: true });
  return { home, userDataDir };
};

afterAll(() => {
  for (const dir of tempDirs) {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

describe("desktop ↔ bridge endpoint discovery contract", () => {
  it("writes the discovery file where the bridge looks for it", async () => {
    const { userDataDir } = makeUserDataDir();
    const discovery = new EndpointDiscoveryFile({ userDataDir, appVersion: "0.0.0-test" });
    expect(discovery.primaryPath).toBe(
      path.join(userDataDir, ENDPOINT_DIRECTORY_NAME, ENDPOINT_FILE_NAME)
    );
  });

  it("round-trips a socket endpoint from the desktop writer to the bridge reader", async () => {
    const { home, userDataDir } = makeUserDataDir();
    const socketPath = path.join(home, "mcp.sock");
    fs.writeFileSync(socketPath, "");

    const discovery = new EndpointDiscoveryFile({ userDataDir, appVersion: "0.0.0-test" });
    await discovery.write({ socketPath });

    const targets = discoverEndpointTargets({ platform: "linux", env: { HOME: home } });
    expect(targets.map((target) => target.socketPath)).toContain(socketPath);
  });
});
