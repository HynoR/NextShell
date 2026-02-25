import { create } from "zustand";
import type { EditorSyntaxMode } from "../utils/detectLanguage";

export interface EditorTabMeta {
  sessionId: string;
  connectionId: string;
  remotePath: string;
  editId: string;
  initialContent: string;
  syntaxMode: EditorSyntaxMode;
  dirty: boolean;
  saving: boolean;
}

interface EditorTabState {
  tabs: Map<string, EditorTabMeta>;
  openTab: (meta: EditorTabMeta) => void;
  closeTab: (sessionId: string) => void;
  setDirty: (sessionId: string, dirty: boolean) => void;
  setSaving: (sessionId: string, saving: boolean) => void;
  setSyntaxMode: (sessionId: string, syntaxMode: EditorSyntaxMode) => void;
  findByRemotePath: (connectionId: string, remotePath: string) => EditorTabMeta | undefined;
  getTab: (sessionId: string) => EditorTabMeta | undefined;
}

export const useEditorTabStore = create<EditorTabState>((set, get) => ({
  tabs: new Map(),
  openTab: (meta) => {
    set((state) => {
      const next = new Map(state.tabs);
      next.set(meta.sessionId, meta);
      return { tabs: next };
    });
  },
  closeTab: (sessionId) => {
    set((state) => {
      const next = new Map(state.tabs);
      next.delete(sessionId);
      return { tabs: next };
    });
  },
  setDirty: (sessionId, dirty) => {
    set((state) => {
      const tab = state.tabs.get(sessionId);
      if (!tab) return state;
      const next = new Map(state.tabs);
      next.set(sessionId, { ...tab, dirty });
      return { tabs: next };
    });
  },
  setSaving: (sessionId, saving) => {
    set((state) => {
      const tab = state.tabs.get(sessionId);
      if (!tab) return state;
      const next = new Map(state.tabs);
      next.set(sessionId, { ...tab, saving });
      return { tabs: next };
    });
  },
  setSyntaxMode: (sessionId, syntaxMode) => {
    set((state) => {
      const tab = state.tabs.get(sessionId);
      if (!tab || tab.syntaxMode === syntaxMode) return state;
      const next = new Map(state.tabs);
      next.set(sessionId, { ...tab, syntaxMode });
      return { tabs: next };
    });
  },
  findByRemotePath: (connectionId, remotePath) => {
    for (const tab of get().tabs.values()) {
      if (tab.connectionId === connectionId && tab.remotePath === remotePath) {
        return tab;
      }
    }
    return undefined;
  },
  getTab: (sessionId) => get().tabs.get(sessionId),
}));
