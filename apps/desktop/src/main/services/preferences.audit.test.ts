import { mergePreferences } from "./preferences";
import { DEFAULT_APP_PREFERENCES } from "../../../../../packages/core/src/index";

const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

(() => {
  const merged = mergePreferences(DEFAULT_APP_PREFERENCES, {
    audit: {
      enabled: true
    }
  });

  assert(merged.audit.enabled === true, "audit enabled patch should be merged");
  assert(
    merged.audit.retentionDays === DEFAULT_APP_PREFERENCES.audit.retentionDays,
    "audit retentionDays should remain unchanged when only enabled is patched"
  );
})();
