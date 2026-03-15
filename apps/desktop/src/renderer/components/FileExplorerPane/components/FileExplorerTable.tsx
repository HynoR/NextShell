import { useMemo, type MouseEvent as ReactMouseEvent } from "react";
import { Table } from "antd";
import type { ColumnsType } from "antd/es/table";
import type { RemoteFileEntry } from "@nextshell/core";
import { fileTypeLabel, formatFileSize, formatModifiedTime } from "../shared";

interface FileExplorerTableProps {
  files: RemoteFileEntry[];
  busy: boolean;
  selectedPaths: string[];
  onSelectionChange: (paths: string[]) => void;
  onNavigate: (path: string) => void;
  onRemoteEdit: (entry: RemoteFileEntry) => void;
  onContextMenu: (event: ReactMouseEvent, row: RemoteFileEntry) => void;
}

export const FileExplorerTable = ({
  files,
  busy,
  selectedPaths,
  onSelectionChange,
  onNavigate,
  onRemoteEdit,
  onContextMenu
}: FileExplorerTableProps) => {
  const columns: ColumnsType<RemoteFileEntry> = useMemo(
    () => [
      {
        title: "文件名",
        dataIndex: "name",
        key: "name",
        sorter: (a, b) => a.name.localeCompare(b.name),
        defaultSortOrder: "ascend",
        render: (_value: string, row: RemoteFileEntry) => (
          <span className="inline-flex items-center gap-1.5">
            <i
              className={
                row.type === "directory"
                  ? "ri-folder-3-fill text-sm shrink-0 leading-none"
                  : "ri-file-text-line text-sm shrink-0 leading-none"
              }
              aria-hidden="true"
            />
            {row.name}
          </span>
        )
      },
      {
        title: "大小",
        dataIndex: "size",
        key: "size",
        width: 90,
        sorter: (a, b) => a.size - b.size,
        render: (value: number, row) => formatFileSize(value, row.type === "directory")
      },
      {
        title: "类型",
        dataIndex: "type",
        key: "type",
        width: 72,
        render: (value: RemoteFileEntry["type"]) => fileTypeLabel(value)
      },
      {
        title: "修改时间",
        dataIndex: "modifiedAt",
        key: "modifiedAt",
        width: 140,
        sorter: (a, b) => a.modifiedAt.localeCompare(b.modifiedAt),
        render: (value: string) => formatModifiedTime(value)
      },
      {
        title: "权限",
        dataIndex: "permissions",
        key: "permissions",
        width: 110
      },
      {
        title: "用户/用户组",
        key: "ownerGroup",
        width: 120,
        render: (_value, row) => `${row.owner}/${row.group}`
      }
    ],
    []
  );

  return (
    <div
      className="fe-table-wrap flex-1 min-h-0 overflow-auto"
      onContextMenu={(event) => event.stopPropagation()}
    >
      <Table
        size="small"
        pagination={false}
        rowKey="path"
        columns={columns}
        dataSource={files}
        loading={busy}
        scroll={{ y: "100%" }}
        rowSelection={{
          selectedRowKeys: selectedPaths,
          onChange: (keys) => {
            onSelectionChange(keys.map((key) => String(key)));
          }
        }}
        onRow={(row) => ({
          onDoubleClick: () => {
            if (row.type === "directory") onNavigate(row.path);
            else onRemoteEdit(row);
          },
          onContextMenu: (event) => onContextMenu(event, row)
        })}
      />
    </div>
  );
};
