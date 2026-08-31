import { describe, expect, test } from "vitest";
import type { ConnectionProfile } from "@nextshell/core";
import { connectionUpsertSchema } from "@nextshell/shared";
import {
  describeCopyOutcome,
  describeMoveOutcome,
  planConnectionMove,
  profileToUpsertPayload
} from "./connectionMove";

const UUID = "11111111-1111-4111-8111-111111111111";
const KEY_UUID = "22222222-2222-4222-8222-222222222222";
const PROXY_UUID = "33333333-3333-4333-8333-333333333333";
const FOLDER_UUID = "44444444-4444-4444-8444-444444444444";
const TARGET_FOLDER_UUID = "55555555-5555-4555-8555-555555555555";

const conn = (patch: Partial<ConnectionProfile> = {}): ConnectionProfile =>
  ({
    id: UUID,
    name: "prod-db",
    host: "10.1.2.3",
    port: 2222,
    username: "ops",
    authType: "password",
    credentialRef: "secret://conn-1",
    hostFingerprint: "SHA256:abc",
    strictHostKeyChecking: true,
    proxyId: PROXY_UUID,
    keepAliveEnabled: true,
    keepAliveIntervalSec: 45,
    terminalEncoding: "gb18030",
    backspaceMode: "ascii-delete",
    deleteMode: "ascii-backspace",
    groupPath: "/server/prod",
    folderId: FOLDER_UUID,
    tags: ["prod", "hk"],
    notes: "跳板机",
    favorite: true,
    monitorSession: true,
    agentAccess: "readonly",
    createdAt: "2026-01-01T00:00:00.000Z",
    updatedAt: "2026-02-01T00:00:00.000Z",
    lastConnectedAt: "2026-03-01T00:00:00.000Z",
    ...patch
  }) as ConnectionProfile;

describe("profileToUpsertPayload — 逐字段保真", () => {
  test("密码连接:业务字段逐个原样带回", () => {
    const profile = conn();
    const payload = profileToUpsertPayload(profile);

    expect(payload.id).toBe(profile.id);
    expect(payload.name).toBe("prod-db");
    expect(payload.host).toBe("10.1.2.3");
    expect(payload.port).toBe(2222);
    expect(payload.username).toBe("ops");
    expect(payload.authType).toBe("password");
    expect(payload.sshKeyId).toBeUndefined();
    expect(payload.folderId).toBe(FOLDER_UUID);
    expect(payload.hostFingerprint).toBe("SHA256:abc");
    expect(payload.strictHostKeyChecking).toBe(true);
    expect(payload.proxyId).toBe(PROXY_UUID);
    expect(payload.keepAliveEnabled).toBe(true);
    expect(payload.keepAliveIntervalSec).toBe(45);
    expect(payload.terminalEncoding).toBe("gb18030");
    expect(payload.backspaceMode).toBe("ascii-delete");
    expect(payload.deleteMode).toBe("ascii-backspace");
    expect(payload.groupPath).toBe("/server/prod");
    expect(payload.tags).toEqual(["prod", "hk"]);
    expect(payload.notes).toBe("跳板机");
    expect(payload.favorite).toBe(true);
    expect(payload.monitorSession).toBe(true);
    expect(payload.agentAccess).toBe("readonly");
  });

  /**
   * 这条是整个移动流程的命门:payload 里一旦出现 password,主进程就会用它重写凭据引用。
   * 不带 = 保留已保存的密码。
   */
  test("绝不带 password 字段", () => {
    const payload = profileToUpsertPayload(conn());
    expect("password" in payload).toBe(false);
    expect(payload.password).toBeUndefined();
  });

  test("私钥连接:sshKeyId 必须保真，否则主进程直接拒绝保存", () => {
    const payload = profileToUpsertPayload(
      conn({ authType: "privateKey", sshKeyId: KEY_UUID, credentialRef: undefined })
    );
    expect(payload.authType).toBe("privateKey");
    expect(payload.sshKeyId).toBe(KEY_UUID);
    // schema 的 superRefine 会拦下 privateKey 却没有 sshKeyId 的组合。
    expect(connectionUpsertSchema.safeParse(payload).success).toBe(true);
  });

  test("Agent 连接:没有密钥也没有凭据，其余字段照旧", () => {
    const payload = profileToUpsertPayload(
      conn({ authType: "agent", sshKeyId: undefined, credentialRef: undefined })
    );
    expect(payload.authType).toBe("agent");
    expect(payload.sshKeyId).toBeUndefined();
    expect(payload.proxyId).toBe(PROXY_UUID);
    expect(payload.keepAliveIntervalSec).toBe(45);
  });

  test("交互式登录同样不带密码", () => {
    const payload = profileToUpsertPayload(conn({ authType: "interactive" }));
    expect(payload.authType).toBe("interactive");
    expect("password" in payload).toBe(false);
  });

  test("三种认证方式的 payload 都能通过 upsert schema", () => {
    const cases: ConnectionProfile[] = [
      conn(),
      conn({ authType: "privateKey", sshKeyId: KEY_UUID }),
      conn({ authType: "agent" })
    ];
    for (const profile of cases) {
      const parsed = connectionUpsertSchema.safeParse(profileToUpsertPayload(profile));
      expect(parsed.success).toBe(true);
    }
  });

  test("可选字段为空时保持为空，不被填成默认值", () => {
    const payload = profileToUpsertPayload(
      conn({
        hostFingerprint: undefined,
        proxyId: undefined,
        keepAliveEnabled: undefined,
        keepAliveIntervalSec: undefined,
        notes: undefined,
        agentAccess: undefined,
        tags: []
      })
    );
    expect(payload.hostFingerprint).toBeUndefined();
    expect(payload.proxyId).toBeUndefined();
    expect(payload.keepAliveEnabled).toBeUndefined();
    expect(payload.keepAliveIntervalSec).toBeUndefined();
    expect(payload.notes).toBeUndefined();
    expect(payload.agentAccess).toBeUndefined();
    expect(payload.tags).toEqual([]);
  });

  test("tags 是副本，改 payload 不会污染原连接", () => {
    const profile = conn();
    const payload = profileToUpsertPayload(profile);
    payload.tags.push("mutated");
    expect(profile.tags).toEqual(["prod", "hk"]);
  });

  test("云连接带上 workspaceId，本地连接不带", () => {
    expect(
      profileToUpsertPayload(conn({ originKind: "cloud", originWorkspaceId: "ws-1" })).workspaceId
    ).toBe("ws-1");
    expect(profileToUpsertPayload(conn({ originKind: "local" })).workspaceId).toBeUndefined();
  });
});

