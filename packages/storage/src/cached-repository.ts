/**
 * CachedConnectionRepository – 内存缓存 + 懒写入装饰器
 *
 * 策略概览（类似游戏存档）：
 *
 *  | 数据              | 读策略         | 写策略                           |
 *  |-------------------|----------------|----------------------------------|
 *  | Connections       | 内存 Map 全量  | write-through（立即落盘）        |
 *  | Preferences       | 内存单例       | write-behind（5 s debounce）     |
 *  | Command History   | 有序 Map + 快照 | write-behind（0.5 s debounce）  |
 *  | Saved Commands    | 内存数组       | write-through                    |
 *  | Template Params   | 内存数组       | write-through + invalidate       |
 *  | Audit Logs        | 不缓存读       | write-behind 队列（30 s / 50条） |
 *  | Migrations        | 内存数组       | 只读（静态，应用启动后不变）     |
 *  | Master Key Meta   | 内存单例       | write-through                    |
 *
 * close() 会先 flush 所有脏数据再关闭底层数据库。
 */

import { randomUUID } from "node:crypto";
import {
  MAX_COMMAND_HISTORY_ENTRIES,
  type AppPreferences,
  type AuditLogRecord,
  type CommandHistoryEntry,
  type CommandTemplateParam,
  type ConnectionListQuery,
  type ConnectionProfile,
  type MasterKeyMeta,
  type MigrationRecord,
  type ProxyProfile,
  type SavedCommand,
  type SshKeyProfile
} from "../../core/src/index";
import type { SecretStoreDB } from "../../security/src/index";
import type { ConnectionRepository, AppendAuditLogInput, SshKeyRepository, ProxyRepository } from "./index";

interface CommandHistoryCache {
  entriesByCommand: Map<string, CommandHistoryEntry>;
  snapshot: CommandHistoryEntry[] | undefined;
}

type CommandHistoryMutation =
  | { type: "push"; command: string }
  | { type: "remove"; command: string }
  | { type: "clear" };

interface BatchedCommandHistoryWriter {
  applyCommandHistoryBatch: (mutations: CommandHistoryMutation[]) => void;
}

interface BatchedAuditLogWriter {
  appendAuditLogs: (payloads: AppendAuditLogInput[]) => void;
}

const compareCommandHistoryForEviction = (
  left: CommandHistoryEntry,
  right: CommandHistoryEntry
): number => {
  if (left.useCount !== right.useCount) {
    return left.useCount - right.useCount;
  }

  return left.lastUsedAt.localeCompare(right.lastUsedAt);
};

// ── Tuning constants ────────────────────────────────────────────────────────
/** 审计日志批量写入间隔 (ms) */
const AUDIT_FLUSH_INTERVAL_MS = 30_000;
/** 审计日志队列超过此数量时立即刷盘 */
const AUDIT_FLUSH_THRESHOLD = 50;
/** 审计日志达到阈值后的异步 flush 延迟 (ms) */
const AUDIT_FLUSH_SOON_DELAY_MS = 10;
/** 命令历史写入 debounce 延迟 (ms) */
const COMMAND_HISTORY_FLUSH_DELAY_MS = 500;
/** 偏好设置写入 debounce 延迟 (ms) */
const PREFS_WRITE_DEBOUNCE_MS = 5_000;

const hasBatchedCommandHistoryWriter = (
  repository: ConnectionRepository
): repository is ConnectionRepository & BatchedCommandHistoryWriter => {
  return typeof (repository as Partial<BatchedCommandHistoryWriter>).applyCommandHistoryBatch === "function";
};

const hasBatchedAuditLogWriter = (
  repository: ConnectionRepository
): repository is ConnectionRepository & BatchedAuditLogWriter => {
  return typeof (repository as Partial<BatchedAuditLogWriter>).appendAuditLogs === "function";
};

export class CachedConnectionRepository implements ConnectionRepository {
  private readonly inner: ConnectionRepository;

  // ── Connections cache ────────────────────────────────────────────────────
  private connList: ConnectionProfile[] | undefined;
  private connById: Map<string, ConnectionProfile> | undefined;

