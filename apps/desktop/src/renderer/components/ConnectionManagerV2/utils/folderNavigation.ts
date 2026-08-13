import type { ConnectionFolder, ConnectionProfile } from "@nextshell/core";

/**
 * 目录导航的纯函数层。树本身的构建在 folderTree.ts;这里是"当前选中目录"衍生出的查询:
 * 面包屑(详情卡里显示位置)与可见连接集。
 */

export interface BreadcrumbSegment {
  /** 根节点为 undefined。 */
  folderId?: string;
  label: string;
}

/** 从当前目录一路向上到根;成环时截断而不是死循环。 */
export const buildBreadcrumb = (
  folderId: string | undefined,
  folders: readonly ConnectionFolder[],
  rootLabel: string
): BreadcrumbSegment[] => {
  const byId = new Map(folders.map((folder) => [folder.id, folder]));
  const trail: BreadcrumbSegment[] = [];
  const seen = new Set<string>();
  let cursor = folderId;
  while (cursor && !seen.has(cursor)) {
    seen.add(cursor);
    const folder = byId.get(cursor);
    if (!folder) {
      break;
    }
    trail.unshift({ folderId: folder.id, label: folder.name });
    cursor = folder.parentId;
  }
  return [{ label: rootLabel }, ...trail];
};

/** 某个目录的所有后代 id(不含自身)。 */
export const collectDescendantIds = (
  folderId: string,
  folders: readonly ConnectionFolder[]
): Set<string> => {
  const childrenByParent = new Map<string, ConnectionFolder[]>();
  for (const folder of folders) {
    const key = folder.parentId ?? "";
    const bucket = childrenByParent.get(key);
    if (bucket) {
      bucket.push(folder);
    } else {
      childrenByParent.set(key, [folder]);
    }
  }
  const result = new Set<string>();
  const queue = [folderId];
  while (queue.length > 0) {
    const current = queue.pop() as string;
    for (const child of childrenByParent.get(current) ?? []) {
      if (result.has(child.id)) {
        continue;
      }
      result.add(child.id);
      queue.push(child.id);
    }
  }
  return result;
};

export interface VisibleConnectionsInput {
  folderId?: string;
  folders: readonly ConnectionFolder[];
  connections: readonly ConnectionProfile[];
  /** 关掉后只看直属连接;开着(默认)时钻取不会丢失全局视野。 */
  includeSubfolders: boolean;
}

export const listVisibleConnections = ({
  folderId,
  folders,
  connections,
  includeSubfolders
}: VisibleConnectionsInput): ConnectionProfile[] => {
  if (!folderId) {
    // 根:含子目录时是整个作用域,否则只有没被归档的顶层连接。
    return includeSubfolders
      ? [...connections]
      : connections.filter((connection) => !connection.folderId);
  }
  if (!includeSubfolders) {
    return connections.filter((connection) => connection.folderId === folderId);
  }
  const subtree = collectDescendantIds(folderId, folders);
  subtree.add(folderId);
  return connections.filter(
    (connection) => connection.folderId && subtree.has(connection.folderId)
  );
};

/**
 * 目录被删除或换了作用域后,当前所在目录可能已经不存在。返回仍然有效的目录 id,否则回到根,
 * 避免界面停在一个查不到的目录上显示空列表。
 */
export const reconcileCurrentFolder = (
  folderId: string | undefined,
  folders: readonly ConnectionFolder[]
): string | undefined =>
  folderId && folders.some((folder) => folder.id === folderId) ? folderId : undefined;
