import { create } from "zustand";
import type {
  ConnectionProfile,
  MonitorSnapshot,
  NetworkSnapshot,
  ProcessSnapshot,
  SessionDescriptor,
  SshKeyProfile,
  ProxyProfile
} from "@nextshell/core";
import { formatDynamicSessionTitle } from "../utils/sessionTitle";

type LocalAwareSessionDescriptor = SessionDescriptor & {
  target?: "remote" | "local";
  connectionId?: string;
};

export type BottomTab = "connections" | "files" | "quick-transfer" | "live-edit" | "commands" | "system-info" | "traceroute";

export interface NetworkPoint {
  inMbps: number;
  outMbps: number;
  capturedAt: string;
}

const NETWORK_RATE_HISTORY_CAP = 50;

function networkRateHistoryKey(connectionId: string, iface: string): string {
  return `${connectionId}:${iface}`;
}

function omitConnectionSnapshot<T>(snapshots: Record<string, T>, connectionId: string): Record<string, T> {
  if (!(connectionId in snapshots)) {
    return snapshots;
  }

  const nextSnapshots = { ...snapshots };
  delete nextSnapshots[connectionId];
  return nextSnapshots;
}

function pruneNetworkRateHistory(
  networkRateHistory: Record<string, NetworkPoint[]>,
  connectionId: string
): Record<string, NetworkPoint[]> {
  const prefix = `${connectionId}:`;
  let changed = false;
  const nextHistory = { ...networkRateHistory };

  for (const key of Object.keys(nextHistory)) {
    if (key.startsWith(prefix)) {
      delete nextHistory[key];
      changed = true;
    }
  }

  return changed ? nextHistory : networkRateHistory;
}

function getSessionConnectionId(session?: SessionDescriptor): string | undefined {
  return (session as LocalAwareSessionDescriptor | undefined)?.connectionId;
}

function isLocalSession(session?: SessionDescriptor): boolean {
  return (session as LocalAwareSessionDescriptor | undefined)?.target === "local";
}

interface WorkspaceState {
  connections: ConnectionProfile[];
  sshKeys: SshKeyProfile[];
  proxies: ProxyProfile[];
  sessions: SessionDescriptor[];
  activeConnectionId?: string;
  activeSessionId?: string;
  monitor?: MonitorSnapshot;
  processSnapshots: Record<string, ProcessSnapshot>;
  networkSnapshots: Record<string, NetworkSnapshot>;
  networkRateHistory: Record<string, NetworkPoint[]>;
  sessionCwdById: Record<string, string>;
  sessionTitlePinnedById: Record<string, boolean>;
  sessionDynamicBaseTitleById: Record<string, string>;
  lastActiveRemoteTerminalByConnection: Record<string, string | undefined>;
  bottomTab: BottomTab;
  setConnections: (connections: ConnectionProfile[]) => void;
  setSshKeys: (keys: SshKeyProfile[]) => void;
  setProxies: (proxies: ProxyProfile[]) => void;
  setActiveConnection: (connectionId?: string) => void;
  upsertSession: (session: SessionDescriptor) => void;
  setSessionStatus: (sessionId: string, status: SessionDescriptor["status"], reason?: string | null) => void;
  removeSession: (sessionId: string) => void;
  removeSessionsByConnection: (connectionId: string) => void;
  reorderSession: (sourceSessionId: string, targetSessionId: string) => void;
  renameSessionTitle: (sessionId: string, title: string) => void;
  setSessionRemoteTitle: (sessionId: string, title: string) => void;
  setActiveSession: (sessionId?: string) => void;
  setMonitor: (snapshot?: MonitorSnapshot) => void;
  setProcessSnapshot: (connectionId: string, snapshot: ProcessSnapshot) => void;
  setNetworkSnapshot: (connectionId: string, snapshot: NetworkSnapshot) => void;
  appendNetworkRate: (connectionId: string, iface: string, point: NetworkPoint) => void;
  clearNetworkRateHistory: (connectionId: string) => void;
  setSessionCwd: (sessionId: string, cwd?: string) => void;
  setBottomTab: (tab: BottomTab) => void;
}

