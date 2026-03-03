import {
  connectionUpsertSchema,
  sessionAuthOverrideSchema
} from "./contracts";

const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

(() => {
  const parsed = connectionUpsertSchema.safeParse({
    name: "interactive-host",
    host: "127.0.0.1",
    port: 22,
    username: "root",
    authType: "interactive",
    groupPath: "/server",
    tags: [],
    favorite: false,
    monitorSession: false
  });

  assert(parsed.success, "connectionUpsertSchema should accept authType=interactive");
  if (!parsed.success) {
    return;
  }
  assert(parsed.data.authType === "interactive", "parsed authType should stay interactive");
})();

(() => {
  const missingPassword = sessionAuthOverrideSchema.safeParse({
    authType: "interactive"
  });

  assert(missingPassword.success === false, "interactive auth override without password should fail");
  if (missingPassword.success) {
    return;
  }
  const firstIssue = missingPassword.error.issues[0];
  assert(firstIssue?.path?.[0] === "password", "missing password issue should point to password field");
})();

(() => {
  const withPassword = sessionAuthOverrideSchema.safeParse({
    username: "root",
    authType: "interactive",
    password: "secret"
  });

  assert(withPassword.success, "interactive auth override with password should pass");
})();
