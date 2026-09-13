import type { ConnectionProfile } from "@nextshell/core";
import type { ConnectionUpsertInput } from "@nextshell/shared";
import type { ManagerScope } from "./scopes";

/**
 * 移动连接没有轻量通道,只能走 `connection.upsert` 的全量 payload。全量的意思是:
 * **schema 里的每个字段都必须原样带回去**,漏掉一个就等于把它清空,而且没有任何报错。
 * 所以这里逐字段展开而不是 `{...profile}` 扩散——后者看起来更省事,却在 profile 加字段时
 * 静默继续对,在 schema 加字段时静默开始错。
 */

export interface ConnectionUpsertOverrides {
  /** `null` = 移到顶层;`undefined` = 不改目录。传了键才生效,没传就沿用连接自身的目录。 */
  folderId?: string | null;
  name?: string;
}

export const profileToUpsertPayload = (
  profile: ConnectionProfile,
  overrides: ConnectionUpsertOverrides = {}
): ConnectionUpsertInput => ({
  id: profile.id,
  // 云连接必须带上 workspaceId,否则主进程按"没给"重解析 origin，会把它当本地资源。
  workspaceId: profile.originKind === "cloud" ? profile.originWorkspaceId : undefined,
  name: "name" in overrides && overrides.name !== undefined ? overrides.name : profile.name,
  host: profile.host,
  port: profile.port,
  username: profile.username,
  authType: profile.authType,
  // password 刻意不出现:主进程只在 payload 带密码时重写凭据引用,不带就保留既有 credentialRef。
  // 带一个空串同样"安全"，但显式不写才能保证以后有人加 `password: values.password` 时被测试拦下。
  sshKeyId: profile.sshKeyId,
  folderId: "folderId" in overrides ? overrides.folderId : profile.folderId,
  hostFingerprint: profile.hostFingerprint,
  strictHostKeyChecking: profile.strictHostKeyChecking,
  proxyId: profile.proxyId,
  keepAliveEnabled: profile.keepAliveEnabled,
  keepAliveIntervalSec: profile.keepAliveIntervalSec,
  terminalEncoding: profile.terminalEncoding,
  backspaceMode: profile.backspaceMode,
  deleteMode: profile.deleteMode,
  // 给了 folderId 时主进程会按目录链重新投影 groupPath；这里带上只是为了满足 schema 的必填。
  groupPath: profile.groupPath,
  tags: [...profile.tags],
  notes: profile.notes,
  favorite: profile.favorite,
  monitorSession: profile.monitorSession
});

export interface MoveOutcome {
  moved: number;
  failed: number;
  targetLabel: string;
}

export interface CopyOutcome {
  copied: number;
  failed: number;
  targetLabel: string;
}

/**
 * 逐条 IPC 会出现"成功一半"的中间态,提示必须如实说清成功/失败各几条。移动与复制共用
 * 这一套口径:两处都是"循环里逐条发,失败的那条不影响别的",报数方式不该各写一句。
 */
const describeBatchOutcome = (
  { done, failed, targetLabel }: { done: number; failed: number; targetLabel: string },
  wording: { verb: string; strandedNote: string }
): string =>
  failed === 0
    ? `已${wording.verb} ${done} 个连接到「${targetLabel}」`
    : `${wording.verb}完成：成功 ${done} 个，失败 ${failed} 个（${wording.strandedNote}）`;

export const describeMoveOutcome = ({ moved, failed, targetLabel }: MoveOutcome): string =>
  describeBatchOutcome(
    { done: moved, failed, targetLabel },
    { verb: "移动", strandedNote: "失败的仍在原位" }
  );

/**
 * 逐条复制到目标作用域。首错就整个 catch 掉的话，用户既不知道已经进去了几个，也不知道
 * 要不要重来——只能自己去目标作用域里数。所以逐条 try/catch 计数，最后如实报数。
 * 「复制到…」与「从本地复制…」两个弹窗共用这一条循环。
 */
export const copyConnectionsToScope = async (
  connectionIds: readonly string[],
  target: ManagerScope,
  targetFolderId: string | undefined
): Promise<{ copied: number; failed: number; failure: unknown }> => {
  let copied = 0;
  let failure: unknown;
  for (const sourceId of connectionIds) {
    try {
      await window.nextshell.resourceOps.copyConnection({
        sourceId,
        targetOriginKind: target.kind,
        targetWorkspaceId: target.workspaceId,
        // 传 id 而不是名字：嵌套目录 a/b 只传 "b" 会落到目标域的另一个位置（或根）。
        targetFolderId
      });
      copied += 1;
    } catch (error) {
      failure = error;
    }
  }
  return { copied, failed: connectionIds.length - copied, failure };
};

/** 跨作用域复制走的是另一条 IPC(要重建密钥),但"成功一半"的报数口径和移动一致。 */
export const describeCopyOutcome = ({ copied, failed, targetLabel }: CopyOutcome): string =>
  describeBatchOutcome(
    { done: copied, failed, targetLabel },
    { verb: "复制", strandedNote: "失败的没有创建副本" }
  );

/**
 * 拖拽落点是不是空操作。目标目录与被拖连接现在所在的目录相同时,一条都不用发——
 * 发了不但白跑一次 IPC,还会弹一个"已移动 0 个"的假成功。
 */
export const planConnectionMove = (
  connections: readonly ConnectionProfile[],
  connectionIds: readonly string[],
  targetFolderId: string | undefined
): ConnectionProfile[] => {
  const wanted = new Set(connectionIds);
  return connections.filter(
    (connection) =>
      wanted.has(connection.id) && (connection.folderId ?? undefined) !== targetFolderId
  );
};
