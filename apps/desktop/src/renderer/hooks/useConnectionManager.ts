import { useCallback } from "react";
import { message } from "antd";
import type { ConnectionUpsertInput } from "@nextshell/shared";
import { useWorkspaceStore } from "../store/useWorkspaceStore";

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
      const reason = error instanceof Error ? error.message : "未知错误";
      message.error(`加载连接失败：${reason}`);
    }
  }, [activeConnectionId, setActiveConnection, setConnections, setMonitor]);

  const handleConnectionSaved = async (payload: ConnectionUpsertInput): Promise<void> => {
    try {
      await window.nextshell.connection.upsert(payload);
      const refreshed = await window.nextshell.connection.list({});
      setConnections(refreshed);
    } catch (error) {
      const reason = error instanceof Error ? error.message : "保存连接失败";
      message.error(reason);
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
      const reason = error instanceof Error ? error.message : "Failed to delete connection";
      message.error(reason);
      setConnections(prevConnections);
    }
  };

  return { loadConnections, handleConnectionSaved, handleConnectionRemoved };
}
