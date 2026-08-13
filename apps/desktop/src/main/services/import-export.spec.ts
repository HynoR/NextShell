import { describe, expect, test } from "vitest";
import type { ConnectionExportFile } from "@nextshell/core";
import {
  enrichImportEntry,
  hashSshKeyContent,
  mapFinalShellAuth,
  matchSshKeyRef,
  parseFinalShellImport,
  parseNextShellImport,
  resolveImportedAuth
} from "./import-export";

const createNextShellExport = (
  groupPath: string,
  patch: Partial<ConnectionExportFile["connections"][number]> = {}
): ConnectionExportFile => ({
  format: "nextshell-connections",
  version: 1,
  exportedAt: "2026-06-15T00:00:00.000Z",
  connections: [
    {
      name: "prod-a",
      host: "10.0.0.1",
      port: 22,
      username: "root",
      authType: "password",
      groupPath,
      tags: [],
      favorite: false,
      terminalEncoding: "utf-8",
      backspaceMode: "ascii-backspace",
      deleteMode: "vt220-delete",
      monitorSession: false,
      ...patch
    }
  ]
});

describe("connection import parsers", () => {
  test("single NextShell import remaps exported groupPath into import zone", () => {
    const entries = parseNextShellImport(createNextShellExport("/server/prod"));

    expect(entries[0]?.groupPath).toBe("/import/prod");
  });

  test("directory NextShell import uses directory-derived groupPath", () => {
    const entries = parseNextShellImport(createNextShellExport("/server/prod"), {
      groupPathOverride: "/import/customer-a/prod"
    });

    expect(entries[0]?.groupPath).toBe("/import/customer-a/prod");
  });

  test("directory FinalShell import uses directory-derived groupPath", () => {
    const entries = parseFinalShellImport(
      {
        name: "legacy-a",
        host: "10.0.0.2",
        port: 22,
        user_name: "admin",
        authentication_type: 2
      },
      {
        groupPathOverride: "/import/legacy/prod"
      }
    );

    expect(entries[0]?.groupPath).toBe("/import/legacy/prod");
    expect(entries[0]?.sourceFormat).toBe("finalshell");
    expect(entries[0]?.authType).toBe("password");
  });

  test("preserves NextShell sshKeyRef on parse", () => {
    const entries = parseNextShellImport(
      createNextShellExport("/server/prod", {
        authType: "privateKey",
        sshKeyRef: { name: "ops-ed25519", fingerprint: "SHA256:abc" }
      })
    );

    expect(entries[0]?.authType).toBe("privateKey");
    expect(entries[0]?.sshKeyRef).toEqual({ name: "ops-ed25519", fingerprint: "SHA256:abc" });
  });
});

describe("FinalShell auth mapping", () => {
  test("treats an entry without secret_key_id as password auth", () => {
    expect(mapFinalShellAuth()).toEqual({ authType: "password" });
    expect(mapFinalShellAuth("   ")).toEqual({ authType: "password" });
  });

  test("maps an entry referencing a key to interactive and flags rebind", () => {
    expect(mapFinalShellAuth("key-1")).toEqual({
      authType: "interactive",
      originalAuth: "privateKey",
      needsKeyRebind: true
    });
    const entries = parseFinalShellImport({
      name: "key-host",
      host: "10.0.0.4",
      port: 22,
      user_name: "ops",
      authentication_type: 2,
      secret_key_id: "key-1"
    });
    expect(entries[0]?.authType).toBe("interactive");
    expect(entries[0]?.originalAuth).toBe("privateKey");
    expect(entries[0]?.needsKeyRebind).toBe(true);
  });

  // authentication_type 的数值编码未经证实,所以它不参与判定:两个数值都必须按密码导入,
  // 否则最常见的密码主机会被误标成“需重新绑定密钥”。
  test.each([1, 2])("ignores authentication_type %i when no key is referenced", (authType) => {
    const entries = parseFinalShellImport({
      name: "pw-host",
      host: "10.0.0.3",
      port: 22,
      user_name: "root",
      authentication_type: authType
    });
    expect(entries[0]?.authType).toBe("password");
    expect(entries[0]?.needsKeyRebind).toBeUndefined();
  });
});

describe("imported SSH key matching", () => {
  const keys = [
    { id: "11111111-1111-4111-8111-111111111111", name: "ops-ed25519", fingerprint: "SHA256:abc" },
    { id: "22222222-2222-4222-8222-222222222222", name: "other", fingerprint: "SHA256:def" }
  ];

  test("matches by fingerprint first", () => {
    expect(matchSshKeyRef({ name: "renamed", fingerprint: "SHA256:abc" }, keys)).toBe(
      "11111111-1111-4111-8111-111111111111"
    );
  });

  test("falls back to a unique name when fingerprint is missing", () => {
    expect(matchSshKeyRef({ name: "ops-ed25519" }, keys)).toBe(
      "11111111-1111-4111-8111-111111111111"
    );
  });

  test("does not guess when the name is duplicated", () => {
    expect(
      matchSshKeyRef({ name: "ops-ed25519" }, [...keys, { id: "dup", name: "ops-ed25519" }])
    ).toBeUndefined();
  });

  test("auto-binds a privateKey import when the fingerprint still exists", () => {
    const parsed = parseNextShellImport(
      createNextShellExport("/server/prod", {
        authType: "privateKey",
        sshKeyRef: { name: "ops-ed25519", fingerprint: "SHA256:abc" }
      })
    )[0]!;
    const enriched = enrichImportEntry(parsed, keys);
    expect(enriched.authType).toBe("privateKey");
    expect(enriched.sshKeyId).toBe("11111111-1111-4111-8111-111111111111");
    expect(enriched.needsKeyRebind).toBe(false);
  });

  test("degrades to interactive and flags rebind when no key matches", () => {
    const resolved = resolveImportedAuth(
      {
        authType: "privateKey",
        sshKeyRef: { name: "missing", fingerprint: "SHA256:nope" }
      },
      keys
    );
    expect(resolved).toEqual({
      authType: "interactive",
      needsKeyRebind: true,
      originalAuth: "privateKey"
    });
    const parsed = parseNextShellImport(
      createNextShellExport("/server/prod", {
        authType: "privateKey",
        sshKeyRef: { name: "missing", fingerprint: "SHA256:nope" }
      })
    )[0]!;
    const enriched = enrichImportEntry(parsed, keys);
    expect(enriched.authType).toBe("privateKey");
    expect(enriched.sshKeyId).toBeUndefined();
    expect(enriched.needsKeyRebind).toBe(true);
  });

  test("honours a user-picked sshKeyId over fingerprint matching", () => {
    const resolved = resolveImportedAuth(
      {
        authType: "interactive",
        originalAuth: "privateKey",
        sshKeyId: "22222222-2222-4222-8222-222222222222"
      },
      keys
    );
    expect(resolved.authType).toBe("privateKey");
    expect(resolved.sshKeyId).toBe("22222222-2222-4222-8222-222222222222");
  });

  test("key-content hash is stable and never claims to be an OpenSSH fingerprint", () => {
    const first = hashSshKeyContent("-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n");
    const second = hashSshKeyContent("-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n");
    expect(first).toMatch(/^sha256-content:/);
    // `SHA256:` 是 ssh-keygen 公钥指纹的格式,留给后续真正解析私钥时使用;
    // 现在这个值不能冒用它,否则两代格式会互相误判。
    expect(first).not.toMatch(/^SHA256:/);
    expect(first).toBe(second);
    expect(hashSshKeyContent("other")).not.toBe(first);
  });
});
