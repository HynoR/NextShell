import { create } from "zustand";
import type {
  AgentActivityEvent,
  AgentOpenRequestEvent,
  AgentSessionControlEvent
} from "@nextshell/shared";

const MAX_AGENT_ACTIVITIES = 100;

interface AgentActivityState {
  activities: AgentActivityEvent[];
  /**
   * Sessions an agent is driving right now, mapped to the client's name. Tabs
   * read this to show the "Agent 控制中" badge, so a user never finds keys
   * appearing in their terminal without knowing who put them there.
   */
  controlledSessions: Record<string, string | null>;
  /** Mirrors the main-process breaker so the panel can show and toggle it. */
  halted: boolean;
  /**
   * Mirrors `preferences.agent.enabled`. Agent access is opt-in, so the sidebar
   * panel renders nothing at all until this is true — synced on app mount and
   * by the settings section whenever it receives a fresh endpoint status.
   */
  enabled: boolean;
  /** `session_open` requests awaiting the user's 授权 / 拒绝 in the dialog. */
  openRequests: AgentOpenRequestEvent[];
  applyEvent: (event: AgentActivityEvent) => void;
  addOpenRequest: (request: AgentOpenRequestEvent) => void;
  removeOpenRequest: (id: string) => void;
  applySessionControl: (event: AgentSessionControlEvent) => void;
  setHalted: (halted: boolean) => void;
  setEnabled: (enabled: boolean) => void;
  clearFinished: () => void;
}

export const useAgentActivityStore = create<AgentActivityState>((set) => ({
  activities: [],
  controlledSessions: {},
  halted: false,
  enabled: false,
  openRequests: [],
  addOpenRequest: (request) =>
    set((state) =>
      state.openRequests.some((item) => item.id === request.id)
        ? state
        : { openRequests: [...state.openRequests, request] }
    ),
  removeOpenRequest: (id) =>
    set((state) => ({ openRequests: state.openRequests.filter((item) => item.id !== id) })),
  applyEvent: (event) =>
    set((state) => {
      const existing = state.activities.findIndex((item) => item.id === event.id);
      const next = [...state.activities];
      if (existing >= 0) next[existing] = event;
      else next.unshift(event);
      next.sort((left, right) => right.createdAt.localeCompare(left.createdAt));
      return { activities: next.slice(0, MAX_AGENT_ACTIVITIES) };
    }),
  applySessionControl: (event) =>
    set((state) => {
      if (!event.controlled) {
        if (!(event.sessionId in state.controlledSessions)) return state;
        const next = { ...state.controlledSessions };
        delete next[event.sessionId];
        return { controlledSessions: next };
      }
      return {
        controlledSessions: { ...state.controlledSessions, [event.sessionId]: event.clientName }
      };
    }),
  // Halting drops every badge: no agent is driving anything any more.
  setHalted: (halted) => set(() => (halted ? { halted, controlledSessions: {} } : { halted })),
  setEnabled: (enabled) => set({ enabled }),
  clearFinished: () =>
    set((state) => ({
      activities: state.activities.filter((activity) => activity.status === "running")
    }))
}));
