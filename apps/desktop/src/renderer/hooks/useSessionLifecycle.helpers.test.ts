import { AUTH_REQUIRED_PREFIX } from "@nextshell/shared";
import {
  DOUBLE_START_COALESCE_MS,
  extractAuthRequiredReason,
  isSessionGenerationCurrent,
  normalizeOpenError,
  resolveCoalescedStart
} from "./useSessionLifecycle.helpers";

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
  assertEqual(
    extracted,
    `${AUTH_REQUIRED_PREFIX}缺少用户名`,
    "should extract auth prefix from wrapped error"
  );
})();

(() => {
  const normalized = normalizeOpenError(
    new Error(
      `Error invoking remote method 'nextshell:session:open': Error: ${AUTH_REQUIRED_PREFIX}缺少密码`
    )
  );
  assertEqual(normalized.authRequired, true, "auth error should be marked");
  assert(
    normalized.reason.startsWith(AUTH_REQUIRED_PREFIX),
    "normalized reason should keep auth prefix"
  );
})();

(() => {
  const normalized = normalizeOpenError(new Error("connection refused"));
  assertEqual(normalized.authRequired, false, "non-auth error should not be marked");
  assertEqual(
    normalized.reason.includes(AUTH_REQUIRED_PREFIX),
    false,
    "non-auth error should not include auth prefix"
  );
})();

(() => {
  const generations = new Map<string, number>([["s1", 2]]);
  const cancelled = new Set<string>();
  assertEqual(
    isSessionGenerationCurrent(generations, cancelled, "s1", 2),
    true,
    "same generation should be current"
  );
  assertEqual(
    isSessionGenerationCurrent(generations, cancelled, "s1", 1),
    false,
    "older generation should be stale"
  );
  cancelled.add("s1");
  assertEqual(
    isSessionGenerationCurrent(generations, cancelled, "s1", 2),
    false,
    "cancelled session should be stale"
  );
})();

// Normal path: a double-click joins the first open instead of opening a
// second tab (and a second SSH channel) on the same host.
(() => {
  const firstOpen = "open-1";
  assertEqual(
    resolveCoalescedStart({ at: 1_000, promise: firstOpen }, 1_120),
    firstOpen,
    "a repeat click inside the window should join the first open"
  );
  assertEqual(
    resolveCoalescedStart({ at: 1_000, promise: firstOpen }, 1_000),
    firstOpen,
    "two calls in the same tick should coalesce"
  );
})();

// Failure paths: nothing recorded, the window elapsed, or the clock stepped
// backwards — each must open its own tab rather than silently reuse a stale
// handshake.
(() => {
  assert(resolveCoalescedStart(undefined, 1_000) === undefined, "a first click must not coalesce");
  assert(
    resolveCoalescedStart({ at: 1_000, promise: "open-1" }, 1_000 + DOUBLE_START_COALESCE_MS) ===
      undefined,
    "a deliberate second connect after the window must open its own tab"
  );
  assert(
    resolveCoalescedStart({ at: 5_000, promise: "open-1" }, 1_000) === undefined,
    "a backwards clock step must not coalesce forever"
  );
})();
