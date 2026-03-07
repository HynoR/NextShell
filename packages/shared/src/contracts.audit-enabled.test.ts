import { DEFAULT_APP_PREFERENCES } from "../../core/src/index";
import { appPreferencesSchema } from "./contracts";

const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

(() => {
  assert(DEFAULT_APP_PREFERENCES.audit.enabled === false, "audit should default to disabled in core defaults");

  const parsed = appPreferencesSchema.parse({});
  assert(parsed.audit.enabled === false, "audit should default to disabled in schema parsing");
})();
