import { DEFAULT_APP_PREFERENCES } from "../../core/src/index";
import {
  appPreferencesPatchSchema,
  appPreferencesSchema
} from "./contracts";

const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

(() => {
  assert(DEFAULT_APP_PREFERENCES.audit.enabled === false, "audit should default to disabled in core defaults");

  const parsed = appPreferencesSchema.parse({});
  assert(parsed.audit.enabled === false, "audit should default to disabled in schema parsing");
  assert(parsed.ai.providerRequestTimeoutSec === 30, "ai provider timeout should default to 30 seconds");
  assert(parsed.ai.providerMaxRetries === 1, "ai provider retries should default to 1");
})();

(() => {
  const parsed = appPreferencesSchema.safeParse({
    window: {
      appearance: "system",
      minimizeToTray: false,
      confirmBeforeClose: true,
      backgroundImagePath: "",
      backgroundOpacity: 60
    }
  });

  assert(parsed.success, "appPreferencesSchema should accept window preferences without layout defaults");
  if (!parsed.success) {
    return;
  }

  assert(
    parsed.data.window.leftSidebarDefaultCollapsed === false,
    "appPreferencesSchema should inject default leftSidebarDefaultCollapsed"
  );
  assert(
    parsed.data.window.bottomWorkbenchDefaultCollapsed === false,
    "appPreferencesSchema should inject default bottomWorkbenchDefaultCollapsed"
  );
})();

(() => {
  const parsed = appPreferencesPatchSchema.safeParse({
    window: {
      leftSidebarDefaultCollapsed: true,
      bottomWorkbenchDefaultCollapsed: true
    }
  });

  assert(parsed.success, "appPreferencesPatchSchema should accept workspace layout booleans");
})();

(() => {
  const parsed = appPreferencesPatchSchema.safeParse({
    ai: {
      providerRequestTimeoutSec: 45,
      providerMaxRetries: 2
    }
  });

  assert(parsed.success, "appPreferencesPatchSchema should accept ai provider runtime settings");
})();

(() => {
  const parsed = appPreferencesSchema.safeParse({
    terminal: {
      backgroundColor: "#000000",
      foregroundColor: "#d8eaff",
      fontSize: 14,
      lineHeight: 1.2
    }
  });

  assert(parsed.success, "appPreferencesSchema should accept terminal preferences without explicit fontFamily");
  if (!parsed.success) {
    return;
  }

  assert(
    parsed.data.terminal.fontFamily === "JetBrains Mono, Menlo, Monaco, monospace",
    "appPreferencesSchema should inject default terminal fontFamily"
  );
})();

(() => {
  const parsed = appPreferencesPatchSchema.safeParse({
    terminal: {
      fontFamily: "\"Fira Code\", monospace"
    }
  });

  assert(parsed.success, "appPreferencesPatchSchema should accept non-empty fontFamily");
})();

(() => {
  const parsed = appPreferencesPatchSchema.safeParse({
    terminal: {
      fontFamily: "   "
    }
  });

  assert(parsed.success === false, "appPreferencesPatchSchema should reject blank fontFamily");
})();

(() => {
  const parsed = appPreferencesSchema.safeParse({
    terminal: {
      backgroundColor: "#000000",
      foregroundColor: "#d8eaff",
      fontSize: 14,
      lineHeight: 1.2,
      localShell: {
        mode: "preset",
        preset: "system",
        customPath: ""
      }
    }
  });

  assert(parsed.success, "appPreferencesSchema should accept terminal localShell preferences");
  if (!parsed.success) {
    return;
  }

  assert(
    parsed.data.terminal.localShell.mode === "preset",
    "appPreferencesSchema should keep localShell mode"
  );
  assert(
    parsed.data.terminal.localShell.preset === "system",
    "appPreferencesSchema should keep localShell preset"
  );
})();

(() => {
  const parsed = appPreferencesPatchSchema.safeParse({
    terminal: {
      localShell: {
        mode: "custom",
        preset: "system",
        customPath: "   "
      }
    }
  });

  assert(
    parsed.success === false,
    "appPreferencesPatchSchema should reject blank custom local shell path"
  );
})();

(() => {
  const parsed = appPreferencesPatchSchema.safeParse({
    terminal: {
      localShell: {
        mode: "custom",
        preset: "system",
        customPath: "/bin/fish"
      }
    }
  });

  assert(parsed.success, "appPreferencesPatchSchema should accept custom local shell path");
})();
