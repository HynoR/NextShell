import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { App as AntdApp, Modal, Table, Tooltip } from "antd";
import type { ColumnsType } from "antd/es/table";
import type { ConnectionProfile, RemoteFileEntry, SessionDescriptor } from "@nextshell/core";
import { usePreferencesStore } from "../store/usePreferencesStore";
import { useTransferQueueStore } from "../store/useTransferQueueStore";
import { pMap } from "../utils/concurrentLimit";
import { formatErrorMessage } from "../utils/errorMessage";
import { resolveInitialRemotePath } from "../utils/remoteHomePath";
import { ConnectionPrompt } from "./ConnectionPrompt";

interface QuickTransferPaneProps {
  connections: ConnectionProfile[];
  sessions: SessionDescriptor[];
}

type SideMode = "local" | "server";
type TransferMode = "packed" | "individual";
type SideId = "left" | "right";
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

interface SideState {
  mode: SideMode;
  connectionId: string;
  path: string;
  pathInput: string;
  files: RemoteFileEntry[];
  selectedPaths: string[];
  loading: boolean;
  remotePathReady: boolean;
}

const createInitialSide = (mode: SideMode, localDefault: string): SideState => ({
  mode,
  connectionId: "",
  path: mode === "local" ? localDefault : "/",
  pathInput: mode === "local" ? localDefault : "/",
  files: [],
  selectedPaths: [],
  loading: false,
  remotePathReady: false
});

const shallowEqualSide = (a: SideState, b: SideState): boolean => {
  return (
    a.mode === b.mode &&
    a.connectionId === b.connectionId &&
    a.path === b.path &&
    a.pathInput === b.pathInput &&
    a.loading === b.loading &&
    a.remotePathReady === b.remotePathReady &&
    a.files === b.files &&
    a.selectedPaths === b.selectedPaths
  );
};

