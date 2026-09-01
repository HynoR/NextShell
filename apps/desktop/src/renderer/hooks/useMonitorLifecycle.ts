import { useCallback, useEffect } from "react";
import { App as AntdApp } from "antd";
import type { SessionDescriptor } from "@nextshell/core";
import { useWorkspaceStore } from "../store/useWorkspaceStore";
import { formatErrorMessage } from "../utils/errorMessage";

export function useMonitorLifecycle(
  activeConnectionId: string | undefined,
  monitorSessionEnabled: boolean | undefined,
  isActiveConnectionTerminalConnected: boolean,
  sessions: SessionDescriptor[]
) {
  const { message } = AntdApp.useApp();
  const setMonitorSnapshot = useWorkspaceStore((state) => state.setMonitorSnapshot);
  const appendNetworkRate = useWorkspaceStore((state) => state.appendNetworkRate);
  const removeSession = useWorkspaceStore((state) => state.removeSession);
  // Receive system monitor snapshots.
  //
  // Every snapshot is stored under its own connectionId — no active-connection
  // filter. Snapshots of the connection we just switched away from used to be
  // thrown away, which is precisely why coming back showed an empty panel; the
  // store keeps one entry per connection and the rate history is already keyed
  // by (connectionId, iface). Not depending on activeConnectionId also keeps
  // this subscription alive across switches instead of re-registering it.
  useEffect(() => {
    const unsubscribe = window.nextshell.monitor.onSystemData((snapshot) => {
      setMonitorSnapshot(snapshot);
      if (snapshot.networkInterface) {
        appendNetworkRate(snapshot.connectionId, snapshot.networkInterface, {
          inMbps: snapshot.networkInMbps,
          outMbps: snapshot.networkOutMbps,
          capturedAt: snapshot.capturedAt
        });
      }
    });
    return () => {
      unsubscribe();
    };
  }, [setMonitorSnapshot, appendNetworkRate]);

  // Start/stop system monitor when connection or terminal status changes.
  //
  // The sidebar follows the active connection; mount starts its monitor and
  // unmount stops it.
  useEffect(() => {
    if (!activeConnectionId) {
      return;
    }

    const shouldStartSystemMonitor = Boolean(
      monitorSessionEnabled && isActiveConnectionTerminalConnected
    );

    if (!shouldStartSystemMonitor) {
      void window.nextshell.monitor
        .stopSystem({ connectionId: activeConnectionId })
        .catch(() => {});
      return;
    }

    let disposed = false;
    void window.nextshell.monitor
      .startSystem({ connectionId: activeConnectionId })
      .catch((error) => {
        if (disposed) return;
        message.error(`启动系统监控失败：${formatErrorMessage(error, "请检查连接状态")}`);
      });

    return () => {
      disposed = true;
      void window.nextshell.monitor
        .stopSystem({ connectionId: activeConnectionId })
        .catch(() => {});
    };
  }, [monitorSessionEnabled, activeConnectionId, isActiveConnectionTerminalConnected]);

  // Remove stale monitor sessions when their terminal disconnects
  useEffect(() => {
    const connectedTerminalConnectionIds = new Set(
      sessions
        .filter((session) => session.type === "terminal" && session.status === "connected")
        .map((session) => session.connectionId)
    );

    const staleMonitorSessionIds = sessions
      .filter(
        (session) =>
          (session.type === "processManager" || session.type === "networkMonitor") &&
          !connectedTerminalConnectionIds.has(session.connectionId)
      )
      .map((session) => session.id);

    if (staleMonitorSessionIds.length === 0) return;

    staleMonitorSessionIds.forEach((sessionId) => {
      removeSession(sessionId);
    });
  }, [sessions, removeSession]);

  /**
   * Focus (or create) the process/network monitor tab of a connection.
   *
   * Reads and dispatches through the workspace store itself — callers used to
   * have to thread `connections`, `setActiveSession`, `setActiveConnection` and
   * `upsertSession` in on every call, which put store internals in their
   * dependency arrays for no reason.
   */
  const openMonitorTab = useCallback(
    (connectionId: string, type: "processManager" | "networkMonitor") => {
      const store = useWorkspaceStore.getState();

      const connection = store.connections.find((item) => item.id === connectionId);
      if (!connection?.monitorSession) {
        message.warning("当前连接未启用监控会话。");
        return;
      }

      const hasConnectedTerminal = store.sessions.some(
        (session) =>
          session.connectionId === connectionId &&
          session.type === "terminal" &&
          session.status === "connected"
      );

      if (!hasConnectedTerminal) {
        message.warning("请先连接 SSH 终端，再启动监控会话。");
        return;
      }

      const existing = store.sessions.find(
        (session) => session.connectionId === connectionId && session.type === type
      );
      if (existing) {
        store.setActiveSession(existing.id);
        store.setActiveConnection(connectionId);
        return;
      }

      const name = connection.name || connection.host || "Server";
      const suffix = type === "processManager" ? "进程管理器" : "网络管理器";
      const now = new Date().toISOString();

      const session: SessionDescriptor = {
        id: crypto.randomUUID(),
        target: "remote",
        connectionId,
        title: `${name}(${suffix})`,
        type,
        status: "connected",
        createdAt: now,
        reconnectable: false
      };

      store.upsertSession(session);
      store.setActiveSession(session.id);
      store.setActiveConnection(connectionId);
    },
    [message]
  );

  return { openMonitorTab };
}
