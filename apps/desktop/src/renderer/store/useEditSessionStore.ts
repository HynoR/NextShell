import { create } from "zustand";
import type { SftpEditSessionInfo, SftpEditStatusEvent } from "@nextshell/shared";

export interface EditSessionState {
  sessions: SftpEditSessionInfo[];
  loading: boolean;

  /** Fetch all active edit sessions from main process. */
  fetchSessions: () => Promise<void>;

  /** Apply a push status event from main process. */
  applyEvent: (event: SftpEditStatusEvent) => void;

  /** Stop a single edit session (also calls IPC). */
  stopSession: (editId: string) => Promise<void>;

  /** Stop all edit sessions (also calls IPC). */
  stopAllSessions: () => Promise<void>;
}

export const useEditSessionStore = create<EditSessionState>((set, get) => ({
  sessions: [],
  loading: false,

  fetchSessions: async () => {
    set({ loading: true });
    try {
      const sessions = await window.nextshell.sftp.editList();
      set({ sessions, loading: false });
    } catch {
      set({ loading: false });
    }
  },

  applyEvent: (event) => {
    set((state) => {
      const { editId, connectionId, remotePath, status } = event;

      // "closed" status → remove the session
      if (status === "closed") {
        return { sessions: state.sessions.filter((s) => s.editId !== editId) };
      }

      const existing = state.sessions.find((s) => s.editId === editId);

      if (existing) {
        // Update status
        const updatedStatus =
          status === "uploading" ? "uploading" as const :
            status === "synced" || status === "editing" ? "editing" as const :
              existing.status;

        return {
          sessions: state.sessions.map((s) =>
            s.editId === editId
              ? { ...s, status: updatedStatus, lastActivityAt: Date.now() }
              : s
          )
        };
      }

      // New session: add it on "downloading" or "editing"
      if (status === "downloading" || status === "editing") {
        const newSession: SftpEditSessionInfo = {
          editId,
          connectionId,
          remotePath,
          localPath: "",
          status: "editing",
          lastActivityAt: Date.now()
        };
        return { sessions: [...state.sessions, newSession] };
      }

      return state;
    });
  },

  stopSession: async (editId) => {
    try {
      await window.nextshell.sftp.editStop({ editId });
      set((state) => ({
        sessions: state.sessions.filter((s) => s.editId !== editId)
      }));
    } catch {
      // will be synced by the next status event
    }
  },

  stopAllSessions: async () => {
    try {
      await window.nextshell.sftp.editStopAll();
      set({ sessions: [] });
    } catch {
      // will be synced by the next status event
    }
  }
}));
