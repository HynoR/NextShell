import { useMemo } from "react";
import { App as AntdApp, Tree } from "antd";
import { usePreferencesStore } from "../../store/usePreferencesStore";
import { useTransferQueueStore } from "../../store/useTransferQueueStore";
import { useWorkspaceStore } from "../../store/useWorkspaceStore";
import { ConnectionPrompt } from "../ConnectionPrompt";
import { getVisibleFileExplorerToolbarActions } from "../FileExplorerPane.toolbar";
import { EDITOR_PRESETS, normalizeRemotePath } from "./shared";
import type { DirTreeNode, FileExplorerPaneProps } from "./types";
import { FileExplorerContextMenu } from "./components/FileExplorerContextMenu";
import { FileExplorerDropOverlay } from "./components/FileExplorerDropOverlay";
import { FileExplorerEditorModal } from "./components/FileExplorerEditorModal";
import { FileExplorerTable } from "./components/FileExplorerTable";
import { FileExplorerToolbar } from "./components/FileExplorerToolbar";
import { useFileActions } from "./hooks/useFileActions";
import { useRemoteCommand } from "./hooks/useRemoteCommand";
import { useRemoteExplorerState } from "./hooks/useRemoteExplorerState";
import { useTransferHandlers } from "./hooks/useTransferHandlers";

