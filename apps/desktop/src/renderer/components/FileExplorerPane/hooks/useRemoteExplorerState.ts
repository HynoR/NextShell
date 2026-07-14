import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { App as AntdApp } from "antd";
import type { ConnectionProfile, RemoteFileEntry } from "@nextshell/core";
import { FILE_EXPLORER_FOLLOW_CWD_DEBOUNCE_MS } from "../../FileExplorerPane.follow";
import { formatErrorMessage } from "../../../utils/errorMessage";
import { resolveInitialRemotePath } from "../../../utils/remoteHomePath";
import { createRemoteExplorerRequestGate } from "../requestGate";
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
  const connectionId = connection?.id;
  const fileRequestGate = useMemo(() => createRemoteExplorerRequestGate(), []);
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
  const connectionIdRef = useRef(connectionId);
  const initialPathRequestIdRef = useRef(0);
  const treeInitRequestIdRef = useRef(0);
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
      const normalizedPath = normalizeRemotePath(path);
      if (pathNameRef.current === normalizedPath) {
        skipHistoryRef.current = false;
        return;
      }

      if (skipHistoryRef.current) {
        skipHistoryRef.current = false;
      } else {
        pushHistory(normalizedPath);
      }
      fileRequestGate.invalidate();
      pathNameRef.current = normalizedPath;
      setPathName(normalizedPath);
    },
    [fileRequestGate, pushHistory]
  );

  useEffect(() => {
    navigateRef.current = navigate;
  }, [navigate]);

  const goBack = useCallback(() => {
    if (historyIndex <= 0) return;
    const prev = pathHistory[historyIndex - 1];
    if (!prev) return;
    skipHistoryRef.current = true;
    fileRequestGate.invalidate();
    pathNameRef.current = prev;
    setHistoryIndex((index) => index - 1);
    setPathName(prev);
  }, [fileRequestGate, historyIndex, pathHistory]);

  const goForward = useCallback(() => {
    if (historyIndex >= pathHistory.length - 1) return;
    const next = pathHistory[historyIndex + 1];
    if (!next) return;
    skipHistoryRef.current = true;
    fileRequestGate.invalidate();
    pathNameRef.current = next;
    setHistoryIndex((index) => index + 1);
    setPathName(next);
  }, [fileRequestGate, historyIndex, pathHistory]);

  const loadFiles = useCallback(async (): Promise<void> => {
    if (!connectionId || !connected || !initialPathReady) {
      fileRequestGate.invalidate();
      setBusy(false);
      setFiles([]);
      setSelectedPaths([]);
      return;
    }

    const normalizedPath = normalizeRemotePath(pathName);
    const request = fileRequestGate.begin(connectionId, normalizedPath);
    const isCurrentRequest = (): boolean =>
      fileRequestGate.isCurrent(request, {
        connectionId: connectionIdRef.current,
        path: normalizeRemotePath(pathNameRef.current)
      });

    setBusy(true);
    try {
      const list = await window.nextshell.sftp.list({
        connectionId,
        path: normalizedPath
      });
      if (!isCurrentRequest()) {
        return;
      }
      setFiles(list);
      setSelectedPaths([]);
    } catch (error) {
      if (!isCurrentRequest()) {
        return;
      }
      message.error(`读取目录失败：${formatErrorMessage(error, "请检查连接状态")}`);
      setFiles([]);
    } finally {
      if (isCurrentRequest()) {
        setBusy(false);
      }
    }
  }, [connectionId, connected, fileRequestGate, initialPathReady, message, pathName]);

  const loadTreeChildren = useCallback(
    async (parentPath: string): Promise<DirTreeNode[]> => {
      if (!connectionId || !connected) return [];
      try {
        const list = await window.nextshell.sftp.list({
          connectionId,
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
    [connectionId, connected]
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
    treeInitRequestIdRef.current += 1;
    const requestId = treeInitRequestIdRef.current;

    if (!connectionId || !connected) {
      setTreeData([]);
      setExpandedKeys([]);
      return;
    }
    const children = await loadTreeChildren("/");
    if (treeInitRequestIdRef.current !== requestId || connectionIdRef.current !== connectionId) {
      return;
    }
    setTreeData([{ key: "/", title: "/", isLeaf: false, children }]);
    setExpandedKeys(["/"]);
  }, [connectionId, connected, loadTreeChildren]);

  useEffect(() => {
    initialPathRequestIdRef.current += 1;
    const requestId = initialPathRequestIdRef.current;
    fileRequestGate.invalidate();

    setSelectedPaths([]);
    setBusy(false);
    skipHistoryRef.current = false;

    if (!connectionId || !connected) {
      setInitialPathReady(false);
      pathNameRef.current = "/";
      setPathName("/");
      setPathHistory([]);
      setHistoryIndex(-1);
      setFiles([]);
      void initTree();
      return;
    }

    setInitialPathReady(false);
    pathNameRef.current = "/";
    setPathName("/");
    setFiles([]);
    void initTree();

    void (async () => {
      const initialPath = normalizeRemotePath(
        await resolveInitialRemotePath(() => window.nextshell.session.getHomeDir({ connectionId }))
      );
      if (initialPathRequestIdRef.current !== requestId) {
        return;
      }
      pathNameRef.current = initialPath;
      setPathName(initialPath);
      setPathHistory([initialPath]);
      setHistoryIndex(0);
      setInitialPathReady(true);
    })();
  }, [connectionId, connected, fileRequestGate, initTree]);

  useEffect(() => {
    connectionIdRef.current = connectionId;
  }, [connectionId]);

  useEffect(() => {
    pathNameRef.current = pathName;
  }, [pathName]);

  useEffect(() => {
    setPathInput(pathName);
  }, [pathName]);

  useEffect(() => {
    if (!connectionId || !connected) setFollowCwd(false);
  }, [connectionId, connected]);

  useEffect(() => {
    if (!connection?.monitorSession) {
      setFollowCwd(false);
    }
  }, [connectionId, connection?.monitorSession]);

  const followCwdTrackingEnabled = Boolean(
    active &&
    followCwd &&
    connectionId &&
    connected &&
    connection?.monitorSession &&
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
    if (!connectionId || !connected || !initialPathReady) {
      fileRequestGate.invalidate();
      setBusy(false);
      setFiles([]);
      setSelectedPaths([]);
      return;
    }
    void loadFiles();
  }, [connectionId, connected, fileRequestGate, initialPathReady, loadFiles, pathName]);

  const findNodeByKey = (nodes: DirTreeNode[], key: string): DirTreeNode | undefined => {
    for (const node of nodes) {
      if (node.key === key) return node;
      if (node.children) {
        const found = findNodeByKey(node.children, key);
        if (found) return found;
      }
    }
    return undefined;
  };

  // 导航后自动把目录树展开/加载到当前路径,使高亮项可见(修复树停在 / 而列表已切换的割裂)
  useEffect(() => {
    if (!connectionId || !connected || !initialPathReady || pathName === "/") {
      return;
    }

    const expandTreeToPath = async (): Promise<void> => {
      const normalized = normalizeRemotePath(pathName);
      const parts = normalized.split("/").filter(Boolean);
      const ancestorPaths: string[] = ["/"];
      for (let i = 0; i < parts.length; i++) {
        ancestorPaths.push("/" + parts.slice(0, i + 1).join("/"));
      }

      let currentTreeData = treeData;
      const newExpandedKeys = expandedKeys.slice();
      let needsUpdate = false;

      // 只展开祖先节点(不含叶子本身),逐级按需懒加载子节点
      for (let i = 0; i < ancestorPaths.length - 1; i++) {
        const ancestorPath = ancestorPaths[i];
        if (!ancestorPath) continue;

        if (!newExpandedKeys.includes(ancestorPath)) {
          newExpandedKeys.push(ancestorPath);
          needsUpdate = true;
        }

        const node = findNodeByKey(currentTreeData, ancestorPath);
        if (node && (!node.children || node.children.length === 0)) {
          const children = await loadTreeChildren(ancestorPath);
          if (connectionIdRef.current !== connectionId) return;
          currentTreeData = updateTreeNode(currentTreeData, ancestorPath, children);
          needsUpdate = true;
        }
      }

      if (needsUpdate) {
        setTreeData(currentTreeData);
        setExpandedKeys(newExpandedKeys);
      }
    };

    void expandTreeToPath();
  }, [
    connectionId,
    connected,
    initialPathReady,
    pathName,
    treeData,
    expandedKeys,
    loadTreeChildren,
    updateTreeNode
  ]);

  const handleTreeExpand = useCallback(
    async (keys: string[], info: { node: DirTreeNode; expanded: boolean }) => {
      setExpandedKeys(keys);
      if (!info.expanded) return;
      const node = info.node;
      if (node.children && node.children.length > 0) return;
      const requestConnectionId = connectionId;
      const children = await loadTreeChildren(node.key);
      if (connectionIdRef.current !== requestConnectionId) return;
      setTreeData((prev) => updateTreeNode(prev, node.key, children));
    },
    [connectionId, loadTreeChildren, updateTreeNode]
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
