import type { MouseEvent } from "react";
import { Tooltip } from "antd";
import type { ConnectionProfile } from "@nextshell/core";
import { ManagerContextMenu } from "./ManagerContextMenu";
import { ManagerTree } from "./ManagerTree";
import type {
  MgrClipboard,
  MgrContextMenuState,
  MgrGroupNode,
  SortMode
} from "../types";

interface ConnectionSidebarProps {
  connections: ConnectionProfile[];
  keyword: string;
  onKeywordChange: (value: string) => void;
  onClearKeyword: () => void;
  onOpenLocalTerminal: () => void;
  onNewConnection: (groupPath?: string) => void;
  tree: MgrGroupNode;
  expanded: Set<string>;
  toggleExpanded: (key: string) => void;
  primarySelectedId?: string;
  selectedIds: Set<string>;
  cutIds: Set<string>;
  renamingId?: string;
  hasVisibleConnections: boolean;
  draggingConnection: ConnectionProfile | null;
  onSelect: (id: string, event: MouseEvent) => void;
  onQuickConnect: (connectionId: string) => void | Promise<void>;
  onConnectionContextMenu: (event: MouseEvent, connectionId: string) => void;
  onGroupContextMenu: (event: MouseEvent, node: MgrGroupNode) => void;
  onGroupCtrlClick: (node: MgrGroupNode) => void;
  onRenameCommit: (connectionId: string, newName: string) => void | Promise<void>;
  onRenameCancel: () => void;
  onEmptyContextMenu: (event: MouseEvent) => void;
  onDragStart: (event: any) => void;
  onDragEnd: (event: any) => void | Promise<void>;
  contextMenu: MgrContextMenuState | null;
  clipboard: MgrClipboard | null;
  sortMode: SortMode;
  onCloseContextMenu: () => void;
  onEditConnection: (connectionId: string) => void;
  onRenameConnection: (connectionId: string) => void;
  onCopyConnections: () => void;
  onCutConnections: () => void;
  onPasteConnections: (targetGroupPath: string) => void | Promise<void>;
  onDeleteConnections: () => void;
  onCopyAddress: (connectionId: string) => void;
  onNewFolder: (parentGroupPath: string) => void | Promise<void>;
  onSortChange: (mode: SortMode) => void;
  onImportNextShell: () => void | Promise<void>;
  onImportFinalShell: () => void | Promise<void>;
  onExportSelected: () => void | Promise<void>;
  onExportAll: () => void | Promise<void>;
  selectedExportCount: number;
  importingPreview: boolean;
  onClearClipboard: () => void;
}

