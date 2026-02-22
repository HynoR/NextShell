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

export type BottomTab = "connections" | "files" | "live-edit" | "commands" | "system-info" | "about";

export interface NetworkPoint {
  inMbps: number;
  outMbps: number;
  capturedAt: string;
}

const NETWORK_RATE_HISTORY_CAP = 50;

function networkRateHistoryKey(connectionId: string, iface: string): string {
  return `${connectionId}:${iface}`;
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
  setActiveSession: (sessionId?: string) => void;
  setMonitor: (snapshot?: MonitorSnapshot) => void;
  setProcessSnapshot: (connectionId: string, snapshot: ProcessSnapshot) => void;
  setNetworkSnapshot: (connectionId: string, snapshot: NetworkSnapshot) => void;
  appendNetworkRate: (connectionId: string, iface: string, point: NetworkPoint) => void;
  clearNetworkRateHistory: (connectionId: string) => void;
  setBottomTab: (tab: BottomTab) => void;
}

export const useWorkspaceStore = create<WorkspaceState>((set) => ({
  connections: [],
  sshKeys: [],
  proxies: [],
  sessions: [],
  bottomTab: "connections",
  processSnapshots: {},
  networkSnapshots: {},
  networkRateHistory: {},
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
      const sessions = state.sessions.filter((session) => session.id !== sessionId);
      const candidateActiveSessionId =
        state.activeSessionId === sessionId ? sessions.at(-1)?.id : state.activeSessionId;
      const nextActiveSession = candidateActiveSessionId
        ? sessions.find((session) => session.id === candidateActiveSessionId)
        : undefined;

      return {
        sessions,
        activeSessionId: nextActiveSession?.id,
        activeConnectionId: nextActiveSession?.connectionId
      };
    }),
  removeSessionsByConnection: (connectionId) =>
    set((state) => {
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

      const prefix = `${connectionId}:`;
      const networkRateHistory = { ...state.networkRateHistory };
      for (const key of Object.keys(networkRateHistory)) {
        if (key.startsWith(prefix)) {
          delete networkRateHistory[key];
        }
      }

      return {
        sessions,
        activeSessionId: nextActiveSession?.id,
        activeConnectionId: nextActiveSession?.connectionId,
        networkRateHistory
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
      )
    })),
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
        activeConnectionId: activeSession.connectionId
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
      const prefix = `${connectionId}:`;
      const networkRateHistory = { ...state.networkRateHistory };
      for (const key of Object.keys(networkRateHistory)) {
        if (key.startsWith(prefix)) {
          delete networkRateHistory[key];
        }
      }
      return { networkRateHistory };
    }),
  setBottomTab: (tab) =>
    set({
      bottomTab:
        tab === "commands" ||
        tab === "files" ||
        tab === "connections" ||
        tab === "live-edit" ||
        tab === "system-info" ||
        tab === "about"
          ? tab
          : "connections"
    })
}));