  // ── Preferences cache ────────────────────────────────────────────────────
  private prefCache: AppPreferences | undefined;
  private prefDirty = false;
  private prefTimer: ReturnType<typeof setTimeout> | undefined;

  // ── Command history cache ────────────────────────────────────────────────
  private histCache: CommandHistoryCache | undefined;
  private histDirty = false;
  private histPending: CommandHistoryMutation[] = [];
  private histTimer: ReturnType<typeof setTimeout> | undefined;

  // ── Saved commands cache ─────────────────────────────────────────────────
  private savedCache: SavedCommand[] | undefined;

  // ── Template params cache ────────────────────────────────────────────────
  private tplCache: CommandTemplateParam[] | undefined;

  // ── Migrations cache (immutable after bootstrap) ─────────────────────────
  private migrCache: MigrationRecord[] | undefined;

  // ── Master key meta cache ────────────────────────────────────────────────
  private mkMeta: { loaded: boolean; value: MasterKeyMeta | undefined } = {
    loaded: false,
    value: undefined
  };

  // ── Device key cache ─────────────────────────────────────────────────────
  private deviceKeyCache: { loaded: boolean; value: string | undefined } = {
    loaded: false,
    value: undefined
  };

  // ── Audit log write-behind queue ─────────────────────────────────────────
  private auditBuf: AppendAuditLogInput[] = [];
  private auditTimer: ReturnType<typeof setInterval> | undefined;
  private auditSoonTimer: ReturnType<typeof setTimeout> | undefined;

  constructor(inner: ConnectionRepository) {
    this.inner = inner;
    this.auditTimer = setInterval(() => this.flushAuditLogsFromTimer("interval"), AUDIT_FLUSH_INTERVAL_MS);
  }

  // ═══════════════════════════════════════════════════════════════════════════
  // Connections – 全量 in-memory，write-through
  // ═══════════════════════════════════════════════════════════════════════════

  private ensureConnections(): void {
    if (!this.connList) {
      this.connList = this.inner.list({});
      this.connById = new Map(this.connList.map((c) => [c.id, c]));
    }
  }

  list(query: ConnectionListQuery): ConnectionProfile[] {
    this.ensureConnections();
    const keyword = query.keyword?.trim().toLowerCase();
    const group = query.group?.trim() || null;

    let result = this.connList!;

    if (query.favoriteOnly) {
      result = result.filter((c) => c.favorite);
    }

    if (group) {
      result = result.filter((c) => {
        return c.groupPath.includes(group);
      });
    }

    if (keyword) {
      result = result.filter((c) => {
        const searchable = `${c.name} ${c.host} ${c.tags.join(" ")} ${c.groupPath} ${c.notes ?? ""}`.toLowerCase();
        return searchable.includes(keyword);
      });
    }

    // 复现 SQL: ORDER BY favorite DESC, name ASC
    return [...result].sort((a, b) => {
      if (a.favorite !== b.favorite) return a.favorite ? -1 : 1;
      return a.name.localeCompare(b.name);
    });
  }

  save(connection: ConnectionProfile): void {
    this.inner.save(connection);
    if (this.connList && this.connById) {
      const idx = this.connList.findIndex((c) => c.id === connection.id);
      if (idx >= 0) {
        this.connList[idx] = connection;
      } else {
        this.connList.push(connection);
      }
      this.connById.set(connection.id, connection);
    }
  }

  remove(id: string): void {
    this.inner.remove(id);
    if (this.connList) {
      this.connList = this.connList.filter((c) => c.id !== id);
      this.connById?.delete(id);
    }
  }

  getById(id: string): ConnectionProfile | undefined {
    this.ensureConnections();
    return this.connById!.get(id);
  }

  seedIfEmpty(connections: ConnectionProfile[]): void {
    this.inner.seedIfEmpty(connections);
    // 种子数据写入后清除缓存，下次读取时从 DB 重新加载
    this.connList = undefined;
    this.connById = undefined;
  }