const omitSessionCwd = (
  sessionCwdById: Record<string, string>,
  sessionId: string
): Record<string, string> => {
  if (!(sessionId in sessionCwdById)) {
    return sessionCwdById;
  }

  const next = { ...sessionCwdById };
  delete next[sessionId];
  return next;
};

const omitSessionRecord = <T>(
  record: Record<string, T>,
  sessionId: string
): Record<string, T> => {
  if (!(sessionId in record)) {
    return record;
  }

  const next = { ...record };
  delete next[sessionId];
  return next;
};

const omitSessionRecords = <T>(
  record: Record<string, T>,
  sessionIds: string[]
): Record<string, T> => {
  let next = record;
  for (const sessionId of sessionIds) {
    next = omitSessionRecord(next, sessionId);
  }
  return next;
};

const omitLastActiveTerminalForSession = (
  lastActiveRemoteTerminalByConnection: Record<string, string | undefined>,
  session?: SessionDescriptor
): Record<string, string | undefined> => {
  if (!session || session.target !== "remote" || session.type !== "terminal" || !session.connectionId) {
    return lastActiveRemoteTerminalByConnection;
  }

  if (lastActiveRemoteTerminalByConnection[session.connectionId] !== session.id) {
    return lastActiveRemoteTerminalByConnection;
  }

  const next = { ...lastActiveRemoteTerminalByConnection };
  delete next[session.connectionId];
  return next;
};