export const QuickTransferPane = ({ connections, sessions }: QuickTransferPaneProps) => {
  const { message } = AntdApp.useApp();
  const preferences = usePreferencesStore((state) => state.preferences);
  const enqueueTask = useTransferQueueStore((state) => state.enqueueTask);
  const markFailed = useTransferQueueStore((state) => state.markFailed);
  const markSuccess = useTransferQueueStore((state) => state.markSuccess);
  const localDefaultDir = preferences.transfer.downloadDefaultDir;

  const [left, setLeft] = useState<SideState>(() => createInitialSide("local", localDefaultDir));
  const [right, setRight] = useState<SideState>(() => createInitialSide("server", localDefaultDir));
  const [transferMode, setTransferMode] = useState<TransferMode>(readTransferModePreference);
  const [transferBusy, setTransferBusy] = useState(false);

  // Target-server picker is shared by either side when in server mode.
  const [pickerOpen, setPickerOpen] = useState(false);
  const [pickerForSide, setPickerForSide] = useState<SideId | null>(null);
  const [pendingConnectionId, setPendingConnectionId] = useState<string>("");

  const leftHomeRequestIdRef = useRef(0);
  const rightHomeRequestIdRef = useRef(0);
  const lastLocalLeftPathRef = useRef(localDefaultDir);
  const lastLocalRightPathRef = useRef(localDefaultDir);

  // Refs mirror the latest side state so async loaders can read fresh values
  // without re-creating their callback identity on every state change (which
  // caused an infinite render loop via load-on-change effects).
  const leftRef = useRef(left);
  const rightRef = useRef(right);
  useEffect(() => {
    leftRef.current = left;
  }, [left]);
  useEffect(() => {
    rightRef.current = right;
  }, [right]);

  const connectedConnectionIdSet = useMemo(() => {
    const ids = new Set<string>();
    for (const session of sessions) {
      if (session.status !== "connected") continue;
      if (session.type !== "terminal") continue;
      if (!session.connectionId) continue;
      ids.add(session.connectionId);
    }
    return ids;
  }, [sessions]);

  const availableConnections = useMemo(
    () => connections.filter((item) => connectedConnectionIdSet.has(item.id)),
    [connections, connectedConnectionIdSet]
  );

  const leftConnection = useMemo(
    () => availableConnections.find((item) => item.id === left.connectionId),
    [availableConnections, left.connectionId]
  );

  const rightConnection = useMemo(
    () => availableConnections.find((item) => item.id === right.connectionId),
    [availableConnections, right.connectionId]
  );

  const leftSelectedEntries = useMemo(() => {
    const selected = new Set(left.selectedPaths);
    return left.files.filter((item) => selected.has(item.path));
  }, [left.files, left.selectedPaths]);

  const rightSelectedEntries = useMemo(() => {
    const selected = new Set(right.selectedPaths);
    return right.files.filter((item) => selected.has(item.path));
  }, [right.files, right.selectedPaths]);

  const columns: ColumnsType<RemoteFileEntry> = useMemo(
    () => [
      {
        title: "名称",
        dataIndex: "name",
        key: "name",
        render: (_v: string, row: RemoteFileEntry) => (
          <span className="inline-flex items-center gap-1.5">
            <i
              className={
                row.type === "directory"
                  ? "ri-folder-3-fill text-sm shrink-0 leading-none"
                  : "ri-file-text-line text-sm shrink-0 leading-none"
              }
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

  const connectionColumns: ColumnsType<ConnectionProfile> = useMemo(
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

  const updateSide = useCallback((id: SideId, patch: Partial<SideState>) => {
    if (id === "left") {
      setLeft((prev) => {
        const next = { ...prev, ...patch };
        if (shallowEqualSide(prev, next)) return prev;
        return next;
      });
    } else {
      setRight((prev) => {
        const next = { ...prev, ...patch };
        if (shallowEqualSide(prev, next)) return prev;
        return next;
      });
    }
  }, []);

  const loadSideFiles = useCallback(
    async (id: SideId): Promise<void> => {
      const side = id === "left" ? leftRef.current : rightRef.current;
      const currentPath = side.path.trim();
      if (!currentPath) return;

      if (side.mode === "server" && (!side.connectionId || !side.remotePathReady)) {
        if (side.files.length > 0 || side.selectedPaths.length > 0) {
          updateSide(id, { files: [], selectedPaths: [] });
        }
        return;
      }

      updateSide(id, { loading: true });
      try {
        if (side.mode === "local") {
          const list = await window.nextshell.sftp.listLocal({ path: currentPath });
          updateSide(id, { files: list, selectedPaths: [], path: currentPath });
        } else {
          const normalizedPath = normalizeRemotePath(currentPath);
          const list = await window.nextshell.sftp.list({
            connectionId: side.connectionId,
            path: normalizedPath
          });
          updateSide(id, { files: list, selectedPaths: [], path: normalizedPath });
        }
      } catch (error) {
        message.error(
          `读取${id === "left" ? "左侧" : "右侧"}目录失败：${formatErrorMessage(error, "请检查路径或连接状态")}`
        );
        if (side.files.length > 0) {
          updateSide(id, { files: [] });
        }
      } finally {
        updateSide(id, { loading: false });
      }
    },
    [message, updateSide]
  );

  const navigateSide = useCallback(
    (id: SideId, nextPath: string) => {
      const side = id === "left" ? leftRef.current : rightRef.current;
      if (side.mode === "local") {
        const trimmed = nextPath.trim();
        if (trimmed) {
          updateSide(id, { path: trimmed });
        }
        return;
      }
      updateSide(id, { path: normalizeRemotePath(nextPath) });
    },
    [updateSide]
  );

  // Keep path input in sync with path
  useEffect(() => {
    updateSide("left", { pathInput: left.path });
  }, [left.path, updateSide]);

  useEffect(() => {
    updateSide("right", { pathInput: right.path });
  }, [right.path, updateSide]);

  useEffect(() => {
    try {
      localStorage.setItem(QUICK_TRANSFER_MODE_STORAGE_KEY, transferMode);
    } catch {
      // ignore storage write errors
    }
  }, [transferMode]);

  useEffect(() => {
    if (left.mode === "local" && left.path.trim().length > 0) {
      lastLocalLeftPathRef.current = left.path;
    }
  }, [left.mode, left.path]);

  useEffect(() => {
    if (right.mode === "local" && right.path.trim().length > 0) {
      lastLocalRightPathRef.current = right.path;
    }
  }, [right.mode, right.path]);

  // Resolve remote home dir when a side switches to server mode + connectionId
  const resolveRemoteHome = useCallback(
    (id: SideId) => {
      const side = id === "left" ? leftRef.current : rightRef.current;
      const requestIdRef = id === "left" ? leftHomeRequestIdRef : rightHomeRequestIdRef;
      requestIdRef.current += 1;
      const requestId = requestIdRef.current;

      updateSide(id, { selectedPaths: [] });

      if (side.mode !== "server" || !side.connectionId) {
        if (side.remotePathReady) {
          updateSide(id, { remotePathReady: false });
        }
        if (side.files.length > 0) {
          updateSide(id, { files: [] });
        }
        return;
      }

      updateSide(id, { remotePathReady: false, path: "/", files: [] });

      const connectionId = side.connectionId;
      void (async () => {
        const initialPath = await resolveInitialRemotePath(() =>
          window.nextshell.session.getHomeDir({ connectionId })
        );
        if (requestIdRef.current !== requestId) {
          return;
        }
        updateSide(id, { path: initialPath, remotePathReady: true });
      })();
    },
    [updateSide]
  );

  // Trigger home resolution whenever a side's server connection changes
  useEffect(() => {
    resolveRemoteHome("left");
  }, [left.mode, left.connectionId, resolveRemoteHome]);

  useEffect(() => {
    resolveRemoteHome("right");
  }, [right.mode, right.connectionId, resolveRemoteHome]);

  // Load files when path / mode / connection readiness changes
  useEffect(() => {
    void loadSideFiles("left");
  }, [loadSideFiles, left.mode, left.path, left.connectionId, left.remotePathReady]);

  useEffect(() => {
    void loadSideFiles("right");
  }, [loadSideFiles, right.mode, right.path, right.connectionId, right.remotePathReady]);

  // Restore local default path when switching back to local mode
  useEffect(() => {
    if (left.mode !== "local") return;
    const next = lastLocalLeftPathRef.current || localDefaultDir;
    if (left.path === next) return;
    updateSide("left", { path: next });
  }, [left.mode, left.path, localDefaultDir, updateSide]);

  useEffect(() => {
    if (right.mode !== "local") return;
    const next = lastLocalRightPathRef.current || localDefaultDir;
    if (right.path === next) return;
    updateSide("right", { path: next });
  }, [right.mode, right.path, localDefaultDir, updateSide]);

  // Drop connectionId if the selected server disconnects
  useEffect(() => {
    if (!left.connectionId) return;
    if (!availableConnections.some((item) => item.id === left.connectionId)) {
      updateSide("left", { connectionId: "" });
    }
  }, [left.connectionId, availableConnections, updateSide]);

  useEffect(() => {
    if (!right.connectionId) return;
    if (!availableConnections.some((item) => item.id === right.connectionId)) {
      updateSide("right", { connectionId: "" });
    }
  }, [right.connectionId, availableConnections, updateSide]);

  // Sync pending picker selection when opening
  useEffect(() => {
    if (!pickerOpen || !pickerForSide) return;
    const current = pickerForSide === "left" ? left.connectionId : right.connectionId;
    setPendingConnectionId(current);
  }, [pickerOpen, pickerForSide, left.connectionId, right.connectionId]);

  const handleParent = useCallback(
    (id: SideId) => {
      const side = id === "left" ? left : right;
      if (side.mode === "local") {
        navigateSide(id, toLocalParentPath(side.path));
        return;
      }
      const normalized = normalizeRemotePath(side.path);
      if (normalized === "/") return;
      navigateSide(id, normalized.slice(0, normalized.lastIndexOf("/")) || "/");
    },
    [left, right, navigateSide]
  );

  const handlePickLocalDirectory = useCallback(
    async (id: SideId): Promise<void> => {
      const side = id === "left" ? left : right;
      try {
        const result = await window.nextshell.dialog.openDirectory({
          title: "选择本机快传目录",
          defaultPath: side.path
        });
        if (result.canceled || !result.filePath) return;
        updateSide(id, { path: result.filePath });
      } catch (error) {
        message.error(`打开目录选择器失败：${formatErrorMessage(error, "请稍后重试")}`);
      }
    },
    [left, right, message, updateSide]
  );

  const openPicker = useCallback(
    (id: SideId): void => {
      if (availableConnections.length === 0) {
        message.warning("暂无可选服务器。请先在标签页中连接目标服务器。");
        return;
      }
      setPickerForSide(id);
      setPickerOpen(true);
    },
    [availableConnections.length, message]
  );

  const confirmPicker = useCallback((): void => {
    if (!pickerForSide) return;
    if (!pendingConnectionId) {
      message.warning("请先选择目标服务器");
      return;
    }
    updateSide(pickerForSide, { connectionId: pendingConnectionId });
    setPickerOpen(false);
    setPickerForSide(null);
  }, [pickerForSide, pendingConnectionId, message, updateSide]);

  const setSideMode = useCallback(
    (id: SideId, mode: SideMode) => {
      updateSide(id, {
        mode,
        connectionId: "",
        files: [],
        selectedPaths: [],
        remotePathReady: false
      });
    },
    [updateSide]
  );

  // ---- Transfer dispatch -------------------------------------------------

  const transferRemoteToLocal = useCallback(
    async (
      connectionId: string,
      sourceEntries: RemoteFileEntry[],
      sourceDir: string,
      localDir: string
    ): Promise<void> => {
      let successCount = 0;
      await pMap(
        sourceEntries,
        async (entry) => {
          const targetPath = joinLocalPath(localDir, entry.name);
          const task = enqueueTask({
            direction: "download",
            connectionId,
            localPath: targetPath,
            remotePath: entry.path
          });
          try {
            await window.nextshell.sftp.download({
              connectionId,
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
        4
      );
      if (successCount > 0) {
        message.success(`快传完成 (${successCount}/${sourceEntries.length})`);
      }
    },
    [enqueueTask, markFailed, markSuccess, message]
  );

  const transferLocalToRemote = useCallback(
    async (
      connectionId: string,
      localEntries: RemoteFileEntry[],
      remoteDir: string
    ): Promise<void> => {
      const localFiles = localEntries.filter((entry) => entry.type === "file");
      if (localFiles.length === 0) {
        message.warning("仅支持选择本机文件上传");
        return;
      }
      let successCount = 0;
      await pMap(
        localFiles,
        async (entry) => {
          const remotePath = normalizeRemotePath(joinRemotePath(remoteDir, entry.name));
          const task = enqueueTask({
            direction: "upload",
            connectionId,
            localPath: entry.path,
            remotePath
          });
          try {
            await window.nextshell.sftp.upload({
              connectionId,
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
        },
        4
      );
      if (successCount > 0) {
        message.success(`快传完成 (${successCount}/${localFiles.length})`);
      }
    },
    [enqueueTask, markFailed, markSuccess, message]
  );

  const transferRemoteToLocalPacked = useCallback(
    async (
      connectionId: string,
      sourceEntries: RemoteFileEntry[],
      sourceDir: string,
      localDir: string
    ): Promise<void> => {
      if (sourceEntries.length === 0) return;
      const normalizedSourceDir = normalizeRemotePath(sourceDir);
      const pathSegment =
        normalizedSourceDir === "/"
          ? "root"
          : (normalizedSourceDir.split("/").filter(Boolean).at(-1) ?? "bundle");
      const archiveBase =
        sourceEntries.length === 1 ? sourceEntries[0]!.name : `${pathSegment}-bundle-${Date.now()}`;
      const archiveName = ensureTarGzName(archiveBase);
      const localArchivePath = joinLocalPath(localDir, archiveName);
      const remoteArchivePath = normalizeRemotePath(
        joinRemotePath(normalizedSourceDir, archiveName)
      );

      const task = enqueueTask({
        direction: "download",
        connectionId,
        localPath: localArchivePath,
        remotePath: remoteArchivePath,
        retryable: false
      });

      try {
        await window.nextshell.sftp.downloadPacked({
          connectionId,
          remoteDir: normalizedSourceDir,
          entryNames: sourceEntries.map((entry) => entry.name),
          localDir,
          archiveName,
          taskId: task.id
        });
        markSuccess(task.id);
        message.success(`打包快传完成 → ${localArchivePath}`);
      } catch (error) {
        const reason = formatErrorMessage(error, "打包下载失败");
        markFailed(task.id, reason);
        message.error(`打包下载失败：${reason}`);
      }
    },
    [enqueueTask, markFailed, markSuccess, message]
  );

  const transferLocalToRemotePacked = useCallback(
    async (
      connectionId: string,
      localEntries: RemoteFileEntry[],
      remoteDir: string
    ): Promise<void> => {
      const localFiles = localEntries.filter((entry) => entry.type === "file");
      if (localFiles.length === 0) {
        message.warning("仅支持选择本机文件打包上传");
        return;
      }
      const archiveBase =
        localFiles.length === 1 ? localFiles[0]!.name : `upload-bundle-${Date.now()}`;
      const archiveName = ensureTarGzName(archiveBase);
      const remoteArchivePath = normalizeRemotePath(joinRemotePath(remoteDir, archiveName));
      const localDisplayPath =
        localFiles.length === 1
          ? localFiles[0]!.path
          : `${localFiles[0]!.path} (+${localFiles.length - 1} files)`;

      const task = enqueueTask({
        direction: "upload",
        connectionId,
        localPath: localDisplayPath,
        remotePath: remoteArchivePath,
        retryable: false
      });

      try {
        await window.nextshell.sftp.uploadPacked({
          connectionId,
          localPaths: localFiles.map((entry) => entry.path),
          remoteDir: normalizeRemotePath(remoteDir),
          archiveName,
          taskId: task.id
        });
        markSuccess(task.id);
        message.success("打包快传完成");
      } catch (error) {
        const reason = formatErrorMessage(error, "打包上传失败");
        markFailed(task.id, reason);
        message.error(`打包上传失败：${reason}`);
      }
    },
    [enqueueTask, markFailed, markSuccess, message]
  );

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
      await pMap(
        sourceEntries,
        async (entry) => {
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
        },
        2
      );
      return successCount;
    },
    [enqueueTask, markFailed, markSuccess, message]
  );

  const isSideReady = (side: SideState): boolean => {
    if (side.mode === "local") return true;
    return Boolean(side.connectionId && side.remotePathReady);
  };

  const handleTransfer = useCallback(
    async (from: SideId, to: SideId): Promise<void> => {
      const src = from === "left" ? left : right;
      const dst = to === "left" ? left : right;
      const selected = from === "left" ? leftSelectedEntries : rightSelectedEntries;

      if (!isSideReady(src) || !isSideReady(dst)) {
        message.warning("请先完成左右两侧的目录加载（本机或已选服务器）");
        return;
      }
      if (selected.length === 0) return;

      // 服务器 -> 服务器
      if (src.mode === "server" && dst.mode === "server") {
        if (!src.connectionId || !dst.connectionId) {
          message.warning("请先选择两侧的目标服务器");
          return;
        }
        const direction = from === "left" ? "download" : "upload";
        try {
          setTransferBusy(true);
          if (transferMode === "packed") {
            await transferBetweenServersPacked(
              src.connectionId,
              src.path,
              selected,
              dst.connectionId,
              dst.path,
              direction
            );
            message.success(
              `快传完成（${from === "left" ? "左侧" : "右侧"}服务器 -> ${from === "left" ? "右侧" : "左侧"}服务器）`
            );
          } else {
            const successCount = await transferBetweenServersIndividual(
              src.connectionId,
              src.path,
              selected,
              dst.connectionId,
              dst.path,
              direction
            );
            if (successCount > 0) {
              message.success(`快传完成 (${successCount}/${selected.length})`);
            }
          }
          await loadSideFiles(to);
        } catch (error) {
          message.error(formatErrorMessage(error, "快传失败"));
        } finally {
          setTransferBusy(false);
        }
        return;
      }

      // 服务器 -> 本机
      if (src.mode === "server" && dst.mode === "local") {
        if (!src.connectionId) {
          message.warning("请先选择源服务器");
          return;
        }
        try {
          setTransferBusy(true);
          if (transferMode === "packed") {
            await transferRemoteToLocalPacked(src.connectionId, selected, src.path, dst.path);
          } else {
            await transferRemoteToLocal(src.connectionId, selected, src.path, dst.path);
          }
          await loadSideFiles(to);
        } finally {
          setTransferBusy(false);
        }
        return;
      }

      // 本机 -> 服务器
      if (src.mode === "local" && dst.mode === "server") {
        if (!dst.connectionId) {
          message.warning("请先选择目标服务器");
          return;
        }
        try {
          setTransferBusy(true);
          if (transferMode === "packed") {
            await transferLocalToRemotePacked(dst.connectionId, selected, dst.path);
          } else {
            await transferLocalToRemote(dst.connectionId, selected, dst.path);
          }
          await loadSideFiles(to);
        } finally {
          setTransferBusy(false);
        }
        return;
      }

      // 本机 -> 本机：不支持
      message.warning("暂不支持本机到本机的快传，请将其中一侧切换为服务器");
    },
    [
      left,
      right,
      leftSelectedEntries,
      rightSelectedEntries,
      transferMode,
      transferBetweenServersPacked,
      transferBetweenServersIndividual,
      transferRemoteToLocalPacked,
      transferRemoteToLocal,
      transferLocalToRemotePacked,
      transferLocalToRemote,
      loadSideFiles,
      message
    ]
  );

  const renderSide = (id: SideId): React.ReactNode => {
    const side = id === "left" ? left : right;
    const connection = id === "left" ? leftConnection : rightConnection;
    const sideLabel = id === "left" ? "左侧" : "右侧";
    const titleConnectionName =
      side.mode === "server" ? (connection ? connection.name : "未选服务器") : "本机";

    const needsServerPick = side.mode === "server" && !side.connectionId;

    return (
      <section className="qtp-pane" key={id}>
        <div className={`qtp-pane-header${id === "right" ? " qtp-pane-header-right" : ""}`}>
          <span className="qtp-pane-title">
            {sideLabel}：{titleConnectionName}
          </span>
          <div className="qtp-mode-switch">
            <button
              className={`qtp-mode-btn${side.mode === "local" ? " active" : ""}`}
              onClick={() => setSideMode(id, "local")}
            >
              本机
            </button>
            <button
              className={`qtp-mode-btn${side.mode === "server" ? " active" : ""}`}
              onClick={() => setSideMode(id, "server")}
            >
              服务器
            </button>
          </div>
        </div>
        {side.mode === "server" ? (
          <div className="qtp-target-row">
            <button className="qtp-pick-btn" onClick={() => openPicker(id)}>
              {connection
                ? `${sideLabel}服务器：${connection.name}`
                : `选择${sideLabel}服务器（仅连接中标签页）`}
            </button>
          </div>
        ) : (
          <div className="qtp-target-row">
            <button className="qtp-pick-btn" onClick={() => void handlePickLocalDirectory(id)}>
              选择本机目录
            </button>
          </div>
        )}
        <div className="qtp-path-row">
          <Tooltip title="上级目录">
            <button
              className="fe-icon-btn"
              onClick={() => handleParent(id)}
              disabled={side.loading}
            >
              <i className="ri-arrow-up-s-line" aria-hidden="true" />
            </button>
          </Tooltip>
          <Tooltip title="刷新">
            <button
              className="fe-icon-btn"
              onClick={() => void loadSideFiles(id)}
              disabled={side.loading}
            >
              <i className="ri-refresh-line" aria-hidden="true" />
            </button>
          </Tooltip>
          <input
            className="fe-path-input qtp-path-input"
            value={side.pathInput}
            onChange={(e) => updateSide(id, { pathInput: e.target.value })}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                navigateSide(id, side.pathInput);
              }
            }}
            placeholder={
              side.mode === "local"
                ? `输入${sideLabel}本机路径后回车`
                : `输入${sideLabel}远端路径后回车`
            }
          />
        </div>
        <div className="qtp-table">
          <Table
            size="small"
            pagination={false}
            rowKey="path"
            columns={columns}
            dataSource={side.files}
            loading={side.loading}
            scroll={{ y: "100%" }}
            locale={{
              emptyText: needsServerPick ? `请先选择${sideLabel}服务器` : "暂无文件"
            }}
            rowSelection={{
              selectedRowKeys: side.selectedPaths,
              onChange: (keys) => updateSide(id, { selectedPaths: keys.map((key) => String(key)) })
            }}
            onRow={(row) => ({
              onDoubleClick: () => {
                if (row.type === "directory") {
                  navigateSide(id, row.path);
                }
              }
            })}
          />
        </div>
        {side.mode === "server" ? (
          <div className="qtp-target-hint">
            {sideLabel}：{connection ? `${connection.name} (${connection.host})` : "未选择"}
          </div>
        ) : null}
      </section>
    );
  };

  const leftReady = isSideReady(left);
  const rightReady = isSideReady(right);

  return (
    <div className="qtp-root">
      <div className="qtp-content">
        {renderSide("left")}

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
              left.loading ||
              right.loading ||
              !leftReady ||
              !rightReady ||
              leftSelectedEntries.length === 0
            }
            onClick={() => void handleTransfer("left", "right")}
          >
            左侧 → 右侧
          </button>
          <button
            className="qtp-transfer-btn"
            disabled={
              transferBusy ||
              left.loading ||
              right.loading ||
              !leftReady ||
              !rightReady ||
              rightSelectedEntries.length === 0
            }
            onClick={() => void handleTransfer("right", "left")}
          >
            右侧 → 左侧
          </button>
        </section>

        {renderSide("right")}
      </div>
      <Modal
        title="选择服务器"
        open={pickerOpen}
        onCancel={() => {
          setPickerOpen(false);
          setPickerForSide(null);
        }}
        onOk={confirmPicker}
        okText="确定"
        cancelText="取消"
      >
        <Table
          size="small"
          pagination={false}
          rowKey="id"
          columns={connectionColumns}
          dataSource={availableConnections}
          locale={{ emptyText: "暂无可选服务器（请先打开并连接对应标签页）" }}
          rowSelection={{
            type: "radio",
            selectedRowKeys: pendingConnectionId ? [pendingConnectionId] : [],
            onChange: (keys) => setPendingConnectionId(String(keys[0] ?? ""))
          }}
          onRow={(row) => ({
            onClick: () => setPendingConnectionId(row.id),
            onDoubleClick: () => {
              if (!pickerForSide) return;
              updateSide(pickerForSide, { connectionId: row.id });
              setPickerOpen(false);
              setPickerForSide(null);
            }
          })}
        />
      </Modal>
    </div>
  );
};
