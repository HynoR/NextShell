import { createServer, type IncomingHttpHeaders } from "node:http";
import { afterAll, beforeAll, beforeEach, expect, test } from "vitest";
import { CloudSyncApiV3Client } from "./cloud-sync-api-v3";

let port = 0;
let lastHeaders: IncomingHttpHeaders = {};
let responseBody: unknown;
const server = createServer((req, res) => {
  lastHeaders = req.headers;
  req.resume();
  req.on("end", () => {
    res.setHeader("content-type", "application/json");
    res.end(JSON.stringify(responseBody));
  });
});

beforeAll(async () => {
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  port = typeof address === "object" && address ? address.port : 0;
});

afterAll(async () => {
  await new Promise<void>((resolve) => server.close(() => resolve()));
});

beforeEach(() => {
  responseBody = { workspaceId: "w", displayName: "W" };
});

test("every request carries a User-Agent (empty UA gets dropped by WAFs)", async () => {
  const client = new CloudSyncApiV3Client();
  const result = await client.resolve({
    apiBaseUrl: `http://127.0.0.1:${port}`,
    workspaceName: "team",
    workspacePassword: "correct-horse-battery",
    ignoreTlsErrors: false,
    clientId: "client-1",
    clientVersion: "1.2.3"
  });
  expect(result.displayName).toBe("W");
  expect(lastHeaders["user-agent"]).toBe("NextShell/1.2.3");
  expect(lastHeaders["x-nextshell-client-id"]).toBe("client-1");
});

test("keeps monitoring and SSH key metadata when pulling a snapshot", async () => {
  const client = new CloudSyncApiV3Client();
  responseBody = {
    headCommitId: "head-1",
    snapshot: {
      snapshotId: "snapshot-1",
      createdAt: "2026-03-15T00:00:00.000Z",
      connections: [
        {
          uuid: "connection-1",
          name: "Prod",
          host: "prod.example.com",
          port: 22,
          username: "root",
          authType: "agent",
          monitorSession: true,
          strictHostKeyChecking: false,
          terminalEncoding: "utf-8",
          backspaceMode: "ascii-backspace",
          deleteMode: "vt220-delete",
          groupPath: "/workspace/team",
          tags: [],
          favorite: false,
          createdAt: "2026-03-15T00:00:00.000Z",
          updatedAt: "2026-03-15T00:00:00.000Z"
        }
      ],
      sshKeys: [
        {
          uuid: "key-1",
          name: "Deploy key",
          keyType: "ssh-ed25519",
          keyBits: 256,
          keyComment: "deploy@example.com",
          fingerprint: "SHA256:fingerprint",
          publicKeyLine: "ssh-ed25519 AAAA deploy@example.com",
          privateKey: {
            v: 1,
            alg: "aes-256-gcm",
            kdf: "scrypt",
            salt: "salt",
            iv: "iv",
            ciphertext: "ciphertext",
            tag: "tag"
          },
          createdAt: "2026-03-15T00:00:00.000Z",
          updatedAt: "2026-03-15T00:00:00.000Z"
        }
      ],
      proxies: []
    }
  };

  const result = await client.pull({
    apiBaseUrl: `http://127.0.0.1:${port}`,
    workspaceName: "team",
    workspacePassword: "correct-horse-battery",
    ignoreTlsErrors: false,
    clientId: "client-1",
    clientVersion: "1.2.3"
  });

  expect(result.snapshot?.connections[0]?.monitorSession).toBe(true);
  expect(result.snapshot?.sshKeys[0]).toMatchObject({
    keyType: "ssh-ed25519",
    keyBits: 256,
    keyComment: "deploy@example.com",
    fingerprint: "SHA256:fingerprint",
    publicKeyLine: "ssh-ed25519 AAAA deploy@example.com"
  });
});
