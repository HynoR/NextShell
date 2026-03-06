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
