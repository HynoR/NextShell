import {
  useCallback,
  useEffect,
  useState,
  type Dispatch,
  type MouseEvent as ReactMouseEvent,
  type SetStateAction
} from "react";
import { App as AntdApp } from "antd";
import type { AppPreferences, ConnectionProfile, RemoteFileEntry } from "@nextshell/core";
import { usePreferencesStore } from "../../../store/usePreferencesStore";
import { pMap } from "../../../utils/concurrentLimit";
import { formatErrorMessage } from "../../../utils/errorMessage";
import { promptModal } from "../../../utils/promptModal";
import { isPermissionDenied, joinRemotePath, normalizeRemotePath, shellEscape } from "../shared";
import type { Clipboard, ContextMenuState } from "../types";

type AppMessage = ReturnType<typeof AntdApp.useApp>["message"];
type AppModal = ReturnType<typeof AntdApp.useApp>["modal"];
type UpdatePreferences = ReturnType<typeof usePreferencesStore.getState>["updatePreferences"];

interface UseFileActionsParams {
  connection?: ConnectionProfile;
  connected: boolean;
  pathName: string;
  /** 该连接当前可写入的远程终端会话，用于「在终端中打开」。 */
  terminalSessionId?: string;
  files: RemoteFileEntry[];
  setFiles: Dispatch<SetStateAction<RemoteFileEntry[]>>;
  selectedPaths: string[];
  selectedEntries: RemoteFileEntry[];
  singleSelected?: RemoteFileEntry;
  loadFiles: () => Promise<void>;
  setBusy: Dispatch<SetStateAction<boolean>>;
  execSSH: (command: string) => Promise<{ ok: boolean; stderr: string }>;
  remoteEditPreferences: AppPreferences["remoteEdit"];
  updatePreferences: UpdatePreferences;
  onOpenEditorTab?: (connectionId: string, remotePath: string) => Promise<void>;
  message: AppMessage;
  modal: AppModal;
}

