import { useCallback, useEffect } from "react";
import { message } from "antd";
import type { SessionDescriptor } from "@nextshell/core";
import { useWorkspaceStore } from "../store/useWorkspaceStore";

export function useMonitorLifecycle(
  activeConnectionId: string | undefined,
  monitorSessionEnabled: boolean | undefined,
  isActiveConnectionTerminalConnected: boolean,
  sessions: SessionDescriptor[]
) {
  const { setMonitor, removeSession } = useWorkspaceStore();

  // Receive system monitor snapshots
  useEffect(() => {
    const unsubscribe = window.nextshell.monitor.onSystemData((snapshot) => {
      if (snapshot.connectionId === activeConnectionId) {
        setMonitor(snapshot);
      }
    });
    return () => { unsubscribe(); };
  }, [activeConnectionId, setMonitor]);

  // Start/stop system monitor when connection or terminal status changes
  useEffect(() => {
    if (!activeConnectionId) {
      setMonitor(undefined);
      return;
    }

    const shouldStartSystemMonitor = Boolean(monitorSessionEnabled && isActiveConnectionTerminalConnected);

    if (!shouldStartSystemMonitor) {
      setMonitor(undefined);
      void window.nextshell.monitor.stopSystem({ connectionId: activeConnectionId }).catch(() => {});
      return;
    }

    let disposed = false;
    void window.nextshell.monitor.startSystem({ connectionId: activeConnectionId }).catch((error) => {
      if (disposed) return;
      const reason = error instanceof Error ? error.message : "启动系统监控失败";
      message.error(reason);
      setMonitor(undefined);
    });

    return () => {
      disposed = true;
      void window.nextshell.monitor.stopSystem({ connectionId: activeConnectionId }).catch(() => {});
    };
  }, [monitorSessionEnabled, activeConnectionId, isActiveConnectionTerminalConnected, setMonitor]);

  // Remove stale monitor sessions when their terminal disconnects
  useEffect(() => {
    const connectedTerminalConnectionIds = new Set(
      sessions
        .filter((session) => session.type === "terminal" && session.status === "connected")
        .map((session) => session.connectionId)
    );

    const staleMonitorSessionIds = sessions
      .filter((session) =>
        (session.type === "processManager" || session.type === "networkMonitor") &&
        !connectedTerminalConnectionIds.has(session.connectionId)
      )
      .map((session) => session.id);

    if (staleMonitorSessionIds.length === 0) return;

    staleMonitorSessionIds.forEach((sessionId) => { removeSession(sessionId); });
  }, [sessions, removeSession]);

  const openMonitorTab = useCallback(
    (
      connectionId: string,
      type: "processManager" | "networkMonitor",
      connections: { id: string; name?: string; host?: string; monitorSession?: boolean }[],
      setActiveSession: (id: string) => void,
      setActiveConnection: (id: string) => void,
      upsertSession: (session: SessionDescriptor) => void
    ) => {
      const connection = connections.find((c) => c.id === connectionId);
      if (!connection?.monitorSession) {
        message.warning("当前连接未启用 Monitor Session。");
        return;
      }

      const hasConnectedTerminal = sessions.some(
        (session) =>
          session.connectionId === connectionId &&
          session.type === "terminal" &&
          session.status === "connected"
      );

      if (!hasConnectedTerminal) {
        message.warning("请先连接 SSH 终端以启动 Monitor Session。");
        return;
      }

      const existing = sessions.find((s) => s.connectionId === connectionId && s.type === type);
      if (existing) {
        setActiveSession(existing.id);
        setActiveConnection(connectionId);
        return;
      }

      const name = connection.name || connection.host || "Server";
      const suffix = type === "processManager" ? "进程管理器" : "网络";
      const now = new Date().toISOString();

      const session: SessionDescriptor = {
        id: crypto.randomUUID(),
        connectionId,
        title: `${name}(${suffix})`,
        type,
        status: "connected",
        createdAt: now,
        reconnectable: false
      };

      upsertSession(session);
      setActiveSession(session.id);
      setActiveConnection(connectionId);
    },
    [sessions]
  );

  return { openMonitorTab };
}
