import { useCallback, useEffect, useRef, useState, type Dispatch, type DragEvent, type SetStateAction } from "react";
import { App as AntdApp } from "antd";
import type { AppPreferences, ConnectionProfile } from "@nextshell/core";
import { usePreferencesStore } from "../../../store/usePreferencesStore";
import { useTransferQueueStore } from "../../../store/useTransferQueueStore";
import { pMap } from "../../../utils/concurrentLimit";
import { formatErrorMessage } from "../../../utils/errorMessage";
import {
  canAcceptSftpFileDrop,
  extractDroppedFilePaths,
  isExternalFileDrag
} from "../../../utils/sftpFileDrop";
import { ensureTarGzName, inferName, joinLocalPath, joinRemotePath, normalizeRemotePath } from "../shared";

type AppMessage = ReturnType<typeof AntdApp.useApp>["message"];
type AppModal = ReturnType<typeof AntdApp.useApp>["modal"];
type UpdatePreferences = ReturnType<typeof usePreferencesStore.getState>["updatePreferences"];
type EnqueueTask = ReturnType<typeof useTransferQueueStore.getState>["enqueueTask"];
type MarkTransferResult = ReturnType<typeof useTransferQueueStore.getState>["markFailed"];
type MarkTransferSuccess = ReturnType<typeof useTransferQueueStore.getState>["markSuccess"];

interface UseTransferHandlersParams {
  connection?: ConnectionProfile;
  connected: boolean;
  active: boolean;
  busy: boolean;
  setBusy: Dispatch<SetStateAction<boolean>>;
  pathName: string;
  loadFiles: () => Promise<void>;
  transferPreferences: AppPreferences["transfer"];
  updatePreferences: UpdatePreferences;
  enqueueTask: EnqueueTask;
  markFailed: MarkTransferResult;
  markSuccess: MarkTransferSuccess;
  message: AppMessage;
  modal: AppModal;
}

