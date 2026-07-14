import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { randomUUID } from "node:crypto";
import assert from "node:assert/strict";
import { describe, test } from "node:test";

import type { ConnectionProfile, ProxyProfile, SshKeyProfile } from "@nextshell/core";
import { DEFAULT_APP_PREFERENCES } from "@nextshell/core";
import { EncryptedSecretVault, generateDeviceKey } from "@nextshell/security";
import { SQLiteConnectionRepository, SQLiteProxyRepository, SQLiteSshKeyRepository } from "@nextshell/storage";
import {
  buildServerSummary,
  buildSshConnectOptions,
  ConnectionTargetAmbiguousError,
  ConnectionTargetNotFoundError,
  createReadonlyCredentialContext,
  resolveConnectionTarget,
  resolveNextShellDataPaths,
  searchServerSummaries
} from "./index";

interface Fixture {
  dbPath: string;
  cleanup: () => void;
}

const now = () => new Date().toISOString();

const createConnectionProfile = (
  overrides: Partial<ConnectionProfile> & Pick<ConnectionProfile, "name" | "host" | "username" | "authType">
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

const createFixture = async (): Promise<Fixture> => {
  const rootDir = fs.mkdtempSync(path.join(os.tmpdir(), "nextshell-runtime-"));
  const dataDir = path.join(rootDir, "storage");
  fs.mkdirSync(dataDir, { recursive: true });
  const dbPath = path.join(dataDir, "nextshell.db");

  const repo = new SQLiteConnectionRepository(dbPath);
  repo.saveAppPreferences({
    ...DEFAULT_APP_PREFERENCES,
    ssh: {
      keepAliveEnabled: true,
      keepAliveIntervalSec: 60
    }
  });

  const deviceKeyHex = generateDeviceKey();
  repo.saveDeviceKey(deviceKeyHex);
  const vault = new EncryptedSecretVault(repo.getSecretStore(), Buffer.from(deviceKeyHex, "hex"));

  const proxyRepo = new SQLiteProxyRepository(repo.getDb());
  const sshKeyRepo = new SQLiteSshKeyRepository(repo.getDb());

  const proxyPasswordRef = await vault.storeCredential("proxy-main", "proxy-secret");
  const proxyProfile: ProxyProfile = {
    id: randomUUID(),
    name: "main-proxy",
    proxyType: "socks5",
    host: "127.0.0.1",
    port: 1080,
    username: "proxy-user",
    credentialRef: proxyPasswordRef,
    createdAt: now(),
    updatedAt: now()
  };
  proxyRepo.save(proxyProfile);

  const privateKeyRef = await vault.storeCredential("ssh-key-main", "PRIVATE KEY CONTENT");
  const passphraseRef = await vault.storeCredential("ssh-key-main-passphrase", "passphrase");
  const sshKey: SshKeyProfile = {
    id: randomUUID(),
    name: "deploy-key",
    keyContentRef: privateKeyRef,
    passphraseRef,
    createdAt: now(),
    updatedAt: now()
  };
  sshKeyRepo.save(sshKey);

  const passwordCredentialRef = await vault.storeCredential("conn-password", "super-secret");
  repo.save(createConnectionProfile({
    name: "server1",
    host: "10.0.0.1",
    username: "root",
    authType: "password",
    credentialRef: passwordCredentialRef,
    proxyId: proxyProfile.id,
    groupPath: "/server/prod",
    tags: ["api", "prod"],
    favorite: true,
    resourceId: "local-default-11111111-1111-1111-1111-111111111111"
  }));

  repo.save(createConnectionProfile({
    name: "server1-backup",
    host: "10.0.0.2",
    username: "root",
    authType: "password",
    credentialRef: passwordCredentialRef,
    groupPath: "/server/prod",
    tags: ["backup"],
    resourceId: "local-default-22222222-2222-2222-2222-222222222222"
  }));

  repo.save(createConnectionProfile({
    name: "bastion",
    host: "10.0.0.10",
    username: "ubuntu",
    authType: "privateKey",
    sshKeyId: sshKey.id,
    groupPath: "/server/infra",
    tags: ["gateway"],
    resourceId: "local-default-33333333-3333-3333-3333-333333333333"
  }));

  repo.save(createConnectionProfile({
    name: "shared",
    host: "192.168.1.20",
    username: "ops",
    authType: "password",
    credentialRef: passwordCredentialRef,
    groupPath: "/server/shared",
    tags: ["team-a"],
    resourceId: "local-default-44444444-4444-4444-4444-444444444444"
  }));

  repo.save(createConnectionProfile({
    name: "shared",
    host: "192.168.1.21",
    username: "ops",
    authType: "password",
    credentialRef: passwordCredentialRef,
    groupPath: "/server/shared",
    tags: ["team-b"],
    resourceId: "local-default-55555555-5555-5555-5555-555555555555"
  }));

  repo.close();

  return {
    dbPath,
    cleanup: () => {
      fs.rmSync(rootDir, { recursive: true, force: true });
    }
  };
};

describe("runtime data path resolution", () => {
  test("uses platform defaults when no overrides are provided", () => {
    const paths = resolveNextShellDataPaths({
      platform: "darwin",
      homeDir: "/Users/example"
    });
    assert.equal(paths.dataDir, "/Users/example/Library/Application Support/NextShell/storage");
    assert.equal(paths.dbPath, "/Users/example/Library/Application Support/NextShell/storage/nextshell.db");
  });

  test("prefers explicit db path override", () => {
    const paths = resolveNextShellDataPaths({
      dbPath: "/tmp/custom/nextshell.db"
    });
    assert.equal(paths.dataDir, "/tmp/custom");
    assert.equal(paths.dbPath, "/tmp/custom/nextshell.db");
  });
});

describe("readonly credential context", () => {
  test("builds SSH options for password auth with proxy", async () => {
    const fixture = await createFixture();
    const context = createReadonlyCredentialContext({ dbPath: fixture.dbPath });
    try {
      const resolved = resolveConnectionTarget(context, "server1");
      const summary = buildServerSummary(resolved.connection);
      assert.equal(summary.nameId, "server1--11111111");

      const options = await buildSshConnectOptions(context, resolved.connection);
      assert.deepEqual(options, {
        host: "10.0.0.1",
        port: 22,
        username: "root",
        authType: "password",
        password: "super-secret",
        hostFingerprint: undefined,
        strictHostKeyChecking: false,
        proxy: {
          type: "socks5",
          host: "127.0.0.1",
          port: 1080,
          username: "proxy-user",
          password: "proxy-secret"
        },
        keepaliveInterval: 60000
      });
    } finally {
      context.close();
      fixture.cleanup();
    }
  });

  test("builds SSH options for private key auth", async () => {
    const fixture = await createFixture();
    const context = createReadonlyCredentialContext({ dbPath: fixture.dbPath });
    try {
      const resolved = resolveConnectionTarget(context, "10.0.0.10");
      const options = await buildSshConnectOptions(context, resolved.connection);
      assert.deepEqual(options, {
        host: "10.0.0.10",
        port: 22,
        username: "ubuntu",
        authType: "privateKey",
        privateKey: "PRIVATE KEY CONTENT",
        passphrase: "passphrase",
        hostFingerprint: undefined,
        strictHostKeyChecking: false,
        proxy: undefined,
        keepaliveInterval: 60000
      });
    } finally {
      context.close();
      fixture.cleanup();
    }
  });
});

describe("connection target resolution", () => {
  test("searches by tags and group path", async () => {
    const fixture = await createFixture();
    const context = createReadonlyCredentialContext({ dbPath: fixture.dbPath });
    try {
      const results = searchServerSummaries(context, "infra");
      assert.deepEqual(results, [
        {
          nameId: "bastion--33333333",
          name: "bastion",
          host: "10.0.0.10",
          port: 22,
          groupPath: "/server/infra",
          tags: ["gateway"],
          favorite: false
        }
      ]);
    } finally {
      context.close();
      fixture.cleanup();
    }
  });

  test("throws an ambiguous error when multiple exact names match", async () => {
    const fixture = await createFixture();
    const context = createReadonlyCredentialContext({ dbPath: fixture.dbPath });
    try {
      let error: unknown;
      try {
        resolveConnectionTarget(context, "shared");
      } catch (caught) {
        error = caught;
      }

      assert.ok(error instanceof ConnectionTargetAmbiguousError);
      assert.deepEqual((error as ConnectionTargetAmbiguousError).candidates, [
        {
          nameId: "shared--44444444",
          name: "shared",
          host: "192.168.1.20",
          port: 22,
          groupPath: "/server/shared",
          tags: ["team-a"],
          favorite: false
        },
        {
          nameId: "shared--55555555",
          name: "shared",
          host: "192.168.1.21",
          port: 22,
          groupPath: "/server/shared",
          tags: ["team-b"],
          favorite: false
        }
      ]);
    } finally {
      context.close();
      fixture.cleanup();
    }
  });

  test("throws a not found error when nothing matches", async () => {
    const fixture = await createFixture();
    const context = createReadonlyCredentialContext({ dbPath: fixture.dbPath });
    try {
      let error: unknown;
      try {
        resolveConnectionTarget(context, "missing-host");
      } catch (caught) {
        error = caught;
      }
      assert.ok(error instanceof ConnectionTargetNotFoundError);
    } finally {
      context.close();
      fixture.cleanup();
    }
  });
});
