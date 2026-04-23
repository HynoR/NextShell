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
    audit: {
      enabled: true
    }
  });

  assert(
    merged.audit.enabled === true,
    "mergePreferences should update audit enabled when only enabled is patched"
  );
  assert(
    merged.audit.retentionDays === DEFAULT_APP_PREFERENCES.audit.retentionDays,
    "mergePreferences should preserve audit retentionDays when omitted"
  );
})();

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
    ai: {
      persistHistory: false
    }
  });

  assertEqual(
    merged.ai.persistHistory,
    false,
    "mergePreferences should update ai persistHistory"
  );
  assertEqual(
    merged.ai.executionTimeoutSec,
    DEFAULT_APP_PREFERENCES.ai.executionTimeoutSec,
    "mergePreferences should preserve other ai settings when persistHistory changes"
  );
})();
