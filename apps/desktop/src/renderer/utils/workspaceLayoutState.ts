export const resolveWorkspacePanelState = (
  storage: Pick<Storage, "getItem"> | undefined,
  key: string,
  defaultValue: boolean
): boolean => {
  try {
    const raw = storage?.getItem(key);
    if (raw === "true") {
      return true;
    }
    if (raw === "false") {
      return false;
    }
  } catch {
    // Ignore storage failures and fall back to the configured default.
  }

  return defaultValue;
};

export const persistWorkspacePanelState = (
  storage: Pick<Storage, "setItem"> | undefined,
  key: string,
  value: boolean
): void => {
  try {
    storage?.setItem(key, String(value));
  } catch {
    // Ignore storage failures so layout toggles still work for the session.
  }
};
