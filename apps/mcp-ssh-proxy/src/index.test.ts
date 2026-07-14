import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { createRequire } from "node:module";
import { describe, test } from "node:test";
import { fileURLToPath } from "node:url";
import { generateKeyPairSync, randomUUID } from "node:crypto";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";
import type { ConnectionProfile } from "@nextshell/core";
import { DEFAULT_APP_PREFERENCES } from "@nextshell/core";
import { EncryptedSecretVault, generateDeviceKey } from "@nextshell/security";
import { SQLiteConnectionRepository } from "@nextshell/storage";
import type {
  Connection,
  AuthContext,
  ExecInfo,
  Server as Ssh2Server,
  ServerChannel,
  Session
} from "ssh2";

const require = createRequire(import.meta.url);
const { Server } = require("ssh2") as { Server: typeof Ssh2Server };

const currentDir = path.dirname(fileURLToPath(import.meta.url));
const serverEntry = path.join(currentDir, "index.ts");
const repoRoot = path.resolve(currentDir, "..");

interface TestFixture {
  dbPath: string;
  cleanup: () => void;
}

interface TestSshServer {
  port: number;
  close: () => Promise<void>;
}

const now = () => new Date().toISOString();

const buildEnv = (overrides: Record<string, string>): Record<string, string> => {
  const env = Object.fromEntries(
    Object.entries(process.env).filter(
      (entry): entry is [string, string] => typeof entry[1] === "string"
    )
  );
  return {
    ...env,
    ...overrides
  };
};

const createConnectionProfile = (
  overrides: Partial<ConnectionProfile> &
    Pick<ConnectionProfile, "name" | "host" | "username" | "authType">
): ConnectionProfile => ({
  id: overrides.id ?? randomUUID(),
  name: overrides.name,
  host: overrides.host,
  port: overrides.port ?? 22,
  username: overrides.username,
  authType: overrides.authType,
  credentialRef: overrides.credentialRef,
  sshKeyId: overrides.sshKeyId,
  hostFingerprint: overrides.hostFingerprint,
  strictHostKeyChecking: overrides.strictHostKeyChecking ?? false,
  proxyId: overrides.proxyId,
  keepAliveEnabled: overrides.keepAliveEnabled,
  keepAliveIntervalSec: overrides.keepAliveIntervalSec,
  terminalEncoding: overrides.terminalEncoding ?? "utf-8",
  backspaceMode: overrides.backspaceMode ?? "ascii-backspace",
  deleteMode: overrides.deleteMode ?? "vt220-delete",
  groupPath: overrides.groupPath ?? "/server",
  tags: overrides.tags ?? [],
  notes: overrides.notes,
  favorite: overrides.favorite ?? false,
  monitorSession: overrides.monitorSession ?? false,
  createdAt: overrides.createdAt ?? now(),
  updatedAt: overrides.updatedAt ?? now(),
  lastConnectedAt: overrides.lastConnectedAt,
  resourceId: overrides.resourceId,
  uuidInScope: overrides.uuidInScope,
  originKind: overrides.originKind,
  originScopeKey: overrides.originScopeKey,
  originWorkspaceId: overrides.originWorkspaceId,
  sshKeyResourceId: overrides.sshKeyResourceId,
  copiedFromResourceId: overrides.copiedFromResourceId
});

const startTestSshServer = async (): Promise<TestSshServer> => {
  const { privateKey } = generateKeyPairSync("rsa", {
    modulusLength: 2048,
    privateKeyEncoding: { type: "pkcs1", format: "pem" },
    publicKeyEncoding: { type: "spki", format: "pem" }
  });

  const server = new Server({ hostKeys: [privateKey] }, (client: Connection) => {
    client.on("authentication", (ctx: AuthContext) => {
      if (ctx.method === "password" && ctx.username === "root" && ctx.password === "super-secret") {
        ctx.accept();
        return;
      }
      ctx.reject();
    });

    client.on("ready", () => {
      client.on("session", (accept: () => Session) => {
        const session = accept();
        session.on(
          "exec",
          (acceptStream: () => ServerChannel, _reject: () => void, info: ExecInfo) => {
            const stream = acceptStream();
            if (info.command === "pwd") {
              stream.write("/home/root\n");
              stream.exit(0);
              stream.end();
              return;
            }
            if (info.command === "whoami") {
              stream.write("root\n");
              stream.exit(0);
              stream.end();
              return;
            }
            if (info.command === "fail") {
              stream.stderr.write("boom\n");
              stream.exit(2);
              stream.end();
              return;
            }
            stream.write(`ran:${info.command}\n`);
            stream.exit(0);
            stream.end();
          }
        );
      });
    });
  });

  await new Promise<void>((resolve, reject) => {
    server.listen(0, "127.0.0.1", (error?: Error) => {
      if (error) {
        reject(error);
        return;
      }
      resolve();
    });
  });

  const address = server.address();
  if (!address || typeof address === "string") {
    throw new Error("Failed to resolve SSH test server address");
  }

  return {
    port: address.port,
    close: async () => {
      await new Promise<void>((resolve, reject) => {
        server.close((error?: Error) => {
          if (error) {
            reject(error);
            return;
          }
          resolve();
        });
      });
    }
  };
};

