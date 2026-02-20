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

export type BottomTab = "connections" | "files" | "live-edit" | "commands" | "disk" | "about";

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
  bottomTab: BottomTab;
  setConnections: (connections: ConnectionProfile[]) => void;
  setSshKeys: (keys: SshKeyProfile[]) => void;
  setProxies: (proxies: ProxyProfile[]) => void;
  setActiveConnection: (connectionId?: string) => void;
  upsertSession: (session: SessionDescriptor) => void;
  setSessionStatus: (sessionId: string, status: SessionDescriptor["status"]) => void;
  removeSession: (sessionId: string) => void;
  removeSessionsByConnection: (connectionId: string) => void;
  reorderSession: (sourceSessionId: string, targetSessionId: string) => void;
  renameSessionTitle: (sessionId: string, title: string) => void;
  setActiveSession: (sessionId?: string) => void;
  setMonitor: (snapshot?: MonitorSnapshot) => void;
  setProcessSnapshot: (connectionId: string, snapshot: ProcessSnapshot) => void;
  setNetworkSnapshot: (connectionId: string, snapshot: NetworkSnapshot) => void;
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
  setSessionStatus: (sessionId, status) =>
    set((state) => ({
      sessions: state.sessions.map((session) =>
        session.id === sessionId ? { ...session, status } : session
      )
    })),
  removeSession: (sessionId) =>
    set((state) => {
      const removed = state.sessions.find((session) => session.id === sessionId);
      const sessions = state.sessions.filter((session) => session.id !== sessionId);
      const nextActiveSessionId =
        state.activeSessionId === sessionId ? sessions.at(-1)?.id : state.activeSessionId;
      const nextActiveConnectionId =
        removed && state.activeConnectionId === removed.connectionId && sessions.every(
          (item) => item.connectionId !== removed.connectionId
        )
          ? undefined
          : state.activeConnectionId;

      return {
        sessions,
        activeSessionId: nextActiveSessionId,
        activeConnectionId: nextActiveConnectionId
      };
    }),
  removeSessionsByConnection: (connectionId) =>
    set((state) => {
      const sessions = state.sessions.filter((session) => session.connectionId !== connectionId);
      const activeSession =
        state.activeSessionId
          ? state.sessions.find((session) => session.id === state.activeSessionId)
          : undefined;

      return {
        sessions,
        activeSessionId:
          activeSession?.connectionId === connectionId ? undefined : state.activeSessionId,
        activeConnectionId:
          state.activeConnectionId === connectionId ? undefined : state.activeConnectionId
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
  setActiveSession: (activeSessionId) => set({ activeSessionId }),
  setMonitor: (monitor) => set({ monitor }),
  setProcessSnapshot: (connectionId, snapshot) =>
    set((state) => ({
      processSnapshots: { ...state.processSnapshots, [connectionId]: snapshot }
    })),
  setNetworkSnapshot: (connectionId, snapshot) =>
    set((state) => ({
      networkSnapshots: { ...state.networkSnapshots, [connectionId]: snapshot }
    })),
  setBottomTab: (tab) =>
    set({
      bottomTab:
        tab === "commands" ||
        tab === "files" ||
        tab === "connections" ||
        tab === "live-edit" ||
        tab === "disk" ||
        tab === "about"
          ? tab
          : "connections"
    })
}));
