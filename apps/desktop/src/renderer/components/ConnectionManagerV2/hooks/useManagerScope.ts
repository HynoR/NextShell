import { useCallback, useEffect, useMemo, useState } from "react";
import type { CloudSyncWorkspaceProfile, ConnectionFolder } from "@nextshell/core";
import { formatErrorMessage } from "../../../utils/errorMessage";
import { buildManagerScopes, resolveActiveScope, type ManagerScope } from "../utils/scopes";
import { reconcileCurrentFolder } from "../utils/folderNavigation";

interface UseManagerScopeOptions {
  open: boolean;
  onError: (message: string) => void;
}

export interface ManagerScopeState {
  scopes: ManagerScope[];
  activeScope: ManagerScope;
  selectScope: (key: string) => void;
  folders: ConnectionFolder[];
  reloadFolders: () => Promise<void>;
  currentFolderId?: string;
  enterFolder: (folderId: string | undefined) => void;
}

/**
 * 作用域与目录的加载。切作用域时目录必须一起换,而且当前目录要清空——目录 id 是按 scope 分区
 * 的,拿旧 id 去新作用域查只会得到一个空列表,看起来像连接丢了。
 */
export const useManagerScope = ({ open, onError }: UseManagerScopeOptions): ManagerScopeState => {
  const [workspaces, setWorkspaces] = useState<CloudSyncWorkspaceProfile[]>([]);
  const [selectedScopeKey, setSelectedScopeKey] = useState<string>();
  const [folders, setFolders] = useState<ConnectionFolder[]>([]);
  const [currentFolderId, setCurrentFolderId] = useState<string>();

  const scopes = useMemo(() => buildManagerScopes(workspaces), [workspaces]);
  const activeScope = useMemo(
    () => resolveActiveScope(scopes, selectedScopeKey),
    [scopes, selectedScopeKey]
  );

  useEffect(() => {
    if (!open) {
      return undefined;
    }
    const load = () => {
      window.nextshell.cloudSync
        .workspaceList()
        .then(setWorkspaces)
        .catch(() => setWorkspaces([]));
    };
    load();
    const unsubscribeStatus = window.nextshell.cloudSync.onStatus(load);
    const unsubscribeApplied = window.nextshell.cloudSync.onApplied(load);
    return () => {
      unsubscribeStatus();
      unsubscribeApplied();
    };
  }, [open]);

  const reloadFolders = useCallback(async () => {
    if (!open) {
      return;
    }
    try {
      setFolders(await window.nextshell.connectionFolder.list({ scopeKey: activeScope.key }));
    } catch (error) {
      onError(`加载目录失败：${formatErrorMessage(error, "请稍后重试")}`);
      setFolders([]);
    }
  }, [activeScope.key, onError, open]);

  useEffect(() => {
    void reloadFolders();
  }, [reloadFolders]);

  // 目录被删除、或作用域切换后 id 不再属于当前域时，回到根。
  useEffect(() => {
    setCurrentFolderId((previous) => reconcileCurrentFolder(previous, folders));
  }, [folders]);

  const selectScope = useCallback((key: string) => {
    setSelectedScopeKey(key);
    setCurrentFolderId(undefined);
    setFolders([]);
  }, []);

  // 必须 memo:调用方把整个返回值当 effect 依赖(外部定位那条就是),每次渲染新建一个对象
  // 会让那些 effect 每帧都跑一遍——轻则白跑,重则和自己的 setState 组成重渲循环。
  return useMemo(
    () => ({
      scopes,
      activeScope,
      selectScope,
      folders,
      reloadFolders,
      currentFolderId,
      enterFolder: setCurrentFolderId
    }),
    [activeScope, currentFolderId, folders, reloadFolders, scopes, selectScope]
  );
};
