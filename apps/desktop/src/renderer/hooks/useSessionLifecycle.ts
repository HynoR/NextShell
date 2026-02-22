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

const MAX_AUTH_RETRIES = 3;

export interface AuthPromptState {
  attempt: number;
  maxAttempts: number;
  initialUsername?: string;
  defaultAuthType: SessionAuthOverrideInput["authType"];
  hasExistingPrivateKey: boolean;
  failureReason?: string;
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

  useEffect(() => {
    return () => {
      const resolve = authPromptResolveRef.current;
      authPromptResolveRef.current = undefined;
      resolve?.(undefined);
    };
  }, []);

  useEffect(() => {
    const unsubscribe = window.nextshell.session.onStatus((event) => {
      setSessionStatus(event.sessionId, event.status);
      if (event.status === "failed" && event.reason) {
        message.error(event.reason);
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

        // ── 1. First attempt: use stored credentials ──────────────────
        let authOverride: SessionAuthOverrideInput | undefined;
        let lastFailureReason: string | undefined;

        try {
          const openedSession = await window.nextshell.session.open({
            connectionId,
            sessionId
          });
          return finalizeSession(openedSession, connection, sessionIndex, sessionId);
        } catch (error) {
          const reason = error instanceof Error ? error.message : "Failed to open SSH session";
          if (!isAuthRequiredFailure(reason)) {
            setSessionStatus(sessionId, "failed");
            return undefined;
          }
          lastFailureReason = stripAuthRequiredPrefix(reason);
        }

        // ── 2. Auth retry loop: up to MAX_AUTH_RETRIES user attempts ──
        for (let attempt = 1; attempt <= MAX_AUTH_RETRIES; attempt += 1) {
          const currentConnection = findConnection();
          const nextInitialUsername = authOverride?.username ?? currentConnection?.username;
          const hasExistingPrivateKey = Boolean(
            currentConnection?.sshKeyId ||
            (authOverride?.authType === "privateKey" &&
              (authOverride.sshKeyId || authOverride.privateKeyContent))
          );
          const nextAuthType = authOverride?.authType ?? resolveDefaultAuthType(currentConnection);

          const nextAuthOverride = await requestAuthOverride({
            attempt,
            maxAttempts: MAX_AUTH_RETRIES,
            initialUsername: nextInitialUsername,
            defaultAuthType: nextAuthType,
            hasExistingPrivateKey,
            failureReason: lastFailureReason
          });

          if (!nextAuthOverride) {
            removeSession(sessionId);
            return undefined;
          }

          authOverride =
            nextAuthOverride.authType === "privateKey" && authOverride?.authType === "privateKey"
              ? {
                  ...nextAuthOverride,
                  sshKeyId: nextAuthOverride.sshKeyId ?? authOverride.sshKeyId,
                  privateKeyContent: nextAuthOverride.privateKeyContent ?? authOverride.privateKeyContent
                }
              : nextAuthOverride;

          setSessionStatus(sessionId, "connecting");

          try {
            const openedSession = await window.nextshell.session.open({
              connectionId,
              sessionId,
              authOverride
            });
            return finalizeSession(openedSession, connection, sessionIndex, sessionId);
          } catch (error) {
            const reason = error instanceof Error ? error.message : "Failed to open SSH session";
            if (!isAuthRequiredFailure(reason)) {
              setSessionStatus(sessionId, "failed");
              return undefined;
            }
            lastFailureReason = stripAuthRequiredPrefix(reason);
          }
        }

        // ── 3. Exhausted all retries ──────────────────────────────────
        setSessionStatus(sessionId, "failed");
        message.error(lastFailureReason ?? "认证失败，已达最大重试次数。");
        return undefined;
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
          const reason = refreshError instanceof Error ? refreshError.message : "刷新连接信息失败";
          message.warning(reason);
        });

        return session;
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
    MAX_AUTH_RETRIES
  };
}
