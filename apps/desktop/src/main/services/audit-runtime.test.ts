import { resolveAuditRuntime } from "./audit-runtime";

const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

(() => {
  const disabled = resolveAuditRuntime({
    enabled: false,
    retentionDays: 7
  });

  assert(disabled.captureEnabled === false, "disabled audit should not capture new logs");
  assert(disabled.runStartupPurge === true, "disabled audit should still purge once on startup");
  assert(disabled.runPeriodicPurge === false, "disabled audit should not schedule periodic purge");
})();

(() => {
  const enabled = resolveAuditRuntime({
    enabled: true,
    retentionDays: 7
  });

  assert(enabled.captureEnabled === true, "enabled audit should capture new logs");
  assert(enabled.runStartupPurge === true, "enabled audit should purge once on startup");
  assert(enabled.runPeriodicPurge === true, "enabled audit should keep periodic purge enabled");
})();