  // ═══════════════════════════════════════════════════════════════════════════
  // Preferences – 内存单例，write-behind (debounce 5 s)
  // ═══════════════════════════════════════════════════════════════════════════

  getAppPreferences(): AppPreferences {
    if (!this.prefCache) {
      this.prefCache = this.inner.getAppPreferences();
    }
    return this.prefCache;
  }

  saveAppPreferences(preferences: AppPreferences): AppPreferences {
    this.prefCache = preferences;
    this.prefDirty = true;
    this.schedulePrefsFlush();
    return preferences;
  }

  private schedulePrefsFlush(): void {
    if (this.prefTimer) clearTimeout(this.prefTimer);
    this.prefTimer = setTimeout(() => this.flushPreferences(), PREFS_WRITE_DEBOUNCE_MS);
  }

  private flushPreferences(): void {
    if (!this.prefDirty || !this.prefCache) return;
    this.inner.saveAppPreferences(this.prefCache);
    this.prefDirty = false;
  }

  // ═══════════════════════════════════════════════════════════════════════════
  // Command History – 有序 Map + 快照，write-behind
  // ═══════════════════════════════════════════════════════════════════════════

  private ensureCommandHistoryCache(): CommandHistoryCache {
    if (!this.histCache) {
      const snapshot = this.inner.listCommandHistory();
      const entriesByCommand = new Map<string, CommandHistoryEntry>();

      for (let index = snapshot.length - 1; index >= 0; index -= 1) {
        const entry = snapshot[index]!;
        entriesByCommand.set(entry.command, entry);
      }

      this.histCache = {
        entriesByCommand,
        snapshot
      };
    }

    return this.histCache;
  }

  private rebuildCommandHistorySnapshot(cache: CommandHistoryCache): CommandHistoryEntry[] {
    const snapshot = Array.from(cache.entriesByCommand.values()).reverse();
    cache.snapshot = snapshot;
    return snapshot;
  }

  private findEvictionCommand(cache: CommandHistoryCache): string | undefined {
    let candidate: CommandHistoryEntry | undefined;

    for (const entry of cache.entriesByCommand.values()) {
      if (!candidate || compareCommandHistoryForEviction(entry, candidate) < 0) {
        candidate = entry;
      }
    }

    return candidate?.command;
  }

  listCommandHistory(): CommandHistoryEntry[] {
    const cache = this.ensureCommandHistoryCache();
    return cache.snapshot ?? this.rebuildCommandHistorySnapshot(cache);
  }

  pushCommandHistory(command: string): CommandHistoryEntry {
    const cache = this.ensureCommandHistoryCache();
    const existing = cache.entriesByCommand.get(command);
    const result: CommandHistoryEntry = {
      command,
      useCount: existing ? existing.useCount + 1 : 1,
      lastUsedAt: new Date().toISOString()
    };

    const hadExistingEntry = cache.entriesByCommand.delete(command);

    if (!hadExistingEntry && cache.entriesByCommand.size >= MAX_COMMAND_HISTORY_ENTRIES) {
      const evictedCommand = this.findEvictionCommand(cache);
      if (evictedCommand) {
        cache.entriesByCommand.delete(evictedCommand);
      }
    }

    cache.entriesByCommand.set(command, result);
    cache.snapshot = undefined;
    this.histPending.push({ type: "push", command });
    this.histDirty = true;
    this.scheduleCommandHistoryFlush();
    return result;
  }

  removeCommandHistory(command: string): void {
    const cache = this.ensureCommandHistoryCache();
    if (cache.entriesByCommand.delete(command)) {
      cache.snapshot = undefined;
    }
    this.histPending.push({ type: "remove", command });
    this.histDirty = true;
    this.scheduleCommandHistoryFlush();
  }

  clearCommandHistory(): void {
    this.histCache = {
      entriesByCommand: new Map(),
      snapshot: []
    };
    this.histPending.push({ type: "clear" });
    this.histDirty = true;
    this.scheduleCommandHistoryFlush();
  }

