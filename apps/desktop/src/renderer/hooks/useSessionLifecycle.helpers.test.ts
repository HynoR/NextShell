import {
  extractAuthRequiredReason,
  isSessionGenerationCurrent,
  normalizeOpenError
} from "./useSessionLifecycle.helpers";

const AUTH_REQUIRED_PREFIX = "AUTH_REQUIRED::";

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
  const wrapped = `Error invoking remote method: Error: ${AUTH_REQUIRED_PREFIX}缺少用户名`;
  const extracted = extractAuthRequiredReason(wrapped);
  assertEqual(extracted, `${AUTH_REQUIRED_PREFIX}缺少用户名`, "should extract auth prefix from wrapped error");
})();

(() => {
  const normalized = normalizeOpenError(
    new Error(`Error invoking remote method 'nextshell:session:open': Error: ${AUTH_REQUIRED_PREFIX}缺少密码`)
  );
  assertEqual(normalized.authRequired, true, "auth error should be marked");
  assert(normalized.reason.startsWith(AUTH_REQUIRED_PREFIX), "normalized reason should keep auth prefix");
})();

(() => {
  const normalized = normalizeOpenError(new Error("connection refused"));
  assertEqual(normalized.authRequired, false, "non-auth error should not be marked");
  assertEqual(normalized.reason.includes(AUTH_REQUIRED_PREFIX), false, "non-auth error should not include auth prefix");
})();

(() => {
  const generations = new Map<string, number>([["s1", 2]]);
  const cancelled = new Set<string>();
  assertEqual(isSessionGenerationCurrent(generations, cancelled, "s1", 2), true, "same generation should be current");
  assertEqual(isSessionGenerationCurrent(generations, cancelled, "s1", 1), false, "older generation should be stale");
  cancelled.add("s1");
  assertEqual(isSessionGenerationCurrent(generations, cancelled, "s1", 2), false, "cancelled session should be stale");
})();

