import {
  claimNextSessionIndex,
  formatSessionTitle,
  resolveSessionBaseTitle
} from "./sessionTitle";

const assertEqual = <T>(actual: T, expected: T, message: string): void => {
  if (actual !== expected) {
    throw new Error(`${message}: expected "${String(expected)}", got "${String(actual)}"`);
  }
};

(() => {
  const counters = new Map<string, number>();

  assertEqual(claimNextSessionIndex(counters, "conn-a"), 1, "conn-a first index");
  assertEqual(claimNextSessionIndex(counters, "conn-a"), 2, "conn-a second index");
  assertEqual(claimNextSessionIndex(counters, "conn-a"), 3, "conn-a third index");

  assertEqual(claimNextSessionIndex(counters, "conn-b"), 1, "conn-b first index");
  assertEqual(claimNextSessionIndex(counters, "conn-b"), 2, "conn-b second index");

  assertEqual(claimNextSessionIndex(counters, "conn-a"), 4, "conn-a index keeps increasing");
})();

(() => {
  const baseFromSession = resolveSessionBaseTitle("prod@10.0.0.1", {
    name: "ignored",
    host: "ignored"
  });
  assertEqual(baseFromSession, "prod@10.0.0.1", "prefer existing session title");

  const baseFromFallback = resolveSessionBaseTitle("   ", {
    name: "prod",
    host: "10.0.0.1"
  });
  assertEqual(baseFromFallback, "prod@10.0.0.1", "fallback to connection name and host");

  const baseDefault = resolveSessionBaseTitle(undefined);
  assertEqual(baseDefault, "session", "fallback to default title");
})();

(() => {
  assertEqual(
    formatSessionTitle("prod@10.0.0.1", 3),
    "prod@10.0.0.1 #3",
    "append index to base title"
  );
})();
