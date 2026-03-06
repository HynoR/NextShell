import { CachedConnectionRepository } from "./cached-repository";
import type { ConnectionRepository } from "./index";

const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

const sleep = (ms: number): Promise<void> => new Promise((resolve) => setTimeout(resolve, ms));

interface MutableHistoryEntry {
  command: string;
  useCount: number;
  lastUsedAt: string;
}

const makeHistoryEntry = (command: string, index: number): MutableHistoryEntry => ({
  command,
  useCount: 1,
  lastUsedAt: new Date(Date.UTC(2024, 0, 1, 0, 0, 100 - index)).toISOString()
});

const createRepositoryStub = (
  initialHistory: MutableHistoryEntry[]
): ConnectionRepository & { pushCalls: string[] } => {
  let store = initialHistory.map((entry) => ({ ...entry }));
  const pushCalls: string[] = [];

  return {
    pushCalls,
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
      pushCalls.push(command);
      const now = new Date().toISOString();
      const next = { command, useCount: 1, lastUsedAt: now };
      store = [next, ...store.filter((entry) => entry.command !== command)];
      return { ...next };
    },
    removeCommandHistory: () => {},
    clearCommandHistory: () => {},
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
  const inner = createRepositoryStub([
    makeHistoryEntry("ls", 0),
    makeHistoryEntry("pwd", 1)
  ]);
  const repository = new CachedConnectionRepository(inner);

  try {
    repository.listCommandHistory();
    const pushed = repository.pushCommandHistory("git status");

    assert(inner.pushCalls.length === 0, "pushCommandHistory should not write through immediately");
    assert(pushed.command === "git status", "push should still return the new history entry");
    assert(
      repository.listCommandHistory()[0]?.command === "git status",
      "command history cache should update optimistically before flush"
    );
  } finally {
    repository.close();
  }
})();

(() => {
  let failFirstFlush = true;
  const batchCalls: Array<Array<{ type: string; command?: string }>> = [];
  const inner = {
    ...createRepositoryStub([makeHistoryEntry("ls", 0)]),
    pushCommandHistory: () => {
      throw new Error("should use batch writer during retry test");
    },
    applyCommandHistoryBatch: (mutations: Array<{ type: string; command?: string }>) => {
      batchCalls.push(mutations);
      if (failFirstFlush) {
        failFirstFlush = false;
        throw new Error("transient command history flush failure");
      }
    }
  };
  const repository = new CachedConnectionRepository(inner);

  try {
    repository.pushCommandHistory("git status");

    let firstError: unknown;
    try {
      repository.flush();
    } catch (error) {
      firstError = error;
    }

    assert(firstError instanceof Error, "first command history flush should surface the batch failure");

    repository.flush();

    assert(batchCalls.length === 2, "failed command history batch should remain queued for retry");
  } finally {
    try {
      repository.close();
    } catch {
      // ignore cleanup failures in retry test
    }
  }
})();

await (async () => {
  let failFirstFlush = true;
  const batchCalls: Array<Array<{ type: string; command?: string }>> = [];
  const uncaughtErrors: Error[] = [];
  const onUncaughtException = (error: Error): void => {
    uncaughtErrors.push(error);
  };
  const inner = {
    ...createRepositoryStub([makeHistoryEntry("ls", 0)]),
    pushCommandHistory: () => {
      throw new Error("timer retry test should use batch writer");
    },
    applyCommandHistoryBatch: (mutations: Array<{ type: string; command?: string }>) => {
      batchCalls.push(mutations);
      if (failFirstFlush) {
        failFirstFlush = false;
        throw new Error("transient command history timer failure");
      }
    }
  };
  const repository = new CachedConnectionRepository(inner);

  process.once("uncaughtException", onUncaughtException);

  try {
    repository.pushCommandHistory("git status");

    await sleep(650);
    await sleep(650);

    assert(uncaughtErrors.length === 0, "timer-based history flush should not leak uncaught exceptions");
    assert(batchCalls.length === 2, "timer-based history flush should retry after a transient failure");
  } finally {
    process.off("uncaughtException", onUncaughtException);
    try {
      repository.close();
    } catch {
      // ignore cleanup failures in timer retry test
    }
  }
})();

(() => {
  const batchCalls: Array<Array<{ type: string; command?: string }>> = [];
  const inner = {
    ...createRepositoryStub([
      makeHistoryEntry("ls", 0),
      makeHistoryEntry("pwd", 1)
    ]),
    pushCommandHistory: () => {
      throw new Error("close flush should use applyCommandHistoryBatch when available");
    },
    applyCommandHistoryBatch: (mutations: Array<{ type: string; command?: string }>) => {
      batchCalls.push(mutations);
    }
  };
  const repository = new CachedConnectionRepository(inner);

  try {
    repository.listCommandHistory();
    repository.pushCommandHistory("git status");
    repository.removeCommandHistory("pwd");

    repository.close();

    assert(batchCalls.length === 1, "close should flush pending command history via one batch");
    assert(
      batchCalls[0]?.map((mutation) => mutation.type).join(",") === "push,remove",
      "batch should preserve pending command history mutation order"
    );
  } catch (error) {
    throw error;
  }
})();

(() => {
  const inner = createRepositoryStub([
    makeHistoryEntry("ls", 0),
    makeHistoryEntry("pwd", 1)
  ]);
  const repository = new CachedConnectionRepository(inner);

  try {
    repository.removeCommandHistory("pwd");

    assert(inner.pushCalls.length === 0, "remove should not force a synchronous push flush");
    assert(
      !repository.listCommandHistory().some((entry) => entry.command === "pwd"),
      "remove should update the in-memory command history view even before the first explicit list preload"
    );
  } finally {
    repository.close();
  }
})();