describe("profileToUpsertPayload — overrides", () => {
  test("folderId 覆盖成目标目录", () => {
    expect(
      profileToUpsertPayload(conn(), { folderId: TARGET_FOLDER_UUID }).folderId
    ).toBe(TARGET_FOLDER_UUID);
  });

  test("folderId 传 null 表示移到顶层——传 undefined 会被主进程理解成「别动目录」", () => {
    expect(profileToUpsertPayload(conn(), { folderId: null }).folderId).toBeNull();
    expect(connectionUpsertSchema.safeParse(profileToUpsertPayload(conn(), { folderId: null })).success).toBe(
      true
    );
  });

  test("没给 folderId 键时沿用连接自身的目录", () => {
    expect(profileToUpsertPayload(conn(), { name: "renamed" }).folderId).toBe(FOLDER_UUID);
  });

  test("name 覆盖只改名字", () => {
    const payload = profileToUpsertPayload(conn(), { name: "renamed" });
    expect(payload.name).toBe("renamed");
    expect(payload.host).toBe("10.1.2.3");
  });
});

describe("planConnectionMove", () => {
  const a = conn({ id: "a", folderId: FOLDER_UUID });
  const b = conn({ id: "b", folderId: undefined });
  const c = conn({ id: "c", folderId: TARGET_FOLDER_UUID });

  test("只挑真正需要换目录的连接", () => {
    expect(planConnectionMove([a, b, c], ["a", "b", "c"], TARGET_FOLDER_UUID).map((x) => x.id)).toEqual([
      "a",
      "b"
    ]);
  });

  test("拖到自己已在的目录 = 空计划(不该弹任何 toast)", () => {
    expect(planConnectionMove([c], ["c"], TARGET_FOLDER_UUID)).toEqual([]);
  });

  test("顶层连接拖到根同样是空计划", () => {
    expect(planConnectionMove([b], ["b"], undefined)).toEqual([]);
  });

  test("忽略不在当前作用域列表里的 id", () => {
    expect(planConnectionMove([a], ["a", "not-here"], TARGET_FOLDER_UUID).map((x) => x.id)).toEqual([
      "a"
    ]);
  });
});

describe("describeMoveOutcome", () => {
  test("全部成功时说清数量与目标", () => {
    expect(describeMoveOutcome({ moved: 3, failed: 0, targetLabel: "亚太" })).toBe(
      "已移动 3 个连接到「亚太」"
    );
  });

  test("部分失败时如实报成功/失败数，不假装成功", () => {
    const text = describeMoveOutcome({ moved: 2, failed: 1, targetLabel: "亚太" });
    expect(text).toContain("成功 2");
    expect(text).toContain("失败 1");
  });
});

describe("describeCopyOutcome", () => {
  test("全部成功时说清数量与目标", () => {
    expect(describeCopyOutcome({ copied: 3, failed: 0, targetLabel: "Team A" })).toBe(
      "已复制 3 个连接到「Team A」"
    );
  });

  // 首错即弃、还只说"复制失败"的话，用户不知道已经进去了几个，只能去目标域里数。
  test("部分失败时如实报成功/失败数", () => {
    const text = describeCopyOutcome({ copied: 2, failed: 1, targetLabel: "Team A" });
    expect(text).toContain("成功 2");
    expect(text).toContain("失败 1");
    expect(text).toContain("没有创建副本");
  });
});
