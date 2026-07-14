import { Dropdown, Tooltip, type MenuProps } from "antd";
import type { Clipboard } from "../types";

interface FileExplorerToolbarProps {
  pathInput: string;
  pathName: string;
  busy: boolean;
  connected: boolean;
  hasConnection: boolean;
  historyIndex: number;
  historyLength: number;
  clipboard: Clipboard | null;
  followCwd: boolean;
  visibleToolbarActions: Set<string>;
  selectedEntryCount: number;
  hasSingleSelection: boolean;
  onPathInputChange: (value: string) => void;
  onPathInputSubmit: () => void;
  onToggleFollowCwd: () => void;
  onRefresh: () => void;
  onBack: () => void;
  onForward: () => void;
  onParent: () => void;
  onUpload: () => void;
  onPackedUpload: () => void;
  onDownload: () => void;
  onPackedDownload: () => void;
  onNewFolder: () => void;
  onNewFile: () => void;
  onRename: () => void;
  onDelete: () => void;
  onPaste: () => void;
}

const icon = (className: string) => <i className={className} aria-hidden="true" />;

export const FileExplorerToolbar = ({
  pathInput,
  pathName,
  busy,
  connected,
  hasConnection,
  historyIndex,
  historyLength,
  clipboard,
  followCwd,
  visibleToolbarActions,
  selectedEntryCount,
  hasSingleSelection,
  onPathInputChange,
  onPathInputSubmit,
  onToggleFollowCwd,
  onRefresh,
  onBack,
  onForward,
  onParent,
  onUpload,
  onPackedUpload,
  onDownload,
  onPackedDownload,
  onNewFolder,
  onNewFile,
  onRename,
  onDelete,
  onPaste
}: FileExplorerToolbarProps) => {
  const uploadMenu: MenuProps = {
    items: [
      { key: "upload", icon: icon("ri-upload-line"), label: "上传文件", onClick: onUpload },
      {
        key: "packed-upload",
        icon: icon("ri-inbox-archive-line"),
        label: "上传并解压",
        onClick: onPackedUpload
      }
    ]
  };

  const downloadMenu: MenuProps = {
    items: [
      { key: "download", icon: icon("ri-download-line"), label: "逐个下载", onClick: onDownload },
      {
        key: "packed-download",
        icon: icon("ri-file-zip-line"),
        label: "打包下载",
        onClick: onPackedDownload
      }
    ]
  };

  const newMenu: MenuProps = {
    items: [
      { key: "new-folder", icon: icon("ri-folder-3-line"), label: "文件夹", onClick: onNewFolder },
      { key: "new-file", icon: icon("ri-file-line"), label: "文件", onClick: onNewFile }
    ]
  };

  return (
    <div className="fe-toolbar">
      <div className="fe-actions">
        {visibleToolbarActions.has("back") && (
          <Tooltip title="后退">
            <button
              className="fe-icon-btn"
              onClick={onBack}
              disabled={historyIndex <= 0}
              aria-label="后退"
            >
              {icon("ri-arrow-left-s-line")}
            </button>
          </Tooltip>
        )}
        {visibleToolbarActions.has("forward") && (
          <Tooltip title="前进">
            <button
              className="fe-icon-btn"
              onClick={onForward}
              disabled={historyIndex >= historyLength - 1}
              aria-label="前进"
            >
              {icon("ri-arrow-right-s-line")}
            </button>
          </Tooltip>
        )}
        {visibleToolbarActions.has("parent") && (
          <Tooltip title="上级目录">
            <button
              className="fe-icon-btn"
              onClick={onParent}
              disabled={pathName === "/" || busy}
              aria-label="上级目录"
            >
              {icon("ri-arrow-up-s-line")}
            </button>
          </Tooltip>
        )}
        {visibleToolbarActions.has("refresh") && (
          <Tooltip title="刷新">
            <button className="fe-icon-btn" onClick={onRefresh} disabled={busy} aria-label="刷新">
              {icon("ri-refresh-line")}
            </button>
          </Tooltip>
        )}
      </div>

      <span className="fe-tb-sep" />

      <div className="fe-path-area">
        <input
          className="fe-path-input"
          value={pathInput}
          onChange={(event) => onPathInputChange(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") onPathInputSubmit();
          }}
          placeholder="输入路径后回车跳转"
          title={pathName}
        />
      </div>

      {visibleToolbarActions.has("follow-cwd") && (
        <Tooltip title="跟随终端目录">
          <span className="inline-flex">
            <button
              className={`fe-icon-btn${followCwd ? " active" : ""}`}
              aria-label="跟随终端目录"
              onClick={onToggleFollowCwd}
              disabled={!hasConnection || !connected}
            >
              {icon("ri-terminal-line")}
            </button>
          </span>
        </Tooltip>
      )}

      <span className="fe-tb-sep" />

      <div className="fe-actions">
        {visibleToolbarActions.has("upload") && (
          <Dropdown menu={uploadMenu} trigger={["click"]} disabled={busy}>
            <button className="fe-icon-btn fe-icon-btn--menu" aria-label="上传" disabled={busy}>
              {icon("ri-upload-2-line")}
              {icon("ri-arrow-down-s-line")}
            </button>
          </Dropdown>
        )}
        {visibleToolbarActions.has("download") && (
          <Dropdown
            menu={downloadMenu}
            trigger={["click"]}
            disabled={selectedEntryCount === 0 || busy}
          >
            <span className="inline-flex">
              <button
                className="fe-icon-btn fe-icon-btn--menu"
                aria-label="下载选中项"
                disabled={selectedEntryCount === 0 || busy}
              >
                {icon("ri-download-2-line")}
                {icon("ri-arrow-down-s-line")}
              </button>
            </span>
          </Dropdown>
        )}
      </div>

      <span className="fe-tb-sep" />

      <div className="fe-actions">
        {visibleToolbarActions.has("new") && (
          <Dropdown menu={newMenu} trigger={["click"]} disabled={busy}>
            <button className="fe-icon-btn fe-icon-btn--menu" aria-label="新建" disabled={busy}>
              {icon("ri-add-line")}
              {icon("ri-arrow-down-s-line")}
            </button>
          </Dropdown>
        )}
        {visibleToolbarActions.has("rename") && (
          <Tooltip title="重命名">
            <span className="inline-flex">
              <button
                className="fe-icon-btn"
                onClick={onRename}
                disabled={!hasSingleSelection || busy}
                aria-label="重命名"
              >
                {icon("ri-edit-line")}
              </button>
            </span>
          </Tooltip>
        )}
        {visibleToolbarActions.has("delete") && (
          <Tooltip title="删除">
            <span className="inline-flex">
              <button
                className="fe-icon-btn danger"
                onClick={onDelete}
                disabled={selectedEntryCount === 0 || busy}
                aria-label="删除"
              >
                {icon("ri-delete-bin-6-line")}
              </button>
            </span>
          </Tooltip>
        )}
        {visibleToolbarActions.has("paste") && clipboard && (
          <Tooltip
            title={`粘贴（${clipboard.mode === "copy" ? "复制" : "移动"} ${clipboard.entries.length} 项）`}
          >
            <span className="inline-flex">
              <button className="fe-icon-btn" aria-label="粘贴" onClick={onPaste} disabled={busy}>
                {icon("ri-clipboard-line")}
              </button>
            </span>
          </Tooltip>
        )}
      </div>
    </div>
  );
};
