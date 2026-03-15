import { useEffect, useLayoutEffect, useRef, useState } from "react";
import type { RemoteFileEntry } from "@nextshell/core";
import type { Clipboard, ContextMenuState } from "../types";

interface FileExplorerContextMenuProps {
  state: ContextMenuState;
  clipboard: Clipboard | null;
  connectionId: string;
  onClose: () => void;
  onRefresh: () => void;
  onDownload: (entries: RemoteFileEntry[]) => void;
  onPackedDownload: (entries: RemoteFileEntry[]) => void;
  onUpload: () => void;
  onPackedUpload: () => void;
  onCopyPath: (entries: RemoteFileEntry[]) => void;
  onCopy: (entries: RemoteFileEntry[]) => void;
  onCut: (entries: RemoteFileEntry[]) => void;
  onPaste: () => void;
  onNewFolder: () => void;
  onNewFile: () => void;
  onRename: (entry: RemoteFileEntry) => void;
  onDelete: (entries: RemoteFileEntry[]) => void;
  onQuickDelete: (entries: RemoteFileEntry[]) => void;
  onRemoteEdit: (entry: RemoteFileEntry) => void;
}

export const FileExplorerContextMenu = ({
  state,
  clipboard,
  connectionId,
  onClose,
  onRefresh,
  onDownload,
  onPackedDownload,
  onUpload,
  onPackedUpload,
  onCopyPath,
  onCopy,
  onCut,
  onPaste,
  onNewFolder,
  onNewFile,
  onRename,
  onDelete,
  onQuickDelete,
  onRemoteEdit
}: FileExplorerContextMenuProps) => {
  const { x, y, entries } = state;
  const hasEntries = entries.length > 0;
  const single = entries.length === 1 ? entries[0] : undefined;
  const hasPaste = Boolean(clipboard);
  const [downloadOpen, setDownloadOpen] = useState(false);
  const [uploadOpen, setUploadOpen] = useState(false);
  const [newOpen, setNewOpen] = useState(false);
  const [pos, setPos] = useState<{ left: number; top: number }>({ left: x, top: y });
  const [visible, setVisible] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);

  useLayoutEffect(() => {
    const el = menuRef.current;
    if (!el) return;

    const { offsetWidth: width, offsetHeight: height } = el;
    const viewportWidth = window.innerWidth;
    const viewportHeight = window.innerHeight;
    const gap = 4;

    let top = y - height - gap;
    if (top < gap) {
      top = y + gap;
    }
    if (top + height > viewportHeight - gap) {
      top = viewportHeight - height - gap;
    }

    let left = x;
    if (left + width > viewportWidth - gap) {
      left = x - width;
    }
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

  return (
    <div
      ref={menuRef}
      className="fe-ctx-menu"
      style={{ left: pos.left, top: pos.top, visibility: visible ? "visible" : "hidden" }}
      onContextMenu={(event) => event.preventDefault()}
    >
      <button className="fe-ctx-item" onClick={() => run(onRefresh)}>
        <span className="fe-ctx-icon"><i className="ri-refresh-line" aria-hidden="true" /></span> 刷新
      </button>

      <div className="fe-ctx-divider" />

      <div
        className="fe-ctx-item fe-ctx-submenu-trigger"
        onMouseEnter={() => {
          setDownloadOpen(true);
          setUploadOpen(false);
        }}
        onMouseLeave={() => setDownloadOpen(false)}
      >
        <span className="fe-ctx-icon"><i className="ri-download-2-line" aria-hidden="true" /></span> 下载
        <span className="fe-ctx-arrow">›</span>
        {downloadOpen && (
          <div className="fe-ctx-submenu">
            <button
              className="fe-ctx-item"
              disabled={!hasEntries}
              onClick={() => run(() => onDownload(entries))}
            >
              <span className="fe-ctx-icon"><i className="ri-download-line" aria-hidden="true" /></span> 逐个下载
            </button>
            <button
              className="fe-ctx-item"
              disabled={!hasEntries}
              onClick={() => run(() => onPackedDownload(entries))}
            >
              <span className="fe-ctx-icon"><i className="ri-file-zip-line" aria-hidden="true" /></span> 打包下载
            </button>
          </div>
        )}
      </div>

      <div
        className="fe-ctx-item fe-ctx-submenu-trigger"
        onMouseEnter={() => {
          setUploadOpen(true);
          setDownloadOpen(false);
        }}
        onMouseLeave={() => setUploadOpen(false)}
      >
        <span className="fe-ctx-icon"><i className="ri-upload-2-line" aria-hidden="true" /></span> 上传
        <span className="fe-ctx-arrow">›</span>
        {uploadOpen && (
          <div className="fe-ctx-submenu">
            <button className="fe-ctx-item" onClick={() => run(onUpload)}>
              <span className="fe-ctx-icon"><i className="ri-upload-line" aria-hidden="true" /></span> 逐个上传
            </button>
            <button className="fe-ctx-item" onClick={() => run(onPackedUpload)}>
              <span className="fe-ctx-icon"><i className="ri-inbox-archive-line" aria-hidden="true" /></span> 打包上传
            </button>
          </div>
        )}
      </div>

      <div className="fe-ctx-divider" />

      <button
        className="fe-ctx-item"
        disabled={!hasEntries}
        onClick={() => run(() => onCopyPath(entries))}
      >
        <span className="fe-ctx-icon"><i className="ri-link-m" aria-hidden="true" /></span> 复制路径
      </button>

      <div className="fe-ctx-divider" />

      <button
        className="fe-ctx-item"
        disabled={!hasEntries}
        onClick={() => run(() => onCopy(entries))}
      >
        <span className="fe-ctx-icon"><i className="ri-file-copy-line" aria-hidden="true" /></span> 复制
      </button>
      <button
        className="fe-ctx-item"
        disabled={!hasEntries}
        onClick={() => run(() => onCut(entries))}
      >
        <span className="fe-ctx-icon"><i className="ri-scissors-cut-line" aria-hidden="true" /></span> 剪切
      </button>
      <button className="fe-ctx-item" disabled={!hasPaste} onClick={() => run(onPaste)}>
        <span className="fe-ctx-icon"><i className="ri-clipboard-line" aria-hidden="true" /></span>
        粘贴
        {clipboard ? (
          <span className="fe-ctx-badge">{clipboard.mode === "copy" ? "复制" : "剪切"}</span>
        ) : null}
      </button>

      <div className="fe-ctx-divider" />

      <div
        className="fe-ctx-item fe-ctx-submenu-trigger"
        onMouseEnter={() => setNewOpen(true)}
        onMouseLeave={() => setNewOpen(false)}
      >
        <span className="fe-ctx-icon"><i className="ri-add-line" aria-hidden="true" /></span> 新建
        <span className="fe-ctx-arrow">›</span>
        {newOpen && (
          <div className="fe-ctx-submenu">
            <button className="fe-ctx-item" onClick={() => run(onNewFolder)}>
              <span className="fe-ctx-icon"><i className="ri-folder-3-line" aria-hidden="true" /></span> 文件夹
            </button>
            <button className="fe-ctx-item" onClick={() => run(onNewFile)}>
              <span className="fe-ctx-icon"><i className="ri-file-line" aria-hidden="true" /></span> 文件
            </button>
          </div>
        )}
      </div>

      <div className="fe-ctx-divider" />

      <button
        className="fe-ctx-item"
        disabled={!single}
        onClick={() => single && run(() => onRename(single))}
      >
        <span className="fe-ctx-icon"><i className="ri-edit-line" aria-hidden="true" /></span> 重命名
      </button>

      <button
        className="fe-ctx-item fe-ctx-danger"
        disabled={!hasEntries}
        onClick={() => run(() => onDelete(entries))}
      >
        <span className="fe-ctx-icon"><i className="ri-delete-bin-6-line" aria-hidden="true" /></span> 删除
      </button>

      <button
        className="fe-ctx-item fe-ctx-danger"
        disabled={!hasEntries}
        onClick={() => run(() => onQuickDelete(entries))}
      >
        <span className="fe-ctx-icon"><i className="ri-flashlight-line" aria-hidden="true" /></span> 快速删除 (rm)
      </button>

      <div className="fe-ctx-divider" />

      <button
        className="fe-ctx-item"
        disabled={!single || single.type === "directory"}
        onClick={() => single && run(() => onRemoteEdit(single))}
      >
        <span className="fe-ctx-icon"><i className="ri-edit-box-line" aria-hidden="true" /></span> 远端编辑
      </button>

      {connectionId && <div className="fe-ctx-connection-hint">连接：{connectionId.slice(0, 8)}…</div>}
    </div>
  );
};
