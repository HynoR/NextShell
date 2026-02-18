import { useCallback, useEffect, useRef, useState } from "react";
import { message } from "antd";
import type { ConnectionProfile, SessionDescriptor } from "@nextshell/core";
import type { SessionAuthOverrideInput } from "@nextshell/shared";
import { AUTH_REQUIRED_PREFIX } from "@nextshell/shared";
import { useWorkspaceStore } from "../store/useWorkspaceStore";
import {
  claimNextSessionIndex,
  formatSessionTitle,
  resolveSessionBaseTitle
} from "../utils/sessionTitle";

const MAX_SESSION_OPEN_ATTEMPTS = 3;

export interface AuthPromptState {
  attempt: number;
  maxAttempts: number;
  initialUsername?: string;
  defaultAuthType: SessionAuthOverrideInput["authType"];
  hasExistingPrivateKey: boolean;
}

const isAuthRequiredFailure = (reason: string): boolean =>
  reason.startsWith(AUTH_REQUIRED_PREFIX);

const stripAuthRequiredPrefix = (reason: string): string =>
  isAuthRequiredFailure(reason) ? reason.slice(AUTH_REQUIRED_PREFIX.length) : reason;

const resolveDefaultAuthType = (connection?: ConnectionProfile): SessionAuthOverrideInput["authType"] =>
  connection?.authType === "privateKey" ? "privateKey" : "password";

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
  const [authPromptState, setAuthPromptState] = useState<AuthPromptState>();
  const authPromptResolveRef = useRef<((value: SessionAuthOverrideInput | undefined) => void) | undefined>(undefined);
  const sessionIndexByConnectionRef = useRef<Map<string, number>>(new Map());

  const requestAuthOverride = useCallback((state: AuthPromptState) => {
    return new Promise<SessionAuthOverrideInput | undefined>((resolve) => {
      authPromptResolveRef.current = resolve;
      setAuthPromptState(state);
    });
  }, []);

  const resolveAuthPrompt = useCallback((value: SessionAuthOverrideInput | undefined) => {
    const resolve = authPromptResolveRef.current;
    authPromptResolveRef.current = undefined;
    setAuthPromptState(undefined);
    resolve?.(value);
  }, []);

  // Clean up pending auth promise on unmount
  useEffect(() => {
    return () => {
      const resolve = authPromptResolveRef.current;
      authPromptResolveRef.current = undefined;
      resolve?.(undefined);
    };
  }, []);

  // Listen for session status events
  useEffect(() => {
    const unsubscribe = window.nextshell.session.onStatus((event) => {
      setSessionStatus(event.sessionId, event.status);
      if (event.status === "failed" && event.reason) {
        if (!isAuthRequiredFailure(event.reason)) {
          message.error(event.reason);
        }
      } else if (event.status === "connected" && event.reason) {
        message.warning(event.reason);
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

        let authOverride: SessionAuthOverrideInput | undefined;
        for (let attempt = 1; attempt <= MAX_SESSION_OPEN_ATTEMPTS; attempt += 1) {
          try {
            const openedSession = await window.nextshell.session.open({
              connectionId,
              sessionId,
              authOverride
            });
            const openedBaseTitle = resolveSessionBaseTitle(openedSession.title, connection);
            const session = {
              ...openedSession,
              title: formatSessionTitle(openedBaseTitle, sessionIndex)
            };
            upsertSession(session);
            setActiveSession(session.id);

            if (attempt > 1) {
              try {
                const refreshedConnections = await window.nextshell.connection.list({});
                setConnections(refreshedConnections);
              } catch (refreshError) {
                const reason = refreshError instanceof Error ? refreshError.message : "刷新连接信息失败";
                message.warning(reason);
              }
            }

            return session;
          } catch (error) {
            const reason = error instanceof Error ? error.message : "Failed to open SSH session";
            const displayReason = stripAuthRequiredPrefix(reason);
            const isAuthFailure = isAuthRequiredFailure(reason);

            if (!isAuthFailure || attempt >= MAX_SESSION_OPEN_ATTEMPTS) {
              setSessionStatus(sessionId, "failed");
              message.error(displayReason);
              return undefined;
            }

            const currentConnection = findConnection();
            const nextInitialUsername = authOverride?.username ?? currentConnection?.username;
            const hasExistingPrivateKey = Boolean(
              currentConnection?.privateKeyPath ||
              currentConnection?.privateKeyRef ||
              (authOverride?.authType === "privateKey" &&
                (authOverride.privateKeyPath || authOverride.privateKeyContent))
            );
            const nextAuthType = authOverride?.authType ?? resolveDefaultAuthType(currentConnection);

            const nextAuthOverride = await requestAuthOverride({
              attempt: attempt + 1,
              maxAttempts: MAX_SESSION_OPEN_ATTEMPTS,
              initialUsername: nextInitialUsername,
              defaultAuthType: nextAuthType,
              hasExistingPrivateKey
            });

            if (!nextAuthOverride) {
              removeSession(sessionId);
              return undefined;
            }

            const mergedAuthOverride: SessionAuthOverrideInput =
              nextAuthOverride.authType === "privateKey" && authOverride?.authType === "privateKey"
                ? {
                    ...nextAuthOverride,
                    privateKeyPath: nextAuthOverride.privateKeyPath ?? authOverride.privateKeyPath,
                    privateKeyContent: nextAuthOverride.privateKeyContent ?? authOverride.privateKeyContent
                  }
                : nextAuthOverride;

            authOverride = mergedAuthOverride;
            setSessionStatus(sessionId, "connecting");
          }
        }

        setSessionStatus(sessionId, "failed");
        return undefined;
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
      removeSession,
      requestAuthOverride,
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
        const reason = error instanceof Error ? error.message : "Failed to close session";
        message.warning(reason);
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

  const handleAuthPromptCancel = useCallback(() => {
    resolveAuthPrompt(undefined);
  }, [resolveAuthPrompt]);

  const handleAuthPromptSubmit = useCallback(async (payload: SessionAuthOverrideInput) => {
    resolveAuthPrompt(payload);
  }, [resolveAuthPrompt]);

  return {
    connectingIds,
    authPromptState,
    startSession,
    activateConnection,
    handleCloseSession,
    handleReconnectSession,
    handleAuthPromptCancel,
    handleAuthPromptSubmit,
    MAX_SESSION_OPEN_ATTEMPTS
  };
}
