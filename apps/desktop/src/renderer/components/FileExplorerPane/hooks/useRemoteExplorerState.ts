import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { App as AntdApp } from "antd";
import type { ConnectionProfile, RemoteFileEntry } from "@nextshell/core";
import { FILE_EXPLORER_FOLLOW_CWD_DEBOUNCE_MS } from "../../FileExplorerPane.follow";
import { formatErrorMessage } from "../../../utils/errorMessage";
import { resolveInitialRemotePath } from "../../../utils/remoteHomePath";
import { normalizeRemotePath } from "../shared";
import type { DirTreeNode } from "../types";

type AppMessage = ReturnType<typeof AntdApp.useApp>["message"];

interface UseRemoteExplorerStateParams {
  connection?: ConnectionProfile;
  connected: boolean;
  active: boolean;
  followSessionId?: string;
  followSessionCwd?: string;
  message: AppMessage;
}

export const useRemoteExplorerState = ({
  connection,
  connected,
  active,
  followSessionId,
  followSessionCwd,
  message
}: UseRemoteExplorerStateParams) => {
  const [pathName, setPathName] = useState("/");
  const [pathInput, setPathInput] = useState("/");
  const [files, setFiles] = useState<RemoteFileEntry[]>([]);
  const [selectedPaths, setSelectedPaths] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [treeData, setTreeData] = useState<DirTreeNode[]>([]);
  const [expandedKeys, setExpandedKeys] = useState<string[]>([]);
  const [pathHistory, setPathHistory] = useState<string[]>([]);
  const [historyIndex, setHistoryIndex] = useState(-1);
  const [initialPathReady, setInitialPathReady] = useState(false);
  const [followCwd, setFollowCwd] = useState(false);
  const skipHistoryRef = useRef(false);
  const pathNameRef = useRef(pathName);
  const initialPathRequestIdRef = useRef(0);
  const followCwdLastRef = useRef<string | null>(null);
  const followCwdDebounceRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const navigateRef = useRef<(path: string) => void>(() => {});

  const selectedEntries = useMemo(() => {
    const selected = new Set(selectedPaths);
    return files.filter((item) => selected.has(item.path));
  }, [files, selectedPaths]);

  const singleSelected = selectedEntries.length === 1 ? selectedEntries[0] : undefined;

  const pushHistory = useCallback(
    (path: string) => {
      setPathHistory((prev) => {
        const next = prev.slice(0, historyIndex + 1);
        next.push(path);
        return next;
      });
      setHistoryIndex((prev) => prev + 1);
    },
    [historyIndex]
  );

  const navigate = useCallback(
    (path: string) => {
      if (skipHistoryRef.current) {
        skipHistoryRef.current = false;
      } else {
        pushHistory(path);
      }
      setPathName(path);
    },
    [pushHistory]
  );

  useEffect(() => {
    navigateRef.current = navigate;
  }, [navigate]);

  const goBack = useCallback(() => {
    if (historyIndex <= 0) return;
    const prev = pathHistory[historyIndex - 1];
    if (!prev) return;
    skipHistoryRef.current = true;
    setHistoryIndex((index) => index - 1);
    setPathName(prev);
  }, [historyIndex, pathHistory]);

  const goForward = useCallback(() => {
    if (historyIndex >= pathHistory.length - 1) return;
    const next = pathHistory[historyIndex + 1];
    if (!next) return;
    skipHistoryRef.current = true;
    setHistoryIndex((index) => index + 1);
    setPathName(next);
  }, [historyIndex, pathHistory]);

  const loadFiles = useCallback(async (): Promise<void> => {
    if (!connection || !connected || !initialPathReady) {
      setFiles([]);
      setSelectedPaths([]);
      return;
    }

    const normalizedPath = normalizeRemotePath(pathName);
    setBusy(true);
    try {
      const list = await window.nextshell.sftp.list({
        connectionId: connection.id,
        path: normalizedPath
      });
      setFiles(list);
      setSelectedPaths([]);
      setPathName(normalizedPath);
    } catch (error) {
      message.error(`读取目录失败：${formatErrorMessage(error, "请检查连接状态")}`);
      setFiles([]);
    } finally {
      setBusy(false);
    }
  }, [connection, connected, initialPathReady, message, pathName]);

  const loadTreeChildren = useCallback(
    async (parentPath: string): Promise<DirTreeNode[]> => {
      if (!connection || !connected) return [];
      try {
        const list = await window.nextshell.sftp.list({
          connectionId: connection.id,
          path: parentPath
        });
        return list
          .filter((file) => file.type === "directory")
          .sort((a, b) => a.name.localeCompare(b.name))
          .map((file) => ({
            key: file.path,
            title: file.name,
            isLeaf: false as const
          }));
      } catch {
        return [];
      }
    },
    [connection, connected]
  );

  const updateTreeNode = useCallback(
    (nodes: DirTreeNode[], key: string, children: DirTreeNode[]): DirTreeNode[] =>
      nodes.map((node) => {
        if (node.key === key) return { ...node, children };
        if (node.children) {
          return { ...node, children: updateTreeNode(node.children, key, children) };
        }
        return node;
      }),
    []
  );

  const initTree = useCallback(async () => {
    if (!connection || !connected) {
      setTreeData([]);
      setExpandedKeys([]);
      return;
    }
    const children = await loadTreeChildren("/");
    setTreeData([{ key: "/", title: "/", isLeaf: false, children }]);
    setExpandedKeys(["/"]);
  }, [connection, connected, loadTreeChildren]);

  useEffect(() => {
    initialPathRequestIdRef.current += 1;
    const requestId = initialPathRequestIdRef.current;

    setSelectedPaths([]);
    skipHistoryRef.current = false;

    if (!connection || !connected) {
      setInitialPathReady(false);
      setPathName("/");
      setPathHistory([]);
      setHistoryIndex(-1);
      setFiles([]);
      void initTree();
      return;
    }

    setInitialPathReady(false);
    setPathName("/");
    setFiles([]);
    void initTree();

    void (async () => {
      const initialPath = await resolveInitialRemotePath(() =>
        window.nextshell.session.getHomeDir({ connectionId: connection.id })
      );
      if (initialPathRequestIdRef.current !== requestId) {
        return;
      }
      setPathName(initialPath);
      setPathHistory([initialPath]);
      setHistoryIndex(0);
      setInitialPathReady(true);
    })();
  }, [connection?.id, connected, initTree]);

  useEffect(() => {
    pathNameRef.current = pathName;
  }, [pathName]);

  useEffect(() => {
    setPathInput(pathName);
  }, [pathName]);

  useEffect(() => {
    if (!connection || !connected) setFollowCwd(false);
  }, [connection?.id, connected]);

  useEffect(() => {
    if (!connection?.monitorSession) {
      setFollowCwd(false);
    }
  }, [connection?.id, connection?.monitorSession]);

  const followCwdTrackingEnabled = Boolean(
    active &&
      followCwd &&
      connection &&
      connected &&
      connection.monitorSession &&
      followSessionId
  );

  useEffect(() => {
    if (!followCwdTrackingEnabled) {
      if (followCwdDebounceRef.current) {
        clearTimeout(followCwdDebounceRef.current);
        followCwdDebounceRef.current = undefined;
      }
      followCwdLastRef.current = null;
      return;
    }

    return () => {
      if (followCwdDebounceRef.current) {
        clearTimeout(followCwdDebounceRef.current);
        followCwdDebounceRef.current = undefined;
      }
    };
  }, [followCwdTrackingEnabled, followSessionId]);

  useEffect(() => {
    if (!followCwdTrackingEnabled || !followSessionCwd) {
      return;
    }

    const normalized = normalizeRemotePath(followSessionCwd);
    if (normalized === followCwdLastRef.current) {
      return;
    }

    followCwdLastRef.current = normalized;
    if (followCwdDebounceRef.current) {
      clearTimeout(followCwdDebounceRef.current);
    }
    followCwdDebounceRef.current = setTimeout(() => {
      followCwdDebounceRef.current = undefined;
      if (pathNameRef.current !== normalized) {
        navigateRef.current(normalized);
      }
    }, FILE_EXPLORER_FOLLOW_CWD_DEBOUNCE_MS);

    return () => {
      if (followCwdDebounceRef.current) {
        clearTimeout(followCwdDebounceRef.current);
        followCwdDebounceRef.current = undefined;
      }
    };
  }, [followCwdTrackingEnabled, followSessionCwd]);

  useEffect(() => {
    if (!connection || !connected || !initialPathReady) {
      setFiles([]);
      setSelectedPaths([]);
      return;
    }
    void loadFiles();
  }, [connection?.id, connected, initialPathReady, loadFiles, pathName]);

  const handleTreeExpand = useCallback(
    async (keys: string[], info: { node: DirTreeNode; expanded: boolean }) => {
      setExpandedKeys(keys);
      if (!info.expanded) return;
      const node = info.node;
      if (node.children && node.children.length > 0) return;
      const children = await loadTreeChildren(node.key);
      setTreeData((prev) => updateTreeNode(prev, node.key, children));
    },
    [loadTreeChildren, updateTreeNode]
  );

  const toParentPath = useCallback((): void => {
    const normalized = normalizeRemotePath(pathName);
    if (normalized === "/") return;
    const next = normalized.slice(0, normalized.lastIndexOf("/")) || "/";
    navigate(next);
  }, [navigate, pathName]);

  const toggleFollowCwd = useCallback(() => {
    if (!connection?.monitorSession) {
      message.info({
        content: "该功能依赖监控会话开关，请先在服务器设置中启用监控会话。",
        duration: 3
      });
      return;
    }
    if (!followSessionId) {
      message.info({
        content: "当前连接暂无可跟随的远程终端。",
        duration: 2
      });
      return;
    }
    const nextFollowCwd = !followCwd;
    if (nextFollowCwd) {
      followCwdLastRef.current = null;
    }
    setFollowCwd(nextFollowCwd);
    message.info({
      content: nextFollowCwd ? "已启用跟随终端目录" : "已关闭跟随终端目录",
      duration: 2
    });
  }, [connection?.monitorSession, followCwd, followSessionId, message]);

  return {
    busy,
    expandedKeys,
    files,
    followCwd,
    goBack,
    goForward,
    handleTreeExpand,
    historyIndex,
    navigate,
    loadFiles,
    pathHistory,
    pathInput,
    pathName,
    selectedEntries,
    selectedPaths,
    setBusy,
    setFiles,
    setPathInput,
    setSelectedPaths,
    singleSelected,
    toParentPath,
    toggleFollowCwd,
    treeData
  };
};
