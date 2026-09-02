import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent as ReactKeyboardEvent,
  type MouseEvent as ReactMouseEvent
} from "react";
import { Table } from "antd";
import type { ColumnsType, TableRef } from "antd/es/table";
import type { RemoteFileEntry } from "@nextshell/core";
import { useTableScrollY } from "../../../hooks/useTableScrollY";
import { formatFileSize, formatModifiedTime } from "../shared";
import {
  DEFAULT_FILE_EXPLORER_SORT,
  fileExplorerSortComparators,
  isEditableKeyboardTarget,
  moveFocusIndex,
  resolveEntryOpenAction,
  resolveFileExplorerKeyAction,
  resolveFileExplorerSort,
  sortRemoteFileEntries
} from "./fileExplorerKeyboard";

interface FileExplorerTableProps {
  files: RemoteFileEntry[];
  busy: boolean;
  selectedPaths: string[];
  onSelectionChange: (paths: string[]) => void;
  onNavigate: (path: string) => void;
  onRemoteEdit: (entry: RemoteFileEntry) => void;
  onContextMenu: (event: ReactMouseEvent, row?: RemoteFileEntry) => void;
  onParent: () => void;
}

// 焦点行用 outline 而非背景:行选中态(rowSelection)已在单元格背景上用 !important 着色,
// outline 画在行上互不干扰;inline 样式避免给 virtual 行新增全局 CSS
const FOCUSED_ROW_STYLE: CSSProperties = {
  outline: "1px solid var(--accent)",
  outlineOffset: -1
};

export const FileExplorerTable = ({
  files,
  busy,
  selectedPaths,
  onSelectionChange,
  onNavigate,
  onRemoteEdit,
  onContextMenu,
  onParent
}: FileExplorerTableProps) => {
  const [tableContainerRef, tableScrollY] = useTableScrollY();
  const tableRef = useRef<TableRef>(null);
  const [sortState, setSortState] = useState(DEFAULT_FILE_EXPLORER_SORT);
  const [focusedPath, setFocusedPath] = useState<string | null>(null);

  // files 是原始顺序;antd 内部按列排序渲染,这里用同一组 comparator 镜像出展示顺序,
  // 方向键才能按视觉顺序移动焦点行
  const displayFiles = useMemo(() => sortRemoteFileEntries(files, sortState), [files, sortState]);
  const focusedIndex = useMemo(
    () => (focusedPath === null ? -1 : displayFiles.findIndex((file) => file.path === focusedPath)),
    [displayFiles, focusedPath]
  );

  // virtual 表格只渲染视口附近的行,焦点行需主动 scrollTo 滚入视野(按 rowKey 定位,与展示顺序无关)
  useEffect(() => {
    if (focusedPath !== null && focusedIndex >= 0) {
      tableRef.current?.scrollTo({ key: focusedPath });
    }
  }, [focusedIndex, focusedPath]);

  const openEntry = (entry: RemoteFileEntry) => {
    const action = resolveEntryOpenAction(entry);
    if (action.type === "navigate") onNavigate(action.path);
    else onRemoteEdit(action.entry);
  };

  const handleKeyDown = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    const action = resolveFileExplorerKeyAction(event);
    // 只接管文件区的按键;事件目标是文本编辑控件时放行(路径输入框不在此子树内,这里是防御性判断)
    if (!action || isEditableKeyboardTarget(event.target as HTMLElement)) return;
    event.preventDefault();

    if (action === "parent") {
      onParent();
      return;
    }
    if (action === "activate") {
      const entry = focusedIndex >= 0 ? displayFiles[focusedIndex] : undefined;
      if (entry) openEntry(entry);
      return;
    }
    const nextIndex = moveFocusIndex(
      focusedIndex,
      action === "moveUp" ? -1 : 1,
      displayFiles.length
    );
    const next = nextIndex >= 0 ? displayFiles[nextIndex] : undefined;
    if (next) setFocusedPath(next.path);
  };

  const columns: ColumnsType<RemoteFileEntry> = useMemo(
    () => [
      {
        title: "文件名",
        dataIndex: "name",
        key: "name",
        width: 360,
        sorter: fileExplorerSortComparators.name,
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
        sorter: fileExplorerSortComparators.size,
        render: (value: number, row) => formatFileSize(value, row.type === "directory")
      },
      {
        title: "修改时间",
        dataIndex: "modifiedAt",
        key: "modifiedAt",
        width: 140,
        sorter: fileExplorerSortComparators.modifiedAt,
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
      ref={tableContainerRef}
      className="fe-table-wrap flex-1 min-h-0 overflow-hidden"
      tabIndex={0}
      aria-label="远程文件列表"
      onKeyDown={handleKeyDown}
      onContextMenu={(event) => onContextMenu(event)}
    >
      <Table
        ref={tableRef}
        size="small"
        virtual
        pagination={false}
        rowKey="path"
        columns={columns}
        dataSource={files}
        loading={busy}
        scroll={{ x: 800, y: tableScrollY }}
        onChange={(_pagination, _filters, sorter) => {
          setSortState(resolveFileExplorerSort(sorter));
        }}
        rowSelection={{
          selectedRowKeys: selectedPaths,
          onChange: (keys) => {
            onSelectionChange(keys.map((key) => String(key)));
          }
        }}
        onRow={(row) => ({
          className: row.path === focusedPath ? "fe-row-focused" : "",
          style: row.path === focusedPath ? FOCUSED_ROW_STYLE : undefined,
          onClick: () => setFocusedPath(row.path),
          onDoubleClick: () => openEntry(row),
          onContextMenu: (event) => onContextMenu(event, row)
        })}
      />
    </div>
  );
};
