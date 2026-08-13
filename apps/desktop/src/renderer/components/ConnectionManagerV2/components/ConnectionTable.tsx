import { useMemo } from "react";
import type { MouseEvent } from "react";
import { Table, Tag, Tooltip } from "antd";
import type { ColumnsType } from "antd/es/table";
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

const SORTABLE: Record<ConnectionColumnKey, ConnectionSortKey | undefined> = {
  name: "name",
  address: "address",
  username: undefined,
  auth: undefined,
  tags: undefined,
  lastConnected: "lastConnected"
};

export const ConnectionTable = ({
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
        width: 160,
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
      return {
        ...definition,
        title: (
          <button
            type="button"
            className="cm2-col-sort"
            onClick={() =>
              onSortChange({
                key: sortKey,
                direction: sort.key === sortKey && sort.direction === "asc" ? "desc" : "asc"
              })
            }
          >
            {definition.title as string}
            {sort.key === sortKey ? (
              <i
                className={sort.direction === "asc" ? "ri-arrow-up-s-line" : "ri-arrow-down-s-line"}
                aria-hidden="true"
              />
            ) : null}
          </button>
        )
      };
    });
  }, [columns, onSortChange, sort]);

  return (
    <Table<ConnectionRow>
      className="cm2-table app-table"
      size="small"
      rowKey={(row) => row.connection.id}
      dataSource={rows}
      columns={tableColumns}
      pagination={false}
      scroll={{ y: "100%" }}
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
  );
};