export const useWorkspaceStore = create<WorkspaceState>((set) => ({
  connections: [],
  sshKeys: [],
  proxies: [],
  sessions: [],
  bottomTab: "connections",
  processSnapshots: {},
  networkSnapshots: {},
  networkRateHistory: {},
  sessionCwdById: {},
  sessionTitlePinnedById: {},
  sessionDynamicBaseTitleById: {},
  lastActiveRemoteTerminalByConnection: {},
  setConnections: (connections) => set({ connections }),
  setSshKeys: (sshKeys) => set({ sshKeys }),
  setProxies: (proxies) => set({ proxies }),
  setActiveConnection: (activeConnectionId) => set({ activeConnectionId }),
  upsertSession: (session) =>
    set((state) => {
      const exists = state.sessions.some((item) => item.id === session.id);
      return {
        sessions: exists
          ? state.sessions.map((item) => (item.id === session.id ? session : item))
          : [...state.sessions, session]
      };
    }),
  setSessionStatus: (sessionId, status, reason) =>
    set((state) => ({
      sessions: state.sessions.map((session) => {
        if (session.id !== sessionId) {
          return session;
        }

        if (reason === null) {
          const { reason: _ignored, ...rest } = session;
          return { ...rest, status };
        }

        if (reason !== undefined) {
          return { ...session, status, reason };
        }

        if (status !== "failed") {
          const { reason: _ignored, ...rest } = session;
          return { ...rest, status };
        }

        return { ...session, status };
      })
    })),
  removeSession: (sessionId) =>
    set((state) => {
      const target = state.sessions.find((session) => session.id === sessionId);
      const sessions = state.sessions.filter((session) => session.id !== sessionId);
      const candidateActiveSessionId =
        state.activeSessionId === sessionId ? sessions.at(-1)?.id : state.activeSessionId;
      const nextActiveSession = candidateActiveSessionId
        ? sessions.find((session) => session.id === candidateActiveSessionId)
        : undefined;
      const nextActiveConnectionId = nextActiveSession
        ? (isLocalSession(nextActiveSession)
            ? state.activeConnectionId
            : getSessionConnectionId(nextActiveSession))
        : undefined;

      const processSnapshots =
        target?.type === "processManager" && getSessionConnectionId(target)
          ? omitConnectionSnapshot(state.processSnapshots, getSessionConnectionId(target)!)
          : state.processSnapshots;
      const networkSnapshots =
        target?.type === "networkMonitor" && getSessionConnectionId(target)
          ? omitConnectionSnapshot(state.networkSnapshots, getSessionConnectionId(target)!)
          : state.networkSnapshots;

      return {
        sessions,
        activeSessionId: nextActiveSession?.id,
        activeConnectionId: nextActiveConnectionId,
        processSnapshots,
        networkSnapshots,
        sessionCwdById: omitSessionCwd(state.sessionCwdById, sessionId),
        sessionTitlePinnedById: omitSessionRecord(state.sessionTitlePinnedById, sessionId),
        sessionDynamicBaseTitleById: omitSessionRecord(state.sessionDynamicBaseTitleById, sessionId),
        lastActiveRemoteTerminalByConnection: omitLastActiveTerminalForSession(
          state.lastActiveRemoteTerminalByConnection,
          target
        )
      };
    }),
  removeSessionsByConnection: (connectionId) =>
    set((state) => {
      const removedSessions = state.sessions.filter((session) => session.connectionId === connectionId);
      const sessions = state.sessions.filter((session) => session.connectionId !== connectionId);
      const hasCurrentActiveSession = Boolean(
        state.activeSessionId && sessions.some((session) => session.id === state.activeSessionId)
      );
      const candidateActiveSessionId = hasCurrentActiveSession
        ? state.activeSessionId
        : sessions.at(-1)?.id;
      const nextActiveSession = candidateActiveSessionId
        ? sessions.find((session) => session.id === candidateActiveSessionId)
        : undefined;
      const nextActiveConnectionId = nextActiveSession
        ? (isLocalSession(nextActiveSession)
            ? state.activeConnectionId
            : getSessionConnectionId(nextActiveSession))
        : undefined;

      let sessionCwdById = state.sessionCwdById;
      for (const removedSession of removedSessions) {
        sessionCwdById = omitSessionCwd(sessionCwdById, removedSession.id);
      }

      let lastActiveRemoteTerminalByConnection = state.lastActiveRemoteTerminalByConnection;
      for (const removedSession of removedSessions) {
        lastActiveRemoteTerminalByConnection = omitLastActiveTerminalForSession(
          lastActiveRemoteTerminalByConnection,
          removedSession
        );
      }

      return {
        sessions,
        activeSessionId: nextActiveSession?.id,
        activeConnectionId: nextActiveConnectionId,
        processSnapshots: omitConnectionSnapshot(state.processSnapshots, connectionId),
        networkSnapshots: omitConnectionSnapshot(state.networkSnapshots, connectionId),
        networkRateHistory: pruneNetworkRateHistory(state.networkRateHistory, connectionId),
        sessionCwdById,
        sessionTitlePinnedById: omitSessionRecords(
          state.sessionTitlePinnedById,
          removedSessions.map((session) => session.id)
        ),
        sessionDynamicBaseTitleById: omitSessionRecords(
          state.sessionDynamicBaseTitleById,
          removedSessions.map((session) => session.id)
        ),
        lastActiveRemoteTerminalByConnection
      };
    }),
  reorderSession: (sourceSessionId, targetSessionId) =>
    set((state) => {
      if (sourceSessionId === targetSessionId) {
        return {};
      }

      const sourceIndex = state.sessions.findIndex((session) => session.id === sourceSessionId);
      const targetIndex = state.sessions.findIndex((session) => session.id === targetSessionId);

      if (sourceIndex < 0 || targetIndex < 0) {
        return {};
      }

      const sessions = [...state.sessions];
      const [source] = sessions.splice(sourceIndex, 1);
      if (!source) {
        return {};
      }
      const adjustedTargetIndex = sourceIndex < targetIndex ? targetIndex - 1 : targetIndex;
      sessions.splice(adjustedTargetIndex, 0, source);

      return { sessions };
    }),
  renameSessionTitle: (sessionId, title) =>
    set((state) => ({
      sessions: state.sessions.map((session) =>
        session.id === sessionId ? { ...session, title } : session
      ),
      sessionTitlePinnedById: { ...state.sessionTitlePinnedById, [sessionId]: true },
      sessionDynamicBaseTitleById: omitSessionRecord(state.sessionDynamicBaseTitleById, sessionId)
    })),
  setSessionRemoteTitle: (sessionId, title) =>
    set((state) => {
      if (state.sessionTitlePinnedById[sessionId]) {
        return {};
      }

      const targetSession = state.sessions.find((session) => session.id === sessionId);
      if (!targetSession) {
        return {};
      }

      const normalizedRemoteTitle = title.trim();
      const baseTitle = state.sessionDynamicBaseTitleById[sessionId] ?? targetSession.title;
      const nextTitle = formatDynamicSessionTitle(baseTitle, normalizedRemoteTitle);

      if (
        targetSession.title === nextTitle &&
        state.sessionDynamicBaseTitleById[sessionId] === baseTitle
      ) {
        return {};
      }

      return {
        sessions: state.sessions.map((session) =>
          session.id === sessionId ? { ...session, title: nextTitle } : session
        ),
        sessionDynamicBaseTitleById: normalizedRemoteTitle
          ? {
              ...state.sessionDynamicBaseTitleById,
              [sessionId]: baseTitle
            }
          : omitSessionRecord(state.sessionDynamicBaseTitleById, sessionId)
      };
    }),
  setActiveSession: (activeSessionId) =>
    set((state) => {
      if (!activeSessionId) {
        return { activeSessionId };
      }

      const activeSession = state.sessions.find((session) => session.id === activeSessionId);
      if (!activeSession) {
        return { activeSessionId };
      }

      return {
        activeSessionId,
        activeConnectionId: isLocalSession(activeSession)
          ? state.activeConnectionId
          : getSessionConnectionId(activeSession),
        lastActiveRemoteTerminalByConnection:
          activeSession.target === "remote" &&
          activeSession.type === "terminal" &&
          activeSession.connectionId
            ? {
                ...state.lastActiveRemoteTerminalByConnection,
                [activeSession.connectionId]: activeSession.id
              }
            : state.lastActiveRemoteTerminalByConnection
      };
    }),
  setMonitor: (monitor) => set({ monitor }),
  setProcessSnapshot: (connectionId, snapshot) =>
    set((state) => ({
      processSnapshots: { ...state.processSnapshots, [connectionId]: snapshot }
    })),
  setNetworkSnapshot: (connectionId, snapshot) =>
    set((state) => ({
      networkSnapshots: { ...state.networkSnapshots, [connectionId]: snapshot }
    })),
  appendNetworkRate: (connectionId, iface, point) =>
    set((state) => {
      const key = networkRateHistoryKey(connectionId, iface);
      const existing = state.networkRateHistory[key] ?? [];
      const latest = existing[existing.length - 1];
      let merged: NetworkPoint[];
      if (latest?.capturedAt === point.capturedAt) {
        merged = [...existing.slice(0, -1), point];
      } else {
        merged = [...existing, point];
      }
      const trimmed = merged.slice(-NETWORK_RATE_HISTORY_CAP);
      return {
        networkRateHistory: { ...state.networkRateHistory, [key]: trimmed }
      };
    }),
  clearNetworkRateHistory: (connectionId) =>
    set((state) => {
      return {
        networkRateHistory: pruneNetworkRateHistory(state.networkRateHistory, connectionId)
      };
    }),
  setSessionCwd: (sessionId, cwd) =>
    set((state) => {
      if (!cwd) {
        return {
          sessionCwdById: omitSessionCwd(state.sessionCwdById, sessionId)
        };
      }

      if (state.sessionCwdById[sessionId] === cwd) {
        return {};
      }

      return {
        sessionCwdById: { ...state.sessionCwdById, [sessionId]: cwd }
      };
    }),
  setBottomTab: (tab) =>
    set({
      bottomTab:
        tab === "commands" ||
        tab === "files" ||
        tab === "quick-transfer" ||
        tab === "connections" ||
        tab === "live-edit" ||
        tab === "system-info" ||
        tab === "traceroute"
          ? tab
          : "connections"
    })
}));
