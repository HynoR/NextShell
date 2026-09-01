import { DEFAULT_APP_PREFERENCES } from "../../../../../packages/core/src/index";
import { mergePreferences } from "./preferences";

const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

const assertEqual = <T>(actual: T, expected: T, message: string): void => {
  if (actual !== expected) {
    throw new Error(`${message}: expected "${String(expected)}", got "${String(actual)}"`);
  }
};

(() => {
  const merged = mergePreferences(DEFAULT_APP_PREFERENCES, {
    window: {
      leftSidebarDefaultCollapsed: true,
      bottomWorkbenchDefaultCollapsed: true
    }
  });

  assertEqual(
    merged.window.leftSidebarDefaultCollapsed,
    true,
    "mergePreferences should update leftSidebarDefaultCollapsed"
  );
  assertEqual(
    merged.window.bottomWorkbenchDefaultCollapsed,
    true,
    "mergePreferences should update bottomWorkbenchDefaultCollapsed"
  );
  assertEqual(
    merged.window.appearance,
    DEFAULT_APP_PREFERENCES.window.appearance,
    "mergePreferences should preserve existing appearance"
  );
  assertEqual(
    merged.window.confirmBeforeClose,
    DEFAULT_APP_PREFERENCES.window.confirmBeforeClose,
    "mergePreferences should preserve existing window settings"
  );
})();

(() => {
  const merged = mergePreferences(DEFAULT_APP_PREFERENCES, {
    window: {
      appearance: "dark"
    }
  });

  assert(
    merged.window.leftSidebarDefaultCollapsed ===
      DEFAULT_APP_PREFERENCES.window.leftSidebarDefaultCollapsed,
    "mergePreferences should preserve left sidebar layout default when omitted"
  );
  assert(
    merged.window.bottomWorkbenchDefaultCollapsed ===
      DEFAULT_APP_PREFERENCES.window.bottomWorkbenchDefaultCollapsed,
    "mergePreferences should preserve bottom layout default when omitted"
  );
})();

(() => {
  const merged = mergePreferences(DEFAULT_APP_PREFERENCES, {
    terminal: {
      localShell: {
        mode: "custom",
        preset: "system",
        customPath: "/bin/fish"
      }
    }
  });

  assertEqual(
    merged.terminal.localShell.mode,
    "custom",
    "mergePreferences should update terminal localShell mode"
  );
  assertEqual(
    merged.terminal.localShell.customPath,
    "/bin/fish",
    "mergePreferences should update terminal localShell custom path"
  );
})();

(() => {
  const merged = mergePreferences(DEFAULT_APP_PREFERENCES, {
    terminal: {
      localShell: {
        mode: "custom",
        preset: "system",
        customPath: "   "
      }
    }
  });

  assertEqual(
    merged.terminal.localShell.customPath,
    DEFAULT_APP_PREFERENCES.terminal.localShell.customPath,
    "mergePreferences should fallback to existing custom path when incoming localShell path is blank"
  );
  assertEqual(
    merged.terminal.localShell.mode,
    "custom",
    "mergePreferences should keep requested localShell mode when only path is invalid"
  );
})();

(() => {
  const merged = mergePreferences(DEFAULT_APP_PREFERENCES, {
    terminal: {
      oscClipboardWrite: false,
      oscClipboardRead: true,
      oscNotifications: false,
      oscTitleUpdates: false,
      hyperlinkConfirm: false,
      shellIntegration: "manual"
    }
  });

  assertEqual(
    merged.terminal.oscClipboardWrite,
    false,
    "mergePreferences should update terminal oscClipboardWrite"
  );
  assertEqual(
    merged.terminal.oscClipboardRead,
    true,
    "mergePreferences should update terminal oscClipboardRead"
  );
  assertEqual(
    merged.terminal.oscNotifications,
    false,
    "mergePreferences should update terminal oscNotifications"
  );
  assertEqual(
    merged.terminal.oscTitleUpdates,
    false,
    "mergePreferences should update terminal oscTitleUpdates"
  );
  assertEqual(
    merged.terminal.hyperlinkConfirm,
    false,
    "mergePreferences should update terminal hyperlinkConfirm"
  );
  assertEqual(
    merged.terminal.shellIntegration,
    "manual",
    "mergePreferences should update terminal shellIntegration"
  );
})();

(() => {
  const merged = mergePreferences(DEFAULT_APP_PREFERENCES, {
    terminal: {
      oscNotifications: false
    }
  });

  assertEqual(
    merged.terminal.oscNotifications,
    false,
    "mergePreferences should update a single OSC toggle"
  );
  assertEqual(
    merged.terminal.oscClipboardWrite,
    DEFAULT_APP_PREFERENCES.terminal.oscClipboardWrite,
    "mergePreferences should preserve oscClipboardWrite when omitted"
  );
  assertEqual(
    merged.terminal.oscClipboardRead,
    DEFAULT_APP_PREFERENCES.terminal.oscClipboardRead,
    "mergePreferences should preserve oscClipboardRead when omitted"
  );
  assertEqual(
    merged.terminal.oscTitleUpdates,
    DEFAULT_APP_PREFERENCES.terminal.oscTitleUpdates,
    "mergePreferences should preserve oscTitleUpdates when omitted"
  );
  assertEqual(
    merged.terminal.hyperlinkConfirm,
    DEFAULT_APP_PREFERENCES.terminal.hyperlinkConfirm,
    "mergePreferences should preserve hyperlinkConfirm when omitted"
  );
  assertEqual(
    merged.terminal.shellIntegration,
    DEFAULT_APP_PREFERENCES.terminal.shellIntegration,
    "mergePreferences should preserve shellIntegration when omitted"
  );
})();

(() => {
  const merged = mergePreferences(DEFAULT_APP_PREFERENCES, {
    terminal: {
      shellIntegration: "bogus" as "auto"
    }
  });

  assertEqual(
    merged.terminal.shellIntegration,
    DEFAULT_APP_PREFERENCES.terminal.shellIntegration,
    "mergePreferences should fall back to current shellIntegration on invalid value"
  );
})();

(() => {
  // The settings UI sends one wallpaper field at a time; the sibling must not
  // be dropped by the nested merge.
  const merged = mergePreferences(DEFAULT_APP_PREFERENCES, {
    terminal: {
      wallpaper: {
        useWebgl: true
      }
    }
  });

  assertEqual(
    merged.terminal.wallpaper.useWebgl,
    true,
    "mergePreferences should apply patched terminal wallpaper useWebgl"
  );
  assertEqual(
    merged.terminal.wallpaper.seeThrough,
    DEFAULT_APP_PREFERENCES.terminal.wallpaper.seeThrough,
    "mergePreferences should preserve terminal wallpaper seeThrough when omitted"
  );
})();

(() => {
  const merged = mergePreferences(DEFAULT_APP_PREFERENCES, {
    terminal: {
      fontSize: 16
    }
  });

  assertEqual(
    merged.terminal.wallpaper.seeThrough,
    DEFAULT_APP_PREFERENCES.terminal.wallpaper.seeThrough,
    "mergePreferences should preserve terminal wallpaper when the block is absent"
  );
  assertEqual(
    merged.terminal.wallpaper.useWebgl,
    DEFAULT_APP_PREFERENCES.terminal.wallpaper.useWebgl,
    "mergePreferences should preserve terminal wallpaper useWebgl when the block is absent"
  );
})();
