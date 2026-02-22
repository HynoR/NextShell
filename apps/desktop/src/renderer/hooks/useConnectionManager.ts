import { useCallback } from "react";
import { message } from "antd";
import type { ConnectionUpsertInput } from "@nextshell/shared";
import { useWorkspaceStore } from "../store/useWorkspaceStore";
import { formatErrorMessage } from "../utils/errorMessage";

export function useConnectionManager() {
  const {
    connections,
    activeConnectionId,
    setConnections,
    setActiveConnection,
    setMonitor,
    removeSessionsByConnection
  } = useWorkspaceStore();

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
        setMonitor(undefined);
      }
    } catch (error) {
      message.error(`加载连接失败：${formatErrorMessage(error, "请稍后重试")}`);
    }
  }, [activeConnectionId, setActiveConnection, setConnections, setMonitor]);

  const handleConnectionSaved = async (payload: ConnectionUpsertInput): Promise<void> => {
    try {
      await window.nextshell.connection.upsert(payload);
      const refreshed = await window.nextshell.connection.list({});
      setConnections(refreshed);
    } catch (error) {
      message.error(`保存连接失败：${formatErrorMessage(error, "请稍后重试")}`);
      void loadConnections();
    }
  };

  const handleConnectionRemoved = async (connectionId: string): Promise<void> => {
    const prevConnections = [...connections];
    setConnections(connections.filter((c) => c.id !== connectionId));
    removeSessionsByConnection(connectionId);

    try {
      await window.nextshell.connection.remove({ id: connectionId });
    } catch (error) {
      message.error(`删除连接失败：${formatErrorMessage(error, "请稍后重试")}`);
      setConnections(prevConnections);
    }
  };

  return { loadConnections, handleConnectionSaved, handleConnectionRemoved };
}
