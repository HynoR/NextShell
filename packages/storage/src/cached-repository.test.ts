import { CachedConnectionRepository } from "./cached-repository";
import type { ConnectionRepository } from "./index";

const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

interface MutableHistoryEntry {
  command: string;
  useCount: number;
  lastUsedAt: string;
}

const MAX_COMMAND_HISTORY_ENTRIES = 500;

const makeHistoryEntry = (command: string, index: number): MutableHistoryEntry => ({
  command,
  useCount: 1,
  lastUsedAt: new Date(Date.UTC(2024, 0, 1, 0, 0, 500 - index)).toISOString()
});

const compareForEviction = (left: MutableHistoryEntry, right: MutableHistoryEntry): number => {
  if (left.useCount !== right.useCount) {
    return left.useCount - right.useCount;
  }

  return left.lastUsedAt.localeCompare(right.lastUsedAt);
};

const evictHistory = (entries: MutableHistoryEntry[]): MutableHistoryEntry[] => {
  if (entries.length <= MAX_COMMAND_HISTORY_ENTRIES) {
    return entries;
  }

  const overflow = entries.length - MAX_COMMAND_HISTORY_ENTRIES;
  const evictedCommands = new Set(
    [...entries]
      .sort(compareForEviction)
      .slice(0, overflow)
      .map((entry) => entry.command)
  );

  return entries.filter((entry) => !evictedCommands.has(entry.command));
};

const createRepositoryStub = (initialHistory: MutableHistoryEntry[]): ConnectionRepository => {
  let store = initialHistory.map((entry) => ({ ...entry }));

  return {
    list: () => [],
    save: () => {},
    remove: () => {},
    getById: () => undefined,
    seedIfEmpty: () => {},
    appendAuditLog: (payload) => ({
      id: `audit-${payload.action}`,
      action: payload.action,
      level: payload.level,
      connectionId: payload.connectionId,
      message: payload.message,
      metadata: payload.metadata,
      createdAt: new Date().toISOString()
    }),
    listAuditLogs: () => [],
    purgeExpiredAuditLogs: () => 0,
    listMigrations: () => [],
    listCommandHistory: () => store.map((entry) => ({ ...entry })),
    pushCommandHistory: (command) => {
      const now = new Date().toISOString();
      const existing = store.find((entry) => entry.command === command);
      if (existing) {
        existing.useCount += 1;
        existing.lastUsedAt = now;
        store = [existing, ...store.filter((entry) => entry.command !== command)];
        return { ...existing };
      }

      const next = { command, useCount: 1, lastUsedAt: now };
      store = evictHistory([next, ...store]);
      return { ...next };
    },
    removeCommandHistory: (command) => {
      store = store.filter((entry) => entry.command !== command);
    },
    clearCommandHistory: () => {
      store = [];
    },
    listSavedCommands: () => [],
    upsertSavedCommand: () => {
      throw new Error("unused in test");
    },
    removeSavedCommand: () => {},
    getAppPreferences: () =>
      ({
        terminal: {},
        transfer: {},
        remoteEdit: {},
        commandCenter: {},
        ssh: {},
        backup: {},
        window: {},
        traceroute: {},
        audit: {}
      }) as never,
    saveAppPreferences: (preferences) => preferences,
    getMasterKeyMeta: () => undefined,
    saveMasterKeyMeta: () => {},
    getDeviceKey: () => undefined,
    saveDeviceKey: () => {},
    getSecretStore: () => ({}) as never,
    listTemplateParams: () => [],
    upsertTemplateParams: () => {},
    clearTemplateParams: () => {},
    backupDatabase: async () => {},
    getDbPath: () => "/tmp/test.db",
    close: () => {}
  };
};

(() => {
  const inner = createRepositoryStub(
    Array.from({ length: 500 }, (_, index) => makeHistoryEntry(`cmd-${index}`, index))
  );
  const repository = new CachedConnectionRepository(inner);

  repository.listCommandHistory();
  repository.pushCommandHistory("brand-new");

  const cached = repository.listCommandHistory();

  assert(cached.length === 500, `command history cache should stay capped at 500, got ${cached.length}`);
  assert(cached[0]?.command === "brand-new", "new command should be moved to the top of the cache");
  assert(
    !cached.some((entry) => entry.command === "cmd-499"),
    "cache should evict the same oldest command as the inner repository"
  );
})();

(() => {
  const history = Array.from({ length: MAX_COMMAND_HISTORY_ENTRIES }, (_, index) => ({
    ...makeHistoryEntry(`cmd-${index}`, index),
    useCount: 2
  }));
  history[120] = {
    command: "least-used",
    useCount: 1,
    lastUsedAt: new Date(Date.UTC(2024, 0, 1, 0, 10, 0)).toISOString()
  };

  const repository = new CachedConnectionRepository(createRepositoryStub(history));

  repository.listCommandHistory();
  repository.pushCommandHistory("brand-new");

  const cached = repository.listCommandHistory();

  assert(
    !cached.some((entry) => entry.command === "least-used"),
    "cache should evict the lowest-use command before evicting an older but more frequently used command"
  );
})();
