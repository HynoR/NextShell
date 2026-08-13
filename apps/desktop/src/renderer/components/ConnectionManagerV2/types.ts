import type { ConnectionImportEntry, ConnectionProfile } from "@nextshell/core";

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

/** 导入预览的一批条目（一个文件或目录扫描结果）。 */
export interface ImportPreviewBatch {
  fileName: string;
  sourcePath?: string;
  sourceKind?: "file" | "directory";
  entries: ConnectionImportEntry[];
}

/**
 * 批量绑定认证的目标。`group` 分支按 groupPath 前缀匹配，是主进程既有的契约形态；
 * V2 只构造 `connections`——作用域切换器保证选区同域，不需要再按路径圈定。
 */
export type BatchAuthTarget =
  | { type: "connections"; connectionIds: string[]; label: string }
  | { type: "group"; groupPath: string; label: string };
