import { Tooltip } from "antd";
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
  onCreateDirectory: () => void;
  onRename: () => void;
  onDelete: () => void;
  onPaste: () => void;
  onClearClipboard: () => void;
}

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
  onCreateDirectory,
  onRename,
  onDelete,
  onPaste,
  onClearClipboard
}: FileExplorerToolbarProps) => (
  <div className="fe-toolbar">
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
    <div className="fe-actions">
      {visibleToolbarActions.has("follow-cwd") && (
        <Tooltip title="跟随终端目录">
          <span className="inline-flex">
            <button
              className={`fe-icon-btn${followCwd ? " active" : ""}`}
              aria-label="跟随终端目录"
              onClick={onToggleFollowCwd}
              disabled={!hasConnection || !connected}
            >
              <i className="ri-terminal-line" aria-hidden="true" />
            </button>
          </span>
        </Tooltip>
      )}
      {visibleToolbarActions.has("refresh") && (
        <Tooltip title="刷新">
          <button className="fe-icon-btn" onClick={onRefresh} disabled={busy} aria-label="刷新">
            <i className="ri-refresh-line" aria-hidden="true" />
          </button>
        </Tooltip>
      )}
      {visibleToolbarActions.has("back") && (
        <Tooltip title="后退">
          <button className="fe-icon-btn" onClick={onBack} disabled={historyIndex <= 0} aria-label="后退">
            <i className="ri-arrow-left-s-line" aria-hidden="true" />
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
            <i className="ri-arrow-right-s-line" aria-hidden="true" />
          </button>
        </Tooltip>
      )}
      {visibleToolbarActions.has("parent") && (
        <Tooltip title="上级目录">
          <button className="fe-icon-btn" onClick={onParent} disabled={pathName === "/" || busy} aria-label="上级目录">
            <i className="ri-arrow-up-s-line" aria-hidden="true" />
          </button>
        </Tooltip>
      )}
      <span className="w-px h-4 bg-[var(--border)] mx-[3px] shrink-0" />
      {visibleToolbarActions.has("mkdir") && (
        <Tooltip title="新建目录">
          <button className="fe-icon-btn" onClick={onCreateDirectory} disabled={busy} aria-label="新建目录">
            <i className="ri-folder-add-line" aria-hidden="true" />
          </button>
        </Tooltip>
      )}
      {visibleToolbarActions.has("rename") && (
        <Tooltip title="重命名">
          <span className="inline-flex">
            <button className="fe-icon-btn" onClick={onRename} disabled={!hasSingleSelection || busy} aria-label="重命名">
              <i className="ri-edit-line" aria-hidden="true" />
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
              <i className="ri-delete-bin-6-line" aria-hidden="true" />
            </button>
          </span>
        </Tooltip>
      )}
      {clipboard && (
        <>
          <span className="w-px h-4 bg-[var(--border)] mx-[3px] shrink-0" />
          <Tooltip title={`粘贴（${clipboard.mode === "copy" ? "复制" : "移动"} ${clipboard.entries.length} 项）`}>
            <span className="inline-flex">
              <button className="fe-icon-btn" aria-label="粘贴" onClick={onPaste} disabled={busy}>
                <i className="ri-clipboard-line" aria-hidden="true" />
              </button>
            </span>
          </Tooltip>
          <Tooltip title="清空剪切板">
            <button className="fe-icon-btn" onClick={onClearClipboard} aria-label="清空剪切板">
              <i className="ri-close-line" aria-hidden="true" />
            </button>
          </Tooltip>
        </>
      )}
    </div>
  </div>
);
