import type { ConnectionImportEntry, ConnectionProfile } from "@nextshell/core";

export type ResourceTab = "connections" | "keys" | "proxies";

/**
 * 表格可显示的列。默认开前五列(D17:「备注」进默认列),其余进「列…」下拉。
 * 这里的键序同时是「列…」下拉重新勾选时的插入序,所以它必须等于期望的显示顺序。
 */
export type ConnectionColumnKey =
  "name" | "address" | "username" | "auth" | "notes" | "tags" | "lastConnected" | "createdAt";

export const DEFAULT_CONNECTION_COLUMNS: ConnectionColumnKey[] = [
  "name",
  "address",
  "username",
  "auth",
  "notes"
];

export const CONNECTION_COLUMN_LABELS: Record<ConnectionColumnKey, string> = {
  name: "名称",
  address: "主机:端口",
  username: "用户名",
  auth: "认证方式",
  notes: "备注",
  tags: "标签",
  lastConnected: "最后连接",
  createdAt: "创建时间"
};

export type ConnectionSortKey = "name" | "address" | "lastConnected" | "createdAt";

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
 * 批量绑定认证的目标。主进程的契约里还留着按 groupPath 前缀匹配的 `group` 分支，
 * 但 V2 只构造 `connections`：按目录批量绑定也是先把该目录递归下的连接 id 收集出来再传，
 * 这样"选中的一批"与"某个目录下的一批"走同一条校验(同作用域、同一批 id)。
 */
export interface BatchAuthTarget {
  type: "connections";
  connectionIds: string[];
  label: string;
}