export const FileExplorerPane = ({
  connection,
  connected,
  followSessionId,
  active,
  onOpenSettings,
  onOpenEditorTab
}: FileExplorerPaneProps) => {
  void onOpenSettings;

  const { message, modal } = AntdApp.useApp();
  const visibleToolbarActions = useMemo(
    () => new Set(getVisibleFileExplorerToolbarActions()),
    []
  );
  const preferences = usePreferencesStore((state) => state.preferences);
  const updatePreferences = usePreferencesStore((state) => state.updatePreferences);
  const enqueueTask = useTransferQueueStore((state) => state.enqueueTask);
  const markFailed = useTransferQueueStore((state) => state.markFailed);
  const markSuccess = useTransferQueueStore((state) => state.markSuccess);
  const followSessionCwd = useWorkspaceStore((state) =>
    followSessionId ? state.sessionCwdById[followSessionId] : undefined
  );

  const explorer = useRemoteExplorerState({
    connection,
    connected,
    active,
    followSessionId,
    followSessionCwd,
    message
  });
  const execSSH = useRemoteCommand({ connection, message });
  const transfers = useTransferHandlers({
    connection,
    connected,
    active,
    busy: explorer.busy,
    setBusy: explorer.setBusy,
    pathName: explorer.pathName,
    loadFiles: explorer.loadFiles,
    transferPreferences: preferences.transfer,
    updatePreferences,
    enqueueTask,
    markFailed,
    markSuccess,
    message,
    modal
  });
  const actions = useFileActions({
    connection,
    connected,
    pathName: explorer.pathName,
    files: explorer.files,
    setFiles: explorer.setFiles,
    selectedPaths: explorer.selectedPaths,
    selectedEntries: explorer.selectedEntries,
    singleSelected: explorer.singleSelected,
    loadFiles: explorer.loadFiles,
    setBusy: explorer.setBusy,
    execSSH,
    remoteEditPreferences: preferences.remoteEdit,
    updatePreferences,
    onOpenEditorTab,
    message,
    modal
  });

  if (!connection) {
    return <ConnectionPrompt message="先选择一个连接再浏览文件。" icon="ri-folder-open-line" />;
  }

  if (!connected) {
    return (
      <ConnectionPrompt
        message="当前连接未建立会话，请双击左侧服务器建立 SSH 连接。"
        icon="ri-links-line"
      />
    );
  }

  return (
    <div
      className={`fe-shell flex h-full overflow-hidden${transfers.dropTargetActive ? " fe-shell--drop-target" : ""}`}
      onContextMenu={(event) => actions.handleContextMenu(event)}
      onDragEnter={transfers.handleDragEnter}
      onDragOver={transfers.handleDragOver}
      onDragLeave={transfers.handleDragLeave}
      onDrop={(event) => {
        void transfers.handleDropUpload(event);
      }}
    >
      <aside className="fe-tree-panel">
        <Tree<DirTreeNode>
          treeData={explorer.treeData}
          expandedKeys={explorer.expandedKeys}
          selectedKeys={[explorer.pathName]}
          onExpand={(keys, info) =>
            void explorer.handleTreeExpand(
              keys.map(String),
              info as unknown as { node: DirTreeNode; expanded: boolean }
            )
          }
          onSelect={(keys) => {
            const selected = keys[0];
            if (selected) explorer.navigate(String(selected));
          }}
          blockNode
          showLine={false}
        />
      </aside>

      <section className="flex-1 min-w-0 flex flex-col overflow-hidden">
        <FileExplorerToolbar
          pathInput={explorer.pathInput}
          pathName={explorer.pathName}
          busy={explorer.busy}
          connected={connected}
          hasConnection={Boolean(connection)}
          historyIndex={explorer.historyIndex}
          historyLength={explorer.pathHistory.length}
          clipboard={actions.clipboard}
          followCwd={explorer.followCwd}
          visibleToolbarActions={visibleToolbarActions}
          selectedEntryCount={explorer.selectedEntries.length}
          hasSingleSelection={Boolean(explorer.singleSelected)}
          onPathInputChange={explorer.setPathInput}
          onPathInputSubmit={() => explorer.navigate(normalizeRemotePath(explorer.pathInput))}
          onToggleFollowCwd={explorer.toggleFollowCwd}
          onRefresh={() => void explorer.loadFiles()}
          onBack={explorer.goBack}
          onForward={explorer.goForward}
          onParent={explorer.toParentPath}
          onCreateDirectory={() => void actions.handleCreateDirectory()}
          onRename={() => void actions.handleRename()}
          onDelete={() => actions.handleDelete()}
          onPaste={() => void actions.handlePaste()}
          onClearClipboard={actions.clearClipboard}
        />

        {actions.clipboard && (
          <div className="fe-clipboard-bar">
            <span className="fe-clipboard-icon">
              <i
                className={
                  actions.clipboard.mode === "copy" ? "ri-file-copy-line" : "ri-scissors-cut-line"
                }
                aria-hidden="true"
              />
            </span>
            <span>
              已{actions.clipboard.mode === "copy" ? "复制" : "剪切"} {actions.clipboard.entries.length} 项——在目标目录右键粘贴或点击工具栏粘贴
            </span>
            <button className="fe-clipboard-clear" onClick={actions.clearClipboard}>
              清空
            </button>
          </div>
        )}

        <FileExplorerTable
          files={explorer.files}
          busy={explorer.busy}
          selectedPaths={explorer.selectedPaths}
          onSelectionChange={explorer.setSelectedPaths}
          onNavigate={explorer.navigate}
          onRemoteEdit={actions.handleRemoteEdit}
          onContextMenu={actions.handleContextMenu}
        />
      </section>

      {actions.contextMenu && (
        <FileExplorerContextMenu
          state={actions.contextMenu}
          clipboard={actions.clipboard}
          connectionId={connection.id}
          onClose={actions.closeContextMenu}
          onRefresh={() => void explorer.loadFiles()}
          onDownload={(entries) => void transfers.handleDownload(entries)}
          onPackedDownload={(entries) => void transfers.handlePackedDownload(entries)}
          onUpload={() => void transfers.handleUpload()}
          onPackedUpload={() => void transfers.handlePackedUpload()}
          onCopyPath={actions.handleCopyPath}
          onCopy={actions.handleCopy}
          onCut={actions.handleCut}
          onPaste={() => void actions.handlePaste()}
          onNewFolder={() => void actions.handleCreateDirectory()}
          onNewFile={() => void actions.handleCreateFile()}
          onRename={(entry) => void actions.handleRename(entry)}
          onDelete={actions.handleDelete}
          onQuickDelete={actions.handleQuickDelete}
          onRemoteEdit={actions.handleRemoteEdit}
        />
      )}

      {transfers.dropTargetActive && <FileExplorerDropOverlay pathName={explorer.pathName} />}

      <FileExplorerEditorModal
        open={actions.editorModalOpen}
        value={actions.editorModalValue}
        presets={EDITOR_PRESETS}
        onChange={actions.setEditorModalValue}
        onOk={actions.handleEditorModalOk}
        onCancel={actions.handleEditorModalCancel}
      />
    </div>
  );
};
