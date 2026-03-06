import type { CommandHistoryEntry } from "@nextshell/core";
import { MAX_COMMAND_HISTORY_ENTRIES } from "../../../../../packages/core/src/index";

export const applyOptimisticCommandHistoryPush = (
  entries: CommandHistoryEntry[],
  nextEntry: CommandHistoryEntry,
  maxEntries = MAX_COMMAND_HISTORY_ENTRIES
): CommandHistoryEntry[] => {
  const next = entries.slice(0, maxEntries);
  const existingIndex = next.findIndex((entry) => entry.command === nextEntry.command);

  if (existingIndex >= 0) {
    next.splice(existingIndex, 1);
  }

  next.unshift(nextEntry);

  if (next.length > maxEntries) {
    next.pop();
  }

  return next;
};

export const applyOptimisticCommandHistoryRemove = (
  entries: CommandHistoryEntry[],
  command: string
): CommandHistoryEntry[] => {
  const existingIndex = entries.findIndex((entry) => entry.command === command);

  if (existingIndex < 0) {
    return entries;
  }

  const next = [...entries];
  next.splice(existingIndex, 1);
  return next;
};
