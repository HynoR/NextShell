import { createServer, type IncomingHttpHeaders } from "node:http";
import { afterAll, beforeAll, expect, test } from "vitest";
import { CloudSyncApiV3Client } from "./cloud-sync-api-v3";

let port = 0;
let lastHeaders: IncomingHttpHeaders = {};
const server = createServer((req, res) => {
  lastHeaders = req.headers;
  req.resume();
  req.on("end", () => {
    res.setHeader("content-type", "application/json");
    res.end(JSON.stringify({ workspaceId: "w", displayName: "W" }));
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