export const ConnectionSidebar = ({
  connections,
  keyword,
  onKeywordChange,
  onClearKeyword,
  onOpenLocalTerminal,
  onNewConnection,
  tree,
  expanded,
  toggleExpanded,
  primarySelectedId,
  selectedIds,
  cutIds,
  renamingId,
  hasVisibleConnections,
  draggingConnection,
  onSelect,
  onQuickConnect,
  onConnectionContextMenu,
  onGroupContextMenu,
  onGroupCtrlClick,
  onRenameCommit,
  onRenameCancel,
  onEmptyContextMenu,
  onDragStart,
  onDragEnd,
  contextMenu,
  clipboard,
  sortMode,
  onCloseContextMenu,
  onEditConnection,
  onRenameConnection,
  onCopyConnections,
  onCutConnections,
  onPasteConnections,
  onDeleteConnections,
  onCopyAddress,
  onNewFolder,
  onSortChange,
  onImportNextShell,
  onImportFinalShell,
  onExportSelected,
  onExportAll,
  selectedExportCount,
  importingPreview,
  onClearClipboard
}: ConnectionSidebarProps) => {
  return (
    <div className="mgr-sidebar">
      <div className="mgr-sidebar-head">
        <div className="mgr-sidebar-title-row">
          <span className="mgr-sidebar-title">全部连接</span>
          {connections.length > 0 ? (
            <span className="mgr-count-badge">{connections.length}</span>
          ) : null}
        </div>
        <div className="mgr-sidebar-title-row">
          <button
            className="mgr-new-btn"
            onClick={onOpenLocalTerminal}
            title="本地终端"
          >
            <i className="ri-terminal-box-line" aria-hidden="true" />
          </button>
          <button
            className="mgr-new-btn"
            onClick={() => onNewConnection()}
            title="新建连接"
          >
            <i className="ri-add-line" aria-hidden="true" />
          </button>
        </div>
      </div>

      <div className="mgr-search-row">
        <i className="ri-search-line mgr-search-icon" aria-hidden="true" />
        <input
          className="mgr-search"
          placeholder="搜索连接..."
          value={keyword}
          onChange={(event) => onKeywordChange(event.target.value)}
        />
        {keyword ? (
          <button
            className="mgr-search-clear"
            onClick={onClearKeyword}
            title="清除"
          >
            <i className="ri-close-line" aria-hidden="true" />
          </button>
        ) : null}
      </div>

      <ManagerTree
        tree={tree}
        expanded={expanded}
        primarySelectedId={primarySelectedId}
        selectedIds={selectedIds}
        cutIds={cutIds}
        renamingId={renamingId}
        keyword={keyword}
        hasVisibleConnections={hasVisibleConnections}
        draggingConnection={draggingConnection}
        toggleExpanded={toggleExpanded}
        onSelect={onSelect}
        onDoubleClick={(id) => void onQuickConnect(id)}
        onQuickConnect={(id) => void onQuickConnect(id)}
        onConnectionContextMenu={onConnectionContextMenu}
        onGroupContextMenu={onGroupContextMenu}
        onGroupCtrlClick={onGroupCtrlClick}
        onRenameCommit={(id, name) => void onRenameCommit(id, name)}
        onRenameCancel={onRenameCancel}
        onEmptyContextMenu={onEmptyContextMenu}
        onDragStart={onDragStart}
        onDragEnd={onDragEnd}
      />

      {contextMenu ? (
        <ManagerContextMenu
          state={contextMenu}
          clipboard={clipboard}
          connections={connections}
          selectedIds={selectedIds}
          sortMode={sortMode}
          onClose={onCloseContextMenu}
          onConnect={(id) => void onQuickConnect(id)}
          onEdit={onEditConnection}
          onRename={onRenameConnection}
          onCopy={onCopyConnections}
          onCut={onCutConnections}
          onPaste={(path) => void onPasteConnections(path)}
          onDelete={onDeleteConnections}
          onCopyAddress={onCopyAddress}
          onNewConnection={onNewConnection}
          onNewFolder={(path) => void onNewFolder(path)}
          onSort={onSortChange}
          onImportNextShell={() => void onImportNextShell()}
          onImportFinalShell={() => void onImportFinalShell()}
          onExportSelected={() => void onExportSelected()}
          onExportAll={() => void onExportAll()}
        />
      ) : null}

      {clipboard ? (
        <div className="mgr-clipboard-bar">
          <span>
            {clipboard.mode === "copy" ? "已复制" : "已剪切"} {clipboard.connectionIds.length} 个连接
          </span>
          <button
            type="button"
            className="mgr-clipboard-clear"
            onClick={onClearClipboard}
            title="清除"
          >
            <i className="ri-close-line" aria-hidden="true" />
          </button>
        </div>
      ) : null}

      <div className="mgr-sidebar-footer">
        <span className="mgr-count">
          {connections.length} 个连接
          {selectedExportCount > 0 ? ` · 已选 ${selectedExportCount}` : ""}
        </span>
        <div className="mgr-sidebar-footer-actions">
          <Tooltip title="导入 NextShell 文件">
            <button type="button" className="mgr-action-btn" onClick={() => void onImportNextShell()} disabled={importingPreview}>
              <i className={importingPreview ? "ri-loader-4-line ri-spin" : "ri-upload-2-line"} />
            </button>
          </Tooltip>
          <Tooltip title="导入 FinalShell 文件">
            <button
              type="button"
              className="mgr-action-btn"
              onClick={() => void onImportFinalShell()}
              disabled={importingPreview}
            >
              <i className="ri-file-upload-line" />
            </button>
          </Tooltip>
          <Tooltip title="导出选中连接">
            <button
              type="button"
              className="mgr-action-btn"
              onClick={() => void onExportSelected()}
              disabled={selectedExportCount === 0}
            >
              <i className="ri-download-cloud-2-line" />
            </button>
          </Tooltip>
          <Tooltip title="导出所有连接">
            <button
              type="button"
              className="mgr-action-btn"
              onClick={() => void onExportAll()}
              disabled={connections.length === 0}
            >
              <i className="ri-download-2-line" />
            </button>
          </Tooltip>
        </div>
      </div>
    </div>
  );
};
