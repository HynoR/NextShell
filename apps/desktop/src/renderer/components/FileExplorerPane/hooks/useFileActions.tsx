import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type Dispatch,
  type MouseEvent as ReactMouseEvent,
  type SetStateAction
} from "react";
import { App as AntdApp, Modal } from "antd";
import type { AppPreferences, ConnectionProfile, RemoteFileEntry } from "@nextshell/core";
import { usePreferencesStore } from "../../../store/usePreferencesStore";
import { pMap } from "../../../utils/concurrentLimit";
import { formatErrorMessage } from "../../../utils/errorMessage";
import { promptModal } from "../../../utils/promptModal";
import { joinRemotePath, normalizeRemotePath, shellEscape } from "../shared";
import type { Clipboard, ContextMenuState } from "../types";

type AppMessage = ReturnType<typeof AntdApp.useApp>["message"];
type AppModal = ReturnType<typeof AntdApp.useApp>["modal"];
type UpdatePreferences = ReturnType<typeof usePreferencesStore.getState>["updatePreferences"];

interface UseFileActionsParams {
  connection?: ConnectionProfile;
  connected: boolean;
  pathName: string;
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
  const [editorModalOpen, setEditorModalOpen] = useState(false);
  const [editorModalValue, setEditorModalValue] = useState(remoteEditPreferences.defaultEditorCommand);
  const pendingEditRef = useRef<RemoteFileEntry | null>(null);

  useEffect(() => {
    setClipboard(null);
    setContextMenu(null);
  }, [connection?.id, connected]);

  useEffect(() => {
    setEditorModalValue(remoteEditPreferences.defaultEditorCommand);
  }, [remoteEditPreferences.defaultEditorCommand]);

  useEffect(() => {
    const unsub = window.nextshell.sftp.onEditStatus((event) => {
      switch (event.status) {
        case "synced":
          message.success({ content: `已同步: ${event.remotePath.split("/").pop()}`, duration: 2 });
          break;
        case "error":
          message.error({ content: event.message ?? "同步失败", duration: 4 });
          break;
        case "closed":
          message.info({ content: `编辑已关闭: ${event.remotePath.split("/").pop()}`, duration: 2 });
          break;
      }
    });
    return unsub;
  }, [message]);

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

  const handleRename = useCallback(async (entry?: RemoteFileEntry): Promise<void> => {
    const target = entry ?? singleSelected;
    if (!connection || !target) return;
    const toPath = await promptModal(modal, "重命名为", undefined, target.path);
    if (!toPath || toPath === target.path) return;
    const normalized = normalizeRemotePath(toPath);
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
  }, [connection, loadFiles, message, modal, setBusy, singleSelected]);

  const handleDelete = useCallback((targets: RemoteFileEntry[] = selectedEntries): void => {
    if (!connection || targets.length === 0) return;
    Modal.confirm({
      title: "删除远端文件",
      content:
        targets.length === 1
          ? `确认删除 ${targets[0]?.path} ?`
          : `确认删除选中的 ${targets.length} 项?`,
      okButtonProps: { danger: true },
      onOk: async () => {
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
          message.error(`删除失败：${formatErrorMessage(error, "请稍后重试")}`);
          setFiles(prevFiles);
        } finally {
          setBusy(false);
        }
      }
    });
  }, [connection, files, loadFiles, message, selectedEntries, setBusy, setFiles]);

  const handleQuickDelete = useCallback((targets: RemoteFileEntry[]): void => {
    if (!connection || targets.length === 0) return;
    Modal.confirm({
      title: "快速删除（rm 命令）",
      content: (
        <div>
          <p>
            将在远端执行 <code>rm -rf</code> 命令，<strong>不可撤销</strong>！
          </p>
          <p>{targets.length === 1 ? targets[0]?.path : `${targets.length} 个文件/目录`}</p>
        </div>
      ),
      okButtonProps: { danger: true },
      okText: "强制删除",
      onOk: async () => {
        const paths = targets.map((entry) => shellEscape(entry.path)).join(" ");
        setBusy(true);
        const { ok } = await execSSH(`rm -rf ${paths}`);
        setBusy(false);
        if (ok) {
          message.success("已删除");
          await loadFiles();
        }
      }
    });
  }, [connection, execSSH, loadFiles, message, setBusy]);

  const handleCopy = useCallback((entries: RemoteFileEntry[]) => {
    if (!connection) return;
    setClipboard({ mode: "copy", entries, sourceConnectionId: connection.id });
    message.success(`已复制 ${entries.length} 项到剪切板`);
  }, [connection, message]);

  const handleCut = useCallback((entries: RemoteFileEntry[]) => {
    if (!connection) return;
    setClipboard({ mode: "cut", entries, sourceConnectionId: connection.id });
    message.success(`已剪切 ${entries.length} 项到剪切板`);
  }, [connection, message]);

  const handlePaste = useCallback(async (): Promise<void> => {
    if (!connection || !clipboard) return;
    if (clipboard.sourceConnectionId !== connection.id) {
      message.warning("仅支持在同一连接内粘贴");
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

  const handleCopyPath = useCallback((entries: RemoteFileEntry[]) => {
    const paths = entries.map((entry) => entry.path).join("\n");
    void navigator.clipboard.writeText(paths);
    message.success("路径已复制到系统剪切板");
  }, [message]);

  const doRemoteEdit = useCallback(async (entry: RemoteFileEntry, editorCmd: string) => {
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
      message.error(`远端编辑失败：${formatErrorMessage(error, "请检查编辑器配置或环境变量")}`);
    } finally {
      setBusy(false);
    }
  }, [connection, message, setBusy]);

  const handleRemoteEdit = useCallback((entry: RemoteFileEntry) => {
    const editorMode = remoteEditPreferences.editorMode ?? "builtin";
    if (editorMode === "builtin" && onOpenEditorTab && connection) {
      void onOpenEditorTab(connection.id, entry.path);
      return;
    }
    const editor = remoteEditPreferences.defaultEditorCommand?.trim() ?? "";
    void doRemoteEdit(entry, editor);
  }, [connection, doRemoteEdit, onOpenEditorTab, remoteEditPreferences.defaultEditorCommand, remoteEditPreferences.editorMode]);

  const handleEditorModalOk = useCallback(() => {
    const cmd = editorModalValue.trim();
    if (!cmd) return;
    void updatePreferences({
      remoteEdit: {
        defaultEditorCommand: cmd
      }
    });
    setEditorModalOpen(false);
    const pending = pendingEditRef.current;
    pendingEditRef.current = null;
    if (pending) {
      void doRemoteEdit(pending, cmd);
    }
  }, [doRemoteEdit, editorModalValue, updatePreferences]);

  const handleEditorModalCancel = useCallback(() => {
    setEditorModalOpen(false);
    pendingEditRef.current = null;
  }, []);

  const handleContextMenu = useCallback((event: ReactMouseEvent, row?: RemoteFileEntry) => {
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
  }, [selectedEntries, selectedPaths]);

  return {
    clearClipboard,
    clipboard,
    closeContextMenu,
    contextMenu,
    editorModalOpen,
    editorModalValue,
    handleContextMenu,
    handleCopy,
    handleCopyPath,
    handleCreateDirectory,
    handleCreateFile,
    handleCut,
    handleDelete,
    handleEditorModalCancel,
    handleEditorModalOk,
    handlePaste,
    handleQuickDelete,
    handleRemoteEdit,
    handleRename,
    setEditorModalValue
  };
};