  private scheduleCommandHistoryFlush(): void {
    if (this.histTimer) clearTimeout(this.histTimer);
    this.histTimer = setTimeout(() => {
      this.histTimer = undefined;
      this.flushCommandHistoryFromTimer();
    }, COMMAND_HISTORY_FLUSH_DELAY_MS);
  }

  private flushCommandHistory(): void {
    if (!this.histDirty || this.histPending.length === 0) {
      return;
    }

    const pending = this.histPending.splice(0);
    try {
      if (hasBatchedCommandHistoryWriter(this.inner)) {
        this.inner.applyCommandHistoryBatch(pending);
        this.histDirty = false;
        return;
      }

      for (const mutation of pending) {
        if (mutation.type === "push") {
          this.inner.pushCommandHistory(mutation.command);
        } else if (mutation.type === "remove") {
          this.inner.removeCommandHistory(mutation.command);
        } else {
          this.inner.clearCommandHistory();
        }
      }
    } catch (error) {
      this.histPending.unshift(...pending);
      throw error;
    }

    this.histDirty = false;
  }

  private flushCommandHistoryFromTimer(): void {
    try {
      this.flushCommandHistory();
    } catch (error) {
      console.warn("[Storage] command history flush failed", error);
      if (this.histDirty && this.histPending.length > 0) {
        this.scheduleCommandHistoryFlush();
      }
    }
  }

  // ═══════════════════════════════════════════════════════════════════════════
  // Saved Commands – 内存数组，write-through
  // ═══════════════════════════════════════════════════════════════════════════

  listSavedCommands(query: { keyword?: string; group?: string }): SavedCommand[] {
    if (!this.savedCache) {
      this.savedCache = this.inner.listSavedCommands({});
    }

    const keyword = query.keyword?.trim().toLowerCase();
    const group = query.group?.trim() || null;

    let result = this.savedCache;

    if (keyword) {
      result = result.filter((c) => {
        const searchable = `${c.name} ${c.command} ${c.description ?? ""}`.toLowerCase();
        return searchable.includes(keyword);
      });
    }

    if (group) {
      result = result.filter((c) => c.group === group);
    }

    // 复现 SQL: ORDER BY group_name ASC, updated_at DESC
    return [...result].sort((a, b) => {
      const gc = a.group.localeCompare(b.group);
      if (gc !== 0) return gc;
      return b.updatedAt.localeCompare(a.updatedAt);
    });
  }

  upsertSavedCommand(input: {
    id?: string;
    name: string;
    description?: string;
    group: string;
    command: string;
    isTemplate: boolean;
  }): SavedCommand {
    const result = this.inner.upsertSavedCommand(input);
    if (this.savedCache) {
      const idx = this.savedCache.findIndex((c) => c.id === result.id);
      if (idx >= 0) {
        this.savedCache[idx] = result;
      } else {
        this.savedCache.push(result);
      }
    }
    return result;
  }

  removeSavedCommand(id: string): void {
    this.inner.removeSavedCommand(id);
    if (this.savedCache) {
      this.savedCache = this.savedCache.filter((c) => c.id !== id);
    }
  }

  // ═══════════════════════════════════════════════════════════════════════════
  // Template Params – 内存数组，write-through + invalidate
  // ═══════════════════════════════════════════════════════════════════════════

  listTemplateParams(commandId?: string): CommandTemplateParam[] {
    if (!this.tplCache) {
      this.tplCache = this.inner.listTemplateParams();
    }
    if (commandId) {
      return this.tplCache.filter((p) => p.commandId === commandId);
    }
    return this.tplCache;
  }

  upsertTemplateParams(commandId: string, params: Record<string, string>): void {
    this.inner.upsertTemplateParams(commandId, params);
    // 直接失效缓存，下次 list 时重新加载
    this.tplCache = undefined;
  }

  clearTemplateParams(commandId: string): void {
    this.inner.clearTemplateParams(commandId);
    if (this.tplCache) {
      this.tplCache = this.tplCache.filter((p) => p.commandId !== commandId);
    }
  }

