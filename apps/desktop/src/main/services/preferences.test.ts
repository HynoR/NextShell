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
