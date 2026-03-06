import { CachedConnectionRepository } from "./cached-repository";
import type { ConnectionRepository } from "./index";

const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

const sleep = (ms: number): Promise<void> => new Promise((resolve) => setTimeout(resolve, ms));

const createRepositoryStub = (): ConnectionRepository & {
  appendCalls: Array<{ action: string }>;
  batchCalls: Array<Array<{ action: string }>>;
  appendAuditLogs: (payloads: Array<{ action: string }>) => void;
} => {
  const appendCalls: Array<{ action: string }> = [];
  const batchCalls: Array<Array<{ action: string }>> = [];

  return {
    appendCalls,
    batchCalls,
    list: () => [],
    save: () => {},
    remove: () => {},
    getById: () => undefined,
    seedIfEmpty: () => {},
    appendAuditLog: (payload) => {
      appendCalls.push({ action: payload.action });
      return {
        id: `audit-${payload.action}`,
        action: payload.action,
        level: payload.level,
        connectionId: payload.connectionId,
        message: payload.message,
        metadata: payload.metadata,
        createdAt: new Date().toISOString()
      };
    },
    appendAuditLogs: (payloads: Array<{ action: string }>) => {
      batchCalls.push(payloads);
    },
    listAuditLogs: () => [],
    purgeExpiredAuditLogs: () => 0,
    listMigrations: () => [],
    listCommandHistory: () => [],
    pushCommandHistory: () => ({
      command: "unused",
      useCount: 1,
      lastUsedAt: new Date().toISOString()
    }),
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
  const inner = createRepositoryStub();
  const repository = new CachedConnectionRepository(inner);

  try {
    for (let index = 0; index < 50; index += 1) {
      repository.appendAuditLog({
        action: `audit-${index}`,
        level: "info",
        message: `message-${index}`
      });
    }

    assert(inner.appendCalls.length === 0, "audit threshold should not synchronously write through");

    repository.close();

    assert(inner.batchCalls.length === 1, "close should flush queued audit logs in one batch");
    assert(inner.batchCalls[0]?.length === 50, "batch should contain every queued audit log");
  } finally {
    repository.close();
  }
})();

(() => {
  let failFirstFlush = true;
  const inner = {
    ...createRepositoryStub(),
    appendAuditLog: () => {
      throw new Error("should use batch writer during retry test");
    },
    appendAuditLogs: (payloads: Array<{ action: string }>) => {
      inner.batchCalls.push(payloads);
      if (failFirstFlush) {
        failFirstFlush = false;
        throw new Error("transient audit flush failure");
      }
    }
  };
  const repository = new CachedConnectionRepository(inner);

  try {
    repository.appendAuditLog({
      action: "audit-retry",
      level: "info",
      message: "retry"
    });

    let firstError: unknown;
    try {
      repository.flush();
    } catch (error) {
      firstError = error;
    }

    assert(firstError instanceof Error, "first audit flush should surface the batch failure");

    repository.flush();

    assert(inner.batchCalls.length === 2, "failed audit batch should remain queued for retry");
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
  const uncaughtErrors: Error[] = [];
  const onUncaughtException = (error: Error): void => {
    uncaughtErrors.push(error);
  };
  const inner = {
    ...createRepositoryStub(),
    appendAuditLog: () => {
      throw new Error("timer retry test should use batch writer");
    },
    appendAuditLogs: (payloads: Array<{ action: string }>) => {
      inner.batchCalls.push(payloads);
      if (failFirstFlush) {
        failFirstFlush = false;
        throw new Error("transient audit timer failure");
      }
    }
  };
  const repository = new CachedConnectionRepository(inner);

  process.once("uncaughtException", onUncaughtException);

  try {
    for (let index = 0; index < 50; index += 1) {
      repository.appendAuditLog({
        action: `timer-audit-${index}`,
        level: "info",
        message: `timer-message-${index}`
      });
    }

    await sleep(50);
    await sleep(50);

    assert(uncaughtErrors.length === 0, "timer-based audit flush should not leak uncaught exceptions");
    assert(inner.batchCalls.length === 2, "timer-based audit flush should retry after a transient failure");
  } finally {
    process.off("uncaughtException", onUncaughtException);
    try {
      repository.close();
    } catch {
      // ignore cleanup failures in timer retry test
    }
  }
})();
