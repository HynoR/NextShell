import { useCallback, useEffect, useRef, useState } from "react";
import { message } from "antd";
import type { ConnectionProfile, SessionDescriptor } from "@nextshell/core";
import { AUTH_REQUIRED_PREFIX } from "@nextshell/shared";
import { useWorkspaceStore } from "../store/useWorkspaceStore";
import { formatErrorMessage } from "../utils/errorMessage";
import {
  claimNextSessionIndex,
  formatSessionTitle,
  resolveSessionBaseTitle
} from "../utils/sessionTitle";

const isAuthRequiredFailure = (reason: string): boolean =>
  reason.startsWith(AUTH_REQUIRED_PREFIX);

const stripAuthRequiredPrefix = (reason: string): string =>
  isAuthRequiredFailure(reason) ? reason.slice(AUTH_REQUIRED_PREFIX.length) : reason;

export function useSessionLifecycle() {
  const {
    connections,
    setConnections,
    upsertSession,
    setSessionStatus,
    removeSession,
    setActiveSession,
    setActiveConnection,
    sessions,
    activeSessionId
  } = useWorkspaceStore();

  const [connectingIds, setConnectingIds] = useState<Set<string>>(new Set());
  const sessionIndexByConnectionRef = useRef<Map<string, number>>(new Map());

  useEffect(() => {
    const unsubscribe = window.nextshell.session.onStatus((event) => {
      setSessionStatus(event.sessionId, event.status);
      if (event.status === "failed" && event.reason) {
        message.error(formatErrorMessage(event.reason, "会话连接失败"));
      } else if (event.status === "connected" && event.reason) {
        message.warning(formatErrorMessage(event.reason, "会话状态已更新"));
      }
    });
    return () => { unsubscribe(); };
  }, [setSessionStatus]);

  const startSession = useCallback(
    async (connectionId: string) => {
      if (connectingIds.has(connectionId)) {
        return undefined;
      }
      setConnectingIds((prev) => new Set(prev).add(connectionId));
      setActiveConnection(connectionId);

      const findConnection = (): ConnectionProfile | undefined =>
        connections.find((item) => item.id === connectionId);

      try {
        const connection = findConnection();
        const now = new Date().toISOString();
        const sessionIndex = claimNextSessionIndex(sessionIndexByConnectionRef.current, connectionId);
        const sessionId = crypto.randomUUID();
        const baseTitle = resolveSessionBaseTitle(undefined, connection);
        const pendingSession: SessionDescriptor = {
          id: sessionId,
          connectionId,
          title: formatSessionTitle(baseTitle, sessionIndex),
          type: "terminal",
          status: "connecting",
          createdAt: now,
          reconnectable: true
        };
        upsertSession(pendingSession);
        setActiveSession(sessionId);

        try {
          const openedSession = await window.nextshell.session.open({
            connectionId,
            sessionId
          });
          return finalizeSession(openedSession, connection, sessionIndex, sessionId);
        } catch (error) {
          const reason = formatErrorMessage(error, "打开 SSH 会话失败");
          const displayReason = isAuthRequiredFailure(reason) ? stripAuthRequiredPrefix(reason) : reason;
          setSessionStatus(sessionId, "failed", displayReason);
          message.error(displayReason);
          return undefined;
        }
      } finally {
        setConnectingIds((prev) => {
          const next = new Set(prev);
          next.delete(connectionId);
          return next;
        });
      }

      function finalizeSession(
        openedSession: SessionDescriptor,
        connection: ConnectionProfile | undefined,
        sessionIndex: number,
        sessionId: string
      ): SessionDescriptor {
        const openedBaseTitle = resolveSessionBaseTitle(openedSession.title, connection);
        const session = {
          ...openedSession,
          title: formatSessionTitle(openedBaseTitle, sessionIndex)
        };
        upsertSession(session);
        setActiveSession(session.id);

        window.nextshell.connection.list({}).then((refreshed) => {
          setConnections(refreshed);
        }).catch((refreshError) => {
          message.warning(formatErrorMessage(refreshError, "刷新连接信息失败"));
        });

        return session;
      }
    },
    [
      connections,
      connectingIds,
      setActiveConnection,
      setActiveSession,
      setConnections,
      setSessionStatus,
      upsertSession
    ]
  );

  const activateConnection = useCallback(
    (connectionId: string) => {
      setActiveConnection(connectionId);
      const active = activeSessionId
        ? sessions.find((session) => session.id === activeSessionId)
        : undefined;
      if (!active || active.connectionId !== connectionId) {
        setActiveSession(undefined);
      }
    },
    [activeSessionId, sessions, setActiveConnection, setActiveSession]
  );

  const handleCloseSession = useCallback(
    async (sessionId: string) => {
      removeSession(sessionId);
      window.nextshell.session.close({ sessionId }).catch((error) => {
        message.warning(formatErrorMessage(error, "关闭会话失败"));
      });
    },
    [removeSession]
  );

  const handleReconnectSession = useCallback(
    async (sessionId: string) => {
      const target = sessions.find((session) => session.id === sessionId);
      if (!target) return;
      setSessionStatus(sessionId, "connecting");
      removeSession(sessionId);
      window.nextshell.session.close({ sessionId }).catch(() => {});
      await startSession(target.connectionId);
    },
    [removeSession, sessions, setSessionStatus, startSession]
  );

  return {
    connectingIds,
    startSession,
    activateConnection,
    handleCloseSession,
    handleReconnectSession
  };
}
