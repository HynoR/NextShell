import type { ConnectionProfile } from "@nextshell/core";

export type ResourceTab = "connections" | "keys" | "proxies";

/** 表格可显示的列。默认只开前四列,其余进「列…」下拉。 */
export type ConnectionColumnKey =
  | "name"
  | "address"
  | "username"
  | "auth"
  | "tags"
  | "lastConnected";

export const DEFAULT_CONNECTION_COLUMNS: ConnectionColumnKey[] = [
  "name",
  "address",
  "username",
  "auth"
];

export const CONNECTION_COLUMN_LABELS: Record<ConnectionColumnKey, string> = {
  name: "名称",
  address: "主机:端口",
  username: "用户名",
  auth: "认证方式",
  tags: "标签",
  lastConnected: "最后连接"
};

export type ConnectionSortKey = "name" | "address" | "lastConnected";

export interface ConnectionSort {
  key: ConnectionSortKey;
  direction: "asc" | "desc";
}

/** 右栏的三种形态:没选中、只读详情、编辑中。 */
export type DetailMode =
  | { kind: "empty" }
  | { kind: "view"; connection: ConnectionProfile }
  | { kind: "edit"; connection?: ConnectionProfile };
