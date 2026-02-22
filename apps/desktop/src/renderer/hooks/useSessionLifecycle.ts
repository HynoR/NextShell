import { useCallback, useEffect, useRef, useState } from "react";
import { message } from "antd";
import type { ConnectionProfile, SessionDescriptor } from "@nextshell/core";
import { AUTH_REQUIRED_PREFIX, type SessionAuthOverrideInput } from "@nextshell/shared";
import { useWorkspaceStore } from "../store/useWorkspaceStore";
import { formatErrorMessage } from "../utils/errorMessage";
import {
  claimNextSessionIndex,
  formatSessionTitle,
  resolveSessionBaseTitle
} from "../utils/sessionTitle";

const extractAuthRequiredReason = (reason: string): string | undefined => {
  const index = reason.indexOf(AUTH_REQUIRED_PREFIX);
  if (index < 0) {
    return undefined;
  }
  return reason.slice(index);
};

const isAuthRequiredFailure = (reason: string): boolean =>
  extractAuthRequiredReason(reason) !== undefined;

type RetrySessionAuthResult =
  | { ok: true }
  | { ok: false; authRequired: boolean; reason: string };

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

  const refreshConnections = useCallback(() => {
    window.nextshell.connection.list({}).then((refreshed) => {
      setConnections(refreshed);
    }).catch((refreshError) => {
      message.warning(formatErrorMessage(refreshError, "刷新连接信息失败"));
    });
  }, [setConnections]);

  const finalizeSession = useCallback(
    (
      openedSession: SessionDescriptor,
      connection: ConnectionProfile | undefined,
      sessionIndex: number
    ): SessionDescriptor => {
      const openedBaseTitle = resolveSessionBaseTitle(openedSession.title, connection);
      const session = {
        ...openedSession,
        title: formatSessionTitle(openedBaseTitle, sessionIndex)
      };
      upsertSession(session);
      setActiveSession(session.id);
      refreshConnections();
      return session;
    },
    [refreshConnections, setActiveSession, upsertSession]
  );

  const finalizeRetriedSession = useCallback(
    (openedSession: SessionDescriptor, preservedTitle: string): SessionDescriptor => {
      const session = {
        ...openedSession,
        title: preservedTitle
      };
      upsertSession(session);
      setActiveSession(session.id);
      refreshConnections();
      return session;
    },
    [refreshConnections, setActiveSession, upsertSession]
  );

  useEffect(() => {
    const unsubscribe = window.nextshell.session.onStatus((event) => {
      setSessionStatus(event.sessionId, event.status, event.reason);
      if (event.status === "failed" && event.reason) {
        if (isAuthRequiredFailure(event.reason)) {
          return;
        }
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
          return finalizeSession(openedSession, connection, sessionIndex);
        } catch (error) {
          const rawReason = formatErrorMessage(error, "打开 SSH 会话失败");
          const reason = extractAuthRequiredReason(rawReason) ?? rawReason;
          setSessionStatus(sessionId, "failed", reason);
          return undefined;
        }
      } finally {
        setConnectingIds((prev) => {
          const next = new Set(prev);
          next.delete(connectionId);
          return next;
        });
      }

    },
    [
      connections,
      connectingIds,
      finalizeSession,
      setActiveConnection,
      setActiveSession,
      setSessionStatus,
      upsertSession
    ]
  );

  const retrySessionAuth = useCallback(
    async (
      sessionId: string,
      authOverride: SessionAuthOverrideInput
    ): Promise<RetrySessionAuthResult> => {
      const target = sessions.find((session) => session.id === sessionId);
      if (!target) {
        return {
          ok: false,
          authRequired: false,
          reason: "会话不存在或已关闭。"
        };
      }

      if (connectingIds.has(target.connectionId)) {
        return {
          ok: false,
          authRequired: false,
          reason: "连接正在建立，请稍后重试。"
        };
      }

      setConnectingIds((prev) => new Set(prev).add(target.connectionId));
      setActiveConnection(target.connectionId);
      setActiveSession(target.id);
      // Clear previous failure reason while retrying.
      setSessionStatus(target.id, "connecting", "");

      try {
        const openedSession = await window.nextshell.session.open({
          connectionId: target.connectionId,
          sessionId: target.id,
          authOverride
        });
        finalizeRetriedSession(openedSession, target.title);
        return { ok: true };
      } catch (error) {
        const rawReason = formatErrorMessage(error, "打开 SSH 会话失败");
        const reason = extractAuthRequiredReason(rawReason) ?? rawReason;
        const authRequired = isAuthRequiredFailure(reason);
        setSessionStatus(target.id, "failed", reason);
        return {
          ok: false,
          authRequired,
          reason
        };
      } finally {
        setConnectingIds((prev) => {
          const next = new Set(prev);
          next.delete(target.connectionId);
          return next;
        });
      }
    },
    [
      connectingIds,
      finalizeRetriedSession,
      sessions,
      setActiveConnection,
      setActiveSession,
      setSessionStatus
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
    retrySessionAuth,
    activateConnection,
    handleCloseSession,
    handleReconnectSession
  };
}
