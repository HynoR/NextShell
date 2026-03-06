import type { CommandHistoryEntry } from "@nextshell/core";
import {
  applyOptimisticCommandHistoryPush,
  applyOptimisticCommandHistoryRemove
} from "./useCommandHistory.helpers";

const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

const makeEntry = (command: string, index: number): CommandHistoryEntry => ({
  command,
  useCount: 1,
  lastUsedAt: new Date(Date.UTC(2024, 0, 1, 0, 0, 500 - index)).toISOString()
});

(() => {
  const entries = Array.from({ length: 500 }, (_, index) => makeEntry(`cmd-${index}`, index));
  const nextEntry = makeEntry("brand-new", 999);

  const result = applyOptimisticCommandHistoryPush(entries, nextEntry, 500);

  assert(result.length === 500, `optimistic push should stay capped at 500, got ${result.length}`);
  assert(result[0]?.command === "brand-new", "optimistic push should place new command at the top");
  assert(
    !result.some((entry) => entry.command === "cmd-499"),
    "optimistic push should drop the oldest entry when the list is full"
  );
})();

(() => {
  const entries = [makeEntry("alpha", 0), makeEntry("beta", 1), makeEntry("gamma", 2)];
  const nextEntry: CommandHistoryEntry = {
    ...entries[1]!,
    useCount: 3,
    lastUsedAt: new Date().toISOString()
  };

  const result = applyOptimisticCommandHistoryPush(entries, nextEntry, 500);

  assert(result.length === 3, "optimistic push should not duplicate existing commands");
  assert(result[0]?.command === "beta", "existing command should be moved to the top");
  assert(result.filter((entry) => entry.command === "beta").length === 1, "existing command should appear once");
})();

(() => {
  const entries = [makeEntry("alpha", 0), makeEntry("beta", 1), makeEntry("gamma", 2)];

  const result = applyOptimisticCommandHistoryRemove(entries, "beta");

  assert(result.length === 2, `optimistic remove should delete exactly one command, got ${result.length}`);
  assert(!result.some((entry) => entry.command === "beta"), "removed command should be absent");
  assert(result[0]?.command === "alpha", "remaining order should be preserved after remove");
})();
