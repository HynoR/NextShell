import { DEFAULT_APP_PREFERENCES } from "../../core/src/index";
import {
  agentCopyClientConfigSchema,
  appPreferencesPatchSchema,
  appPreferencesSchema,
  connectionUpsertSchema
} from "./contracts";

const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

const baseConnection = {
  name: "prod-hk",
  host: "10.0.0.1",
  port: 22,
  username: "root",
  authType: "password" as const,
  password: "secret",
  groupPath: "/server/prod"
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
  assert(parsed.agent.tcpEnabled === false, "agent TCP listener must default to disabled");
  assert(
    parsed.agent.allowedLocalRoots.length === 0,
    "agent allowedLocalRoots should default to an empty list"
  );
})();

(() => {
  // Preferences persisted before this feature existed carry no agent block.
  const parsed = appPreferencesSchema.safeParse({
  });

  assert(parsed.success, "appPreferencesSchema should accept preferences without an agent block");
  if (!parsed.success) {
    return;
  }

  assert(parsed.data.agent.enabled === false, "a missing agent block must not enable the endpoint");
})();

(() => {
  const parsed = appPreferencesPatchSchema.safeParse({
    agent: { tcpPort: 70000 }
  });

  assert(
    parsed.success === false,
    "appPreferencesPatchSchema should reject an out-of-range tcpPort"
  );
})();

(() => {
  const parsed = connectionUpsertSchema.safeParse(baseConnection);

  assert(parsed.success, "connectionUpsertSchema should accept a payload without agentAccess");
  if (!parsed.success) {
    return;
  }

  assert(
    parsed.data.agentAccess === undefined,
    "an omitted agentAccess must stay undefined so the service can keep the stored level"
  );
})();

(() => {
  const parsed = connectionUpsertSchema.safeParse({ ...baseConnection, agentAccess: "full" });

  assert(parsed.success, "connectionUpsertSchema should accept an explicit agentAccess level");
  if (!parsed.success) {
    return;
  }

  assert(parsed.data.agentAccess === "full", "an explicit agentAccess level must be preserved");
})();

(() => {
  const parsed = connectionUpsertSchema.safeParse({ ...baseConnection, agentAccess: "admin" });

  assert(parsed.success === false, "connectionUpsertSchema should reject an unknown access level");
})();

(() => {
  const parsed = agentCopyClientConfigSchema.parse({});

  assert(parsed.client === "claude-code", "copy-client-config should default to claude-code");
})();

(() => {
  const parsed = agentCopyClientConfigSchema.safeParse({ client: "vim" });

  assert(parsed.success === false, "copy-client-config should reject an unknown client kind");
})();
