import { describe, expect, test } from "vitest";
import type { ConnectionExportFile } from "@nextshell/core";
import {
  enrichImportEntry,
  mapFinalShellAuth,
  matchSshKeyRef,
  materializeFolderChain,
  parseFinalShellImport,
  parseNextShellImport,
  resolveImportedAuth,
  type ImportFolderNode,
  type ImportFolderStore
} from "./import-export";

/** 内存假仓储：better-sqlite3 是 Electron ABI，单测里跑不了真实目录表。 */
const createFolderStore = (
  seed: ImportFolderNode[] = []
): ImportFolderStore & { nodes: ImportFolderNode[]; created: string[] } => {
  const nodes = [...seed];
  const created: string[] = [];
  return {
    nodes,
    created,
    list: () => nodes,
    create: (name, parentId) => {
      const node: ImportFolderNode = { id: `f${nodes.length + 1}`, name, parentId };
      nodes.push(node);
      created.push(name);
      return node;
    }
  };
};

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
  // 往返导入保留原路径：过去 remapToImportZone 会把自己导出的 /server/prod 改写成 /import/prod，
  // 用户组织好的结构在往返一次后被抹平。
  test("single NextShell import keeps the exported groupPath", () => {
    const entries = parseNextShellImport(createNextShellExport("/server/prod"));

    expect(entries[0]?.groupPath).toBe("/server/prod");
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

describe("materializeFolderChain", () => {
  test("creates one folder per segment and returns the leaf", () => {
    const store = createFolderStore();
    const leaf = materializeFolderChain(["prod", "asia"], undefined, store);

    expect(store.created).toEqual(["prod", "asia"]);
    expect(store.nodes).toEqual([
      { id: "f1", name: "prod", parentId: undefined },
      { id: "f2", name: "asia", parentId: "f1" }
    ]);
    expect(leaf).toBe("f2");
  });

  test("returns the root folder unchanged for an empty chain", () => {
    const store = createFolderStore();
    expect(materializeFolderChain([], undefined, store)).toBeUndefined();
    expect(materializeFolderChain([], "root", store)).toBe("root");
    expect(store.created).toEqual([]);
  });

  // 重复导入同一份文件不能每次都多出一整套同名目录——库里的唯一索引也会直接拒掉第二次。
  test("reuses an existing folder with the same name under the same parent", () => {
    const store = createFolderStore([
      { id: "existing", name: "prod" },
      { id: "other", name: "prod", parentId: "existing" }
    ]);
    const leaf = materializeFolderChain(["prod", "asia"], undefined, store);

    expect(leaf).toBe("f3");
    expect(store.created).toEqual(["asia"]);
    expect(store.nodes.find((node) => node.id === "f3")?.parentId).toBe("existing");
  });

  test("is idempotent across repeated imports of the same structure", () => {
    const store = createFolderStore();
    const first = materializeFolderChain(["prod", "asia"], undefined, store);
    const second = materializeFolderChain(["prod", "asia"], undefined, store);

    expect(second).toBe(first);
    expect(store.created).toEqual(["prod", "asia"]);
  });

  // 同名但在不同父目录下是两个不同的目录，不能被复用逻辑合并掉。
  test("does not reuse a same-named folder that lives under a different parent", () => {
    const store = createFolderStore([{ id: "elsewhere", name: "asia", parentId: "somewhere" }]);
    const leaf = materializeFolderChain(["asia"], undefined, store);

    expect(leaf).toBe("f2");
    expect(store.created).toEqual(["asia"]);
  });

  test("hangs the whole chain under the given root folder", () => {
    const store = createFolderStore();
    const leaf = materializeFolderChain(["prod"], "root", store);

    expect(store.nodes[0]).toEqual({ id: "f1", name: "prod", parentId: "root" });
    expect(leaf).toBe("f1");
  });

  test("skips blank segments instead of creating a nameless folder", () => {
    const store = createFolderStore();
    expect(materializeFolderChain(["prod", "  ", "db"], undefined, store)).toBe("f2");
    expect(store.created).toEqual(["prod", "db"]);
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
});
