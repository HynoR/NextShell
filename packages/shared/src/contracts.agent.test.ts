import { DEFAULT_APP_PREFERENCES } from "../../core/src/index";
import {
  agentCopyClientConfigSchema,
  appPreferencesPatchSchema,
  appPreferencesSchema
} from "./contracts";
const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

(() => {
  assert(
    DEFAULT_APP_PREFERENCES.agent.enabled === false,
    "agent endpoint must default to disabled in core defaults"
  );

  const parsed = appPreferencesSchema.parse({});
  assert(
    parsed.agent.enabled === false,
    "agent endpoint must default to disabled in schema parsing"
  );
  assert(parsed.agent.execTimeoutSec === 60, "agent exec timeout should default to 60s");
  assert(parsed.agent.blacklist.length === 0, "agent blacklist should default to an empty list");
})();

(() => {
  // Preferences persisted before this feature existed carry no agent block.
  const parsed = appPreferencesSchema.safeParse({});

  assert(parsed.success, "appPreferencesSchema should accept preferences without an agent block");
  if (!parsed.success) {
    return;
  }

  assert(parsed.data.agent.enabled === false, "a missing agent block must not enable the endpoint");
})();

(() => {
  const parsed = appPreferencesPatchSchema.safeParse({
    agent: { execTimeoutSec: 0 }
  });

  assert(
    parsed.success === false,
    "appPreferencesPatchSchema should reject an out-of-range execTimeoutSec"
  );
})();

(() => {
  const parsed = appPreferencesPatchSchema.safeParse({
    agent: { blacklist: [" kubectl delete ", "", "^rm\\s"] }
  });

  assert(parsed.success, "appPreferencesPatchSchema should accept a blacklist patch");
  if (!parsed.success) {
    return;
  }
  assert(
    parsed.data.agent?.blacklist?.length === 2,
    "blank blacklist entries must be filtered out by the patch schema"
  );
})();

(() => {
  const parsed = agentCopyClientConfigSchema.parse({});

  assert(parsed.client === "claude-code", "copy-client-config should default to claude-code");
})();

(() => {
  const parsed = agentCopyClientConfigSchema.safeParse({ client: "vim" });

  assert(parsed.success === false, "copy-client-config should reject an unknown client kind");
})();
