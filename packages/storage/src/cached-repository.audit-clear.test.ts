import { CachedConnectionRepository } from "./cached-repository";
import type { ConnectionRepository } from "./index";

const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

const createRepositoryStub = (): ConnectionRepository & { clearCalls: number } => {
  let records = [
    {
      id: "audit-1",
      action: "test.audit",
      level: "info" as const,
      message: "first",
      createdAt: new Date().toISOString()
    },
    {
      id: "audit-2",
      action: "test.audit",
      level: "info" as const,
      message: "second",
      createdAt: new Date().toISOString()
    }
  ];

  const repository = {
    clearCalls: 0,
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
    clearAuditLogs: () => {
      const deleted = records.length;
      records = [];
      return deleted;
    },
    purgeExpiredAuditLogs: () => 0,
    listCommandHistory: () => [],
    pushCommandHistory: (command) => ({
      command,
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
    clearTemplateParams: () => {},
    backupDatabase: async () => {},
    getDbPath: () => "/tmp/test.db",
    close: () => {}
  } satisfies Partial<ConnectionRepository> & { clearCalls: number };

  return repository as unknown as ConnectionRepository & { clearCalls: number };
};

(() => {
  const inner = createRepositoryStub();
  const repository = new CachedConnectionRepository(inner);

  try {
    const deleted = repository.clearAuditLogs();

    assert(deleted === 2, "clearAuditLogs should return deleted row count");
    assert(repository.clearAuditLogs() === 0, "clearAuditLogs should have removed all audit records");
  } finally {
    repository.close();
  }
})();
