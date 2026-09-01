import { DEFAULT_APP_PREFERENCES } from "../../core/src/index";
import { appPreferencesPatchSchema, appPreferencesSchema } from "./contracts";

const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

(() => {
  const parsed = appPreferencesSchema.safeParse({
    traceroute: {
      nexttracePath: "/usr/local/bin/nexttrace",
      powProvider: "sakura",
      protocol: "tcp",
      port: 443,
      showTracerouteTab: true
    }
  });

  assert(parsed.success, "appPreferencesSchema should accept the reduced traceroute preferences");
  if (!parsed.success) return;

  assert(
    Object.keys(parsed.data.traceroute).sort().join(",") === "nexttracePath,powProvider",
    "appPreferencesSchema should drop legacy traceroute fields"
  );
  assert(parsed.data.traceroute.powProvider === "sakura", "powProvider should be preserved");

  const patch = appPreferencesPatchSchema.safeParse({
    traceroute: { powProvider: "sakura", language: "en" }
  });
  assert(patch.success, "appPreferencesPatchSchema should accept reduced traceroute patches");
  if (!patch.success) return;
  assert(
    Object.keys(patch.data.traceroute ?? {}).join(",") === "powProvider",
    "appPreferencesPatchSchema should drop legacy traceroute fields"
  );
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

  assert(
    parsed.success,
    "appPreferencesSchema should accept window preferences without layout defaults"
  );
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
  const parsed = appPreferencesSchema.safeParse({
    terminal: {
      backgroundColor: "#000000",
      foregroundColor: "#d8eaff",
      fontSize: 14,
      lineHeight: 1.2
    }
  });

  assert(
    parsed.success,
    "appPreferencesSchema should accept terminal preferences without explicit fontFamily"
  );
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
      fontFamily: '"Fira Code", monospace'
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

(() => {
  assert(
    DEFAULT_APP_PREFERENCES.terminal.oscClipboardWrite === true,
    "oscClipboardWrite should default to true in core defaults"
  );
  assert(
    DEFAULT_APP_PREFERENCES.terminal.oscClipboardRead === false,
    "oscClipboardRead should default to false in core defaults"
  );
  assert(
    DEFAULT_APP_PREFERENCES.terminal.oscNotifications === true,
    "oscNotifications should default to true in core defaults"
  );
  assert(
    DEFAULT_APP_PREFERENCES.terminal.oscTitleUpdates === true,
    "oscTitleUpdates should default to true in core defaults"
  );
  assert(
    DEFAULT_APP_PREFERENCES.terminal.hyperlinkConfirm === true,
    "hyperlinkConfirm should default to true in core defaults"
  );
  assert(
    DEFAULT_APP_PREFERENCES.terminal.shellIntegration === "auto",
    "shellIntegration should default to auto in core defaults"
  );

  const parsed = appPreferencesSchema.parse({});
  assert(
    parsed.terminal.oscClipboardWrite === true,
    "oscClipboardWrite should default to true in schema parsing"
  );
  assert(
    parsed.terminal.oscClipboardRead === false,
    "oscClipboardRead should default to false in schema parsing"
  );
  assert(
    parsed.terminal.oscNotifications === true,
    "oscNotifications should default to true in schema parsing"
  );
  assert(
    parsed.terminal.oscTitleUpdates === true,
    "oscTitleUpdates should default to true in schema parsing"
  );
  assert(
    parsed.terminal.hyperlinkConfirm === true,
    "hyperlinkConfirm should default to true in schema parsing"
  );
  assert(
    parsed.terminal.shellIntegration === "auto",
    "shellIntegration should default to auto in schema parsing"
  );
})();

(() => {
  const parsed = appPreferencesSchema.safeParse({
    terminal: {
      backgroundColor: "#000000",
      foregroundColor: "#d8eaff",
      fontSize: 14,
      lineHeight: 1.2,
      oscClipboardRead: true,
      shellIntegration: "off"
    }
  });

  assert(parsed.success, "appPreferencesSchema should accept OSC terminal preferences");
  if (!parsed.success) {
    return;
  }

  assert(
    parsed.data.terminal.oscClipboardRead === true,
    "appPreferencesSchema should keep explicit oscClipboardRead"
  );
  assert(
    parsed.data.terminal.shellIntegration === "off",
    "appPreferencesSchema should keep explicit shellIntegration"
  );
  assert(
    parsed.data.terminal.oscClipboardWrite === true,
    "appPreferencesSchema should inject default oscClipboardWrite"
  );
  assert(
    parsed.data.terminal.hyperlinkConfirm === true,
    "appPreferencesSchema should inject default hyperlinkConfirm"
  );
})();

(() => {
  const parsed = appPreferencesPatchSchema.safeParse({
    terminal: {
      oscClipboardWrite: false,
      oscClipboardRead: true,
      oscNotifications: false,
      oscTitleUpdates: false,
      hyperlinkConfirm: false,
      shellIntegration: "manual"
    }
  });

  assert(parsed.success, "appPreferencesPatchSchema should accept OSC terminal patch");
})();

(() => {
  const parsed = appPreferencesPatchSchema.safeParse({
    terminal: {
      shellIntegration: "bogus"
    }
  });

  assert(
    parsed.success === false,
    "appPreferencesPatchSchema should reject invalid shellIntegration value"
  );
})();

(() => {
  const parsed = appPreferencesSchema.safeParse({
    terminal: {
      oscNotifications: "yes"
    }
  });

  assert(
    parsed.success === false,
    "appPreferencesSchema should reject non-boolean oscNotifications"
  );
})();

(() => {
  assert(
    DEFAULT_APP_PREFERENCES.terminal.wallpaper.seeThrough === true,
    "terminal wallpaper seeThrough should default to true in core defaults"
  );
  assert(
    DEFAULT_APP_PREFERENCES.terminal.wallpaper.useWebgl === false,
    "terminal wallpaper useWebgl should default to false in core defaults"
  );

  const parsed = appPreferencesSchema.parse({});
  assert(
    parsed.terminal.wallpaper.seeThrough === true,
    "appPreferencesSchema should inject default terminal wallpaper seeThrough"
  );
  assert(
    parsed.terminal.wallpaper.useWebgl === false,
    "appPreferencesSchema should inject default terminal wallpaper useWebgl"
  );
})();

(() => {
  // Preferences persisted before this feature existed carry no wallpaper block.
  const parsed = appPreferencesSchema.safeParse({
    terminal: {
      backgroundColor: "#000000",
      foregroundColor: "#d8eaff",
      wallpaper: {
        useWebgl: true
      }
    }
  });

  assert(parsed.success, "appPreferencesSchema should accept a partial terminal wallpaper block");
  if (!parsed.success) {
    return;
  }

  assert(
    parsed.data.terminal.wallpaper.useWebgl === true,
    "appPreferencesSchema should keep explicit terminal wallpaper useWebgl"
  );
  assert(
    parsed.data.terminal.wallpaper.seeThrough === true,
    "appPreferencesSchema should inject default seeThrough alongside explicit useWebgl"
  );
})();

(() => {
  const parsed = appPreferencesPatchSchema.safeParse({
    terminal: {
      wallpaper: {
        seeThrough: false
      }
    }
  });

  assert(
    parsed.success,
    "appPreferencesPatchSchema should accept a single terminal wallpaper field"
  );
})();

(() => {
  const parsed = appPreferencesPatchSchema.safeParse({
    terminal: {
      wallpaper: {
        seeThrough: "off"
      }
    }
  });

  assert(
    parsed.success === false,
    "appPreferencesPatchSchema should reject non-boolean terminal wallpaper seeThrough"
  );
})();