  // ═══════════════════════════════════════════════════════════════════════════
  // Audit Logs – write-behind 队列
  // ═══════════════════════════════════════════════════════════════════════════

  appendAuditLog(payload: AppendAuditLogInput): AuditLogRecord {
    // 在内存中生成返回值（调用方均不使用返回值，仅满足接口）
    const record: AuditLogRecord = {
      id: randomUUID(),
      action: payload.action,
      level: payload.level,
      connectionId: payload.connectionId,
      message: payload.message,
      metadata: payload.metadata,
      createdAt: new Date().toISOString()
    };
    this.auditBuf.push(payload);
    if (this.auditBuf.length >= AUDIT_FLUSH_THRESHOLD) {
      this.scheduleAuditLogFlushSoon();
    }
    return record;
  }

  listAuditLogs(limit?: number): AuditLogRecord[] {
    // 读取前先落盘，确保结果包含最近的缓冲记录
    this.flushAuditLogs();
    return this.inner.listAuditLogs(limit);
  }

  clearAuditLogs(): number {
    this.auditBuf = [];
    return this.inner.clearAuditLogs();
  }

  purgeExpiredAuditLogs(retentionDays: number): number {
    this.flushAuditLogs();
    return this.inner.purgeExpiredAuditLogs(retentionDays);
  }

  private flushAuditLogs(): void {
    if (this.auditBuf.length === 0) return;
    const batch = this.auditBuf.splice(0);
    try {
      if (hasBatchedAuditLogWriter(this.inner)) {
        this.inner.appendAuditLogs(batch);
        return;
      }

      for (const payload of batch) {
        this.inner.appendAuditLog(payload);
      }
    } catch (error) {
      this.auditBuf.unshift(...batch);
      throw error;
    }
  }

  private scheduleAuditLogFlushSoon(): void {
    if (this.auditSoonTimer) {
      return;
    }

    this.auditSoonTimer = setTimeout(() => {
      this.auditSoonTimer = undefined;
      this.flushAuditLogsFromTimer("threshold");
    }, AUDIT_FLUSH_SOON_DELAY_MS);
  }

  private flushAuditLogsFromTimer(trigger: "interval" | "threshold"): void {
    try {
      this.flushAuditLogs();
    } catch (error) {
      console.warn(`[Storage] audit log flush failed (${trigger})`, error);
      if (trigger === "threshold" && this.auditBuf.length > 0) {
        this.scheduleAuditLogFlushSoon();
      }
    }
  }

  // ═══════════════════════════════════════════════════════════════════════════
  // Migrations – 只读，启动后不变
  // ═══════════════════════════════════════════════════════════════════════════

  listMigrations(): MigrationRecord[] {
    if (!this.migrCache) {
      this.migrCache = this.inner.listMigrations();
    }
    return this.migrCache;
  }

  // ═══════════════════════════════════════════════════════════════════════════
  // Master Key Meta – 内存单例，write-through
  // ═══════════════════════════════════════════════════════════════════════════

  getMasterKeyMeta(): MasterKeyMeta | undefined {
    if (!this.mkMeta.loaded) {
      this.mkMeta = { loaded: true, value: this.inner.getMasterKeyMeta() };
    }
    return this.mkMeta.value;
  }

  saveMasterKeyMeta(meta: MasterKeyMeta): void {
    this.inner.saveMasterKeyMeta(meta);
    this.mkMeta = { loaded: true, value: meta };
  }

  getDeviceKey(): string | undefined {
    if (!this.deviceKeyCache.loaded) {
      this.deviceKeyCache = { loaded: true, value: this.inner.getDeviceKey() };
    }
    return this.deviceKeyCache.value;
  }

  saveDeviceKey(key: string): void {
    this.inner.saveDeviceKey(key);
    this.deviceKeyCache = { loaded: true, value: key };
  }

  // ═══════════════════════════════════════════════════════════════════════════
  // Pass-through / Lifecycle
  // ═══════════════════════════════════════════════════════════════════════════

  getSecretStore(): SecretStoreDB {
    return this.inner.getSecretStore();
  }

