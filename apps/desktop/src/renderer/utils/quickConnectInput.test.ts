import type { ConnectionProfile } from "@nextshell/core";
import {
  buildQuickConnectUpsertInput,
  DEFAULT_QUICK_CONNECT_PORT,
  findExistingByAddress,
  parseQuickConnectInput
} from "./quickConnectInput";

const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

const assertEqual = <T>(actual: T, expected: T, message: string): void => {
  if (actual !== expected) {
    throw new Error(`${message}: expected "${String(expected)}", got "${String(actual)}"`);
  }
};

const makeConnection = (patch: Partial<ConnectionProfile>): ConnectionProfile => ({
  id: patch.id ?? "conn-1",
  name: patch.name ?? "default",
  host: patch.host ?? "127.0.0.1",
  port: patch.port ?? 22,
  username: patch.username ?? "root",
  authType: patch.authType ?? "password",
  credentialRef: patch.credentialRef,
  sshKeyId: patch.sshKeyId,
  hostFingerprint: patch.hostFingerprint,
  strictHostKeyChecking: patch.strictHostKeyChecking ?? false,
  proxyId: patch.proxyId,
  portForwards: patch.portForwards ?? [],
  keepaliveMode: patch.keepaliveMode ?? "inherit",
  terminalEncoding: patch.terminalEncoding ?? "utf-8",
  backspaceMode: patch.backspaceMode ?? "ascii-backspace",
  deleteMode: patch.deleteMode ?? "vt220-delete",
  groupPath: patch.groupPath ?? "/server",
  tags: patch.tags ?? [],
  notes: patch.notes,
  favorite: patch.favorite ?? false,
  monitorSession: patch.monitorSession ?? true,
  createdAt: patch.createdAt ?? "2026-01-01T00:00:00.000Z",
  updatedAt: patch.updatedAt ?? "2026-01-01T00:00:00.000Z",
  lastConnectedAt: patch.lastConnectedAt
});

(() => {
  const result = parseQuickConnectInput("root@10.0.0.1:22");
  assert(result.ok, "root@10.0.0.1:22 should parse");
  if (!result.ok) return;
  assertEqual(result.value.username, "root", "username should parse");
  assertEqual(result.value.host, "10.0.0.1", "host should parse");
  assertEqual(result.value.port, 22, "port should parse");
})();

(() => {
  const result = parseQuickConnectInput("root@10.0.0.1");
  assert(result.ok, "root@10.0.0.1 should parse");
  if (!result.ok) return;
  assertEqual(result.value.port, DEFAULT_QUICK_CONNECT_PORT, "default port should be 22");
})();

(() => {
  const result = parseQuickConnectInput("  root@Example.com:2202  ");
  assert(result.ok, "trimmed input should parse");
  if (!result.ok) return;
  assertEqual(result.value.username, "root", "trimmed username should match");
  assertEqual(result.value.host, "Example.com", "trimmed host should match");
  assertEqual(result.value.port, 2202, "trimmed port should match");
})();

(() => {
  const missingUsername = parseQuickConnectInput("@10.0.0.1:22");
  assert(!missingUsername.ok, "missing username should fail");

  const missingHost = parseQuickConnectInput("root@:22");
  assert(!missingHost.ok, "missing host should fail");
})();

(() => {
  const nonNumericPort = parseQuickConnectInput("root@10.0.0.1:abc");
  assert(!nonNumericPort.ok, "non-numeric port should fail");

  const outOfRangePort = parseQuickConnectInput("root@10.0.0.1:70000");
  assert(!outOfRangePort.ok, "out-of-range port should fail");
})();

(() => {
  const connections = [
    makeConnection({ id: "1", username: "root", host: "Example.COM", port: 22 }),
    makeConnection({ id: "2", username: "admin", host: "10.0.0.8", port: 22 })
  ];

  const parsed = parseQuickConnectInput("root@example.com:22");
  assert(parsed.ok, "match target should parse");
  if (!parsed.ok) return;

  const existing = findExistingByAddress(connections, parsed.value);
  assertEqual(existing?.id, "1", "matching should ignore host case");
})();

(() => {
  const parsed = parseQuickConnectInput("root@prod.internal");
  assert(parsed.ok, "upsert target should parse");
  if (!parsed.ok) return;

  const payload = buildQuickConnectUpsertInput(parsed.value);
  assertEqual(payload.name, "root@prod.internal", "name should use username@host");
  assertEqual(payload.authType, "password", "default authType should be password");
  assertEqual(payload.groupPath, "/server", "default group path should be /server");
  assertEqual(payload.monitorSession, true, "default monitor session should be true");
})();
