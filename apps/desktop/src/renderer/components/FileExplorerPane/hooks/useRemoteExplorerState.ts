import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { App as AntdApp } from "antd";
import type { ConnectionProfile, RemoteFileEntry } from "@nextshell/core";
import {
  FILE_EXPLORER_FOLLOW_CWD_DEBOUNCE_MS,
  shouldSuppressFollowNavigation
} from "../../FileExplorerPane.follow";
import { formatErrorMessage } from "../../../utils/errorMessage";
import { resolveInitialRemotePath } from "../../../utils/remoteHomePath";
import { readLastSftpPath, writeLastSftpPath } from "../../../utils/sftpLastPath";
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

/**
 * 一个连接一个面板实例（WorkspaceLayout 按 connectionId 各挂一个 FileExplorerPane，
 * 非活动的 hidden，断开即卸载），所以 connectionId 在实例生命周期内不变：
 * 缓存/恢复/归属守卫/请求闸门这些切连接机制一概不需要，只剩冷启动定位、
 * 导航/历史、目录树和跟随终端。
 */
export const useRemoteExplorerState = ({
  connection,
  connected,
  active,
  followSessionId,
  followSessionCwd,
  message
}: UseRemoteExplorerStateParams) => {
  const connectionId = connection?.id;
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
  const treeInitRequestIdRef = useRef(0);
  // 单调递增的请求序号：快速连点目录时，后发的 list 天然作废弃先发的慢响应。
  const loadRequestIdRef = useRef(0);
  const followCwdLastRef = useRef<string | null>(null);
  const followCwdDebounceRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const navigateRef = useRef<(path: string) => void>(() => {});
  // 最近一次用户手动导航的时间戳,用于压制终端跟随的抢占。
  const manualNavAtRef = useRef<number | undefined>(undefined);
  const followNavigatingRef = useRef(false);
  // 冷启动探测上次目录时已拿到该目录的列表,紧随其后的加载效应跳过这次重复请求。
  const preloadedPathRef = useRef<string | undefined>(undefined);

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
      // 终端跟随触发的导航不算「用户手动浏览」,其余全部算。
      if (followNavigatingRef.current) {
        followNavigatingRef.current = false;
      } else {
        manualNavAtRef.current = Date.now();
      }
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
      pathNameRef.current = normalizedPath;
      setPathName(normalizedPath);
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
    manualNavAtRef.current = Date.now();
    skipHistoryRef.current = true;
    pathNameRef.current = prev;
    setHistoryIndex((index) => index - 1);
    setPathName(prev);
  }, [historyIndex, pathHistory]);

  const goForward = useCallback(() => {
    if (historyIndex >= pathHistory.length - 1) return;
    const next = pathHistory[historyIndex + 1];
    if (!next) return;
    manualNavAtRef.current = Date.now();
    skipHistoryRef.current = true;
    pathNameRef.current = next;
    setHistoryIndex((index) => index + 1);
    setPathName(next);
  }, [historyIndex, pathHistory]);

  const loadFiles = useCallback(async (): Promise<void> => {
    if (!connectionId || !connected || !initialPathReady) {
      loadRequestIdRef.current += 1;
      setBusy(false);
      setFiles([]);
      setSelectedPaths([]);
      return;
    }

    const normalizedPath = normalizeRemotePath(pathName);
    const requestId = ++loadRequestIdRef.current;
    const isCurrentRequest = (): boolean => loadRequestIdRef.current === requestId;

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
  }, [connectionId, connected, initialPathReady, message, pathName]);

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
    if (treeInitRequestIdRef.current !== requestId) {
      return;
    }
    setTreeData([{ key: "/", title: "/", isLeaf: false, children }]);
    setExpandedKeys(["/"]);
  }, [connectionId, connected, loadTreeChildren]);

  // 隐藏面板不抢着冷启动:N 台同时连就是 ~3N 次 SFTP 往返打给没打开过的面板,首次 active 后才拉。
  const [everActive, setEverActive] = useState(active);
  useEffect(() => {
    if (active) setEverActive(true);
  }, [active]);

  // 冷启动（实例挂载 / 重连后重新挂载）：清干净上一实例无关的初始值,
  // 重建目录树,并解析初始目录（上次浏览的目录 → home）。
  useEffect(() => {
    initialPathRequestIdRef.current += 1;
    const requestId = initialPathRequestIdRef.current;
    loadRequestIdRef.current += 1;

    // 选中项一律不跨会话沿用：陈旧选中会让删除/移动打在意料之外的文件上。
    setSelectedPaths([]);
    setBusy(false);
    skipHistoryRef.current = false;

    if (!connectionId || !connected || !everActive) return;

    setInitialPathReady(false);
    pathNameRef.current = "/";
    setPathName("/");
    setPathHistory([]);
    setHistoryIndex(-1);
    setFiles([]);
    setTreeData([]);
    setExpandedKeys([]);
    void initTree();

    void (async () => {
      // 优先回到该连接上次浏览的目录；目录已不存在（list 失败）则回退 home。
      let initialPath: string | undefined;
      // 探测成功时顺手把这份列表留下来当初始数据,省掉紧接着的重复 list。
      let probedFiles: RemoteFileEntry[] | undefined;
      const stored = readLastSftpPath(connectionId);
      const storedPath = stored ? normalizeRemotePath(stored) : undefined;
      if (storedPath) {
        try {
          probedFiles = await window.nextshell.sftp.list({
            connectionId,
            path: storedPath
          });
          initialPath = storedPath;
        } catch {
          initialPath = undefined;
          probedFiles = undefined;
        }
      }
      if (initialPath === undefined) {
        initialPath = await resolveInitialRemotePath(() =>
          window.nextshell.session.getHomeDir({ connectionId })
        );
      }
      const normalized = normalizeRemotePath(initialPath);
      if (initialPathRequestIdRef.current !== requestId) {
        return;
      }
      pathNameRef.current = normalized;
      if (probedFiles && normalized === storedPath) {
        // 探测那一次 list 就是这个目录的内容,直接当初始数据用,
        // 并跳过紧随其后的那次重复请求。
        setFiles(probedFiles);
        preloadedPathRef.current = normalized;
      }
      setPathName(normalized);
      setPathHistory([normalized]);
      setHistoryIndex(0);
      setInitialPathReady(true);
    })();
  }, [connectionId, connected, everActive, initTree]);

  // 记录该连接的落脚点,供下次挂载时恢复。
  useEffect(() => {
    if (!connectionId || !initialPathReady) return;
    writeLastSftpPath(connectionId, pathName);
  }, [connectionId, initialPathReady, pathName]);

  useEffect(() => {
    pathNameRef.current = pathName;
  }, [pathName]);

  useEffect(() => {
    setPathInput(pathName);
  }, [pathName]);

  const followCwdTrackingEnabled = Boolean(
    active && followCwd && connectionId && connected && followSessionId
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

    // 用户刚手动导航过,这次终端 cd 不抢占面板；下次 cd 再正常跟随。
    if (shouldSuppressFollowNavigation(manualNavAtRef.current, Date.now())) {
      return;
    }

    followCwdLastRef.current = normalized;
    if (followCwdDebounceRef.current) {
      clearTimeout(followCwdDebounceRef.current);
    }
    followCwdDebounceRef.current = setTimeout(() => {
      followCwdDebounceRef.current = undefined;
      // 防抖等待期间用户又动了面板,同样让位。
      if (shouldSuppressFollowNavigation(manualNavAtRef.current, Date.now())) {
        return;
      }
      if (pathNameRef.current !== normalized) {
        followNavigatingRef.current = true;
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
      loadRequestIdRef.current += 1;
      setBusy(false);
      setFiles([]);
      setSelectedPaths([]);
      return;
    }

    const normalizedPath = normalizeRemotePath(pathName);
    if (preloadedPathRef.current === normalizedPath) {
      preloadedPathRef.current = undefined;
      return;
    }

    void loadFiles();
  }, [connectionId, connected, initialPathReady, loadFiles, pathName]);

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
      // 用户主动开启跟随,立即生效,不受手动浏览压制期影响。
      manualNavAtRef.current = undefined;
    }
    setFollowCwd(nextFollowCwd);
    message.info({
      content: nextFollowCwd ? "已启用跟随终端目录" : "已关闭跟随终端目录",
      duration: 2
    });
  }, [followCwd, followSessionId, message]);

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