  backupDatabase(targetPath: string): Promise<void> {
    // 备份前先落盘所有脏数据
    this.flush();
    return this.inner.backupDatabase(targetPath);
  }

  getDbPath(): string {
    return this.inner.getDbPath();
  }

  /** 立即将所有挂起的脏数据写入数据库。 */
  flush(): void {
    this.flushPreferences();
    this.flushCommandHistory();
    this.flushAuditLogs();
  }

  /** flush + 关闭底层数据库连接。 */
  close(): void {
    this.flush();
    if (this.prefTimer) clearTimeout(this.prefTimer);
    if (this.histTimer) clearTimeout(this.histTimer);
    if (this.auditSoonTimer) clearTimeout(this.auditSoonTimer);
    if (this.auditTimer) clearInterval(this.auditTimer);
    this.inner.close();
  }
}

// ═══════════════════════════════════════════════════════════════════════════
// CachedSshKeyRepository – 全量内存缓存，write-through
// ═══════════════════════════════════════════════════════════════════════════

export class CachedSshKeyRepository implements SshKeyRepository {
  private readonly inner: SshKeyRepository;
  private cache: SshKeyProfile[] | undefined;
  private byId: Map<string, SshKeyProfile> | undefined;

  constructor(inner: SshKeyRepository) {
    this.inner = inner;
  }

  private ensure(): void {
    if (!this.cache) {
      this.cache = this.inner.list();
      this.byId = new Map(this.cache.map((k) => [k.id, k]));
    }
  }

  list(): SshKeyProfile[] {
    this.ensure();
    return [...this.cache!].sort((a, b) => a.name.localeCompare(b.name));
  }

  getById(id: string): SshKeyProfile | undefined {
    this.ensure();
    return this.byId!.get(id);
  }

  save(key: SshKeyProfile): void {
    this.inner.save(key);
    if (this.cache && this.byId) {
      const idx = this.cache.findIndex((k) => k.id === key.id);
      if (idx >= 0) {
        this.cache[idx] = key;
      } else {
        this.cache.push(key);
      }
      this.byId.set(key.id, key);
    }
  }

  remove(id: string): void {
    this.inner.remove(id);
    if (this.cache) {
      this.cache = this.cache.filter((k) => k.id !== id);
      this.byId?.delete(id);
    }
  }

  getReferencingConnectionIds(keyId: string): string[] {
    return this.inner.getReferencingConnectionIds(keyId);
  }
}

// ═══════════════════════════════════════════════════════════════════════════
// CachedProxyRepository – 全量内存缓存，write-through
// ═══════════════════════════════════════════════════════════════════════════

export class CachedProxyRepository implements ProxyRepository {
  private readonly inner: ProxyRepository;
  private cache: ProxyProfile[] | undefined;
  private byId: Map<string, ProxyProfile> | undefined;

  constructor(inner: ProxyRepository) {
    this.inner = inner;
  }

  private ensure(): void {
    if (!this.cache) {
      this.cache = this.inner.list();
      this.byId = new Map(this.cache.map((p) => [p.id, p]));
    }
  }

  list(): ProxyProfile[] {
    this.ensure();
    return [...this.cache!].sort((a, b) => a.name.localeCompare(b.name));
  }

  getById(id: string): ProxyProfile | undefined {
    this.ensure();
    return this.byId!.get(id);
  }

  save(proxy: ProxyProfile): void {
    this.inner.save(proxy);
    if (this.cache && this.byId) {
      const idx = this.cache.findIndex((p) => p.id === proxy.id);
      if (idx >= 0) {
        this.cache[idx] = proxy;
      } else {
        this.cache.push(proxy);
      }
      this.byId.set(proxy.id, proxy);
    }
  }

  remove(id: string): void {
    this.inner.remove(id);
    if (this.cache) {
      this.cache = this.cache.filter((p) => p.id !== id);
      this.byId?.delete(id);
    }
  }

  getReferencingConnectionIds(proxyId: string): string[] {
    return this.inner.getReferencingConnectionIds(proxyId);
  }
}
