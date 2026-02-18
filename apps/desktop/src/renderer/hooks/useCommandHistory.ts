import { useCallback, useEffect, useRef, useState } from "react";
import type { CommandHistoryEntry } from "@nextshell/core";

export type { CommandHistoryEntry };

export const useCommandHistory = () => {
  const [entries, setEntries] = useState<CommandHistoryEntry[]>([]);
  const navigatorRef = useRef({ index: -1, snapshot: [] as string[] });

  const reload = useCallback(async () => {
    const list = await window.nextshell.commandHistory.list();
    setEntries(list);
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  const push = useCallback(
    async (command: string) => {
      const trimmed = command.trim();
      if (!trimmed) {
        return;
      }

      // Optimistic: prepend immediately
      const optimisticEntry: CommandHistoryEntry = {
        command: trimmed,
        useCount: 1,
        lastUsedAt: new Date().toISOString()
      };
      setEntries((prev) => {
        // Dedupe: remove existing entry with same command
        const filtered = prev.filter((e) => e.command !== trimmed);
        return [optimisticEntry, ...filtered];
      });
      navigatorRef.current = { index: -1, snapshot: [] };

      // Fire-and-forget: persist to DB
      window.nextshell.commandHistory.push({ command: trimmed }).catch(() => {
        // Silent — will be synced on next reload
      });
    },
    []
  );

  const remove = useCallback(
    async (command: string) => {
      // Optimistic: remove immediately
      const prev = entries;
      setEntries((current) => current.filter((e) => e.command !== command));

      try {
        await window.nextshell.commandHistory.remove({ command });
      } catch {
        // Rollback
        setEntries(prev);
      }
    },
    [entries]
  );

  const clear = useCallback(async () => {
    // Optimistic: clear immediately
    const prev = entries;
    setEntries([]);

    try {
      await window.nextshell.commandHistory.clear();
    } catch {
      // Rollback
      setEntries(prev);
    }
  }, [entries]);

  const search = useCallback(
    (query: string): CommandHistoryEntry[] => {
      if (!query.trim()) {
        return entries;
      }

      const lower = query.toLowerCase();
      return entries.filter((e) => e.command.toLowerCase().includes(lower));
    },
    [entries]
  );

  const navigateUp = useCallback((): string | undefined => {
    const nav = navigatorRef.current;
    if (nav.index === -1) {
      nav.snapshot = entries.map((e) => e.command);
    }

    if (nav.snapshot.length === 0) {
      return undefined;
    }

    const nextIndex = Math.min(nav.index + 1, nav.snapshot.length - 1);
    nav.index = nextIndex;
    return nav.snapshot[nextIndex];
  }, [entries]);

  const navigateDown = useCallback((): string | undefined => {
    const nav = navigatorRef.current;
    if (nav.index <= 0) {
      nav.index = -1;
      return "";
    }

    nav.index -= 1;
    return nav.snapshot[nav.index];
  }, []);

  const resetNavigation = useCallback(() => {
    navigatorRef.current = { index: -1, snapshot: [] };
  }, []);

  return {
    entries,
    push,
    remove,
    clear,
    search,
    navigateUp,
    navigateDown,
    resetNavigation
  };
};
