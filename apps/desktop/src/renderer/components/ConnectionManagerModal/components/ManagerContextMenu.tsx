import { useEffect, useLayoutEffect, useRef, useState } from "react";
import type { ConnectionProfile } from "@nextshell/core";
import type { MgrClipboard, MgrContextMenuState, SortMode } from "../types";

interface ManagerContextMenuProps {
  state: MgrContextMenuState;
  clipboard: MgrClipboard | null;
  connections: ConnectionProfile[];
  selectedIds: Set<string>;
  sortMode: SortMode;
  onClose: () => void;
  onConnect: (connectionId: string) => void;
  onEdit: (connectionId: string) => void;
  onRename: (connectionId: string) => void;
  onCopy: () => void;
  onCut: () => void;
  onPaste: (targetGroupPath: string) => void;
  onDelete: () => void;
  onCopyAddress: (connectionId: string) => void;
  onNewConnection: (groupPath?: string) => void;
  onNewFolder: (parentGroupPath: string) => void;
  onSort: (mode: SortMode) => void;
  onImportNextShell: () => void;
  onImportFinalShell: () => void;
  onExportSelected: () => void;
  onExportAll: () => void;
}

export const ManagerContextMenu = ({
  state,
  clipboard,
  connections,
  selectedIds,
  sortMode,
  onClose,
  onConnect,
  onEdit,
  onRename,
  onCopy,
  onCut,
  onPaste,
  onDelete,
  onCopyAddress,
  onNewConnection,
  onNewFolder,
  onSort,
  onImportNextShell,
  onImportFinalShell,
  onExportSelected,
  onExportAll
}: ManagerContextMenuProps) => {
  const { x, y, target } = state;
  const menuRef = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<{ left: number; top: number }>({ left: x, top: y });
  const [visible, setVisible] = useState(false);
  const [newOpen, setNewOpen] = useState(false);
  const [sortOpen, setSortOpen] = useState(false);
  const [importOpen, setImportOpen] = useState(false);
  const [exportOpen, setExportOpen] = useState(false);

  useLayoutEffect(() => {
    const element = menuRef.current;
    if (!element) return;
    const { offsetWidth: width, offsetHeight: height } = element;
    const viewportWidth = window.innerWidth;
    const viewportHeight = window.innerHeight;
    const gap = 4;
    let top = y - height - gap;
    if (top < gap) top = y + gap;
    if (top + height > viewportHeight - gap) top = viewportHeight - height - gap;
    let left = x;
    if (left + width > viewportWidth - gap) left = x - width;
    if (left < gap) left = gap;
    setPos({ left, top });
    setVisible(true);
  }, [x, y]);

  useEffect(() => {
    const handler = (event: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(event.target as Node)) {
        onClose();
      }
    };
    window.addEventListener("mousedown", handler);
    return () => window.removeEventListener("mousedown", handler);
  }, [onClose]);

  const run = (fn: () => void) => {
    fn();
    onClose();
  };

  const isConnection = target.type === "connection";
  const connection = isConnection
    ? connections.find((item) => item.id === target.connectionId)
    : undefined;
  const isLocalConnection = connection?.originKind !== "cloud";
  const hasPaste = Boolean(clipboard);
  const targetGroupPath = target.type === "group"
    ? target.groupPath
    : target.type === "connection" && connection
      ? connection.groupPath
      : "/server";
  const multiCount = selectedIds.size;

  return (
    <div
      ref={menuRef}
      className="mgr-ctx-menu"
      style={{ left: pos.left, top: pos.top, visibility: visible ? "visible" : "hidden" }}
      onContextMenu={(event) => event.preventDefault()}
    >
      {isConnection && connection ? (
        <>
          <button className="mgr-ctx-item" onClick={() => run(() => onConnect(connection.id))}>
            <span className="mgr-ctx-icon"><i className="ri-terminal-box-line" aria-hidden="true" /></span> 连接
          </button>
          <button className="mgr-ctx-item" onClick={() => run(() => onEdit(connection.id))}>
            <span className="mgr-ctx-icon"><i className="ri-edit-line" aria-hidden="true" /></span> 编辑
          </button>
          <button className="mgr-ctx-item" onClick={() => run(() => onRename(connection.id))}>
            <span className="mgr-ctx-icon"><i className="ri-pencil-line" aria-hidden="true" /></span> 重命名
          </button>
          <div className="mgr-ctx-divider" />
        </>
      ) : null}

      {(isConnection || target.type === "group" || target.type === "empty") && (
        <>
          {isConnection ? (
            <>
              <button className="mgr-ctx-item" onClick={() => run(onCopy)} disabled={multiCount === 0}>
                <span className="mgr-ctx-icon"><i className="ri-file-copy-line" aria-hidden="true" /></span> 复制
                {multiCount > 1 ? <span className="mgr-ctx-badge">{multiCount}</span> : null}
              </button>
              {isLocalConnection ? (
                <button className="mgr-ctx-item" onClick={() => run(onCut)} disabled={multiCount === 0}>
                  <span className="mgr-ctx-icon"><i className="ri-scissors-cut-line" aria-hidden="true" /></span> 剪切
                  {multiCount > 1 ? <span className="mgr-ctx-badge">{multiCount}</span> : null}
                </button>
              ) : null}
            </>
          ) : null}
          <button className="mgr-ctx-item" onClick={() => run(() => onPaste(targetGroupPath))} disabled={!hasPaste}>
            <span className="mgr-ctx-icon"><i className="ri-clipboard-line" aria-hidden="true" /></span> 粘贴
            {hasPaste && clipboard ? (
              <span className="mgr-ctx-badge">{clipboard.mode === "copy" ? "复制" : "剪切"}</span>
            ) : null}
          </button>
          {isConnection ? <div className="mgr-ctx-divider" /> : null}
        </>
      )}

      {isConnection ? (
        <>
          <button className="mgr-ctx-item mgr-ctx-danger" onClick={() => run(onDelete)}>
            <span className="mgr-ctx-icon"><i className="ri-delete-bin-6-line" aria-hidden="true" /></span> 删除
            {multiCount > 1 ? <span className="mgr-ctx-badge">{multiCount}</span> : null}
          </button>
          <div className="mgr-ctx-divider" />
        </>
      ) : null}

      {isConnection && connection ? (
        <>
          <button className="mgr-ctx-item" onClick={() => run(() => onCopyAddress(connection.id))}>
            <span className="mgr-ctx-icon"><i className="ri-link-m" aria-hidden="true" /></span> 复制地址
          </button>
          <div className="mgr-ctx-divider" />
        </>
      ) : null}

      <div
        className="mgr-ctx-item mgr-ctx-submenu-trigger"
        onMouseEnter={() => setNewOpen(true)}
        onMouseLeave={() => setNewOpen(false)}
      >
        <span className="mgr-ctx-icon"><i className="ri-add-line" aria-hidden="true" /></span> 新建
        <span className="mgr-ctx-arrow">›</span>
        {newOpen ? (
          <div className="mgr-ctx-submenu">
            <button className="mgr-ctx-item" onClick={() => run(() => onNewConnection(targetGroupPath))}>
              <span className="mgr-ctx-icon"><i className="ri-terminal-box-line" aria-hidden="true" /></span> SSH连接(Linux)
            </button>
            <button className="mgr-ctx-item" onClick={() => run(() => onNewFolder(targetGroupPath))}>
              <span className="mgr-ctx-icon"><i className="ri-folder-3-line" aria-hidden="true" /></span> 文件夹
            </button>
          </div>
        ) : null}
      </div>

      <div
        className="mgr-ctx-item mgr-ctx-submenu-trigger"
        onMouseEnter={() => setSortOpen(true)}
        onMouseLeave={() => setSortOpen(false)}
      >
        <span className="mgr-ctx-icon"><i className="ri-sort-asc" aria-hidden="true" /></span> 排序
        <span className="mgr-ctx-arrow">›</span>
        {sortOpen ? (
          <div className="mgr-ctx-submenu">
            <button className={`mgr-ctx-item${sortMode === "name" ? " mgr-ctx-active" : ""}`} onClick={() => run(() => onSort("name"))}>
              <span className="mgr-ctx-icon"><i className="ri-sort-alphabet-asc" aria-hidden="true" /></span> 按名称
            </button>
            <button className={`mgr-ctx-item${sortMode === "host" ? " mgr-ctx-active" : ""}`} onClick={() => run(() => onSort("host"))}>
              <span className="mgr-ctx-icon"><i className="ri-global-line" aria-hidden="true" /></span> 按地址
            </button>
            <button className={`mgr-ctx-item${sortMode === "createdAt" ? " mgr-ctx-active" : ""}`} onClick={() => run(() => onSort("createdAt"))}>
              <span className="mgr-ctx-icon"><i className="ri-time-line" aria-hidden="true" /></span> 按创建时间
            </button>
          </div>
        ) : null}
      </div>

      <div className="mgr-ctx-divider" />

      <div
        className="mgr-ctx-item mgr-ctx-submenu-trigger"
        onMouseEnter={() => setImportOpen(true)}
        onMouseLeave={() => setImportOpen(false)}
      >
        <span className="mgr-ctx-icon"><i className="ri-upload-2-line" aria-hidden="true" /></span> 导入
        <span className="mgr-ctx-arrow">›</span>
        {importOpen ? (
          <div className="mgr-ctx-submenu">
            <button className="mgr-ctx-item" onClick={() => run(onImportNextShell)}>
              <span className="mgr-ctx-icon"><i className="ri-file-line" aria-hidden="true" /></span> NextShell 文件
            </button>
            <button className="mgr-ctx-item" onClick={() => run(onImportFinalShell)}>
              <span className="mgr-ctx-icon"><i className="ri-file-upload-line" aria-hidden="true" /></span> FinalShell 文件
            </button>
          </div>
        ) : null}
      </div>

      <div
        className="mgr-ctx-item mgr-ctx-submenu-trigger"
        onMouseEnter={() => setExportOpen(true)}
        onMouseLeave={() => setExportOpen(false)}
      >
        <span className="mgr-ctx-icon"><i className="ri-download-2-line" aria-hidden="true" /></span> 导出
        <span className="mgr-ctx-arrow">›</span>
        {exportOpen ? (
          <div className="mgr-ctx-submenu">
            <button className="mgr-ctx-item" onClick={() => run(onExportSelected)} disabled={multiCount === 0}>
              <span className="mgr-ctx-icon"><i className="ri-checkbox-multiple-line" aria-hidden="true" /></span> 导出选中
              {multiCount > 0 ? <span className="mgr-ctx-badge">{multiCount}</span> : null}
            </button>
            <button className="mgr-ctx-item" onClick={() => run(onExportAll)} disabled={connections.length === 0}>
              <span className="mgr-ctx-icon"><i className="ri-download-line" aria-hidden="true" /></span> 导出全部
            </button>
          </div>
        ) : null}
      </div>
    </div>
  );
};
