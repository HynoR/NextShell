import { memo, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { MouseEvent, RefObject } from "react";
import { Table, Tag, Tooltip } from "antd";
import type { ColumnsType } from "antd/es/table";
import type { SortOrder } from "antd/es/table/interface";
import { formatRelativeTime } from "../../../utils/formatTime";
import type { ConnectionRow } from "../utils/connectionRows";
import {
  CONNECTION_COLUMN_LABELS,
  type ConnectionColumnKey,
  type ConnectionSort,
  type ConnectionSortKey
} from "../types";

interface ConnectionTableProps {
  rows: ConnectionRow[];
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
}

const SORTABLE: Partial<Record<ConnectionColumnKey, ConnectionSortKey>> = {
  name: "name",
  address: "address",
  lastConnected: "lastConnected"
};

/** 表头(size=small)约 39px;虚拟列表需要的是表体高度。 */
const HEADER_HEIGHT = 39;

/**
 * 表体高度必须是数字才能让 antd 滚动/虚拟列表生效——`scroll.y="100%"` 在没有完整高度链的
 * 容器里解析不出来,表体会无限增高,这正是"列表不能滚动"的根因。
 */
const useBodyHeight = (): [RefObject<HTMLDivElement | null>, number] => {
  const ref = useRef<HTMLDivElement>(null);
  const [height, setHeight] = useState(0);
  useLayoutEffect(() => {
    const element = ref.current;
    if (!element || typeof ResizeObserver === "undefined") {
      return undefined;
    }
    const observer = new ResizeObserver(() => setHeight(element.clientHeight));
    observer.observe(element);
    setHeight(element.clientHeight);
    return () => observer.disconnect();
  }, []);
  return [ref, height];
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
  onRowContextMenu
}: ConnectionTableProps) => {
  const [wrapRef, wrapHeight] = useBodyHeight();

  const tableColumns = useMemo<ColumnsType<ConnectionRow>>(() => {
    const definitions: Record<ConnectionColumnKey, ColumnsType<ConnectionRow>[number]> = {
      name: {
        title: CONNECTION_COLUMN_LABELS.name,
        key: "name",
        ellipsis: true,
        render: (_value, row) => (
          <span className="cm2-cell-name">
            {row.connection.favorite ? (
              <i className="ri-star-fill cm2-star" aria-hidden="true" />
            ) : null}
            {row.connection.name}
          </span>
        )
      },
      address: {
        title: CONNECTION_COLUMN_LABELS.address,
        key: "address",
        width: 180,
        ellipsis: true,
        render: (_value, row) => <span className="cm2-cell-mono">{row.address}</span>
      },
      username: {
        title: CONNECTION_COLUMN_LABELS.username,
        key: "username",
        width: 110,
        ellipsis: true,
        render: (_value, row) => row.connection.username || <span className="cm2-muted">—</span>
      },
      auth: {
        title: CONNECTION_COLUMN_LABELS.auth,
        key: "auth",
        width: 150,
        ellipsis: true,
        render: (_value, row) =>
          row.authMissing ? (
            <Tooltip title="私钥认证但没有可用密钥，连接会失败">
              <span className="cm2-auth-missing">
                <i className="ri-error-warning-line" aria-hidden="true" />
                {row.authLabel}
              </span>
            </Tooltip>
          ) : (
            row.authLabel
          )
      },
      tags: {
        title: CONNECTION_COLUMN_LABELS.tags,
        key: "tags",
        width: 160,
        render: (_value, row) =>
          row.connection.tags.length === 0 ? (
            <span className="cm2-muted">—</span>
          ) : (
            <>
              {row.connection.tags.slice(0, 2).map((tag) => (
                <Tag key={tag} bordered={false}>
                  {tag}
                </Tag>
              ))}
              {row.connection.tags.length > 2 ? (
                <Tag bordered={false}>+{row.connection.tags.length - 2}</Tag>
              ) : null}
            </>
          )
      },
      lastConnected: {
        title: CONNECTION_COLUMN_LABELS.lastConnected,
        key: "lastConnected",
        width: 120,
        render: (_value, row) =>
          row.connection.lastConnectedAt ? (
            formatRelativeTime(row.connection.lastConnectedAt)
          ) : (
            <span className="cm2-muted">从未连接</span>
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
        // 排序在外层(经过过滤/目录圈定之后)做,这里只报告用户点了哪一列。
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

  // 虚拟表要求 scroll.x/y 都是数字;x 给个下限,无宽度的名称列会自动吃满剩余宽度。
  // 首帧(和 SSR)还没量到高度,先渲染普通表,量到后切虚拟。
  const bodyHeight = Math.max(48, wrapHeight - HEADER_HEIGHT);
  const sizing =
    wrapHeight > 0 ? { virtual: true, scroll: { x: 560, y: bodyHeight } } : {};

  return (
    <div ref={wrapRef} className="cm2-table-wrap">
      {
        <Table<ConnectionRow>
          className="cm2-table app-table"
          size="small"
          {...sizing}
          rowKey={(row) => row.connection.id}
          dataSource={rows}
          columns={tableColumns}
          pagination={false}
          locale={{ emptyText: "此处没有连接" }}
          rowSelection={{
            selectedRowKeys: selectedIds,
            onChange: (keys) => onSelectionChange(keys.map(String)),
            columnWidth: 36
          }}
          rowClassName={(row) =>
            row.connection.id === focusedId ? "cm2-row cm2-row--focused" : "cm2-row"
          }
          onRow={(row) => ({
            onClick: () => onFocus(row.connection.id),
            onDoubleClick: () => onConnect(row.connection.id),
            onContextMenu: (event) => onRowContextMenu(event, row.connection.id)
          })}
        />
      }
    </div>
  );
};

export const ConnectionTable = memo(ConnectionTableInner);