const createFixture = async (port: number): Promise<TestFixture> => {
  const rootDir = fs.mkdtempSync(path.join(os.tmpdir(), "nextshell-mcp-ssh-proxy-"));
  const dataDir = path.join(rootDir, "storage");
  fs.mkdirSync(dataDir, { recursive: true });
  const dbPath = path.join(dataDir, "nextshell.db");

  const repo = new SQLiteConnectionRepository(dbPath);
  repo.saveAppPreferences({
    ...DEFAULT_APP_PREFERENCES,
    ssh: {
      keepAliveEnabled: true,
      keepAliveIntervalSec: 15
    }
  });

  const deviceKeyHex = generateDeviceKey();
  repo.saveDeviceKey(deviceKeyHex);
  const vault = new EncryptedSecretVault(repo.getSecretStore(), Buffer.from(deviceKeyHex, "hex"));
  const credentialRef = await vault.storeCredential("conn-server1", "super-secret");

  repo.save(
    createConnectionProfile({
      name: "server1",
      host: "127.0.0.1",
      port,
      username: "root",
      authType: "password",
      credentialRef,
      groupPath: "/server/test",
      tags: ["fixture"],
      favorite: true,
      resourceId: "local-default-11111111-1111-1111-1111-111111111111"
    })
  );

  repo.close();

  return {
    dbPath,
    cleanup: () => {
      fs.rmSync(rootDir, { recursive: true, force: true });
    }
  };
};

describe("nextshell mcp ssh proxy", () => {
  test("starts over stdio and executes remote commands with stored credentials", async () => {
    const sshServer = await startTestSshServer();
    const fixture = await createFixture(sshServer.port);
    const transport = new StdioClientTransport({
      command: process.execPath,
      args: ["--import", "tsx", serverEntry],
      cwd: repoRoot,
      env: buildEnv({
        NEXTSHELL_DB_PATH: fixture.dbPath
      }),
      stderr: "pipe"
    });
    const client = new Client(
      { name: "nextshell-mcp-ssh-proxy-test", version: "0.1.0" },
      { capabilities: {} }
    );

    try {
      await client.connect(transport);

      const tools = await client.listTools();
      const toolNames = tools.tools.map((tool) => tool.name);
      assert.ok(toolNames.includes("nextshell/list"));
      assert.ok(toolNames.includes("nextshell/connect"));
      assert.ok(toolNames.includes("nextshell/exec"));

      const listResult = await client.callTool({
        name: "nextshell/list",
        arguments: {}
      });
      const listedServers = (listResult.structuredContent as { servers: Array<{ nameId: string }> })
        .servers;
      assert.equal(listedServers.length, 1);
      assert.equal(listedServers[0]?.nameId, "server1--11111111");

      const connectResult = await client.callTool({
        name: "nextshell/connect",
        arguments: { target: "server1" }
      });
      const connectPayload = connectResult.structuredContent as {
        ok: boolean;
        sessionId: string;
      };
      assert.equal(connectPayload.ok, true);

      const execPwd = await client.callTool({
        name: "nextshell/exec",
        arguments: {
          sessionId: connectPayload.sessionId,
          command: "pwd"
        }
      });
      const execPwdPayload = execPwd.structuredContent as {
        ok: boolean;
        stdout: string;
        exitCode: number;
      };
      assert.equal(execPwdPayload.ok, true);
      assert.equal(execPwdPayload.stdout, "/home/root\n");
      assert.equal(execPwdPayload.exitCode, 0);

      const disconnectResult = await client.callTool({
        name: "nextshell/disconnect",
        arguments: {
          sessionId: connectPayload.sessionId
        }
      });
      const disconnectPayload = disconnectResult.structuredContent as {
        disconnected: boolean;
      };
      assert.equal(disconnectPayload.disconnected, true);

      const connectByNameId = await client.callTool({
        name: "nextshell/connect",
        arguments: { target: "server1--11111111" }
      });
      const connectByNameIdPayload = connectByNameId.structuredContent as {
        ok: boolean;
        sessionId: string;
      };
      assert.equal(connectByNameIdPayload.ok, true);

      const execWhoami = await client.callTool({
        name: "nextshell/exec",
        arguments: {
          sessionId: connectByNameIdPayload.sessionId,
          command: "whoami"
        }
      });
      const execWhoamiPayload = execWhoami.structuredContent as {
        stdout: string;
      };
      assert.equal(execWhoamiPayload.stdout, "root\n");
    } finally {
      await client.close().catch(() => undefined);
      await sshServer.close().catch(() => undefined);
      fixture.cleanup();
    }
  });

  test("fails fast when NextShell data is missing", async () => {
    const missingDbPath = path.join(os.tmpdir(), `missing-${randomUUID()}.db`);
    const child = spawn(process.execPath, ["--import", "tsx", serverEntry], {
      cwd: repoRoot,
      env: buildEnv({
        NEXTSHELL_DB_PATH: missingDbPath
      }),
      stdio: ["ignore", "pipe", "pipe"]
    });

    child.stdout.resume();

    const stderrChunks: string[] = [];
    child.stderr.on("data", (chunk) => {
      stderrChunks.push(String(chunk));
    });

    const exitCode = await new Promise<number | null>((resolve) => {
      child.once("exit", (code) => {
        resolve(code);
      });
    });

    assert.equal(exitCode, 1);
    assert.match(stderrChunks.join(""), /NextShell data not found/);
  });
});
