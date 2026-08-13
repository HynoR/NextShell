import type { ConnectionProfile } from "@nextshell/core";

/**
 * 右键菜单的命令集。单选与多选是两组不同的命令,而不是同一组加上"影响 N 个"——用户右击一行
 * 时想改的是那一行,右击选区时想做的是批量操作。
 */
export type RowCommand =
  | "edit"
  | "connect"
  | "rename"
  | "copyAddress"
  | "copyToScope"
  | "bindAuth"
  | "export"
  | "delete";

export interface RowCommandContext {
  /** 被右击的那一行。 */
  targetId: string;
  /** 当前勾选的行。 */
  selectedIds: readonly string[];
}

export interface RowCommandPlan {
  /** 命令实际作用的连接 id。右击选区外的行时以那一行为准,而不是沉默地作用于选区。 */
  affectedIds: string[];
  commands: RowCommand[];
  isBulk: boolean;
}

export const planRowCommands = ({ targetId, selectedIds }: RowCommandContext): RowCommandPlan => {
  const withinSelection = selectedIds.includes(targetId);
  const affectedIds = withinSelection && selectedIds.length > 1 ? [...selectedIds] : [targetId];
  const isBulk = affectedIds.length > 1;

  return {
    affectedIds,
    isBulk,
    commands: isBulk
      ? ["bindAuth", "copyToScope", "export", "delete"]
      : ["edit", "connect", "rename", "copyAddress", "copyToScope", "export", "delete"]
  };
};

export const describeAffected = (
  affectedIds: readonly string[],
  connections: readonly ConnectionProfile[]
): string => {
  if (affectedIds.length === 1) {
    const only = connections.find((item) => item.id === affectedIds[0]);
    return only ? `「${only.name}」` : "该连接";
  }
  return `${affectedIds.length} 个连接`;
};
