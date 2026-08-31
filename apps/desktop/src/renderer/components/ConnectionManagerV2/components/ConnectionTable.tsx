import { memo, useCallback, useImperativeHandle, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { DragEvent, MouseEvent, RefObject } from "react";
import { Table, Tag, Tooltip } from "antd";
import type { TableRef } from "antd/es/table";
import type { ColumnsType } from "antd/es/table";
import type { SortOrder } from "antd/es/table/interface";
import type { ConnectionFolder } from "@nextshell/core";
import { formatDateTime, formatRelativeTime } from "../../../utils/formatTime";
import {
  CONNECTION_DRAG_MIME,
  isInternalConnectionDrag,
  parseConnectionDragIds,
  serializeConnectionDragIds
} from "../utils/managerDrop";
import {
  connectionIdFromRowKey,
  connectionRowKey,
  mergeCollapsedKeys,
  type ConnectionTableRow
} from "../utils/connectionTableRows";
import {
  CONNECTION_COLUMN_LABELS,
  type ConnectionColumnKey,
  type ConnectionSort,
  type ConnectionSortKey
} from "../types";

/*
 * D18 后未接线,保留待用户决定删除或做视图切换(中栏已换成 `ConnectionGrid` 图标网格)。
 */

/** 外部定位到某个连接时用:展开祖先目录并把行滚进视野。 */
export interface ConnectionTableHandle {
  revealConnection: (connectionId: string, ancestorFolderRowKeys: readonly string[]) => void;
}

interface ConnectionTableProps {
  /** 层级行(目录 + 连接混排);平铺时就是一串连接行。 */
  rows: ConnectionTableRow[];
  columns: ConnectionColumnKey[];
  sort: ConnectionSort;
  onSortChange: (sort: ConnectionSort) => void;
  selectedIds: string[];
  onSelectionChange: (ids: string[]) => void;
  focusedId?: string;
  /** 单击=只读详情。不进入编辑态，浏览时不可能误改。 */
  onFocus: (connectionId: string) => void;
  /** 双击=关闭对话框并直连。 */
  onConnect: (connectionId: string) => void;
  onRowContextMenu: (event: MouseEvent, connectionId: string) => void;
  /** 双击目录行=钻取进去。 */
  onEnterFolder: (folderId: string) => void;
  onFolderContextMenu: (event: MouseEvent, folder: ConnectionFolder) => void;
  /** 把一批连接拖到某个目录行上。 */
  onDropConnections: (connectionIds: string[], folderId: string) => void;
  tableRef?: RefObject<ConnectionTableHandle | null>;
}

const SORTABLE: Partial<Record<ConnectionColumnKey, ConnectionSortKey>> = {
  name: "name",
  address: "address",
  lastConnected: "lastConnected",
  createdAt: "createdAt"
};

/** 表头(size=small)约 39px;虚拟列表需要的是表体高度。 */
const HEADER_HEIGHT = 39;

/** 勾选列宽,与 rowSelection.columnWidth 一致。 */
const SELECTION_COLUMN_WIDTH = 36;

/**
 * 除名称外每列的定宽。虚拟表(tableLayout: fixed)不会给无宽列分配剩余空间——无宽列直接
 * 塌成 0(名称列曾因此整列隐形,展开图标和缩进都挤在 0 宽单元格里溢出),所以名称列的
 * "吃满剩余"必须自己量出来:容器宽 - 这些定宽之和 - 勾选列。
 */
const FIXED_COLUMN_WIDTHS: Record<Exclude<ConnectionColumnKey, "name">, number> = {
  address: 200,
  username: 110,
  auth: 150,
  notes: 170,
  tags: 160,
  lastConnected: 120,
  createdAt: 120
};

const MIN_NAME_COLUMN_WIDTH = 220;

export const resolveNameColumnWidth = (
  wrapWidth: number,
  visibleColumns: readonly ConnectionColumnKey[]
): number => {
  const fixed = visibleColumns.reduce(
    (sum, key) => (key === "name" ? sum : sum + FIXED_COLUMN_WIDTHS[key]),
    SELECTION_COLUMN_WIDTH
  );
  // 8px 留给竖向滚动条,余数向下取整避免出现横向滚动条。
  return Math.max(MIN_NAME_COLUMN_WIDTH, Math.floor(wrapWidth - fixed - 8));
};

/**
 * 表体宽高都必须是数字才能让 antd 滚动/虚拟列表生效——`scroll.y="100%"` 在没有完整高度链的
 * 容器里解析不出来,表体会无限增高,这正是"列表不能滚动"的根因;宽度则供名称列计算用。
 */
const useBodySize = (): [RefObject<HTMLDivElement | null>, { width: number; height: number }] => {
  const ref = useRef<HTMLDivElement>(null);
  const [size, setSize] = useState({ width: 0, height: 0 });
  useLayoutEffect(() => {
    const element = ref.current;
    if (!element || typeof ResizeObserver === "undefined") {
      return undefined;
    }
    const measure = (): void =>
      setSize({ width: element.clientWidth, height: element.clientHeight });
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    measure();
    return () => observer.disconnect();
  }, []);
  return [ref, size];
};

/** 拖多条时给一个"移动 N 个"的跟随标签,否则用户只看到一行在飞。 */
const attachDragGhost = (event: DragEvent<HTMLElement>, count: number): void => {
  if (count < 2 || typeof document === "undefined") {
    return;
  }
  const ghost = document.createElement("div");
  ghost.className = "cm2-drag-ghost";
  ghost.textContent = `移动 ${count} 个连接`;
  document.body.appendChild(ghost);
  event.dataTransfer.setDragImage(ghost, 12, 12);
  // 立刻移除会让某些平台抓不到快照，交给下一个任务队列。
  window.setTimeout(() => ghost.remove(), 0);
};

const ConnectionTableInner = ({
  rows,
  columns,
  sort,
  onSortChange,
  selectedIds,
  onSelectionChange,
  focusedId,
  onFocus,
  onConnect,
  onRowContextMenu,
  onEnterFolder,
  onFolderContextMenu,
  onDropConnections,
  tableRef
}: ConnectionTableProps) => {
  const [wrapRef, wrapSize] = useBodySize();
  const wrapHeight = wrapSize.height;
  // 记"收起了哪些"而不是"展开了哪些":默认全展开才对得起「含子目录」——都收起来的话，
  // 开与关看到的东西几乎一样。记收起还能让新出现的目录默认可见，而不是凭空多一个折叠块。
  const [collapsedKeys, setCollapsedKeys] = useState<readonly string[]>([]);
  const [dropFolderKey, setDropFolderKey] = useState<string>();
  const instanceRef = useRef<TableRef>(null);

  const allFolderKeys = useMemo(() => {
    const keys: string[] = [];
    const walk = (items: readonly ConnectionTableRow[]): void => {
      for (const item of items) {
        if (item.kind === "folder") {
          keys.push(item.key);
          walk(item.children ?? []);
        }
      }
    };
    walk(rows);
    return keys;
  }, [rows]);

  const expandedKeys = useMemo(
    () => allFolderKeys.filter((key) => !collapsedKeys.includes(key)),
    [allFolderKeys, collapsedKeys]
  );

  useImperativeHandle(
    tableRef,
    () => ({
      revealConnection: (connectionId, ancestorFolderRowKeys) => {
        setCollapsedKeys((previous) =>
          previous.filter((key) => !ancestorFolderRowKeys.includes(key))
        );
        // 目录是上面这次 setState 才展开的，行要等下一帧才存在。
        window.requestAnimationFrame(() => {
          instanceRef.current?.scrollTo({ key: connectionRowKey(connectionId) });
        });
      }
    }),
    []
  );

  const nameColumnWidth = resolveNameColumnWidth(wrapSize.width, columns);

  const tableColumns = useMemo<ColumnsType<ConnectionTableRow>>(() => {
    // 目录行只在名称列有内容:名称格横跨整行(colSpan),其余列格 colSpan:0 消失,
    // 否则目录行是一串空格子,视觉上只剩一个孤零零的展开图标。
    const folderSpanCell = (row: ConnectionTableRow): { colSpan?: number } =>
      row.kind === "folder" ? { colSpan: columns.length } : {};
    const folderHiddenCell = (row: ConnectionTableRow): { colSpan?: number } =>
      row.kind === "folder" ? { colSpan: 0 } : {};

    const definitions: Record<ConnectionColumnKey, ColumnsType<ConnectionTableRow>[number]> = {
      name: {
        title: CONNECTION_COLUMN_LABELS.name,
        key: "name",
        width: nameColumnWidth,
        ellipsis: true,
        onCell: folderSpanCell,
        render: (_value, row) =>
          row.kind === "folder" ? (
            <span className="cm2-cell-folder">
              <i className="ri-folder-3-line" aria-hidden="true" />
              <span className="cm2-cell-folder-name">{row.folder.name}</span>
              <span className="cm2-tree-count">{row.count}</span>
            </span>
          ) : (
            <span className="cm2-cell-name">
              {row.row.connection.favorite ? (
                <i className="ri-star-fill cm2-star" aria-hidden="true" />
              ) : null}
              {row.row.connection.name}
            </span>
          )
      },
      address: {
        title: CONNECTION_COLUMN_LABELS.address,
        key: "address",
        width: FIXED_COLUMN_WIDTHS.address,
        ellipsis: true,
        onCell: folderHiddenCell,
        render: (_value, row) =>
          row.kind === "folder" ? null : <span className="cm2-cell-mono">{row.row.address}</span>
      },
      username: {
        title: CONNECTION_COLUMN_LABELS.username,
        key: "username",
        width: FIXED_COLUMN_WIDTHS.username,
        ellipsis: true,
        onCell: folderHiddenCell,
        render: (_value, row) =>
          row.kind === "folder" ? null : row.row.connection.username || null
      },
      auth: {
        title: CONNECTION_COLUMN_LABELS.auth,
        key: "auth",
        width: FIXED_COLUMN_WIDTHS.auth,
        ellipsis: true,
        onCell: folderHiddenCell,
        render: (_value, row) => {
          if (row.kind === "folder") {
            return null;
          }
          return row.row.authMissing ? (
            <Tooltip title="私钥认证但没有可用密钥，连接会失败">
              <span className="cm2-auth-missing">
                <i className="ri-error-warning-line" aria-hidden="true" />
                {row.row.authLabel}
              </span>
            </Tooltip>
          ) : (
            row.row.authLabel
          );
        }
      },
      notes: {
        title: CONNECTION_COLUMN_LABELS.notes,
        key: "notes",
        width: FIXED_COLUMN_WIDTHS.notes,
        ellipsis: true,
        onCell: folderHiddenCell,
        render: (_value, row) => {
          if (row.kind === "folder") {
            return null;
          }
          const notes = row.row.connection.notes?.trim();
          // 备注常是整段文字，单行省略后靠原生 title 看全文。空值留白——满列 "—" 是噪音。
          return notes ? (
            <span className="cm2-cell-notes" title={notes}>
              {notes}
            </span>
          ) : null;
        }
      },
      tags: {
        title: CONNECTION_COLUMN_LABELS.tags,
        key: "tags",
        width: FIXED_COLUMN_WIDTHS.tags,
        onCell: folderHiddenCell,
        render: (_value, row) => {
          if (row.kind === "folder") {
            return null;
          }
          const tags = row.row.connection.tags;
          return tags.length === 0 ? null : (
            <>
              {tags.slice(0, 2).map((tag) => (
                <Tag key={tag} bordered={false}>
                  {tag}
                </Tag>
              ))}
              {tags.length > 2 ? <Tag bordered={false}>+{tags.length - 2}</Tag> : null}
            </>
          );
        }
      },
      lastConnected: {
        title: CONNECTION_COLUMN_LABELS.lastConnected,
        key: "lastConnected",
        width: FIXED_COLUMN_WIDTHS.lastConnected,
        onCell: folderHiddenCell,
        render: (_value, row) => {
          if (row.kind === "folder") {
            return null;
          }
          return row.row.connection.lastConnectedAt ? (
            formatRelativeTime(row.row.connection.lastConnectedAt)
          ) : (
            <span className="cm2-muted">从未连接</span>
          );
        }
      },
      createdAt: {
        title: CONNECTION_COLUMN_LABELS.createdAt,
        key: "createdAt",
        width: FIXED_COLUMN_WIDTHS.createdAt,
        onCell: folderHiddenCell,
        render: (_value, row) =>
          row.kind === "folder" ? null : (
            <span title={formatDateTime(row.row.connection.createdAt)}>
              {formatRelativeTime(row.row.connection.createdAt)}
            </span>
          )
      }
    };

    return columns.map((key) => {
      const sortKey = SORTABLE[key];
      const definition = definitions[key];
      if (!sortKey) {
        return definition;
      }
      const sortOrder: SortOrder | null =
        sort.key === sortKey ? (sort.direction === "asc" ? "ascend" : "descend") : null;
      return {
        ...definition,
        // 排序在外层(经过过滤/目录圈定之后)做，而且是逐层排;这里只报告用户点了哪一列。
        sorter: true,
        sortOrder,
        onHeaderCell: () => ({
          onClick: () =>
            onSortChange({
              key: sortKey,
              direction: sort.key === sortKey && sort.direction === "asc" ? "desc" : "asc"
            })
        })
      };
    });
  }, [columns, onSortChange, sort]);

  const handleDragStart = useCallback(
    (event: DragEvent<HTMLElement>, connectionId: string) => {
      // 拖的是选区里的一条 → 拖整个选区；拖的是选区外的一条 → 只拖它自己。
      const ids = selectedIds.includes(connectionId) ? selectedIds : [connectionId];
      event.dataTransfer.setData(CONNECTION_DRAG_MIME, serializeConnectionDragIds(ids));
      event.dataTransfer.effectAllowed = "move";
      attachDragGhost(event, ids.length);
    },
    [selectedIds]
  );

  const handleFolderDragOver = useCallback((event: DragEvent<HTMLElement>, key: string) => {
    if (!isInternalConnectionDrag(event.dataTransfer)) {
      return;
    }
    // 不 preventDefault 就没有 drop 事件；只对自己的拖拽类型这么做，文件拖入照旧走外层遮罩。
    event.preventDefault();
    event.stopPropagation();
    event.dataTransfer.dropEffect = "move";
    setDropFolderKey(key);
  }, []);

  const handleFolderDrop = useCallback(
    (event: DragEvent<HTMLElement>, folderId: string) => {
      setDropFolderKey(undefined);
      if (!isInternalConnectionDrag(event.dataTransfer)) {
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      const ids = parseConnectionDragIds(event.dataTransfer.getData(CONNECTION_DRAG_MIME));
      if (ids.length > 0) {
        onDropConnections(ids, folderId);
      }
    },
    [onDropConnections]
  );

  // 虚拟表要求 scroll.x/y 都是数字;x = 全部可见列宽之和(名称列宽已按容器实算),
  // 保证不出现横向滚动条。首帧(和 SSR)还没量到尺寸,先渲染普通表,量到后切虚拟。
  const bodyHeight = Math.max(48, wrapHeight - HEADER_HEIGHT);
  const totalWidth =
    nameColumnWidth +
    columns.reduce(
      (sum, key) => (key === "name" ? sum : sum + FIXED_COLUMN_WIDTHS[key]),
      SELECTION_COLUMN_WIDTH
    );
  const sizing = wrapHeight > 0 ? { virtual: true, scroll: { x: totalWidth, y: bodyHeight } } : {};

  return (
    <div ref={wrapRef} className="cm2-table-wrap">
      <Table<ConnectionTableRow>
        ref={instanceRef}
        className="cm2-table app-table"
        size="small"
        {...sizing}
        rowKey={(row) => row.key}
        dataSource={rows}
        columns={tableColumns}
        pagination={false}
        locale={{ emptyText: "此处没有连接" }}
        expandable={{
          expandedRowKeys: expandedKeys,
          onExpandedRowsChange: (keys) => {
            // 只报告当前视图里的展开行，所以只能合并式更新：整体重算会把视图外
            // (别的目录、搜索前)的折叠状态一起抹掉。
            const nextExpanded = new Set(keys.map(String));
            setCollapsedKeys((previous) =>
              mergeCollapsedKeys(previous, allFolderKeys, nextExpanded)
            );
          },
          indentSize: 14,
          // antd 默认的 +/- 方块换成 caret;连接行给同宽占位,与目录行的文本对齐。
          expandIcon: ({ expanded, onExpand, record }) =>
            record.kind === "folder" && record.children ? (
              <button
                type="button"
                className={`cm2-expand${expanded ? " cm2-expand--open" : ""}`}
                aria-label={expanded ? "收起目录" : "展开目录"}
                onClick={(event) => {
                  event.stopPropagation();
                  onExpand(record, event);
                }}
              >
                <i className="ri-arrow-right-s-line" aria-hidden="true" />
              </button>
            ) : (
              <span className="cm2-expand cm2-expand--leaf" aria-hidden="true" />
            )
        }}
        rowSelection={{
          selectedRowKeys: selectedIds.map(connectionRowKey),
          onChange: (keys) =>
            onSelectionChange(
              keys
                .map((key) => connectionIdFromRowKey(String(key)))
                .filter((id): id is string => Boolean(id))
            ),
          columnWidth: 36,
          // 目录行没有 checkbox：勾选是"选中一批连接"的动作，目录不是连接。
          // disabled 同时把它排除在表头全选之外，renderCell 再把那个空格子擦掉。
          getCheckboxProps: (row) => (row.kind === "folder" ? { disabled: true } : {}),
          renderCell: (_checked, row, _index, node) => (row.kind === "folder" ? null : node)
        }}
        rowClassName={(row) => {
          const classes = ["cm2-row"];
          if (row.kind === "folder") {
            classes.push("cm2-row--folder");
            if (row.key === dropFolderKey) {
              classes.push("cm2-row--drop-target");
            }
          } else if (row.row.connection.id === focusedId) {
            classes.push("cm2-row--focused");
          }
          return classes.join(" ");
        }}
        onRow={(row) =>
          row.kind === "folder"
            ? {
                // 单击=展开/收起，双击=钻取。行本身不可拖，但可以接收连接。
                onClick: () =>
                  setCollapsedKeys((previous) =>
                    previous.includes(row.key)
                      ? previous.filter((key) => key !== row.key)
                      : [...previous, row.key]
                  ),
                onDoubleClick: () => onEnterFolder(row.folder.id),
                onContextMenu: (event) => {
                  event.preventDefault();
                  onFolderContextMenu(event, row.folder);
                },
                onDragOver: (event) => handleFolderDragOver(event, row.key),
                onDragLeave: () => setDropFolderKey(undefined),
                onDrop: (event) => handleFolderDrop(event, row.folder.id)
              }
            : {
                draggable: true,
                onDragStart: (event) => handleDragStart(event, row.row.connection.id),
                onClick: () => onFocus(row.row.connection.id),
                onDoubleClick: () => onConnect(row.row.connection.id),
                onContextMenu: (event) => onRowContextMenu(event, row.row.connection.id)
              }
        }
      />
    </div>
  );
};

export const ConnectionTable = memo(ConnectionTableInner);
