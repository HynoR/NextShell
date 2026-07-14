import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { Dropdown, type MenuProps } from "antd";
import type { RemoteFileEntry } from "@nextshell/core";
import type { Clipboard, ContextMenuState } from "../types";

interface FileExplorerContextMenuProps {
  state: ContextMenuState;
  clipboard: Clipboard | null;
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
  onRemoteEdit: (entry: RemoteFileEntry) => void;
}

const icon = (className: string) => <i className={className} aria-hidden="true" />;

export const FileExplorerContextMenu = ({
  state,
  clipboard,
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
  onRemoteEdit
}: FileExplorerContextMenuProps) => {
  const { x, y, entries } = state;
  const hasEntries = entries.length > 0;
  const single = entries.length === 1 ? entries[0] : undefined;
  const editableFile = single && single.type !== "directory" ? single : undefined;
  const hasPaste = Boolean(clipboard);
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
      const target = event.target as HTMLElement;
      // 忽略 antd Dropdown 弹层内的点击，避免子菜单未执行就被外层菜单关闭。
      if (
        menuRef.current &&
        !menuRef.current.contains(target) &&
        !target.closest?.(".ant-dropdown")
      ) {
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

  const downloadMenu: MenuProps = {
    items: [
      { key: "download", icon: icon("ri-download-line"), label: "逐个下载", onClick: () => run(() => onDownload(entries)) },
      { key: "packed-download", icon: icon("ri-file-zip-line"), label: "打包下载", onClick: () => run(() => onPackedDownload(entries)) }
    ]
  };

  const uploadMenu: MenuProps = {
    items: [
      { key: "upload", icon: icon("ri-upload-line"), label: "上传文件", onClick: () => run(onUpload) },
      { key: "packed-upload", icon: icon("ri-inbox-archive-line"), label: "上传并解压", onClick: () => run(onPackedUpload) }
    ]
  };

  const newMenu: MenuProps = {
    items: [
      { key: "new-folder", icon: icon("ri-folder-3-line"), label: "文件夹", onClick: () => run(onNewFolder) },
      { key: "new-file", icon: icon("ri-file-line"), label: "文件", onClick: () => run(onNewFile) }
    ]
  };

  return (
    <div
      ref={menuRef}
      className="fe-ctx-menu"
      style={{ left: pos.left, top: pos.top, visibility: visible ? "visible" : "hidden" }}
      onContextMenu={(event) => event.preventDefault()}
    >
      {hasEntries ? (
        <>
          {editableFile ? (
            <button className="fe-ctx-item" onClick={() => run(() => onRemoteEdit(editableFile))}>
              <span className="fe-ctx-icon">{icon("ri-edit-box-line")}</span> 编辑
            </button>
          ) : null}

          <Dropdown menu={downloadMenu} trigger={["click"]} placement="bottomLeft">
            <div className="fe-ctx-item fe-ctx-submenu-trigger">
              <span className="fe-ctx-icon">{icon("ri-download-2-line")}</span> 下载
              <span className="fe-ctx-arrow">›</span>
            </div>
          </Dropdown>

          <div className="fe-ctx-divider" />

          <button className="fe-ctx-item" onClick={() => run(() => onCopy(entries))}>
            <span className="fe-ctx-icon">{icon("ri-file-copy-line")}</span> 复制
          </button>
          <button className="fe-ctx-item" onClick={() => run(() => onCut(entries))}>
            <span className="fe-ctx-icon">{icon("ri-scissors-cut-line")}</span> 剪切
          </button>
          {hasPaste ? (
            <button className="fe-ctx-item" onClick={() => run(onPaste)}>
              <span className="fe-ctx-icon">{icon("ri-clipboard-line")}</span>
              粘贴
              {clipboard ? (
                <span className="fe-ctx-badge">{clipboard.mode === "copy" ? "复制" : "剪切"}</span>
              ) : null}
            </button>
          ) : null}

          <button
            className="fe-ctx-item"
            disabled={!single}
            onClick={() => single && run(() => onRename(single))}
          >
            <span className="fe-ctx-icon">{icon("ri-edit-line")}</span> 重命名
          </button>

          <button className="fe-ctx-item" onClick={() => run(() => onCopyPath(entries))}>
            <span className="fe-ctx-icon">{icon("ri-link-m")}</span> 复制路径
          </button>

          <div className="fe-ctx-divider" />

          <button className="fe-ctx-item fe-ctx-danger" onClick={() => run(() => onDelete(entries))}>
            <span className="fe-ctx-icon">{icon("ri-delete-bin-6-line")}</span> 删除
          </button>
        </>
      ) : (
        <>
          <button className="fe-ctx-item" onClick={() => run(onRefresh)}>
            <span className="fe-ctx-icon">{icon("ri-refresh-line")}</span> 刷新
          </button>

          <Dropdown menu={uploadMenu} trigger={["click"]} placement="bottomLeft">
            <div className="fe-ctx-item fe-ctx-submenu-trigger">
              <span className="fe-ctx-icon">{icon("ri-upload-2-line")}</span> 上传
              <span className="fe-ctx-arrow">›</span>
            </div>
          </Dropdown>

          {hasPaste ? (
            <button className="fe-ctx-item" onClick={() => run(onPaste)}>
              <span className="fe-ctx-icon">{icon("ri-clipboard-line")}</span>
              粘贴
              {clipboard ? (
                <span className="fe-ctx-badge">{clipboard.mode === "copy" ? "复制" : "剪切"}</span>
              ) : null}
            </button>
          ) : null}

          <div className="fe-ctx-divider" />

          <Dropdown menu={newMenu} trigger={["click"]} placement="bottomLeft">
            <div className="fe-ctx-item fe-ctx-submenu-trigger">
              <span className="fe-ctx-icon">{icon("ri-add-line")}</span> 新建
              <span className="fe-ctx-arrow">›</span>
            </div>
          </Dropdown>
        </>
      )}
    </div>
  );
};
