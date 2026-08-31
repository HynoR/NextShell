import type { ConnectionFolder, ConnectionProfile } from "@nextshell/core";

/**
 * 目录树的数据层。树形导航(替代钻取式)要求一次性拿到完整层级与每个子树的连接数——
 * 计数让用户不点开就知道里面有没有东西。
 */

/** 根节点(作用域本身)在 antd Tree 里的 key。目录 id 不可能与它撞名。 */
export const FOLDER_TREE_ROOT_KEY = "__scope_root__";

export interface FolderTreeNode {
  folder: ConnectionFolder;
  /** 该目录子树(含自身)内的连接数。 */
  count: number;
  children: FolderTreeNode[];
}

const compareFolders = (left: ConnectionFolder, right: ConnectionFolder): number =>
  left.sortIndex !== right.sortIndex
    ? left.sortIndex - right.sortIndex
    : left.name.localeCompare(right.name);

/**
 * 构建目录树。脏数据防御:parentId 指向不存在的目录时按顶层处理;成环的目录整环丢弃
 * (环上的节点谁都不是谁的祖先,没有合法的挂载点)。
 */
export const buildFolderTree = (
  folders: readonly ConnectionFolder[],
  connections: readonly ConnectionProfile[]
): FolderTreeNode[] => {
  const byId = new Map(folders.map((folder) => [folder.id, folder]));
  const directCounts = new Map<string, number>();
  for (const connection of connections) {
    if (connection.folderId && byId.has(connection.folderId)) {
      directCounts.set(connection.folderId, (directCounts.get(connection.folderId) ?? 0) + 1);
    }
  }

  // 环检测:从每个节点向上走,能到根(parentId 为空或悬空)才算可挂载。
  const reachesRoot = new Map<string, boolean>();
  const canReachRoot = (id: string): boolean => {
    const cached = reachesRoot.get(id);
    if (cached !== undefined) {
      return cached;
    }
    const trail: string[] = [];
    let cursor: string | undefined = id;
    let result = false;
    while (cursor) {
      if (reachesRoot.has(cursor)) {
        result = reachesRoot.get(cursor) as boolean;
        break;
      }
      if (trail.includes(cursor)) {
        result = false; // 成环
        break;
      }
      trail.push(cursor);
      const folder = byId.get(cursor);
      if (!folder) {
        result = false;
        break;
      }
      if (!folder.parentId || !byId.has(folder.parentId)) {
        result = true;
        break;
      }
      cursor = folder.parentId;
    }
    for (const item of trail) {
      reachesRoot.set(item, result);
    }
    return result;
  };

  const build = (parentId: string | undefined): FolderTreeNode[] =>
    folders
      .filter((folder) => {
        if (!canReachRoot(folder.id)) {
          return false;
        }
        const effectiveParent =
          folder.parentId && byId.has(folder.parentId) ? folder.parentId : undefined;
        return effectiveParent === parentId;
      })
      .sort(compareFolders)
      .map((folder) => {
        const children = build(folder.id);
        return {
          folder,
          children,
          count:
            (directCounts.get(folder.id) ?? 0) +
            children.reduce((sum, child) => sum + child.count, 0)
        };
      });

  return build(undefined);
};

/**
 * 每个目录的完整路径标签(`a / b / c`)。编辑表单的目录下拉用它——不同深度的同名目录
 * 只显示名字时无法区分。
 */
export const buildFolderPathLabels = (
  folders: readonly ConnectionFolder[]
): Map<string, string> => {
  const byId = new Map(folders.map((folder) => [folder.id, folder]));
  const labels = new Map<string, string>();
  const resolve = (id: string, seen: Set<string>): string => {
    const cached = labels.get(id);
    if (cached !== undefined) {
      return cached;
    }
    const folder = byId.get(id);
    if (!folder) {
      return "";
    }
    let label = folder.name;
    if (folder.parentId && byId.has(folder.parentId) && !seen.has(folder.parentId)) {
      seen.add(folder.parentId);
      const parentLabel = resolve(folder.parentId, seen);
      if (parentLabel) {
        label = `${parentLabel} / ${folder.name}`;
      }
    }
    labels.set(id, label);
    return label;
  };
  for (const folder of folders) {
    resolve(folder.id, new Set([folder.id]));
  }
  return labels;
};

/** 某一层的目录,按树上显示的顺序(sortIndex 优先,同值按名称)。 */
export const listSiblingFolders = (
  parentId: string | undefined,
  folders: readonly ConnectionFolder[]
): ConnectionFolder[] => {
  const byId = new Map(folders.map((folder) => [folder.id, folder]));
  return folders
    .filter((folder) => {
      const effectiveParent =
        folder.parentId && byId.has(folder.parentId) ? folder.parentId : undefined;
      return effectiveParent === parentId;
    })
    .sort(compareFolders);
};

export interface FolderReorderStep {
  id: string;
  sortIndex: number;
}

/**
 * 把 dragId 插到 dropId 的前/后,算出该层需要写回的 (id, sortIndex)。
 *
 * 整层重编号 0..n-1 而不是只改被拖的那个:目录创建时 sortIndex 一律是 0,只改一个会得到
 * 一堆并列的 0,顺序又退回按名称排——用户拖了但看不出任何变化。返回值只含真正变了的项,
 * 免得为没动的目录白发 IPC。dragId 与 dropId 同一个、或 dropId 不存在时返回空计划。
 */
export const planSiblingReorder = ({
  folders,
  dragId,
  dropId,
  placeAfter
}: {
  folders: readonly ConnectionFolder[];
  dragId: string;
  dropId: string;
  placeAfter: boolean;
}): FolderReorderStep[] => {
  const byId = new Map(folders.map((folder) => [folder.id, folder]));
  const dragged = byId.get(dragId);
  const dropTarget = byId.get(dropId);
  if (!dragged || !dropTarget || dragId === dropId) {
    return [];
  }
  const targetParentId =
    dropTarget.parentId && byId.has(dropTarget.parentId) ? dropTarget.parentId : undefined;

  const ordered = listSiblingFolders(targetParentId, folders)
    .map((folder) => folder.id)
    .filter((id) => id !== dragId);
  const anchor = ordered.indexOf(dropId);
  if (anchor < 0) {
    return [];
  }
  ordered.splice(placeAfter ? anchor + 1 : anchor, 0, dragId);

  return ordered
    .map((id, index) => ({ id, sortIndex: index }))
    .filter((step) => byId.get(step.id)?.sortIndex !== step.sortIndex);
};

/** 判断 dragId 是否为 targetId 的祖先(或就是它自己)——这样的移动会把子树挂到自己里面。 */
export const isSelfOrAncestor = (
  dragId: string,
  targetId: string | undefined,
  folders: readonly ConnectionFolder[]
): boolean => {
  if (!targetId) {
    return false;
  }
  const byId = new Map(folders.map((folder) => [folder.id, folder]));
  const seen = new Set<string>();
  let cursor: string | undefined = targetId;
  while (cursor && !seen.has(cursor)) {
    if (cursor === dragId) {
      return true;
    }
    seen.add(cursor);
    cursor = byId.get(cursor)?.parentId;
  }
  return false;
};
