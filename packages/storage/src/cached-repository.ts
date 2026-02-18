/**
 * CachedConnectionRepository – 内存缓存 + 懒写入装饰器
 *
 * 策略概览（类似游戏存档）：
 *
 *  | 数据              | 读策略         | 写策略                           |
 *  |-------------------|----------------|----------------------------------|
 *  | Connections       | 内存 Map 全量  | write-through（立即落盘）        |
 *  | Preferences       | 内存单例       | write-behind（5 s debounce）     |
 *  | Command History   | 内存数组       | write-through                    |
 *  | Saved Commands    | 内存数组       | write-through                    |
 *  | Template Params   | 内存数组       | write-through + invalidate       |
 *  | Audit Logs        | 不缓存读       | write-behind 队列（30 s / 50条） |
 *  | Migrations        | 内存数组       | 只读（静态，应用启动后不变）     |
 *  | Master Key Meta   | 内存单例       | write-through                    |
 *
 * close() 会先 flush 所有脏数据再关闭底层数据库。
 */

import { randomUUID } from "node:crypto";
import type {
  AppPreferences,
  AuditLogRecord,
  CommandHistoryEntry,
  CommandTemplateParam,
  ConnectionListQuery,
  ConnectionProfile,
  MasterKeyMeta,
  MigrationRecord,
  SavedCommand
} from "../../core/src/index";
import type { SecretStoreDB } from "../../security/src/index";
import type { ConnectionRepository, AppendAuditLogInput } from "./index";

// ── Tuning constants ────────────────────────────────────────────────────────
/** 审计日志批量写入间隔 (ms) */
const AUDIT_FLUSH_INTERVAL_MS = 30_000;
/** 审计日志队列超过此数量时立即刷盘 */
const AUDIT_FLUSH_THRESHOLD = 50;
/** 偏好设置写入 debounce 延迟 (ms) */
const PREFS_WRITE_DEBOUNCE_MS = 5_000;

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
  private histCache: CommandHistoryEntry[] | undefined;

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

  // ── Audit log write-behind queue ─────────────────────────────────────────
  private auditBuf: AppendAuditLogInput[] = [];
  private auditTimer: ReturnType<typeof setInterval> | undefined;

  constructor(inner: ConnectionRepository) {
    this.inner = inner;
    this.auditTimer = setInterval(() => this.flushAuditLogs(), AUDIT_FLUSH_INTERVAL_MS);
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
        // 复现 SQL: group_path LIKE '%<group>%'  （group_path 以 JSON 字符串存储）
        const gpJson = JSON.stringify(c.groupPath);
        return gpJson.includes(group);
      });
    }

    if (keyword) {
      result = result.filter((c) => {
        const tagsJson = JSON.stringify(c.tags);
        const searchable = `${c.name} ${c.host} ${tagsJson} ${c.notes ?? ""}`.toLowerCase();
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
  // Command History – 内存数组，write-through
  // ═══════════════════════════════════════════════════════════════════════════

  listCommandHistory(): CommandHistoryEntry[] {
    if (!this.histCache) {
      this.histCache = this.inner.listCommandHistory();
    }
    return this.histCache;
  }

  pushCommandHistory(command: string): CommandHistoryEntry {
    const result = this.inner.pushCommandHistory(command);
    if (this.histCache) {
      // 去重 + 置顶
      this.histCache = [result, ...this.histCache.filter((e) => e.command !== command)];
    }
    return result;
  }

  removeCommandHistory(command: string): void {
    this.inner.removeCommandHistory(command);
    if (this.histCache) {
      this.histCache = this.histCache.filter((e) => e.command !== command);
    }
  }

  clearCommandHistory(): void {
    this.inner.clearCommandHistory();
    this.histCache = [];
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
      this.flushAuditLogs();
    }
    return record;
  }

  listAuditLogs(limit?: number): AuditLogRecord[] {
    // 读取前先落盘，确保结果包含最近的缓冲记录
    this.flushAuditLogs();
    return this.inner.listAuditLogs(limit);
  }

  private flushAuditLogs(): void {
    if (this.auditBuf.length === 0) return;
    const batch = this.auditBuf.splice(0);
    for (const payload of batch) {
      this.inner.appendAuditLog(payload);
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
    this.flushAuditLogs();
  }

  /** flush + 关闭底层数据库连接。 */
  close(): void {
    this.flush();
    if (this.prefTimer) clearTimeout(this.prefTimer);
    if (this.auditTimer) clearInterval(this.auditTimer);
    this.inner.close();
  }
}
