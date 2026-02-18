import { create } from "zustand";
import {
  DEFAULT_APP_PREFERENCES,
  type AppPreferences
} from "@nextshell/core";
import type { AppPreferencesPatchInput } from "@nextshell/shared";

const LEGACY_DOWNLOAD_DIR_KEY = "nextshell.downloadDir";
const LEGACY_EDITOR_CMD_KEY = "nextshell.editorCommand";
const LEGACY_MIGRATION_FLAG_KEY = "nextshell.preferences.migrated.v1";

const clonePreferences = (prefs: AppPreferences): AppPreferences => ({
  transfer: { ...prefs.transfer },
  remoteEdit: { ...prefs.remoteEdit },
  commandCenter: { ...prefs.commandCenter },
  terminal: { ...prefs.terminal },
  backup: { ...prefs.backup }
});

const readLegacyValue = (key: string): string | undefined => {
  try {
    const value = localStorage.getItem(key)?.trim();
    if (!value) {
      return undefined;
    }
    return value;
  } catch {
    return undefined;
  }
};

const setLegacyMigrated = (): void => {
  try {
    localStorage.setItem(LEGACY_MIGRATION_FLAG_KEY, "1");
  } catch {
    // ignore
  }
};

const clearLegacyKeys = (): void => {
  try {
    localStorage.removeItem(LEGACY_DOWNLOAD_DIR_KEY);
    localStorage.removeItem(LEGACY_EDITOR_CMD_KEY);
  } catch {
    // ignore
  }
};

const buildLegacyPatch = (current: AppPreferences): AppPreferencesPatchInput | undefined => {
  try {
    if (localStorage.getItem(LEGACY_MIGRATION_FLAG_KEY) === "1") {
      return undefined;
    }
  } catch {
    return undefined;
  }

  const legacyDownloadDir = readLegacyValue(LEGACY_DOWNLOAD_DIR_KEY);
  const legacyEditorCommand = readLegacyValue(LEGACY_EDITOR_CMD_KEY);

  const patch: AppPreferencesPatchInput = {};

  if (
    legacyDownloadDir &&
    current.transfer.downloadDefaultDir === DEFAULT_APP_PREFERENCES.transfer.downloadDefaultDir
  ) {
    patch.transfer = {
      ...(patch.transfer ?? {}),
      downloadDefaultDir: legacyDownloadDir
    };
  }

  if (
    legacyEditorCommand &&
    current.remoteEdit.defaultEditorCommand === DEFAULT_APP_PREFERENCES.remoteEdit.defaultEditorCommand
  ) {
    patch.remoteEdit = {
      ...(patch.remoteEdit ?? {}),
      defaultEditorCommand: legacyEditorCommand
    };
  }

  if (!patch.transfer && !patch.remoteEdit && !patch.commandCenter) {
    setLegacyMigrated();
    return undefined;
  }

  return patch;
};

interface PreferencesState {
  preferences: AppPreferences;
  loading: boolean;
  initialized: boolean;
  initialize: () => Promise<void>;
  updatePreferences: (patch: AppPreferencesPatchInput) => Promise<AppPreferences>;
}

export const usePreferencesStore = create<PreferencesState>((set, get) => ({
  preferences: clonePreferences(DEFAULT_APP_PREFERENCES),
  loading: false,
  initialized: false,
  initialize: async () => {
    const state = get();
    if (state.loading || state.initialized) {
      return;
    }

    set({ loading: true });
    try {
      let preferences = await window.nextshell.settings.get();
      const legacyPatch = buildLegacyPatch(preferences);

      if (legacyPatch) {
        preferences = await window.nextshell.settings.update(legacyPatch);
      }

      setLegacyMigrated();
      clearLegacyKeys();
      set({
        preferences,
        initialized: true,
        loading: false
      });
    } catch {
      set({
        preferences: clonePreferences(DEFAULT_APP_PREFERENCES),
        initialized: true,
        loading: false
      });
    }
  },
  updatePreferences: async (patch) => {
    // Optimistic update: apply changes to UI immediately
    const prev = clonePreferences(get().preferences);
    const optimistic: AppPreferences = {
      transfer: { ...prev.transfer, ...(patch.transfer ?? {}) },
      remoteEdit: { ...prev.remoteEdit, ...(patch.remoteEdit ?? {}) },
      commandCenter: { ...prev.commandCenter, ...(patch.commandCenter ?? {}) },
      terminal: { ...prev.terminal, ...(patch.terminal ?? {}) },
      backup: { ...prev.backup, ...(patch.backup ?? {}) }
    };
    set({ preferences: optimistic });

    try {
      const confirmed = await window.nextshell.settings.update(patch);
      set({ preferences: confirmed });
      return confirmed;
    } catch {
      // Rollback on failure
      set({ preferences: prev });
      return prev;
    }
  }
}));
