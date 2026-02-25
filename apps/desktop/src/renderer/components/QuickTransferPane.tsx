import { useCallback, useEffect, useMemo, useState } from "react";
import { App as AntdApp, Modal, Table, Tooltip, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import type { ConnectionProfile, RemoteFileEntry, SessionDescriptor } from "@nextshell/core";
import { usePreferencesStore } from "../store/usePreferencesStore";
import { useTransferQueueStore } from "../store/useTransferQueueStore";
import { pMap } from "../utils/concurrentLimit";
import { formatErrorMessage } from "../utils/errorMessage";

interface QuickTransferPaneProps {
  sourceConnection?: ConnectionProfile;
  connected: boolean;
  active: boolean;
  connections: ConnectionProfile[];
  sessions: SessionDescriptor[];
}

type TargetMode = "local" | "server";
type TransferMode = "packed" | "individual";
const QUICK_TRANSFER_MODE_STORAGE_KEY = "nextshell.quickTransfer.transferMode";

const normalizeRemotePath = (rawPath: string): string => {
  const value = rawPath.trim();
  if (!value) return "/";
  const withSlash = value.startsWith("/") ? value : `/${value}`;
  return withSlash.replace(/\/+/g, "/").replace(/\/$/, "") || "/";
};

const joinRemotePath = (base: string, next: string): string => {
  const root = normalizeRemotePath(base);
  const clean = next.trim().replace(/^\/+/, "").replace(/\/+$/, "");
  if (!clean) return root;
  return root === "/" ? `/${clean}` : `${root}/${clean}`;
};

const joinLocalPath = (base: string, next: string): string => {
  if (base.endsWith("/") || base.endsWith("\\")) return `${base}${next}`;
  return `${base}/${next}`;
};

const toLocalParentPath = (rawPath: string): string => {
  const trimmed = rawPath.trim().replace(/[\\/]+$/, "");
  if (!trimmed) return rawPath;
  if (/^[A-Za-z]:$/.test(trimmed)) {
    return `${trimmed}\\`;
  }

  const slashIndex = Math.max(trimmed.lastIndexOf("/"), trimmed.lastIndexOf("\\"));
  if (slashIndex < 0) {
    return trimmed;
  }
  if (slashIndex === 0) {
    return trimmed.startsWith("/") ? "/" : trimmed;
  }

  return trimmed.slice(0, slashIndex);
};

const formatFileSize = (size: number, isDir: boolean): string => {
  if (isDir) return "";
  if (size === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(size) / Math.log(1024));
  const val = size / Math.pow(1024, i);
  return `${val.toFixed(i > 0 ? 1 : 0)} ${units[i]}`;
};

const formatModifiedTime = (iso: string): string => {
  if (!iso) return "";
  try {
    const d = new Date(iso);
    const y = d.getFullYear();
    const m = String(d.getMonth() + 1).padStart(2, "0");
    const day = String(d.getDate()).padStart(2, "0");
    const h = String(d.getHours()).padStart(2, "0");
    const min = String(d.getMinutes()).padStart(2, "0");
    return `${y}/${m}/${day} ${h}:${min}`;
  } catch {
    return iso;
  }
};

const ensureTarGzName = (name: string): string => {
  const trimmed = name.trim();
  if (!trimmed) return "archive.tar.gz";
  if (trimmed.toLowerCase().endsWith(".tar.gz")) return trimmed;
  return `${trimmed}.tar.gz`;
};

const readTransferModePreference = (): TransferMode => {
  try {
    const saved = localStorage.getItem(QUICK_TRANSFER_MODE_STORAGE_KEY);
    return saved === "individual" ? "individual" : "packed";
  } catch {
    return "packed";
  }
};

export const QuickTransferPane = ({
  sourceConnection,
  connected,
  active,
  connections,
  sessions
}: QuickTransferPaneProps) => {
  const { message } = AntdApp.useApp();
  const preferences = usePreferencesStore((state) => state.preferences);
  const enqueueTask = useTransferQueueStore((state) => state.enqueueTask);
  const markFailed = useTransferQueueStore((state) => state.markFailed);
  const markSuccess = useTransferQueueStore((state) => state.markSuccess);

  const [leftPath, setLeftPath] = useState("/");
  const [leftPathInput, setLeftPathInput] = useState("/");
  const [leftFiles, setLeftFiles] = useState<RemoteFileEntry[]>([]);
  const [leftSelectedPaths, setLeftSelectedPaths] = useState<string[]>([]);
  const [leftLoading, setLeftLoading] = useState(false);

  const [targetMode, setTargetMode] = useState<TargetMode>("local");
  const [transferMode, setTransferMode] = useState<TransferMode>(readTransferModePreference);
  const [targetConnectionId, setTargetConnectionId] = useState<string>("");
  const [targetPickerOpen, setTargetPickerOpen] = useState(false);
  const [pendingTargetConnectionId, setPendingTargetConnectionId] = useState<string>("");
  const [rightPath, setRightPath] = useState(preferences.transfer.downloadDefaultDir);
  const [rightPathInput, setRightPathInput] = useState(preferences.transfer.downloadDefaultDir);
  const [rightFiles, setRightFiles] = useState<RemoteFileEntry[]>([]);
  const [rightSelectedPaths, setRightSelectedPaths] = useState<string[]>([]);
  const [rightLoading, setRightLoading] = useState(false);
  const [transferBusy, setTransferBusy] = useState(false);

  const leftSelectedEntries = useMemo(() => {
    const selected = new Set(leftSelectedPaths);
    return leftFiles.filter((item) => selected.has(item.path));
  }, [leftFiles, leftSelectedPaths]);

  const rightSelectedEntries = useMemo(() => {
    const selected = new Set(rightSelectedPaths);
    return rightFiles.filter((item) => selected.has(item.path));
  }, [rightFiles, rightSelectedPaths]);

  const targetConnectionIdSet = useMemo(() => {
    const ids = new Set<string>();
    for (const session of sessions) {
      if (session.status !== "connected") continue;
      if (session.connectionId === sourceConnection?.id) continue;
      ids.add(session.connectionId);
    }
    return ids;
  }, [sessions, sourceConnection?.id]);

  const targetConnections = useMemo(
    () => connections.filter((item) => targetConnectionIdSet.has(item.id)),
    [connections, targetConnectionIdSet]
  );

  const targetConnection = useMemo(
    () => targetConnections.find((item) => item.id === targetConnectionId),
    [targetConnectionId, targetConnections]
  );

  const columns: ColumnsType<RemoteFileEntry> = useMemo(
    () => [
      {
        title: "名称",
        dataIndex: "name",
        key: "name",
        render: (_v: string, row: RemoteFileEntry) => (
          <span className="inline-flex items-center gap-1.5">
            <i
              className={row.type === "directory" ? "ri-folder-3-fill text-sm shrink-0 leading-none" : "ri-file-text-line text-sm shrink-0 leading-none"}
              aria-hidden="true"
            />
            {row.name}
          </span>
        )
      },
      {
        title: "大小",
        dataIndex: "size",
        key: "size",
        width: 86,
        render: (v: number, row) => formatFileSize(v, row.type === "directory")
      },
      {
        title: "修改时间",
        dataIndex: "modifiedAt",
        key: "modifiedAt",
        width: 140,
        render: (v: string) => formatModifiedTime(v)
      }
    ],
    []
  );

  const targetConnectionColumns: ColumnsType<ConnectionProfile> = useMemo(
    () => [
      {
        title: "连接名称",
        dataIndex: "name",
        key: "name"
      },
      {
        title: "地址",
        key: "host",
        render: (_v: unknown, row: ConnectionProfile) => `${row.host}:${row.port}`
      }
    ],
    []
  );

  const loadLeftFiles = useCallback(async (): Promise<void> => {
    if (!active) {
      return;
    }
    if (!sourceConnection || !connected) {
      setLeftFiles([]);
      setLeftSelectedPaths([]);
      return;
    }

    const normalizedPath = normalizeRemotePath(leftPath);
    setLeftLoading(true);
    try {
      const list = await window.nextshell.sftp.list({
        connectionId: sourceConnection.id,
        path: normalizedPath
      });
      setLeftFiles(list);
      setLeftSelectedPaths([]);
      setLeftPath(normalizedPath);
    } catch (error) {
      message.error(`读取左侧目录失败：${formatErrorMessage(error, "请检查连接状态")}`);
      setLeftFiles([]);
    } finally {
      setLeftLoading(false);
    }
  }, [active, connected, leftPath, message, sourceConnection]);

  const loadRightFiles = useCallback(async (): Promise<void> => {
    if (!active) {
      return;
    }
    const currentPath = rightPath.trim();
    if (!currentPath) {
      return;
    }

    if (targetMode === "server" && !targetConnectionId) {
      setRightFiles([]);
      setRightSelectedPaths([]);
      return;
    }

    setRightLoading(true);
    try {
      if (targetMode === "local") {
        const list = await window.nextshell.sftp.listLocal({ path: currentPath });
        setRightFiles(list);
        setRightSelectedPaths([]);
        setRightPath(currentPath);
      } else {
        const normalizedPath = normalizeRemotePath(currentPath);
        const list = await window.nextshell.sftp.list({
          connectionId: targetConnectionId,
          path: normalizedPath
        });
        setRightFiles(list);
        setRightSelectedPaths([]);
        setRightPath(normalizedPath);
      }
    } catch (error) {
      message.error(`读取右侧目录失败：${formatErrorMessage(error, "请检查路径或连接状态")}`);
      setRightFiles([]);
    } finally {
      setRightLoading(false);
    }
  }, [active, message, rightPath, targetConnectionId, targetMode]);

  const leftNavigate = useCallback((nextPath: string) => {
    setLeftPath(normalizeRemotePath(nextPath));
  }, []);

  const rightNavigate = useCallback((nextPath: string) => {
    if (targetMode === "local") {
      const trimmed = nextPath.trim();
      if (trimmed) {
        setRightPath(trimmed);
      }
      return;
    }
    setRightPath(normalizeRemotePath(nextPath));
  }, [targetMode]);

  useEffect(() => {
    setLeftPathInput(leftPath);
  }, [leftPath]);

  useEffect(() => {
    setRightPathInput(rightPath);
  }, [rightPath]);

  useEffect(() => {
    try {
      localStorage.setItem(QUICK_TRANSFER_MODE_STORAGE_KEY, transferMode);
    } catch {
      // ignore storage write errors
    }
  }, [transferMode]);

  useEffect(() => {
    setLeftPath("/");
    setLeftSelectedPaths([]);
  }, [sourceConnection?.id, connected]);

  useEffect(() => {
    if (!active) return;
    setTargetMode("local");
    setTargetConnectionId("");
    setPendingTargetConnectionId("");
    setRightFiles([]);
    setRightSelectedPaths([]);
  }, [active, sourceConnection?.id]);

  useEffect(() => {
    if (!targetPickerOpen) return;
    setPendingTargetConnectionId(targetConnectionId);
  }, [targetConnectionId, targetPickerOpen]);

  useEffect(() => {
    if (!targetConnectionId) return;
    const exists = targetConnections.some((item) => item.id === targetConnectionId);
    if (!exists) {
      setTargetConnectionId("");
    }
  }, [targetConnectionId, targetConnections]);

  useEffect(() => {
    if (targetMode !== "local") return;
    if (rightPath.trim().length > 0) return;
    setRightPath(preferences.transfer.downloadDefaultDir);
  }, [preferences.transfer.downloadDefaultDir, rightPath, targetMode]);

  useEffect(() => {
    void loadLeftFiles();
  }, [loadLeftFiles]);

  useEffect(() => {
    void loadRightFiles();
  }, [loadRightFiles, targetMode, rightPath, targetConnectionId]);

  const handleLeftParent = useCallback(() => {
    const normalized = normalizeRemotePath(leftPath);
    if (normalized === "/") return;
    const next = normalized.slice(0, normalized.lastIndexOf("/")) || "/";
    leftNavigate(next);
  }, [leftNavigate, leftPath]);

  const handleRightParent = useCallback(() => {
    if (targetMode === "local") {
      rightNavigate(toLocalParentPath(rightPath));
      return;
    }
    const normalized = normalizeRemotePath(rightPath);
    if (normalized === "/") return;
    rightNavigate(normalized.slice(0, normalized.lastIndexOf("/")) || "/");
  }, [rightNavigate, rightPath, targetMode]);

  const handlePickLocalDirectory = useCallback(async (): Promise<void> => {
    try {
      const result = await window.nextshell.dialog.openDirectory({
        title: "选择本机快传目录",
        defaultPath: rightPath
      });
      if (result.canceled || !result.filePath) return;
      setRightPath(result.filePath);
    } catch (error) {
      message.error(`打开目录选择器失败：${formatErrorMessage(error, "请稍后重试")}`);
    }
  }, [message, rightPath]);

  const handleOpenTargetPicker = useCallback((): void => {
    if (targetConnections.length === 0) {
      message.warning("暂无可选目标服务器。请先在标签页中连接目标服务器。");
      return;
    }
    setPendingTargetConnectionId(targetConnectionId);
    setTargetPickerOpen(true);
  }, [message, targetConnectionId, targetConnections.length]);

  const handleConfirmTargetPicker = useCallback((): void => {
    if (!pendingTargetConnectionId) {
      message.warning("请先选择目标服务器");
      return;
    }
    if (pendingTargetConnectionId !== targetConnectionId) {
      setTargetConnectionId(pendingTargetConnectionId);
      setRightPath("/");
    }
    setTargetPickerOpen(false);
  }, [message, pendingTargetConnectionId, targetConnectionId]);

  const transferLeftToRightLocalIndividual = useCallback(async (): Promise<void> => {
    if (!sourceConnection || leftSelectedEntries.length === 0) return;
    const basePath = rightPath.trim();
    if (!basePath) return;

    let successCount = 0;
    setTransferBusy(true);
    await pMap(leftSelectedEntries, async (entry) => {
      const targetPath = joinLocalPath(basePath, entry.name);
      const task = enqueueTask({
        direction: "download",
        connectionId: sourceConnection.id,
        localPath: targetPath,
        remotePath: entry.path
      });
      try {
        await window.nextshell.sftp.download({
          connectionId: sourceConnection.id,
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
    }, 4);
    setTransferBusy(false);

    if (successCount > 0) {
      message.success(`快传完成 (${successCount}/${leftSelectedEntries.length})`);
      await loadRightFiles();
    }
  }, [
    enqueueTask,
    leftSelectedEntries,
    loadRightFiles,
    markFailed,
    markSuccess,
    message,
    rightPath,
    sourceConnection
  ]);

  const transferRightToLeftLocalIndividual = useCallback(async (): Promise<void> => {
    if (!sourceConnection || rightSelectedEntries.length === 0) return;

    const localFiles = rightSelectedEntries.filter((entry) => entry.type === "file");
    if (localFiles.length === 0) {
      message.warning("仅支持从右侧选择本机文件上传");
      return;
    }

    let successCount = 0;
    setTransferBusy(true);
    await pMap(localFiles, async (entry) => {
      const remotePath = normalizeRemotePath(joinRemotePath(leftPath, entry.name));
      const task = enqueueTask({
        direction: "upload",
        connectionId: sourceConnection.id,
        localPath: entry.path,
        remotePath
      });
      try {
        await window.nextshell.sftp.upload({
          connectionId: sourceConnection.id,
          localPath: entry.path,
          remotePath,
          taskId: task.id
        });
        markSuccess(task.id);
        successCount += 1;
      } catch (error) {
        const reason = formatErrorMessage(error, "上传失败");
        markFailed(task.id, reason);
        message.error(`上传失败：${entry.name}（${reason}）`);
      }
    }, 4);
    setTransferBusy(false);

    if (successCount > 0) {
      message.success(`快传完成 (${successCount}/${localFiles.length})`);
      await loadLeftFiles();
    }
  }, [
    enqueueTask,
    leftPath,
    loadLeftFiles,
    markFailed,
    markSuccess,
    message,
    rightSelectedEntries,
    sourceConnection
  ]);

  const transferLeftToRightLocalPacked = useCallback(async (): Promise<void> => {
    if (!sourceConnection || leftSelectedEntries.length === 0) return;
    const basePath = rightPath.trim();
    if (!basePath) return;

    const normalizedSourceDir = normalizeRemotePath(leftPath);
    const pathSegment = normalizedSourceDir === "/"
      ? "root"
      : normalizedSourceDir.split("/").filter(Boolean).at(-1) ?? "bundle";
    const archiveBase = leftSelectedEntries.length === 1
      ? leftSelectedEntries[0]!.name
      : `${pathSegment}-bundle-${Date.now()}`;
    const archiveName = ensureTarGzName(archiveBase);
    const localArchivePath = joinLocalPath(basePath, archiveName);
    const remoteArchivePath = normalizeRemotePath(joinRemotePath(normalizedSourceDir, archiveName));

    setTransferBusy(true);
    const task = enqueueTask({
      direction: "download",
      connectionId: sourceConnection.id,
      localPath: localArchivePath,
      remotePath: remoteArchivePath,
      retryable: false
    });

    try {
      await window.nextshell.sftp.downloadPacked({
        connectionId: sourceConnection.id,
        remoteDir: normalizedSourceDir,
        entryNames: leftSelectedEntries.map((entry) => entry.name),
        localDir: basePath,
        archiveName,
        taskId: task.id
      });
      markSuccess(task.id);
      message.success(`打包快传完成 → ${localArchivePath}`);
      await loadRightFiles();
    } catch (error) {
      const reason = formatErrorMessage(error, "打包下载失败");
      markFailed(task.id, reason);
      message.error(`打包下载失败：${reason}`);
    } finally {
      setTransferBusy(false);
    }
  }, [
    enqueueTask,
    leftPath,
    leftSelectedEntries,
    loadRightFiles,
    markFailed,
    markSuccess,
    message,
    rightPath,
    sourceConnection
  ]);

  const transferRightToLeftLocalPacked = useCallback(async (): Promise<void> => {
    if (!sourceConnection || rightSelectedEntries.length === 0) return;

    const localFiles = rightSelectedEntries.filter((entry) => entry.type === "file");
    if (localFiles.length === 0) {
      message.warning("仅支持从右侧选择本机文件打包上传");
      return;
    }

    const archiveBase = localFiles.length === 1
      ? localFiles[0]!.name
      : `upload-bundle-${Date.now()}`;
    const archiveName = ensureTarGzName(archiveBase);
    const remoteArchivePath = normalizeRemotePath(joinRemotePath(leftPath, archiveName));
    const localDisplayPath = localFiles.length === 1
      ? localFiles[0]!.path
      : `${localFiles[0]!.path} (+${localFiles.length - 1} files)`;

    setTransferBusy(true);
    const task = enqueueTask({
      direction: "upload",
      connectionId: sourceConnection.id,
      localPath: localDisplayPath,
      remotePath: remoteArchivePath,
      retryable: false
    });

    try {
      await window.nextshell.sftp.uploadPacked({
        connectionId: sourceConnection.id,
        localPaths: localFiles.map((entry) => entry.path),
        remoteDir: normalizeRemotePath(leftPath),
        archiveName,
        taskId: task.id
      });
      markSuccess(task.id);
      message.success("打包快传完成");
      await loadLeftFiles();
    } catch (error) {
      const reason = formatErrorMessage(error, "打包上传失败");
      markFailed(task.id, reason);
      message.error(`打包上传失败：${reason}`);
    } finally {
      setTransferBusy(false);
    }
  }, [
    enqueueTask,
    leftPath,
    loadLeftFiles,
    markFailed,
    markSuccess,
    message,
    rightSelectedEntries,
    sourceConnection
  ]);

  const transferBetweenServersPacked = useCallback(
    async (
      sourceId: string,
      sourceDir: string,
      sourceEntries: RemoteFileEntry[],
      targetId: string,
      targetDir: string,
      direction: "upload" | "download"
    ): Promise<void> => {
      if (sourceEntries.length === 0) return;

      const task = enqueueTask({
        direction,
        connectionId: sourceId,
        localPath: `${sourceId}(${normalizeRemotePath(sourceDir)})`,
        remotePath: `${targetId}(${normalizeRemotePath(targetDir)})`,
        retryable: false
      });

      try {
        await window.nextshell.sftp.transferPacked({
          sourceConnectionId: sourceId,
          sourceDir: normalizeRemotePath(sourceDir),
          entryNames: sourceEntries.map((entry) => entry.name),
          targetConnectionId: targetId,
          targetDir: normalizeRemotePath(targetDir),
          taskId: task.id
        });
        markSuccess(task.id);
      } catch (error) {
        const reason = formatErrorMessage(error, "跨服务器快传失败");
        markFailed(task.id, reason);
        throw new Error(reason);
      }
    },
    [enqueueTask, markFailed, markSuccess]
  );

  const transferBetweenServersIndividual = useCallback(
    async (
      sourceId: string,
      sourceDir: string,
      sourceEntries: RemoteFileEntry[],
      targetId: string,
      targetDir: string,
      direction: "upload" | "download"
    ): Promise<number> => {
      if (sourceEntries.length === 0) return 0;
      let successCount = 0;
      await pMap(sourceEntries, async (entry) => {
        const normalizedSourceDir = normalizeRemotePath(sourceDir);
        const normalizedTargetDir = normalizeRemotePath(targetDir);
        const task = enqueueTask({
          direction,
          connectionId: sourceId,
          localPath: `${sourceId}:${joinRemotePath(normalizedSourceDir, entry.name)}`,
          remotePath: `${targetId}:${joinRemotePath(normalizedTargetDir, entry.name)}`,
          retryable: false
        });

        try {
          await window.nextshell.sftp.transferPacked({
            sourceConnectionId: sourceId,
            sourceDir: normalizedSourceDir,
            entryNames: [entry.name],
            targetConnectionId: targetId,
            targetDir: normalizedTargetDir,
            archiveName: ensureTarGzName(entry.name),
            taskId: task.id
          });
          markSuccess(task.id);
          successCount += 1;
        } catch (error) {
          const reason = formatErrorMessage(error, "跨服务器逐个快传失败");
          markFailed(task.id, reason);
          message.error(`逐个快传失败：${entry.name}（${reason}）`);
        }
      }, 2);
      return successCount;
    },
    [enqueueTask, markFailed, markSuccess, message]
  );

  const handleLeftToRight = useCallback(async (): Promise<void> => {
    if (!sourceConnection || leftSelectedEntries.length === 0) return;

    if (targetMode === "local") {
      if (transferMode === "packed") {
        await transferLeftToRightLocalPacked();
      } else {
        await transferLeftToRightLocalIndividual();
      }
      return;
    }

    if (!targetConnectionId) {
      message.warning("请先选择右侧目标服务器");
      return;
    }

    try {
      setTransferBusy(true);
      if (transferMode === "packed") {
        await transferBetweenServersPacked(
          sourceConnection.id,
          leftPath,
          leftSelectedEntries,
          targetConnectionId,
          rightPath,
          "download"
        );
        message.success("快传完成（左侧服务器 -> 右侧服务器）");
      } else {
        const successCount = await transferBetweenServersIndividual(
          sourceConnection.id,
          leftPath,
          leftSelectedEntries,
          targetConnectionId,
          rightPath,
          "download"
        );
        if (successCount > 0) {
          message.success(`快传完成 (${successCount}/${leftSelectedEntries.length})`);
        }
      }
      await loadRightFiles();
    } catch (error) {
      message.error(formatErrorMessage(error, "快传失败"));
    } finally {
      setTransferBusy(false);
    }
  }, [
    leftPath,
    leftSelectedEntries,
    loadRightFiles,
    message,
    rightPath,
    sourceConnection,
    targetConnectionId,
    targetMode,
    transferBetweenServersIndividual,
    transferBetweenServersPacked,
    transferLeftToRightLocalIndividual,
    transferLeftToRightLocalPacked,
    transferMode
  ]);

  const handleRightToLeft = useCallback(async (): Promise<void> => {
    if (!sourceConnection || rightSelectedEntries.length === 0) return;

    if (targetMode === "local") {
      if (transferMode === "packed") {
        await transferRightToLeftLocalPacked();
      } else {
        await transferRightToLeftLocalIndividual();
      }
      return;
    }

    if (!targetConnectionId) {
      message.warning("请先选择右侧目标服务器");
      return;
    }

    try {
      setTransferBusy(true);
      if (transferMode === "packed") {
        await transferBetweenServersPacked(
          targetConnectionId,
          rightPath,
          rightSelectedEntries,
          sourceConnection.id,
          leftPath,
          "upload"
        );
        message.success("快传完成（右侧服务器 -> 左侧服务器）");
      } else {
        const successCount = await transferBetweenServersIndividual(
          targetConnectionId,
          rightPath,
          rightSelectedEntries,
          sourceConnection.id,
          leftPath,
          "upload"
        );
        if (successCount > 0) {
          message.success(`快传完成 (${successCount}/${rightSelectedEntries.length})`);
        }
      }
      await loadLeftFiles();
    } catch (error) {
      message.error(formatErrorMessage(error, "快传失败"));
    } finally {
      setTransferBusy(false);
    }
  }, [
    leftPath,
    loadLeftFiles,
    message,
    rightPath,
    rightSelectedEntries,
    sourceConnection,
    targetConnectionId,
    targetMode,
    transferBetweenServersIndividual,
    transferBetweenServersPacked,
    transferMode,
    transferRightToLeftLocalIndividual,
    transferRightToLeftLocalPacked
  ]);

  if (!sourceConnection) {
    return <Typography.Text className="text-[var(--t3)]">先选择一个连接再使用文件快传。</Typography.Text>;
  }

  if (!connected) {
    return <Typography.Text className="text-[var(--t3)]">当前连接未建立会话，请先连接后再使用文件快传。</Typography.Text>;
  }

  return (
    <div className="qtp-root">
      <section className="qtp-pane">
        <div className="qtp-pane-header">
          <span className="qtp-pane-title">左侧：{sourceConnection.name}</span>
        </div>
        <div className="qtp-path-row">
          <Tooltip title="上级目录">
            <button className="fe-icon-btn" onClick={handleLeftParent} disabled={leftLoading}>
              <i className="ri-arrow-up-s-line" aria-hidden="true" />
            </button>
          </Tooltip>
          <Tooltip title="刷新">
            <button className="fe-icon-btn" onClick={() => void loadLeftFiles()} disabled={leftLoading}>
              <i className="ri-refresh-line" aria-hidden="true" />
            </button>
          </Tooltip>
          <input
            className="fe-path-input qtp-path-input"
            value={leftPathInput}
            onChange={(e) => setLeftPathInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                leftNavigate(leftPathInput);
              }
            }}
            placeholder="输入左侧远端路径后回车"
          />
        </div>
        <div className="qtp-table">
          <Table
            size="small"
            pagination={false}
            rowKey="path"
            columns={columns}
            dataSource={leftFiles}
            loading={leftLoading}
            scroll={{ y: "100%" }}
            rowSelection={{
              selectedRowKeys: leftSelectedPaths,
              onChange: (keys) => setLeftSelectedPaths(keys.map((key) => String(key)))
            }}
            onRow={(row) => ({
              onDoubleClick: () => {
                if (row.type === "directory") {
                  leftNavigate(row.path);
                }
              }
            })}
          />
        </div>
      </section>

      <section className="qtp-transfer-col">
        <div className="qtp-transfer-pref">
          <span className="qtp-transfer-pref-label">传输方式</span>
          <div className="qtp-mode-switch qtp-transfer-mode">
            <button
              className={`qtp-mode-btn${transferMode === "packed" ? " active" : ""}`}
              disabled={transferBusy}
              onClick={() => setTransferMode("packed")}
            >
              打包
            </button>
            <button
              className={`qtp-mode-btn${transferMode === "individual" ? " active" : ""}`}
              disabled={transferBusy}
              onClick={() => setTransferMode("individual")}
            >
              逐个
            </button>
          </div>
        </div>
        <button
          className="qtp-transfer-btn"
          disabled={
            transferBusy ||
            leftLoading ||
            rightLoading ||
            leftSelectedEntries.length === 0 ||
            (targetMode === "server" && !targetConnectionId)
          }
          onClick={() => void handleLeftToRight()}
        >
          左侧 → 右侧
        </button>
        <button
          className="qtp-transfer-btn"
          disabled={
            transferBusy ||
            leftLoading ||
            rightLoading ||
            rightSelectedEntries.length === 0 ||
            (targetMode === "server" && !targetConnectionId)
          }
          onClick={() => void handleRightToLeft()}
        >
          右侧 → 左侧
        </button>
      </section>

      <section className="qtp-pane">
        <div className="qtp-pane-header qtp-pane-header-right">
          <span className="qtp-pane-title">右侧目标</span>
          <div className="qtp-mode-switch">
            <button
              className={`qtp-mode-btn${targetMode === "local" ? " active" : ""}`}
              onClick={() => {
                setTargetMode("local");
              }}
            >
              本机
            </button>
            <button
              className={`qtp-mode-btn${targetMode === "server" ? " active" : ""}`}
              onClick={() => {
                setTargetMode("server");
                setTargetConnectionId("");
                setPendingTargetConnectionId("");
                setRightFiles([]);
                setRightSelectedPaths([]);
              }}
            >
              服务器
            </button>
          </div>
        </div>
        {targetMode === "server" ? (
          <div className="qtp-target-row">
            <button
              className="qtp-pick-btn"
              onClick={handleOpenTargetPicker}
            >
              {targetConnection ? `目标服务器：${targetConnection.name}` : "选择目标服务器（仅连接中标签页）"}
            </button>
          </div>
        ) : (
          <div className="qtp-target-row">
            <button className="qtp-pick-btn" onClick={() => void handlePickLocalDirectory()}>
              选择本机目录
            </button>
          </div>
        )}
        <div className="qtp-path-row">
          <Tooltip title="上级目录">
            <button className="fe-icon-btn" onClick={handleRightParent} disabled={rightLoading}>
              <i className="ri-arrow-up-s-line" aria-hidden="true" />
            </button>
          </Tooltip>
          <Tooltip title="刷新">
            <button className="fe-icon-btn" onClick={() => void loadRightFiles()} disabled={rightLoading}>
              <i className="ri-refresh-line" aria-hidden="true" />
            </button>
          </Tooltip>
          <input
            className="fe-path-input qtp-path-input"
            value={rightPathInput}
            onChange={(e) => setRightPathInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                rightNavigate(rightPathInput);
              }
            }}
            placeholder={targetMode === "local" ? "输入右侧本机路径后回车" : "输入右侧远端路径后回车"}
          />
        </div>
        <div className="qtp-table">
          <Table
            size="small"
            pagination={false}
            rowKey="path"
            columns={columns}
            dataSource={rightFiles}
            loading={rightLoading}
            scroll={{ y: "100%" }}
            locale={{
              emptyText: targetMode === "server" && !targetConnectionId
                ? "先选择目标服务器"
                : "暂无文件"
            }}
            rowSelection={{
              selectedRowKeys: rightSelectedPaths,
              onChange: (keys) => setRightSelectedPaths(keys.map((key) => String(key)))
            }}
            onRow={(row) => ({
              onDoubleClick: () => {
                if (row.type === "directory") {
                  rightNavigate(row.path);
                }
              }
            })}
          />
        </div>
        {targetMode === "server" ? (
          <div className="qtp-target-hint">
            目标：{targetConnection ? `${targetConnection.name} (${targetConnection.host})` : "未选择"}
          </div>
        ) : null}
      </section>
      <Modal
        title="选择目标服务器"
        open={targetPickerOpen}
        onCancel={() => setTargetPickerOpen(false)}
        onOk={handleConfirmTargetPicker}
        okText="确定"
        cancelText="取消"
      >
        <Table
          size="small"
          pagination={false}
          rowKey="id"
          columns={targetConnectionColumns}
          dataSource={targetConnections}
          locale={{ emptyText: "暂无可选服务器（请先打开并连接对应标签页）" }}
          rowSelection={{
            type: "radio",
            selectedRowKeys: pendingTargetConnectionId ? [pendingTargetConnectionId] : [],
            onChange: (keys) => setPendingTargetConnectionId(String(keys[0] ?? ""))
          }}
          onRow={(row) => ({
            onClick: () => setPendingTargetConnectionId(row.id),
            onDoubleClick: () => {
              setPendingTargetConnectionId(row.id);
              setTargetConnectionId(row.id);
              setRightPath("/");
              setTargetPickerOpen(false);
            }
          })}
        />
      </Modal>
    </div>
  );
};