export const useTransferHandlers = ({
  connection,
  connected,
  active,
  busy,
  setBusy,
  pathName,
  loadFiles,
  transferPreferences,
  updatePreferences,
  enqueueTask,
  markFailed,
  markSuccess,
  message,
  modal
}: UseTransferHandlersParams) => {
  const dragDepthRef = useRef(0);
  const [dropTargetActive, setDropTargetActive] = useState(false);

  useEffect(() => {
    dragDepthRef.current = 0;
    setDropTargetActive(false);
  }, [connection?.id, active]);

  const syncUploadDefaultDir = useCallback(
    (localPaths: string[]): void => {
      const firstFile = localPaths[0];
      if (!firstFile) {
        return;
      }
      const firstDir = firstFile.replace(/[\\/][^\\/]+$/, "");
      if (firstDir && firstDir !== transferPreferences.uploadDefaultDir) {
        void updatePreferences({
          transfer: {
            uploadDefaultDir: firstDir
          }
        });
      }
    },
    [transferPreferences.uploadDefaultDir, updatePreferences]
  );

  const uploadLocalFiles = useCallback(
    async (localPaths: string[]): Promise<void> => {
      if (!connection || localPaths.length === 0) return;

      syncUploadDefaultDir(localPaths);

      let successCount = 0;
      setBusy(true);

      try {
        await pMap(
          localPaths,
          async (localPath) => {
            const remotePath = normalizeRemotePath(joinRemotePath(pathName, inferName(localPath)));
            const task = enqueueTask({
              direction: "upload",
              connectionId: connection.id,
              localPath,
              remotePath
            });

            try {
              await window.nextshell.sftp.upload({
                connectionId: connection.id,
                localPath,
                remotePath,
                taskId: task.id
              });
              markSuccess(task.id);
              successCount += 1;
            } catch (error) {
              const reason = formatErrorMessage(error, "上传失败");
              markFailed(task.id, reason);
              message.error(`上传失败：${inferName(localPath)}（${reason}）`);
            }
          },
          5
        );

        if (successCount > 0) {
          message.success(`上传完成 (${successCount}/${localPaths.length})`);
        }
        await loadFiles();
      } catch (error) {
        message.error(`上传失败：${formatErrorMessage(error, "请稍后重试")}`);
      } finally {
        setBusy(false);
      }
    },
    [connection, enqueueTask, loadFiles, markFailed, markSuccess, message, pathName, setBusy, syncUploadDefaultDir]
  );

  const confirmDropUpload = useCallback(
    (filePaths: string[]): Promise<boolean> =>
      new Promise((resolve) => {
        modal.confirm({
          title: "上传拖拽文件",
          content:
            filePaths.length === 1
              ? `将 ${inferName(filePaths[0] ?? "")} 上传到当前目录 ${pathName}？`
              : `将 ${filePaths.length} 个文件上传到当前目录 ${pathName}？`,
          okText: "上传",
          cancelText: "取消",
          onOk: () => resolve(true),
          onCancel: () => resolve(false)
        });
      }),
    [modal, pathName]
  );

  const handleUpload = useCallback(async (): Promise<void> => {
    if (!connection) return;

    try {
      const picked = await window.nextshell.dialog.openFiles({
        title: "选择要上传的本地文件",
        defaultPath: transferPreferences.uploadDefaultDir,
        multi: true
      });

      if (picked.canceled || picked.filePaths.length === 0) {
        return;
      }
      await uploadLocalFiles(picked.filePaths);
    } catch (error) {
      message.error(`上传失败：${formatErrorMessage(error, "请稍后重试")}`);
    }
  }, [connection, message, transferPreferences.uploadDefaultDir, uploadLocalFiles]);

  const handlePackedUpload = useCallback(async (): Promise<void> => {
    if (!connection) return;

    try {
      const picked = await window.nextshell.dialog.openFiles({
        title: "选择要打包上传的本地文件",
        defaultPath: transferPreferences.uploadDefaultDir,
        multi: true
      });
      if (picked.canceled || picked.filePaths.length === 0) {
        return;
      }

      const firstFile = picked.filePaths[0]!;
      syncUploadDefaultDir(picked.filePaths);

      const archiveBase =
        picked.filePaths.length === 1 ? inferName(firstFile) : `upload-bundle-${Date.now()}`;
      const archiveName = ensureTarGzName(archiveBase);
      const remotePath = normalizeRemotePath(joinRemotePath(pathName, archiveName));
      const localDisplayPath =
        picked.filePaths.length === 1
          ? firstFile
          : `${firstFile} (+${picked.filePaths.length - 1} files)`;

      setBusy(true);
      const task = enqueueTask({
        direction: "upload",
        connectionId: connection.id,
        localPath: localDisplayPath,
        remotePath,
        retryable: false
      });

      try {
        await window.nextshell.sftp.uploadPacked({
          connectionId: connection.id,
          localPaths: picked.filePaths,
          remoteDir: normalizeRemotePath(pathName),
          archiveName,
          taskId: task.id
        });
        markSuccess(task.id);
        message.success("打包上传完成");
        await loadFiles();
      } catch (error) {
        const reason = formatErrorMessage(error, "打包上传失败");
        markFailed(task.id, reason);
        message.error(`打包上传失败：${reason}`);
      }
    } catch (error) {
      message.error(`打包上传失败：${formatErrorMessage(error, "请稍后重试")}`);
    } finally {
      setBusy(false);
    }
  }, [
    connection,
    enqueueTask,
    loadFiles,
    markFailed,
    markSuccess,
    message,
    pathName,
    setBusy,
    syncUploadDefaultDir,
    transferPreferences.uploadDefaultDir
  ]);

  const handleDragEnter = useCallback((event: DragEvent<HTMLDivElement>): void => {
    if (!isExternalFileDrag(event.dataTransfer)) {
      return;
    }
    event.preventDefault();
    if (!canAcceptSftpFileDrop({ active, connected, hasConnection: Boolean(connection), busy })) {
      return;
    }
    dragDepthRef.current += 1;
    if (!dropTargetActive) {
      setDropTargetActive(true);
    }
  }, [active, busy, connected, connection, dropTargetActive]);

  const handleDragOver = useCallback((event: DragEvent<HTMLDivElement>): void => {
    if (!isExternalFileDrag(event.dataTransfer)) {
      return;
    }
    event.preventDefault();
    if (!canAcceptSftpFileDrop({ active, connected, hasConnection: Boolean(connection), busy })) {
      return;
    }
    event.dataTransfer.dropEffect = "copy";
    if (!dropTargetActive) {
      setDropTargetActive(true);
    }
  }, [active, busy, connected, connection, dropTargetActive]);

  const handleDragLeave = useCallback((event: DragEvent<HTMLDivElement>): void => {
    if (!dropTargetActive) {
      return;
    }
    event.preventDefault();
    dragDepthRef.current = Math.max(0, dragDepthRef.current - 1);
    if (dragDepthRef.current === 0) {
      setDropTargetActive(false);
    }
  }, [dropTargetActive]);

  const handleDropUpload = useCallback(async (event: DragEvent<HTMLDivElement>): Promise<void> => {
    dragDepthRef.current = 0;
    setDropTargetActive(false);
    if (!isExternalFileDrag(event.dataTransfer)) {
      return;
    }

    event.preventDefault();
    if (!canAcceptSftpFileDrop({ active, connected, hasConnection: Boolean(connection), busy })) {
      return;
    }
    const result = extractDroppedFilePaths(event.dataTransfer, window.nextshell.getFilePathForDrop);
    if (result.paths.length === 0) {
      console.warn("[sftpFileDrop] drop extraction failed", {
        allPathsEmpty: result.allPathsEmpty,
        itemCount: event.dataTransfer?.items?.length ?? 0,
        fileCount: event.dataTransfer?.files?.length ?? 0
      });
      if (result.allPathsEmpty) {
        message.warning("无法读取拖入文件的路径，请尝试使用上传按钮选择文件");
      } else {
        message.warning("当前仅支持拖入文件");
      }
      return;
    }
    console.info("[sftpFileDrop] extracted paths:", result.paths);

    const confirmed = await confirmDropUpload(result.paths);
    if (!confirmed) {
      return;
    }

    await uploadLocalFiles(result.paths);
  }, [active, busy, confirmDropUpload, connected, connection, message, uploadLocalFiles]);

  const handleDownload = useCallback(
    async (
      entries: { name: string; path: string }[],
      targetBaseDir?: string,
      persistDefaultDir = false
    ): Promise<void> => {
      if (!connection || entries.length === 0) return;
      const localBasePath = (targetBaseDir || transferPreferences.downloadDefaultDir).trim();
      if (!localBasePath) return;

      if (persistDefaultDir && localBasePath !== transferPreferences.downloadDefaultDir) {
        void updatePreferences({
          transfer: {
            downloadDefaultDir: localBasePath
          }
        });
      }

      try {
        let successCount = 0;
        setBusy(true);

        await pMap(
          entries,
          async (entry) => {
            const targetPath = joinLocalPath(localBasePath, entry.name);
            const task = enqueueTask({
              direction: "download",
              connectionId: connection.id,
              localPath: targetPath,
              remotePath: entry.path
            });
            try {
              await window.nextshell.sftp.download({
                connectionId: connection.id,
                remotePath: entry.path,
                localPath: targetPath,
                taskId: task.id
              });
              markSuccess(task.id);
              successCount += 1;
            } catch (error) {
              const reason = formatErrorMessage(error, "下载失败");
              markFailed(task.id, reason);
              message.error(`下载失败：${entry.name}（${reason}）`);
            }
          },
          5
        );

        if (successCount > 0) {
          message.success(`下载完成 (${successCount}/${entries.length}) → ${localBasePath}`);
        }
      } catch (error) {
        message.error(`下载失败：${formatErrorMessage(error, "请稍后重试")}`);
      } finally {
        setBusy(false);
      }
    },
    [
      connection,
      enqueueTask,
      markFailed,
      markSuccess,
      message,
      setBusy,
      transferPreferences.downloadDefaultDir,
      updatePreferences
    ]
  );

  const handlePackedDownload = useCallback(
    async (
      entries: { name: string }[],
      targetBaseDir?: string,
      persistDefaultDir = false
    ): Promise<void> => {
      if (!connection || entries.length === 0) return;
      const localBasePath = (targetBaseDir || transferPreferences.downloadDefaultDir).trim();
      if (!localBasePath) return;

      if (persistDefaultDir && localBasePath !== transferPreferences.downloadDefaultDir) {
        void updatePreferences({
          transfer: {
            downloadDefaultDir: localBasePath
          }
        });
      }

      const normalizedCurrentPath = normalizeRemotePath(pathName);
      const pathSegment =
        normalizedCurrentPath === "/"
          ? "root"
          : normalizedCurrentPath.split("/").filter(Boolean).at(-1) ?? "bundle";
      const archiveBase =
        entries.length === 1 ? entries[0]!.name : `${pathSegment}-bundle-${Date.now()}`;
      const archiveName = ensureTarGzName(archiveBase);
      const localArchivePath = joinLocalPath(localBasePath, archiveName);
      const remoteArchivePath = normalizeRemotePath(joinRemotePath(pathName, archiveName));

      setBusy(true);
      const task = enqueueTask({
        direction: "download",
        connectionId: connection.id,
        localPath: localArchivePath,
        remotePath: remoteArchivePath,
        retryable: false
      });

      try {
        await window.nextshell.sftp.downloadPacked({
          connectionId: connection.id,
          remoteDir: normalizedCurrentPath,
          entryNames: entries.map((entry) => entry.name),
          localDir: localBasePath,
          archiveName,
          taskId: task.id
        });
        markSuccess(task.id);
        message.success(`打包下载完成 → ${localArchivePath}`);
      } catch (error) {
        const reason = formatErrorMessage(error, "打包下载失败");
        markFailed(task.id, reason);
        message.error(`打包下载失败：${reason}`);
      } finally {
        setBusy(false);
      }
    },
    [
      connection,
      enqueueTask,
      markFailed,
      markSuccess,
      message,
      pathName,
      setBusy,
      transferPreferences.downloadDefaultDir,
      updatePreferences
    ]
  );

  return {
    dropTargetActive,
    handleDownload,
    handleDragEnter,
    handleDragLeave,
    handleDragOver,
    handleDropUpload,
    handlePackedDownload,
    handlePackedUpload,
    handleUpload
  };
};
