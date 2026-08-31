import type { ConnectionFolder } from "@nextshell/core";
import type { ConnectionSort } from "../types";
import { collectDescendantIds } from "./folderNavigation";
import { listSiblingFolders } from "./folderTree";
import {
  RECENT_ROOT_LIMIT,
  selectRecentConnections,
  sortConnectionRows,
  type ConnectionRow
} from "./connectionRows";

/**
 * Finder 式图标网格(D18)的数据层。中栏不再是一棵可以铺开上千行的表——它每次只渲染
 * **当前这一层**:子目录 + 直属连接,根目录再补一段「最近连接」。磁贴数因此与连接总量脱钩
 * (顶层目录数 + 根直属 + ≤30),1000+ 连接打开时的卡顿就是被全量渲染吃掉的那部分。
 */

export const GRID_FOLDER_KEY_PREFIX = "folder:";
export const GRID_CONNECTION_KEY_PREFIX = "connection:";

export interface GridFolderItem {
  kind: "folder";
  key: string;
  folder: ConnectionFolder;
  /** 子树(含自身)内的连接数,磁贴上显示为「N 台」。 */
  count: number;
}

export interface GridConnectionItem {
  kind: "connection";
  key: string;
  row: ConnectionRow;
}

export type GridItem = GridFolderItem | GridConnectionItem;

export type GridSectionKey = "browse" | "recent" | "search";

export interface GridSection {
  key: GridSectionKey;
  /** 浏览区没有标题——它就是"这个目录里的东西",加个标题只是噪音。 */
  title?: string;
  items: GridItem[];
}

export const RECENT_SECTION_TITLE = "最近连接";

const toConnectionItem = (row: ConnectionRow): GridConnectionItem => ({
  kind: "connection",
  key: `${GRID_CONNECTION_KEY_PREFIX}${row.connection.id}`,
  row
});

/** 每个目录的**直属**连接数。子树计数由它逐层累加,避免为每个目录重扫一遍连接。 */
const buildDirectCounts = (rows: readonly ConnectionRow[]): Map<string, number> => {
  const counts = new Map<string, number>();
  for (const row of rows) {
    const folderId = row.connection.folderId;
    if (folderId) {
      counts.set(folderId, (counts.get(folderId) ?? 0) + 1);
    }
  }
  return counts;
};

const countSubtree = (
  folderId: string,
  folders: readonly ConnectionFolder[],
  directCounts: Map<string, number>
): number => {
  let total = directCounts.get(folderId) ?? 0;
  for (const descendantId of collectDescendantIds(folderId, folders)) {
    total += directCounts.get(descendantId) ?? 0;
  }
  return total;
};

/** 中栏视图:默认「最近连接」——用户不需要一打开就面对整个根目录,浏览是左树主动点出来的。 */
export type GridViewMode = "recent" | "browse";

export interface BuildGridSectionsInput {
  /** 当前作用域内的**全部**连接(已按关键词过滤)。分层由本函数自己做。 */
  rows: readonly ConnectionRow[];
  folders: readonly ConnectionFolder[];
  currentFolderId?: string;
  searching: boolean;
  mode: GridViewMode;
  sort: ConnectionSort;
  recentLimit?: number;
}

/**
 * 搜索中 → 单段平铺(搜索本来就无视目录)。「最近连接」态 → 只有 ≤recentLimit 个连接磁贴,
 * 渲染量与总量、与根目录形状都无关。浏览态 → 当前层的子目录 + 直属连接,不再追加最近段
 * (那已经是独立视图,重复出现只是噪音)。空段不产出——渲染层据此判断"此目录为空"。
 */
export const buildGridSections = ({
  rows,
  folders,
  currentFolderId,
  searching,
  mode,
  sort,
  recentLimit = RECENT_ROOT_LIMIT
}: BuildGridSectionsInput): GridSection[] => {
  if (searching) {
    const items = sortConnectionRows(rows, sort).map(toConnectionItem);
    return items.length > 0 ? [{ key: "search", items }] : [];
  }

  if (mode === "recent") {
    const rowById = new Map(rows.map((row) => [row.connection.id, row]));
    const items = selectRecentConnections(
      rows.map((row) => row.connection),
      recentLimit
    )
      .map((connection) => rowById.get(connection.id))
      .filter((row): row is ConnectionRow => Boolean(row))
      .map(toConnectionItem);
    return items.length > 0 ? [{ key: "recent", items }] : [];
  }

  const directCounts = buildDirectCounts(rows);
  const folderItems: GridItem[] = listSiblingFolders(currentFolderId, folders).map((folder) => ({
    kind: "folder",
    key: `${GRID_FOLDER_KEY_PREFIX}${folder.id}`,
    folder,
    count: countSubtree(folder.id, folders, directCounts)
  }));

  // 目录被删干净之后连接可能挂着一个查不到的 folderId。把它当顶层处理,否则这台机器
  // 在网格里永久不可见(只能靠搜索捞),而用户并没有做过任何隐藏它的操作。
  const folderIds = new Set(folders.map((folder) => folder.id));
  const isDirectChild = (row: ConnectionRow): boolean => {
    const folderId = row.connection.folderId;
    const effective = folderId && folderIds.has(folderId) ? folderId : undefined;
    return effective === currentFolderId;
  };

  const directRows = rows.filter(isDirectChild);
  const sections: GridSection[] = [];
  const browseItems: GridItem[] = [
    ...folderItems,
    ...sortConnectionRows(directRows, sort).map(toConnectionItem)
  ];
  if (browseItems.length > 0) {
    sections.push({ key: "browse", items: browseItems });
  }

  return sections;
};

/**
 * 网格上真正露出来的连接 id。选中/详情的剪枝以它为准:网格分层之后"作用域里存在"不再
 * 等于"看得见",拿全量集去剪枝会让选中停留在一个已经翻页走掉的磁贴上。
 */
export const collectGridConnectionIds = (sections: readonly GridSection[]): string[] => {
  const ids: string[] = [];
  for (const section of sections) {
    for (const item of section.items) {
      if (item.kind === "connection") {
        ids.push(item.row.connection.id);
      }
    }
  }
  return ids;
};

export type GridSortValue = "name" | "lastConnected" | "createdAt";

export interface GridSortOption {
  value: GridSortValue;
  label: string;
  sort: ConnectionSort;
}

const NAME_SORT: ConnectionSort = { key: "name", direction: "asc" };

/**
 * 路径栏的排序下拉。方向不是随手取的默认值,是被 `sortConnectionRows` 的口径反推出来的:
 * lastConnected 的比较器自身就是"最近在前"(为了让「从未连接」无论升降序都沉底),所以
 * **asc 才是「最近连接」这个标签的意思**;createdAt 走字面时间序,desc 才是"最新创建在前"。
 * 两者取同一个 direction 会让其中一个反过来。
 */
export const GRID_SORT_OPTIONS: readonly GridSortOption[] = [
  { value: "name", label: "名称", sort: NAME_SORT },
  { value: "lastConnected", label: "最近连接", sort: { key: "lastConnected", direction: "asc" } },
  { value: "createdAt", label: "创建时间", sort: { key: "createdAt", direction: "desc" } }
];

export const resolveGridSort = (value: GridSortValue): ConnectionSort =>
  GRID_SORT_OPTIONS.find((option) => option.value === value)?.sort ?? NAME_SORT;

/** 反向映射。网格不再提供「主机」排序(列的概念随 D17 一起废止),它落回名称。 */
export const gridSortValue = (sort: ConnectionSort): GridSortValue =>
  sort.key === "lastConnected" || sort.key === "createdAt" ? sort.key : "name";
