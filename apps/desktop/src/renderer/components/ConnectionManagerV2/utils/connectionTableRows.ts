/*
 * D18 后未接线,保留待用户决定删除或做视图切换(中栏已换成 `utils/gridItems.ts` 的磁贴分段)。
 */

import type { ConnectionFolder } from "@nextshell/core";
import type { ConnectionSort } from "../types";
import { sortConnectionRows, type ConnectionRow } from "./connectionRows";
import { buildFolderTree, type FolderTreeNode } from "./folderTree";

/**
 * 中栏表格的层级 row 模型(D16)。目录与连接混排在同一棵树里:目录行可展开、带递归计数,
 * 连接行直接摊开列。抽成纯函数是因为"哪一条挂在哪一层"有一堆边界(空目录、脏 parentId、
 * 搜索时的平铺、含子目录关),UI 里写会全部变成不可测的分支。
 */

export const FOLDER_ROW_PREFIX = "folder:";
export const CONNECTION_ROW_PREFIX = "conn:";

/** 目录行与连接行共用同一份 rowKey 空间，加前缀才不会撞 id。 */
export const folderRowKey = (folderId: string): string => `${FOLDER_ROW_PREFIX}${folderId}`;
export const connectionRowKey = (connectionId: string): string =>
  `${CONNECTION_ROW_PREFIX}${connectionId}`;

/** 非连接行(目录行)返回 undefined——勾选、选中态都只认连接。 */
export const connectionIdFromRowKey = (key: string): string | undefined =>
  key.startsWith(CONNECTION_ROW_PREFIX) ? key.slice(CONNECTION_ROW_PREFIX.length) : undefined;

export interface ConnectionTableFolderRow {
  key: string;
  kind: "folder";
  folder: ConnectionFolder;
  /** 该目录子树(含自身)内的连接数,按当前过滤结果统计。 */
  count: number;
  /** 空目录不给 children——给了空数组 rc-table 会画出一个点不开的展开箭头。 */
  children?: ConnectionTableRow[];
}

export interface ConnectionTableConnectionRow {
  key: string;
  kind: "connection";
  row: ConnectionRow;
  children?: undefined;
}

export type ConnectionTableRow = ConnectionTableFolderRow | ConnectionTableConnectionRow;

export interface ConnectionTableRowsInput {
  /** 已经按关键词过滤过的连接行。 */
  rows: readonly ConnectionRow[];
  folders: readonly ConnectionFolder[];
  /** 当前所在目录;根为 undefined。 */
  currentFolderId?: string;
  sort: ConnectionSort;
  /** 平铺:搜索中(无视目录)或「含子目录」关闭时。 */
  flat: boolean;
}

const toConnectionRow = (row: ConnectionRow): ConnectionTableConnectionRow => ({
  key: connectionRowKey(row.connection.id),
  kind: "connection",
  row
});

const findNode = (
  nodes: readonly FolderTreeNode[],
  folderId: string
): FolderTreeNode | undefined => {
  for (const node of nodes) {
    if (node.folder.id === folderId) {
      return node;
    }
    const found = findNode(node.children, folderId);
    if (found) {
      return found;
    }
  }
  return undefined;
};

const collectNodeIds = (nodes: readonly FolderTreeNode[], into: Set<string>): void => {
  for (const node of nodes) {
    into.add(node.folder.id);
    collectNodeIds(node.children, into);
  }
};

/**
 * 组装层级行。层内顺序:目录行恒在前(沿用目录树的 sortIndex→name 口径,与左栏一致),
 * 连接行在后并按当前排序键排。
 *
 * 兜底:folderId 悬空/成环、或指向当前子树之外的连接,一律挂到顶层而不是丢掉——
 * 静默消失的连接是找不回来的。
 */
export const buildConnectionTableRows = ({
  rows,
  folders,
  currentFolderId,
  sort,
  flat
}: ConnectionTableRowsInput): ConnectionTableRow[] => {
  if (flat) {
    return sortConnectionRows(rows, sort).map(toConnectionRow);
  }

  const roots = buildFolderTree(
    folders,
    rows.map((row) => row.connection)
  );
  const startNodes = currentFolderId ? (findNode(roots, currentFolderId)?.children ?? []) : roots;

  const renderedFolderIds = new Set<string>();
  collectNodeIds(startNodes, renderedFolderIds);

  const byFolder = new Map<string, ConnectionRow[]>();
  const loose: ConnectionRow[] = [];
  for (const row of rows) {
    const folderId = row.connection.folderId;
    if (folderId && renderedFolderIds.has(folderId)) {
      const bucket = byFolder.get(folderId);
      if (bucket) {
        bucket.push(row);
      } else {
        byFolder.set(folderId, [row]);
      }
    } else {
      loose.push(row);
    }
  }

  const toFolderRow = (node: FolderTreeNode): ConnectionTableFolderRow => {
    const children: ConnectionTableRow[] = [
      ...node.children.map(toFolderRow),
      ...sortConnectionRows(byFolder.get(node.folder.id) ?? [], sort).map(toConnectionRow)
    ];
    const base: ConnectionTableFolderRow = {
      key: folderRowKey(node.folder.id),
      kind: "folder",
      folder: node.folder,
      count: node.count
    };
    return children.length > 0 ? { ...base, children } : base;
  };

  return [...startNodes.map(toFolderRow), ...sortConnectionRows(loose, sort).map(toConnectionRow)];
};

/**
 * 合并式更新「收起了哪些目录」。`onExpandedRowsChange` 报告的是**当前视图里**的展开行,
 * 拿它整体重算折叠集会把视图外(别的目录、搜索前、含子目录关掉时看不见的层)的折叠状态
 * 一并抹掉:钻进一个目录再退出来,原先收起的目录全开了。
 *
 * 所以只在当前可见的 key 上增删,其余原样保留——和 `revealConnection` 只 filter 掉祖先
 * 是同一个思路。
 */
export const mergeCollapsedKeys = (
  previous: readonly string[],
  visibleFolderKeys: readonly string[],
  expandedKeys: ReadonlySet<string>
): string[] => {
  const visible = new Set(visibleFolderKeys);
  return [
    // 视图外的折叠状态原样留着。
    ...previous.filter((key) => !visible.has(key)),
    // 视图内的以这次报告为准。
    ...visibleFolderKeys.filter((key) => !expandedKeys.has(key))
  ];
};

/**
 * 从根到该目录(含自身)的目录行 key。外部定位到某个连接时用它把祖先一路展开——
 * 只展开直接父目录的话,连接仍然藏在折叠的爷爷目录里。
 */
export const folderAncestorRowKeys = (
  folderId: string | undefined,
  folders: readonly ConnectionFolder[]
): string[] => {
  const byId = new Map(folders.map((folder) => [folder.id, folder]));
  const keys: string[] = [];
  const seen = new Set<string>();
  let cursor = folderId;
  while (cursor && !seen.has(cursor)) {
    seen.add(cursor);
    const folder = byId.get(cursor);
    if (!folder) {
      break;
    }
    keys.unshift(folderRowKey(folder.id));
    cursor = folder.parentId;
  }
  return keys;
};
