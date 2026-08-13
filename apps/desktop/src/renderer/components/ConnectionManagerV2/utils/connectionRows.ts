import type { ConnectionProfile, SshKeyProfile } from "@nextshell/core";
import type { ConnectionSort } from "../types";

/**
 * 表格行的派生数据。认证列直接把"用哪把密钥"摊开,导入一批机器后能一眼看出哪些还没绑上——
 * 旧管理器要逐个点开表单才知道。
 */
export interface ConnectionRow {
  connection: ConnectionProfile;
  address: string;
  authLabel: string;
  /** 私钥认证但没绑到有效密钥;表格里标红。 */
  authMissing: boolean;
}

const AUTH_LABELS: Record<ConnectionProfile["authType"], string> = {
  password: "密码",
  interactive: "交互式",
  privateKey: "私钥",
  agent: "Agent"
};

export const buildConnectionRow = (
  connection: ConnectionProfile,
  sshKeys: readonly SshKeyProfile[]
): ConnectionRow => {
  const address = `${connection.host}:${connection.port}`;
  if (connection.authType !== "privateKey") {
    return {
      connection,
      address,
      authLabel: AUTH_LABELS[connection.authType],
      authMissing: false
    };
  }
  const key = connection.sshKeyId
    ? sshKeys.find((candidate) => candidate.id === connection.sshKeyId)
    : undefined;
  return {
    connection,
    address,
    authLabel: key ? `私钥 · ${key.name}` : "私钥 · 未绑定",
    // 引用了一把已经不存在的密钥同样算未绑定：连的时候一样会失败。
    authMissing: !key
  };
};

const compareText = (left: string, right: string): number =>
  left.localeCompare(right, undefined, { numeric: true, sensitivity: "base" });

/** 从未连接的排在最后,不论升降序——它们没有可比的时间,混在中间只会干扰。 */
const compareLastConnected = (left: ConnectionProfile, right: ConnectionProfile): number => {
  const leftAt = left.lastConnectedAt ? Date.parse(left.lastConnectedAt) : Number.NaN;
  const rightAt = right.lastConnectedAt ? Date.parse(right.lastConnectedAt) : Number.NaN;
  const leftMissing = Number.isNaN(leftAt);
  const rightMissing = Number.isNaN(rightAt);
  if (leftMissing && rightMissing) {
    return 0;
  }
  if (leftMissing) {
    return 1;
  }
  if (rightMissing) {
    return -1;
  }
  return rightAt - leftAt;
};

export const sortConnectionRows = (
  rows: readonly ConnectionRow[],
  sort: ConnectionSort
): ConnectionRow[] => {
  const sorted = [...rows].sort((left, right) => {
    if (sort.key === "address") {
      return compareText(left.address, right.address);
    }
    if (sort.key === "lastConnected") {
      const byTime = compareLastConnected(left.connection, right.connection);
      return byTime !== 0 ? byTime : compareText(left.connection.name, right.connection.name);
    }
    return compareText(left.connection.name, right.connection.name);
  });

  if (sort.direction === "desc") {
    // lastConnected 的比较器本身已是"最近在前"，反向时把缺失项保持在末尾。
    if (sort.key === "lastConnected") {
      const present = sorted.filter((row) => row.connection.lastConnectedAt);
      const missing = sorted.filter((row) => !row.connection.lastConnectedAt);
      return [...present.reverse(), ...missing];
    }
    sorted.reverse();
  }
  return sorted;
};

/** 搜索:名称、地址、用户名、标签、备注。 */
export const filterConnectionRows = (
  rows: readonly ConnectionRow[],
  keyword: string
): ConnectionRow[] => {
  const tokens = keyword
    .toLowerCase()
    .split(/\s+/)
    .filter((token) => token.length > 0);
  if (tokens.length === 0) {
    return [...rows];
  }
  return rows.filter((row) => {
    const haystack = [
      row.connection.name,
      row.address,
      row.connection.username,
      row.connection.tags.join(" "),
      row.connection.notes ?? ""
    ]
      .join(" ")
      .toLowerCase();
    return tokens.every((token) => haystack.includes(token));
  });
};