export const useFileActions = ({
  connection,
  connected,
  pathName,
  terminalSessionId,
  files,
  setFiles,
  selectedPaths,
  selectedEntries,
  singleSelected,
  loadFiles,
  setBusy,
  execSSH,
  remoteEditPreferences,
  updatePreferences,
  onOpenEditorTab,
  message,
  modal
}: UseFileActionsParams) => {
  const [clipboard, setClipboard] = useState<Clipboard | null>(null);
  const [contextMenu, setContextMenu] = useState<ContextMenuState | null>(null);
  const [deleteTargets, setDeleteTargets] = useState<RemoteFileEntry[] | null>(null);

  useEffect(() => {
    setClipboard(null);
    setContextMenu(null);
    setDeleteTargets(null);
  }, [connection?.id, connected]);

  const clearClipboard = useCallback(() => {
    setClipboard(null);
  }, []);

  const closeContextMenu = useCallback(() => {
    setContextMenu(null);
  }, []);

  const handleCreateDirectory = useCallback(async (): Promise<void> => {
    if (!connection) return;
    const folderName = await promptModal(modal, "新建目录名称");
    if (!folderName) return;
    const targetPath = joinRemotePath(pathName, folderName);
    try {
      setBusy(true);
      await window.nextshell.sftp.mkdir({ connectionId: connection.id, path: targetPath });
      message.success("目录已创建");
      await loadFiles();
    } catch (error) {
      message.error(`创建目录失败：${formatErrorMessage(error, "请检查目录名称")}`);
    } finally {
      setBusy(false);
    }
  }, [connection, loadFiles, message, modal, pathName, setBusy]);

  const handleCreateFile = useCallback(async (): Promise<void> => {
    if (!connection) return;
    const fileName = await promptModal(modal, "新建文件名称");
    if (!fileName) return;
    const targetPath = joinRemotePath(pathName, fileName);
    setBusy(true);
    const { ok } = await execSSH(`touch ${shellEscape(targetPath)}`);
    setBusy(false);
    if (ok) {
      message.success("文件已创建");
      await loadFiles();
    }
  }, [connection, execSSH, loadFiles, message, modal, pathName, setBusy]);

  const handleRename = useCallback(
    async (entry?: RemoteFileEntry): Promise<void> => {
      const target = entry ?? singleSelected;
      if (!connection || !target) return;
      // 预填文件名并选中不含扩展名的部分；跨目录移动走「剪切/粘贴」。
      const newName = await promptModal(modal, "重命名为", undefined, target.name, true);
      if (!newName || newName === target.name) return;
      const parentDir = target.path.slice(0, target.path.lastIndexOf("/")) || "/";
      const normalized = normalizeRemotePath(joinRemotePath(parentDir, newName));
      if (normalized === target.path) return;
      setBusy(true);
      try {
        await window.nextshell.sftp.rename({
          connectionId: connection.id,
          fromPath: target.path,
          toPath: normalized
        });
        message.success("重命名成功");
        await loadFiles();
      } catch (error) {
        message.error(`重命名失败：${formatErrorMessage(error, "请稍后重试")}`);
      } finally {
        setBusy(false);
      }
    },
    [connection, loadFiles, message, modal, setBusy, singleSelected]
  );

  // 强制删除：远端执行 rm -rf（权限/只读场景的兜底，不可恢复）。
  const runForceDelete = useCallback(
    async (targets: RemoteFileEntry[]): Promise<void> => {
      if (!connection) return;
      const paths = targets.map((entry) => shellEscape(entry.path)).join(" ");
      setBusy(true);
      const { ok } = await execSSH(`rm -rf ${paths}`);
      setBusy(false);
      if (ok) {
        message.success("已删除");
        await loadFiles();
      }
    },
    [connection, execSSH, loadFiles, message, setBusy]
  );

  // 安全删除：走 SFTP remove，带乐观更新（失败回滚）。
  const runSafeDelete = useCallback(
    async (targets: RemoteFileEntry[]): Promise<void> => {
      if (!connection) return;
      const prevFiles = [...files];
      const targetPaths = new Set(targets.map((target) => target.path));
      setFiles((prev) => prev.filter((file) => !targetPaths.has(file.path)));

      try {
        setBusy(true);
        await pMap(
          targets,
          async (entry) => {
            await window.nextshell.sftp.remove({
              connectionId: connection.id,
              path: entry.path,
              type: entry.type
            });
          },
          5
        );
        message.success("删除成功");
        await loadFiles();
      } catch (error) {
        setFiles(prevFiles);
        const reason = formatErrorMessage(error, "请稍后重试");
        if (isPermissionDenied(reason)) {
          // 仅权限/只读场景才需要 rm -rf：普通 sftp.remove 对目录已递归。
          modal.confirm({
            title: "删除被拒绝（权限不足）",
            content: "普通删除因权限不足失败。要在远端执行 rm -rf 强制重试吗？该操作不可恢复。",
            okText: "rm -rf 重试",
            okButtonProps: { danger: true },
            cancelText: "取消",
            onOk: () => runForceDelete(targets)
          });
        } else {
          message.error(`删除失败：${reason}`);
        }
      } finally {
        setBusy(false);
      }
    },
    [connection, files, loadFiles, message, modal, runForceDelete, setBusy, setFiles]
  );

  const requestDelete = useCallback(
    (targets: RemoteFileEntry[] = selectedEntries): void => {
      if (!connection || targets.length === 0) return;
      setDeleteTargets(targets);
    },
    [connection, selectedEntries]
  );

  const cancelDelete = useCallback(() => {
    setDeleteTargets(null);
  }, []);

  const confirmDelete = useCallback(async (): Promise<void> => {
    const targets = deleteTargets;
    if (!targets || targets.length === 0) {
      setDeleteTargets(null);
      return;
    }
    // 保持对话框开启（显示确认 loading）直到操作完成后再关闭。
    await runSafeDelete(targets);
    setDeleteTargets(null);
  }, [deleteTargets, runSafeDelete]);

  const handleCopy = useCallback(
    (entries: RemoteFileEntry[]) => {
      if (!connection) return;
      setClipboard({ mode: "copy", entries, sourceConnectionId: connection.id });
      message.success(`已复制 ${entries.length} 项到剪贴板`);
    },
    [connection, message]
  );

  const handleCut = useCallback(
    (entries: RemoteFileEntry[]) => {
      if (!connection) return;
      setClipboard({ mode: "cut", entries, sourceConnectionId: connection.id });
      message.success(`已剪切 ${entries.length} 项到剪贴板`);
    },
    [connection, message]
  );

  const handlePaste = useCallback(async (): Promise<void> => {
    if (!connection || !clipboard) return;
    if (clipboard.sourceConnectionId !== connection.id) {
      message.warning("剪贴板内容来自其他连接，跨连接传输请使用右键「发送到服务器…」");
      return;
    }

    const destDir = normalizeRemotePath(pathName);
    setBusy(true);

    const ops = clipboard.entries.map((entry) => {
      const destPath = joinRemotePath(destDir, entry.name);
      if (clipboard.mode === "copy") {
        const flag = entry.type === "directory" ? "-r" : "";
        return execSSH(`cp ${flag} ${shellEscape(entry.path)} ${shellEscape(destPath)}`);
      }
      return execSSH(`mv ${shellEscape(entry.path)} ${shellEscape(destPath)}`);
    });

    const results = await Promise.all(ops);
    setBusy(false);

    const failed = results.filter((result) => !result.ok).length;
    if (failed === 0) {
      message.success(
        `${clipboard.mode === "copy" ? "复制" : "移动"}完成（${clipboard.entries.length} 项）`
      );
      if (clipboard.mode === "cut") setClipboard(null);
    } else {
      message.warning(`操作完成，${failed} 项失败`);
    }

    await loadFiles();
  }, [clipboard, connection, execSSH, loadFiles, message, pathName, setBusy]);

  const handleCopyPath = useCallback(
    (entries: RemoteFileEntry[]) => {
      const paths = entries.map((entry) => entry.path).join("\n");
      void navigator.clipboard.writeText(paths);
      message.success("路径已复制到系统剪贴板");
    },
    [message]
  );

  // 「在终端中打开」：向该连接的活动终端发送 cd，与「跟随终端」互为反向。
  const handleOpenInTerminal = useCallback(() => {
    if (!terminalSessionId) {
      message.info({ content: "当前连接暂无可用的远程终端。", duration: 2 });
      return;
    }
    window.nextshell.session
      .write({
        sessionId: terminalSessionId,
        data: `cd ${shellEscape(normalizeRemotePath(pathName))}\n`
      })
      .catch(() => message.error("发送命令失败"));
  }, [message, pathName, terminalSessionId]);

  const doRemoteEdit = useCallback(
    async (entry: RemoteFileEntry, editorCmd: string) => {
      if (!connection) return;
      setBusy(true);
      try {
        await window.nextshell.sftp.editOpen({
          connectionId: connection.id,
          remotePath: entry.path,
          editorCommand: editorCmd
        });
        message.success(`已打开远端编辑: ${entry.name}`);
      } catch (error) {
        message.error(`远端编辑失败：${formatErrorMessage(error, "请检查编辑器配置")}`);
      } finally {
        setBusy(false);
      }
    },
    [connection, message, setBusy]
  );

  const handleRemoteEdit = useCallback(
    (entry: RemoteFileEntry) => {
      const editorMode = remoteEditPreferences.editorMode ?? "builtin";
      if (editorMode === "builtin" && onOpenEditorTab && connection) {
        void onOpenEditorTab(connection.id, entry.path);
        return;
      }
      const editor = remoteEditPreferences.defaultEditorCommand?.trim() ?? "";
      void doRemoteEdit(entry, editor);
    },
    [
      connection,
      doRemoteEdit,
      onOpenEditorTab,
      remoteEditPreferences.defaultEditorCommand,
      remoteEditPreferences.editorMode
    ]
  );

  const handleContextMenu = useCallback(
    (event: ReactMouseEvent, row?: RemoteFileEntry) => {
      event.preventDefault();
      event.stopPropagation();

      let targetEntries: RemoteFileEntry[];
      if (row) {
        targetEntries =
          selectedPaths.includes(row.path) && selectedEntries.length > 0 ? selectedEntries : [row];
      } else {
        targetEntries = [];
      }

      setContextMenu({ x: event.clientX, y: event.clientY, entries: targetEntries });
    },
    [selectedEntries, selectedPaths]
  );

  return {
    clearClipboard,
    clipboard,
    closeContextMenu,
    contextMenu,
    deleteTargets,
    requestDelete,
    cancelDelete,
    confirmDelete,
    handleContextMenu,
    handleCopy,
    handleCopyPath,
    handleCreateDirectory,
    handleCreateFile,
    handleCut,
    handleOpenInTerminal,
    handlePaste,
    handleRemoteEdit,
    handleRename
  };
};
