import {
  deleteSessionFromCollections,
  retainSessionsInCollections,
  setBoundedSessionMapEntry
} from "./sessionScopedCollections";

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
  const generations = new Map<string, number>([
    ["s1", 1],
    ["s2", 2]
  ]);
  const toasts = new Map<string, string>([
    ["s1", "toast-1"],
    ["s2", "toast-2"]
  ]);
  const cancelled = new Set<string>(["s1", "s3"]);

  deleteSessionFromCollections("s1", [generations, toasts, cancelled]);

  assertEqual(generations.has("s1"), false, "generation entry should be removed");
  assertEqual(toasts.has("s1"), false, "toast entry should be removed");
  assertEqual(cancelled.has("s1"), false, "cancelled entry should be removed");
  assertEqual(generations.get("s2"), 2, "other generation entries should remain");
})();

(() => {
  const knownSessionIds = new Set(["s2"]);
  const buffers = new Map<string, string>([
    ["s1", "old"],
    ["s2", "keep"]
  ]);
  const status = new Map<string, string>([
    ["s1", "failed"],
    ["s2", "connected"]
  ]);
  const reconnectPending = new Set<string>(["s1", "s2"]);

  retainSessionsInCollections(knownSessionIds, [buffers, status, reconnectPending]);

  assertEqual(buffers.has("s1"), false, "stale buffer should be removed");
  assertEqual(status.has("s1"), false, "stale status should be removed");
  assertEqual(reconnectPending.has("s1"), false, "stale reconnect flag should be removed");
  assertEqual(buffers.get("s2"), "keep", "known session buffer should remain");
})();

(() => {
  const buffers = new Map<string, string>();

  setBoundedSessionMapEntry(buffers, "s1", "one", 2);
  setBoundedSessionMapEntry(buffers, "s2", "two", 2);
  setBoundedSessionMapEntry(buffers, "s1", "one-new", 2);
  setBoundedSessionMapEntry(buffers, "s3", "three", 2);

  assertEqual(buffers.size, 2, "buffer map should respect size limit");
  assertEqual(buffers.has("s1"), true, "recently updated session should be retained");
  assertEqual(buffers.has("s2"), false, "oldest unpinned session should be evicted");
  assertEqual(buffers.get("s3"), "three", "new entry should be stored");
})();

(() => {
  const buffers = new Map<string, string>([
    ["s1", "visible"],
    ["s2", "stale"]
  ]);

  setBoundedSessionMapEntry(buffers, "s3", "new", 2, ["s1"]);

  assertEqual(buffers.size, 2, "buffer map should still respect size limit when pinning");
  assertEqual(buffers.has("s1"), true, "pinned session should not be evicted first");
  assertEqual(buffers.has("s2"), false, "non-pinned oldest session should be evicted");
  assertEqual(buffers.has("s3"), true, "new entry should remain");
})();

(() => {
  const buffers = new Map<string, string>([
    ["s1", "one"],
    ["s2", "two"]
  ]);

  setBoundedSessionMapEntry(buffers, "s3", "three", 0);

  assertEqual(buffers.size, 0, "non-positive max should clear bounded map");
  assert(buffers.size === 0, "buffer map should be empty");
})();
