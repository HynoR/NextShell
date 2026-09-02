import { useCallback } from "react";
import { App as AntdApp } from "antd";
import type { ConnectionUpsertInput } from "@nextshell/shared";
import { useWorkspaceStore } from "../store/useWorkspaceStore";
import { formatErrorMessage } from "../utils/errorMessage";
import { persistConnectionWrite } from "./useConnectionManager.helpers";

export function useConnectionManager() {
  const { message } = AntdApp.useApp();
  const connections = useWorkspaceStore((state) => state.connections);
  const activeConnectionId = useWorkspaceStore((state) => state.activeConnectionId);
  const setConnections = useWorkspaceStore((state) => state.setConnections);
  const setActiveConnection = useWorkspaceStore((state) => state.setActiveConnection);
  const removeMonitorSnapshot = useWorkspaceStore((state) => state.removeMonitorSnapshot);
  const removeSessionsByConnection = useWorkspaceStore((state) => state.removeSessionsByConnection);

  const loadConnections = useCallback(async () => {
    try {
      const list = await window.nextshell.connection.list({});
      setConnections(list);
      const first = list[0];

      if (!activeConnectionId && first) {
        setActiveConnection(first.id);
        return;
      }

      if (activeConnectionId && !list.some((connection) => connection.id === activeConnectionId)) {
        setActiveConnection(first?.id);
        // The connection is gone (deleted elsewhere / cloud sync): its cached
        // monitor snapshot can never be refreshed, so drop just that entry.
        removeMonitorSnapshot(activeConnectionId);
      }
    } catch (error) {
      message.error(`加载连接失败：${formatErrorMessage(error, "请稍后重试")}`);
    }
  }, [activeConnectionId, setActiveConnection, setConnections, removeMonitorSnapshot]);

  const handleConnectionSaved = async (payload: ConnectionUpsertInput): Promise<void> => {
    await persistConnectionWrite(async () => {
      await window.nextshell.connection.upsert(payload);
      const refreshed = await window.nextshell.connection.list({});
      setConnections(refreshed);
    }, loadConnections);
  };

  const handleConnectionRemoved = async (connectionId: string): Promise<void> => {
    const prevConnections = [...connections];
    setConnections(connections.filter((c) => c.id !== connectionId));
    removeSessionsByConnection(connectionId);
    // 侧栏系统监控那份缓存快照一起清掉。
    removeMonitorSnapshot(connectionId);

    try {
      await window.nextshell.connection.remove({ id: connectionId });
    } catch (error) {
      message.error(`删除连接失败：${formatErrorMessage(error, "请稍后重试")}`);
      setConnections(prevConnections);
      throw error;
    }
  };

  return { loadConnections, handleConnectionSaved, handleConnectionRemoved };
}
