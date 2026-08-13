import { LOCAL_DEFAULT_SCOPE_KEY } from "../../core/src/index";

/**
 * 目录树 → `groupPath` 字符串的投影。
 *
 * `folderId` 才是本地的唯一真相,但 `groupPath` 不能删:云同步线协议
 * (`repoConnectionSchema.groupPath`)、MCP 工具 schema 与导出文件格式都读它,三者都不是我们
 * 能单方面更改的契约。所以它降级为"写入时由目录链算出来并存下"的派生值。
 *
 * 投影必须保持与旧数据一致的前缀,否则同一个 workspace 里的其他设备会把连接看成换了分组:
 * 本地是 `/server/…`,云是 `/workspace/<slug>/…`。这两个前缀是**线格式**,不是用户可见的分区。
 */

export interface FolderPathNode {
  id: string;
  name: string;
  parentId?: string;
}

export const LOCAL_GROUP_PATH_ROOT = "/server";
export const WORKSPACE_GROUP_PATH_ROOT = "/workspace";

/** 与云同步、目录回填两处保持一致的 workspace slug 规则。 */
export const workspaceSlug = (workspaceName: string): string =>
  workspaceName
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9._-]+/g, "-")
    .replace(/-+/g, "-")
    .replace(/^-|-$/g, "") || "workspace";

/**
 * 从叶目录一路向上收集到顶层,返回根→叶顺序的名称。
 *
 * 带访问集合是因为数据库里的 parent_id 理论上可能成环(仓储层拦得住正常写入,但外部工具、
 * 手工改库或未来的同步逻辑都可能塞进环);成环时宁可截断也不能让调用方死循环。
 */
export const resolveFolderNames = (
  folderId: string | undefined,
  folders: readonly FolderPathNode[]
): string[] => {
  if (!folderId) {
    return [];
  }
  const byId = new Map(folders.map((folder) => [folder.id, folder]));
  const names: string[] = [];
  const seen = new Set<string>();
  let cursor: string | undefined = folderId;
  while (cursor && !seen.has(cursor)) {
    seen.add(cursor);
    const folder: FolderPathNode | undefined = byId.get(cursor);
    if (!folder) {
      break;
    }
    names.unshift(folder.name);
    cursor = folder.parentId;
  }
  return names;
};

export interface DeriveGroupPathInput {
  scopeKey: string;
  /** 云 scope 必填;缺失时按本地投影,以免造出 `/workspace` 这种没有 slug 的幽灵根。 */
  workspaceName?: string;
  /** 根→叶顺序的目录名。 */
  folderNames: readonly string[];
}

export const deriveGroupPath = ({
  scopeKey,
  workspaceName,
  folderNames
}: DeriveGroupPathInput): string => {
  const root =
    scopeKey === LOCAL_DEFAULT_SCOPE_KEY || !workspaceName
      ? LOCAL_GROUP_PATH_ROOT
      : `${WORKSPACE_GROUP_PATH_ROOT}/${workspaceSlug(workspaceName)}`;
  const suffix = folderNames
    .map((name) => name.trim())
    .filter((name) => name.length > 0)
    .join("/");
  return suffix ? `${root}/${suffix}` : root;
};
