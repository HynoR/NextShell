import path from "node:path";
import { randomUUID } from "node:crypto";
import { createRequire } from "node:module";
import type Database from "better-sqlite3";

export { CachedConnectionRepository } from "./cached-repository";
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
import { DEFAULT_APP_PREFERENCES as DEFAULT_APP_PREFERENCES_VALUE } from "../../core/src/index";
import type { SecretStoreDB } from "../../security/src/index";

const require = createRequire(import.meta.url);

interface BetterSqlite3Module {
  new (filename: string): Database.Database;
}

interface ConnectionRow {
  id: string;
  name: string;
  host: string;
  port: number;
  username: string;
  auth_type: ConnectionProfile["authType"];
  credential_ref: string | null;
  private_key_path: string | null;
  private_key_ref: string | null;
  host_fingerprint: string | null;
  strict_host_key_checking: number;
  proxy_type: "none" | "socks4" | "socks5" | null;
  proxy_host: string | null;
  proxy_port: number | null;
  proxy_username: string | null;
  proxy_credential_ref: string | null;
  terminal_encoding: "utf-8" | "gb18030" | "gbk" | "big5" | null;
  backspace_mode: "ascii-backspace" | "ascii-delete" | null;
  delete_mode: "vt220-delete" | "ascii-delete" | "ascii-backspace" | null;
  group_path: string;
  tags: string;
  notes: string | null;
  favorite: number;
  monitor_session: number;
  created_at: string;
  updated_at: string;
  last_connected_at: string | null;
}

interface MigrationRow {
  version: number;
  name: string;
  applied_at: string;
}

interface AuditLogRow {
  id: string;
  action: string;
  level: "info" | "warn" | "error";
  connection_id: string | null;
  message: string;
  metadata_json: string | null;
  created_at: string;
}

interface CommandHistoryRow {
  command: string;
  use_count: number;
  last_used_at: string;
}

interface SavedCommandRow {
  id: string;
  name: string;
  description: string | null;
  group_name: string;
  command: string;
  is_template: number;
  created_at: string;
  updated_at: string;
}

interface AppSettingRow {
  key: string;
  value_json: string;
  updated_at: string;
}

interface MigrationDefinition {
  version: number;
  name: string;
  apply: (db: Database.Database) => void;
}

export interface AppendAuditLogInput {
  action: string;
  level: "info" | "warn" | "error";
  connectionId?: string;
  message: string;
  metadata?: Record<string, unknown>;
}

const loadDatabaseDriver = (): BetterSqlite3Module => {
  const moduleName = `better-sqlite${3}`;
  return require(moduleName) as BetterSqlite3Module;
};

const toJSON = (value: string[]): string => JSON.stringify(value);

const fromJSON = (value: string): string[] => {
  try {
    const parsed = JSON.parse(value);
    return Array.isArray(parsed) ? parsed.filter((item): item is string => typeof item === "string") : [];
  } catch {
    return [];
  }
};

const fromMetadataJSON = (value: string | null): Record<string, unknown> | undefined => {
  if (!value) {
    return undefined;
  }

  try {
    const parsed = JSON.parse(value);
    return parsed && typeof parsed === "object" ? (parsed as Record<string, unknown>) : undefined;
  } catch {
    return undefined;
  }
};

const toMetadataJSON = (value: Record<string, unknown> | undefined): string | null => {
  if (!value) {
    return null;
  }

  return JSON.stringify(value);
};

const rowToConnection = (row: ConnectionRow): ConnectionProfile => {
  return {
    id: row.id,
    name: row.name,
    host: row.host,
    port: row.port,
    username: row.username,
    authType: row.auth_type,
    credentialRef: row.credential_ref ?? undefined,
    privateKeyPath: row.private_key_path ?? undefined,
    privateKeyRef: row.private_key_ref ?? undefined,
    hostFingerprint: row.host_fingerprint ?? undefined,
    strictHostKeyChecking: row.strict_host_key_checking === 1,
    proxyType: row.proxy_type === "socks4" || row.proxy_type === "socks5" ? row.proxy_type : "none",
    proxyHost: row.proxy_host ?? undefined,
    proxyPort: row.proxy_port ?? undefined,
    proxyUsername: row.proxy_username ?? undefined,
    proxyCredentialRef: row.proxy_credential_ref ?? undefined,
    terminalEncoding:
      row.terminal_encoding === "gb18030" ||
      row.terminal_encoding === "gbk" ||
      row.terminal_encoding === "big5"
        ? row.terminal_encoding
        : "utf-8",
    backspaceMode: row.backspace_mode === "ascii-delete" ? "ascii-delete" : "ascii-backspace",
    deleteMode:
      row.delete_mode === "ascii-delete" || row.delete_mode === "ascii-backspace"
        ? row.delete_mode
        : "vt220-delete",
    groupPath: fromJSON(row.group_path),
    tags: fromJSON(row.tags),
    notes: row.notes ?? undefined,
    favorite: row.favorite === 1,
    monitorSession: (row as ConnectionRow & { monitor_session?: number }).monitor_session === 1,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
    lastConnectedAt: row.last_connected_at ?? undefined
  };
};

const rowToMigration = (row: MigrationRow): MigrationRecord => {
  return {
    version: row.version,
    name: row.name,
    appliedAt: row.applied_at
  };
};

const rowToCommandHistory = (row: CommandHistoryRow): CommandHistoryEntry => ({
  command: row.command,
  useCount: row.use_count,
  lastUsedAt: row.last_used_at
});

const rowToSavedCommand = (row: SavedCommandRow): SavedCommand => ({
  id: row.id,
  name: row.name,
  description: row.description ?? undefined,
  group: row.group_name,
  command: row.command,
  isTemplate: row.is_template === 1,
  createdAt: row.created_at,
  updatedAt: row.updated_at
});

const rowToAuditLog = (row: AuditLogRow): AuditLogRecord => {
  return {
    id: row.id,
    action: row.action,
    level: row.level,
    connectionId: row.connection_id ?? undefined,
    message: row.message,
    metadata: fromMetadataJSON(row.metadata_json),
    createdAt: row.created_at
  };
};

const cloneDefaultPreferences = (): AppPreferences => {
  return {
    transfer: { ...DEFAULT_APP_PREFERENCES_VALUE.transfer },
    remoteEdit: { ...DEFAULT_APP_PREFERENCES_VALUE.remoteEdit },
    commandCenter: { ...DEFAULT_APP_PREFERENCES_VALUE.commandCenter },
    terminal: { ...DEFAULT_APP_PREFERENCES_VALUE.terminal },
    backup: { ...DEFAULT_APP_PREFERENCES_VALUE.backup }
  };
};

const parseAppPreferences = (value: string | null): AppPreferences => {
  const fallback = cloneDefaultPreferences();
  if (!value) {
    return fallback;
  }

  try {
    const parsed = JSON.parse(value) as Partial<AppPreferences> | null;
    if (!parsed || typeof parsed !== "object") {
      return fallback;
    }

    return {
      transfer: {
        uploadDefaultDir:
          typeof parsed.transfer?.uploadDefaultDir === "string" &&
          parsed.transfer.uploadDefaultDir.trim().length > 0
            ? parsed.transfer.uploadDefaultDir.trim()
            : fallback.transfer.uploadDefaultDir,
        downloadDefaultDir:
          typeof parsed.transfer?.downloadDefaultDir === "string" &&
          parsed.transfer.downloadDefaultDir.trim().length > 0
            ? parsed.transfer.downloadDefaultDir.trim()
            : fallback.transfer.downloadDefaultDir
      },
      remoteEdit: {
        defaultEditorCommand:
          typeof parsed.remoteEdit?.defaultEditorCommand === "string" &&
          parsed.remoteEdit.defaultEditorCommand.trim().length > 0
            ? parsed.remoteEdit.defaultEditorCommand.trim()
            : fallback.remoteEdit.defaultEditorCommand
      },
      commandCenter: {
        rememberTemplateParams:
          typeof parsed.commandCenter?.rememberTemplateParams === "boolean"
            ? parsed.commandCenter.rememberTemplateParams
            : fallback.commandCenter.rememberTemplateParams
      },
      terminal: {
        backgroundColor:
          typeof parsed.terminal?.backgroundColor === "string" &&
          /^#[0-9a-fA-F]{6}$/.test(parsed.terminal.backgroundColor.trim())
            ? parsed.terminal.backgroundColor.trim()
            : fallback.terminal.backgroundColor,
        foregroundColor:
          typeof parsed.terminal?.foregroundColor === "string" &&
          /^#[0-9a-fA-F]{6}$/.test(parsed.terminal.foregroundColor.trim())
            ? parsed.terminal.foregroundColor.trim()
            : fallback.terminal.foregroundColor,
        fontSize:
          typeof parsed.terminal?.fontSize === "number" &&
          Number.isInteger(parsed.terminal.fontSize) &&
          parsed.terminal.fontSize >= 10 &&
          parsed.terminal.fontSize <= 24
            ? parsed.terminal.fontSize
            : fallback.terminal.fontSize,
        lineHeight:
          typeof parsed.terminal?.lineHeight === "number" &&
          Number.isFinite(parsed.terminal.lineHeight) &&
          parsed.terminal.lineHeight >= 1 &&
          parsed.terminal.lineHeight <= 2
            ? parsed.terminal.lineHeight
            : fallback.terminal.lineHeight
      },
      backup: {
        remotePath:
          typeof parsed.backup?.remotePath === "string"
            ? parsed.backup.remotePath
            : fallback.backup.remotePath,
        rclonePath:
          typeof parsed.backup?.rclonePath === "string"
            ? parsed.backup.rclonePath
            : fallback.backup.rclonePath,
        defaultBackupConflictPolicy:
          parsed.backup?.defaultBackupConflictPolicy === "skip" ||
          parsed.backup?.defaultBackupConflictPolicy === "force"
            ? parsed.backup.defaultBackupConflictPolicy
            : fallback.backup.defaultBackupConflictPolicy,
        defaultRestoreConflictPolicy:
          parsed.backup?.defaultRestoreConflictPolicy === "skip_older" ||
          parsed.backup?.defaultRestoreConflictPolicy === "force"
            ? parsed.backup.defaultRestoreConflictPolicy
            : fallback.backup.defaultRestoreConflictPolicy,
        rememberPassword:
          typeof parsed.backup?.rememberPassword === "boolean"
            ? parsed.backup.rememberPassword
            : fallback.backup.rememberPassword,
        lastBackupAt:
          typeof parsed.backup?.lastBackupAt === "string"
            ? parsed.backup.lastBackupAt
            : fallback.backup.lastBackupAt
      }
    };
  } catch {
    return fallback;
  }
};

const hasColumn = (db: Database.Database, table: string, column: string): boolean => {
  const rows = db.prepare(`PRAGMA table_info(${table})`).all() as Array<{ name: string }>;
  return rows.some((row) => row.name === column);
};

const ensureColumn = (
  db: Database.Database,
  table: string,
  column: string,
  definition: string
): void => {
  if (hasColumn(db, table, column)) {
    return;
  }

  db.exec(`ALTER TABLE ${table} ADD COLUMN ${definition}`);
};

const migrations: MigrationDefinition[] = [
  {
    version: 1,
    name: "create_connections_table",
    apply: (db) => {
      db.exec(`
        CREATE TABLE IF NOT EXISTS connections (
          id TEXT PRIMARY KEY,
          name TEXT NOT NULL,
          host TEXT NOT NULL,
          port INTEGER NOT NULL,
          username TEXT NOT NULL,
          auth_type TEXT NOT NULL,
          credential_ref TEXT,
          private_key_path TEXT,
          private_key_ref TEXT,
          host_fingerprint TEXT,
          strict_host_key_checking INTEGER NOT NULL DEFAULT 0,
          proxy_type TEXT NOT NULL DEFAULT 'none',
          proxy_host TEXT,
          proxy_port INTEGER,
          proxy_username TEXT,
          proxy_credential_ref TEXT,
          terminal_encoding TEXT NOT NULL DEFAULT 'utf-8',
          backspace_mode TEXT NOT NULL DEFAULT 'ascii-backspace',
          delete_mode TEXT NOT NULL DEFAULT 'vt220-delete',
          group_path TEXT NOT NULL,
          tags TEXT NOT NULL,
          notes TEXT,
          favorite INTEGER NOT NULL DEFAULT 0,
          created_at TEXT NOT NULL,
          updated_at TEXT NOT NULL,
          last_connected_at TEXT
        );

        CREATE INDEX IF NOT EXISTS idx_connections_name ON connections(name);
        CREATE INDEX IF NOT EXISTS idx_connections_host ON connections(host);
        CREATE INDEX IF NOT EXISTS idx_connections_updated_at ON connections(updated_at DESC);
      `);
    }
  },
  {
    version: 2,
    name: "add_connection_security_columns",
    apply: (db) => {
      ensureColumn(db, "connections", "private_key_ref", "private_key_ref TEXT");
      ensureColumn(db, "connections", "host_fingerprint", "host_fingerprint TEXT");
      ensureColumn(
        db,
        "connections",
        "strict_host_key_checking",
        "strict_host_key_checking INTEGER NOT NULL DEFAULT 0"
      );
    }
  },
  {
    version: 3,
    name: "create_audit_logs_table",
    apply: (db) => {
      db.exec(`
        CREATE TABLE IF NOT EXISTS audit_logs (
          id TEXT PRIMARY KEY,
          action TEXT NOT NULL,
          level TEXT NOT NULL,
          connection_id TEXT,
          message TEXT NOT NULL,
          metadata_json TEXT,
          created_at TEXT NOT NULL
        );

        CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at DESC);
        CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON audit_logs(action);
      `);
    }
  },
  {
    version: 4,
    name: "create_command_history_table",
    apply: (db) => {
      db.exec(`
        CREATE TABLE IF NOT EXISTS command_history (
          command TEXT PRIMARY KEY,
          use_count INTEGER NOT NULL DEFAULT 1,
          last_used_at TEXT NOT NULL
        );

        CREATE INDEX IF NOT EXISTS idx_command_history_last_used_at ON command_history(last_used_at DESC);
        CREATE INDEX IF NOT EXISTS idx_command_history_use_count ON command_history(use_count ASC);
      `);
    }
  },
  {
    version: 5,
    name: "add_connection_proxy_columns",
    apply: (db) => {
      ensureColumn(db, "connections", "proxy_type", "proxy_type TEXT NOT NULL DEFAULT 'none'");
      ensureColumn(db, "connections", "proxy_host", "proxy_host TEXT");
      ensureColumn(db, "connections", "proxy_port", "proxy_port INTEGER");
      ensureColumn(db, "connections", "proxy_username", "proxy_username TEXT");
      ensureColumn(db, "connections", "proxy_credential_ref", "proxy_credential_ref TEXT");
    }
  },
  {
    version: 6,
    name: "add_connection_terminal_columns",
    apply: (db) => {
      ensureColumn(
        db,
        "connections",
        "terminal_encoding",
        "terminal_encoding TEXT NOT NULL DEFAULT 'utf-8'"
      );
      ensureColumn(
        db,
        "connections",
        "backspace_mode",
        "backspace_mode TEXT NOT NULL DEFAULT 'ascii-backspace'"
      );
      ensureColumn(
        db,
        "connections",
        "delete_mode",
        "delete_mode TEXT NOT NULL DEFAULT 'vt220-delete'"
      );
    }
  },
  {
    version: 7,
    name: "create_saved_commands_table",
    apply: (db) => {
      db.exec(`
        CREATE TABLE IF NOT EXISTS saved_commands (
          id TEXT PRIMARY KEY,
          name TEXT NOT NULL,
          description TEXT,
          group_name TEXT NOT NULL DEFAULT '默认',
          command TEXT NOT NULL,
          is_template INTEGER NOT NULL DEFAULT 0,
          created_at TEXT NOT NULL,
          updated_at TEXT NOT NULL
        );
        CREATE INDEX IF NOT EXISTS idx_saved_commands_group ON saved_commands(group_name);
        CREATE INDEX IF NOT EXISTS idx_saved_commands_updated_at ON saved_commands(updated_at DESC);
      `);
    }
  },
  {
    version: 8,
    name: "create_app_settings_table",
    apply: (db) => {
      db.exec(`
        CREATE TABLE IF NOT EXISTS app_settings (
          key TEXT PRIMARY KEY,
          value_json TEXT NOT NULL,
          updated_at TEXT NOT NULL
        );
        CREATE INDEX IF NOT EXISTS idx_app_settings_updated_at ON app_settings(updated_at DESC);
      `);
    }
  },
  {
    version: 9,
    name: "add_connection_monitor_session_column",
    apply: (db) => {
      ensureColumn(
        db,
        "connections",
        "monitor_session",
        "monitor_session INTEGER NOT NULL DEFAULT 0"
      );
    }
  },
  {
    version: 10,
    name: "create_secret_store_table",
    apply: (db) => {
      db.exec(`
        CREATE TABLE IF NOT EXISTS secret_store (
          id TEXT PRIMARY KEY,
          purpose TEXT NOT NULL,
          ciphertext_b64 TEXT NOT NULL,
          iv_b64 TEXT NOT NULL,
          tag_b64 TEXT NOT NULL,
          aad TEXT NOT NULL,
          created_at TEXT NOT NULL,
          updated_at TEXT NOT NULL
        );
        CREATE INDEX IF NOT EXISTS idx_secret_store_purpose ON secret_store(purpose);
      `);
    }
  },
  {
    version: 11,
    name: "create_command_template_params_table",
    apply: (db) => {
      db.exec(`
        CREATE TABLE IF NOT EXISTS command_template_params (
          id TEXT PRIMARY KEY,
          command_id TEXT NOT NULL,
          param_name TEXT NOT NULL,
          param_value TEXT NOT NULL,
          updated_at TEXT NOT NULL
        );
        CREATE INDEX IF NOT EXISTS idx_template_params_command_id ON command_template_params(command_id);
        CREATE UNIQUE INDEX IF NOT EXISTS idx_template_params_command_param ON command_template_params(command_id, param_name);
      `);
    }
  },
  {
    version: 12,
    name: "add_device_key_setting",
    apply: (_db) => {
      // Device key is stored as an app_setting row.
      // The actual generation + insertion happens at runtime in container.ts on first launch.
      // This migration is a no-op placeholder for version tracking.
    }
  }
];

export interface ConnectionRepository {
  list: (query: ConnectionListQuery) => ConnectionProfile[];
  save: (connection: ConnectionProfile) => void;
  remove: (id: string) => void;
  getById: (id: string) => ConnectionProfile | undefined;
  seedIfEmpty: (connections: ConnectionProfile[]) => void;
  appendAuditLog: (payload: AppendAuditLogInput) => AuditLogRecord;
  listAuditLogs: (limit?: number) => AuditLogRecord[];
  listMigrations: () => MigrationRecord[];
  listCommandHistory: () => CommandHistoryEntry[];
  pushCommandHistory: (command: string) => CommandHistoryEntry;
  removeCommandHistory: (command: string) => void;
  clearCommandHistory: () => void;
  listSavedCommands: (query: { keyword?: string; group?: string }) => SavedCommand[];
  upsertSavedCommand: (input: {
    id?: string;
    name: string;
    description?: string;
    group: string;
    command: string;
    isTemplate: boolean;
  }) => SavedCommand;
  removeSavedCommand: (id: string) => void;
  getAppPreferences: () => AppPreferences;
  saveAppPreferences: (preferences: AppPreferences) => AppPreferences;
  getMasterKeyMeta: () => MasterKeyMeta | undefined;
  saveMasterKeyMeta: (meta: MasterKeyMeta) => void;
  getDeviceKey: () => string | undefined;
  saveDeviceKey: (key: string) => void;
  getSecretStore: () => SecretStoreDB;
  listTemplateParams: (commandId?: string) => CommandTemplateParam[];
  upsertTemplateParams: (commandId: string, params: Record<string, string>) => void;
  clearTemplateParams: (commandId: string) => void;
  backupDatabase: (targetPath: string) => Promise<void>;
  getDbPath: () => string;
  close: () => void;
}

export class SQLiteConnectionRepository implements ConnectionRepository {
  private readonly db: Database.Database;
  private readonly resolvedDbPath: string;
  private secretStoreInstance: SQLiteSecretStore | undefined;

  constructor(dbPath: string) {
    const DatabaseCtor = loadDatabaseDriver();
    const resolved = path.resolve(dbPath);
    this.resolvedDbPath = resolved;
    this.db = new DatabaseCtor(resolved);
    this.bootstrap();
  }

  private bootstrap(): void {
    this.db.pragma("journal_mode = WAL");
    this.db.pragma("foreign_keys = ON");

    this.db.exec(`
      CREATE TABLE IF NOT EXISTS schema_migrations (
        version INTEGER PRIMARY KEY,
        name TEXT NOT NULL,
        applied_at TEXT NOT NULL
      );
    `);

    const applied = new Set(
      (
        this.db.prepare("SELECT version FROM schema_migrations ORDER BY version ASC").all() as Array<{
          version: number;
        }>
      ).map((row) => row.version)
    );

    for (const migration of migrations.sort((a, b) => a.version - b.version)) {
      if (applied.has(migration.version)) {
        continue;
      }

      const tx = this.db.transaction(() => {
        migration.apply(this.db);
        this.db.prepare(
          `
            INSERT INTO schema_migrations (version, name, applied_at)
            VALUES (@version, @name, @applied_at)
          `
        ).run({
          version: migration.version,
          name: migration.name,
          applied_at: new Date().toISOString()
        });
      });

      tx();
    }
  }

  seedIfEmpty(connections: ConnectionProfile[]): void {
    const row = this.db.prepare("SELECT COUNT(*) AS total FROM connections").get() as {
      total: number;
    };

    if (row.total > 0) {
      return;
    }

    const tx = this.db.transaction((items: ConnectionProfile[]) => {
      for (const item of items) {
        this.save(item);
      }
    });

    tx(connections);
  }

  list(query: ConnectionListQuery): ConnectionProfile[] {
    const keyword = query.keyword?.trim().toLowerCase();
    const keywordLike = keyword ? `%${keyword}%` : null;
    const group = query.group?.trim() || null;
    const favorite = query.favoriteOnly ? 1 : null;

    const rows = this.db
      .prepare(
        `
          SELECT
            id,
            name,
            host,
            port,
            username,
            auth_type,
            credential_ref,
            private_key_path,
            private_key_ref,
            host_fingerprint,
            strict_host_key_checking,
            proxy_type,
            proxy_host,
            proxy_port,
            proxy_username,
            proxy_credential_ref,
            terminal_encoding,
            backspace_mode,
            delete_mode,
            group_path,
            tags,
            notes,
            favorite,
            monitor_session,
            created_at,
            updated_at,
            last_connected_at
          FROM connections
          WHERE (@favorite IS NULL OR favorite = @favorite)
            AND (@group IS NULL OR group_path LIKE '%' || @group || '%')
            AND (
              @keywordLike IS NULL
              OR lower(name || ' ' || host || ' ' || tags || ' ' || ifnull(notes, '')) LIKE @keywordLike
            )
          ORDER BY favorite DESC, name ASC
        `
      )
      .all({
        favorite,
        group,
        keywordLike
      }) as ConnectionRow[];

    return rows.map(rowToConnection);
  }

  save(connection: ConnectionProfile): void {
    this.db
      .prepare(
        `
          INSERT INTO connections (
            id,
            name,
            host,
            port,
            username,
            auth_type,
            credential_ref,
            private_key_path,
            private_key_ref,
            host_fingerprint,
            strict_host_key_checking,
            proxy_type,
            proxy_host,
            proxy_port,
            proxy_username,
            proxy_credential_ref,
            terminal_encoding,
            backspace_mode,
            delete_mode,
            group_path,
            tags,
            notes,
            favorite,
            monitor_session,
            created_at,
            updated_at,
            last_connected_at
          ) VALUES (
            @id,
            @name,
            @host,
            @port,
            @username,
            @auth_type,
            @credential_ref,
            @private_key_path,
            @private_key_ref,
            @host_fingerprint,
            @strict_host_key_checking,
            @proxy_type,
            @proxy_host,
            @proxy_port,
            @proxy_username,
            @proxy_credential_ref,
            @terminal_encoding,
            @backspace_mode,
            @delete_mode,
            @group_path,
            @tags,
            @notes,
            @favorite,
            @monitor_session,
            @created_at,
            @updated_at,
            @last_connected_at
          )
          ON CONFLICT(id) DO UPDATE SET
            name = excluded.name,
            host = excluded.host,
            port = excluded.port,
            username = excluded.username,
            auth_type = excluded.auth_type,
            credential_ref = excluded.credential_ref,
            private_key_path = excluded.private_key_path,
            private_key_ref = excluded.private_key_ref,
            host_fingerprint = excluded.host_fingerprint,
            strict_host_key_checking = excluded.strict_host_key_checking,
            proxy_type = excluded.proxy_type,
            proxy_host = excluded.proxy_host,
            proxy_port = excluded.proxy_port,
            proxy_username = excluded.proxy_username,
            proxy_credential_ref = excluded.proxy_credential_ref,
            terminal_encoding = excluded.terminal_encoding,
            backspace_mode = excluded.backspace_mode,
            delete_mode = excluded.delete_mode,
            group_path = excluded.group_path,
            tags = excluded.tags,
            notes = excluded.notes,
            favorite = excluded.favorite,
            monitor_session = excluded.monitor_session,
            created_at = excluded.created_at,
            updated_at = excluded.updated_at,
            last_connected_at = excluded.last_connected_at
        `
      )
      .run({
        id: connection.id,
        name: connection.name,
        host: connection.host,
        port: connection.port,
        username: connection.username,
        auth_type: connection.authType,
        credential_ref: connection.credentialRef ?? null,
        private_key_path: connection.privateKeyPath ?? null,
        private_key_ref: connection.privateKeyRef ?? null,
        host_fingerprint: connection.hostFingerprint ?? null,
        strict_host_key_checking: connection.strictHostKeyChecking ? 1 : 0,
        proxy_type: connection.proxyType,
        proxy_host: connection.proxyHost ?? null,
        proxy_port: connection.proxyPort ?? null,
        proxy_username: connection.proxyUsername ?? null,
        proxy_credential_ref: connection.proxyCredentialRef ?? null,
        terminal_encoding: connection.terminalEncoding,
        backspace_mode: connection.backspaceMode,
        delete_mode: connection.deleteMode,
        group_path: toJSON(connection.groupPath),
        tags: toJSON(connection.tags),
        notes: connection.notes ?? null,
        favorite: connection.favorite ? 1 : 0,
        monitor_session: connection.monitorSession ? 1 : 0,
        created_at: connection.createdAt,
        updated_at: connection.updatedAt,
        last_connected_at: connection.lastConnectedAt ?? null
      });
  }

  remove(id: string): void {
    this.db.prepare("DELETE FROM connections WHERE id = ?").run(id);
  }

  getById(id: string): ConnectionProfile | undefined {
    const row = this.db
      .prepare(
        `
          SELECT
            id,
            name,
            host,
            port,
            username,
            auth_type,
            credential_ref,
            private_key_path,
            private_key_ref,
            host_fingerprint,
            strict_host_key_checking,
            proxy_type,
            proxy_host,
            proxy_port,
            proxy_username,
            proxy_credential_ref,
            terminal_encoding,
            backspace_mode,
            delete_mode,
            group_path,
            tags,
            notes,
            favorite,
            monitor_session,
            created_at,
            updated_at,
            last_connected_at
          FROM connections
          WHERE id = ?
        `
      )
      .get(id) as ConnectionRow | undefined;

    if (!row) {
      return undefined;
    }

    return rowToConnection(row);
  }

  appendAuditLog(payload: AppendAuditLogInput): AuditLogRecord {
    const record: AuditLogRecord = {
      id: randomUUID(),
      action: payload.action,
      level: payload.level,
      connectionId: payload.connectionId,
      message: payload.message,
      metadata: payload.metadata,
      createdAt: new Date().toISOString()
    };

    this.db.prepare(
      `
        INSERT INTO audit_logs (
          id,
          action,
          level,
          connection_id,
          message,
          metadata_json,
          created_at
        ) VALUES (
          @id,
          @action,
          @level,
          @connection_id,
          @message,
          @metadata_json,
          @created_at
        )
      `
    ).run({
      id: record.id,
      action: record.action,
      level: record.level,
      connection_id: record.connectionId ?? null,
      message: record.message,
      metadata_json: toMetadataJSON(record.metadata),
      created_at: record.createdAt
    });

    return record;
  }

  listAuditLogs(limit = 100): AuditLogRecord[] {
    const rows = this.db.prepare(
      `
        SELECT
          id,
          action,
          level,
          connection_id,
          message,
          metadata_json,
          created_at
        FROM audit_logs
        ORDER BY created_at DESC
        LIMIT @limit
      `
    ).all({ limit }) as AuditLogRow[];

    return rows.map(rowToAuditLog);
  }

  listMigrations(): MigrationRecord[] {
    const rows = this.db.prepare(
      `
        SELECT version, name, applied_at
        FROM schema_migrations
        ORDER BY version ASC
      `
    ).all() as MigrationRow[];

    return rows.map(rowToMigration);
  }

  private readonly MAX_COMMAND_HISTORY = 500;

  listCommandHistory(): CommandHistoryEntry[] {
    const rows = this.db.prepare(
      `
        SELECT command, use_count, last_used_at
        FROM command_history
        ORDER BY last_used_at DESC
      `
    ).all() as CommandHistoryRow[];

    return rows.map(rowToCommandHistory);
  }

  pushCommandHistory(command: string): CommandHistoryEntry {
    const now = new Date().toISOString();

    this.db.prepare(
      `
        INSERT INTO command_history (command, use_count, last_used_at)
        VALUES (@command, 1, @now)
        ON CONFLICT(command) DO UPDATE SET
          use_count = use_count + 1,
          last_used_at = @now
      `
    ).run({ command, now });

    this.evictCommandHistory();

    const row = this.db.prepare(
      "SELECT command, use_count, last_used_at FROM command_history WHERE command = ?"
    ).get(command) as CommandHistoryRow;

    return rowToCommandHistory(row);
  }

  removeCommandHistory(command: string): void {
    this.db.prepare("DELETE FROM command_history WHERE command = ?").run(command);
  }

  clearCommandHistory(): void {
    this.db.exec("DELETE FROM command_history");
  }

  private evictCommandHistory(): void {
    const countRow = this.db.prepare(
      "SELECT COUNT(*) AS total FROM command_history"
    ).get() as { total: number };

    if (countRow.total <= this.MAX_COMMAND_HISTORY) {
      return;
    }

    const excess = countRow.total - this.MAX_COMMAND_HISTORY;
    this.db.prepare(
      `
        DELETE FROM command_history
        WHERE command IN (
          SELECT command FROM command_history
          ORDER BY use_count ASC, last_used_at ASC
          LIMIT @excess
        )
      `
    ).run({ excess });
  }

  listSavedCommands(query: { keyword?: string; group?: string }): SavedCommand[] {
    const keyword = query.keyword?.trim().toLowerCase();
    const keywordLike = keyword ? `%${keyword}%` : null;
    const group = query.group?.trim() || null;

    let sql = `
      SELECT id, name, description, group_name, command, is_template, created_at, updated_at
      FROM saved_commands
      WHERE 1=1
    `;
    const params: Record<string, string | number> = {};
    if (keywordLike) {
      sql += " AND (LOWER(name) LIKE @keyword OR LOWER(command) LIKE @keyword OR LOWER(description) LIKE @keyword)";
      params.keyword = keywordLike;
    }
    if (group) {
      sql += " AND group_name = @group";
      params.group = group;
    }
    sql += " ORDER BY group_name ASC, updated_at DESC";

    const rows = this.db.prepare(sql).all(params) as SavedCommandRow[];
    return rows.map(rowToSavedCommand);
  }

  upsertSavedCommand(input: {
    id?: string;
    name: string;
    description?: string;
    group: string;
    command: string;
    isTemplate: boolean;
  }): SavedCommand {
    const now = new Date().toISOString();
    const id = input.id ?? randomUUID();
    const description = input.description?.trim() || null;
    const groupName = input.group.trim() || "默认";

    this.db.prepare(
      `
        INSERT INTO saved_commands (id, name, description, group_name, command, is_template, created_at, updated_at)
        VALUES (@id, @name, @description, @groupName, @command, @isTemplate, @now, @now)
        ON CONFLICT(id) DO UPDATE SET
          name = @name,
          description = @description,
          group_name = @groupName,
          command = @command,
          is_template = @isTemplate,
          updated_at = @now
      `
    ).run({
      id,
      name: input.name.trim(),
      description,
      groupName,
      command: input.command.trim(),
      isTemplate: input.isTemplate ? 1 : 0,
      now
    });

    const row = this.db.prepare(
      "SELECT id, name, description, group_name, command, is_template, created_at, updated_at FROM saved_commands WHERE id = ?"
    ).get(id) as SavedCommandRow;

    return rowToSavedCommand(row);
  }

  removeSavedCommand(id: string): void {
    this.db.prepare("DELETE FROM saved_commands WHERE id = ?").run(id);
  }

  getAppPreferences(): AppPreferences {
    const row = this.db.prepare(
      "SELECT key, value_json, updated_at FROM app_settings WHERE key = ?"
    ).get("app_preferences") as AppSettingRow | undefined;

    return parseAppPreferences(row?.value_json ?? null);
  }

  saveAppPreferences(preferences: AppPreferences): AppPreferences {
    const now = new Date().toISOString();
    const normalized = parseAppPreferences(JSON.stringify(preferences));

    this.db.prepare(
      `
        INSERT INTO app_settings (key, value_json, updated_at)
        VALUES (@key, @value_json, @updated_at)
        ON CONFLICT(key) DO UPDATE SET
          value_json = excluded.value_json,
          updated_at = excluded.updated_at
      `
    ).run({
      key: "app_preferences",
      value_json: JSON.stringify(normalized),
      updated_at: now
    });

    return normalized;
  }

  getMasterKeyMeta(): MasterKeyMeta | undefined {
    const row = this.db.prepare(
      "SELECT key, value_json, updated_at FROM app_settings WHERE key = ?"
    ).get("master_key_meta") as AppSettingRow | undefined;

    if (!row?.value_json) {
      return undefined;
    }

    try {
      const parsed = JSON.parse(row.value_json) as Partial<MasterKeyMeta>;
      if (
        typeof parsed.salt === "string" &&
        typeof parsed.n === "number" &&
        typeof parsed.r === "number" &&
        typeof parsed.p === "number" &&
        typeof parsed.verifier === "string"
      ) {
        return parsed as MasterKeyMeta;
      }
      return undefined;
    } catch {
      return undefined;
    }
  }

  saveMasterKeyMeta(meta: MasterKeyMeta): void {
    const now = new Date().toISOString();
    this.db.prepare(
      `
        INSERT INTO app_settings (key, value_json, updated_at)
        VALUES (@key, @value_json, @updated_at)
        ON CONFLICT(key) DO UPDATE SET
          value_json = excluded.value_json,
          updated_at = excluded.updated_at
      `
    ).run({
      key: "master_key_meta",
      value_json: JSON.stringify(meta),
      updated_at: now
    });
  }

  getDeviceKey(): string | undefined {
    const row = this.db.prepare(
      "SELECT value_json FROM app_settings WHERE key = ?"
    ).get("device_key") as { value_json: string } | undefined;
    if (!row?.value_json) return undefined;
    try {
      const parsed = JSON.parse(row.value_json);
      return typeof parsed === "string" ? parsed : undefined;
    } catch {
      return undefined;
    }
  }

  saveDeviceKey(key: string): void {
    const now = new Date().toISOString();
    this.db.prepare(
      `INSERT INTO app_settings (key, value_json, updated_at)
       VALUES (@key, @value_json, @updated_at)
       ON CONFLICT(key) DO UPDATE SET
         value_json = excluded.value_json,
         updated_at = excluded.updated_at`
    ).run({ key: "device_key", value_json: JSON.stringify(key), updated_at: now });
  }

  getSecretStore(): SecretStoreDB {
    if (!this.secretStoreInstance) {
      this.secretStoreInstance = new SQLiteSecretStore(this.db);
    }
    return this.secretStoreInstance;
  }

  listTemplateParams(commandId?: string): CommandTemplateParam[] {
    if (commandId) {
      const rows = this.db.prepare(
        "SELECT id, command_id, param_name, param_value, updated_at FROM command_template_params WHERE command_id = ? ORDER BY param_name ASC"
      ).all(commandId) as Array<{ id: string; command_id: string; param_name: string; param_value: string; updated_at: string }>;
      return rows.map((r) => ({
        id: r.id,
        commandId: r.command_id,
        paramName: r.param_name,
        paramValue: r.param_value,
        updatedAt: r.updated_at
      }));
    }

    const rows = this.db.prepare(
      "SELECT id, command_id, param_name, param_value, updated_at FROM command_template_params ORDER BY command_id ASC, param_name ASC"
    ).all() as Array<{ id: string; command_id: string; param_name: string; param_value: string; updated_at: string }>;
    return rows.map((r) => ({
      id: r.id,
      commandId: r.command_id,
      paramName: r.param_name,
      paramValue: r.param_value,
      updatedAt: r.updated_at
    }));
  }

  upsertTemplateParams(commandId: string, params: Record<string, string>): void {
    const now = new Date().toISOString();
    const tx = this.db.transaction(() => {
      for (const [paramName, paramValue] of Object.entries(params)) {
        const existing = this.db.prepare(
          "SELECT id FROM command_template_params WHERE command_id = ? AND param_name = ?"
        ).get(commandId, paramName) as { id: string } | undefined;

        const id = existing?.id ?? randomUUID();
        this.db.prepare(
          `
            INSERT INTO command_template_params (id, command_id, param_name, param_value, updated_at)
            VALUES (@id, @command_id, @param_name, @param_value, @updated_at)
            ON CONFLICT(command_id, param_name) DO UPDATE SET
              param_value = excluded.param_value,
              updated_at = excluded.updated_at
          `
        ).run({
          id,
          command_id: commandId,
          param_name: paramName,
          param_value: paramValue,
          updated_at: now
        });
      }
    });
    tx();
  }

  clearTemplateParams(commandId: string): void {
    this.db.prepare("DELETE FROM command_template_params WHERE command_id = ?").run(commandId);
  }

  async backupDatabase(targetPath: string): Promise<void> {
    await this.db.backup(targetPath);
  }

  getDbPath(): string {
    return this.resolvedDbPath;
  }

  close(): void {
    this.db.close();
  }
}

// ─── SQLiteSecretStore ──────────────────────────────────────────────────────

class SQLiteSecretStore implements SecretStoreDB {
  constructor(private readonly db: Database.Database) {}

  putSecret(id: string, purpose: string, ciphertextB64: string, ivB64: string, tagB64: string, aad: string): void {
    const now = new Date().toISOString();
    this.db.prepare(
      `
        INSERT INTO secret_store (id, purpose, ciphertext_b64, iv_b64, tag_b64, aad, created_at, updated_at)
        VALUES (@id, @purpose, @ciphertext_b64, @iv_b64, @tag_b64, @aad, @created_at, @updated_at)
        ON CONFLICT(id) DO UPDATE SET
          purpose = excluded.purpose,
          ciphertext_b64 = excluded.ciphertext_b64,
          iv_b64 = excluded.iv_b64,
          tag_b64 = excluded.tag_b64,
          aad = excluded.aad,
          updated_at = excluded.updated_at
      `
    ).run({
      id,
      purpose,
      ciphertext_b64: ciphertextB64,
      iv_b64: ivB64,
      tag_b64: tagB64,
      aad,
      created_at: now,
      updated_at: now
    });
  }

  getSecret(id: string): { ciphertext_b64: string; iv_b64: string; tag_b64: string; aad: string } | undefined {
    const row = this.db.prepare(
      "SELECT ciphertext_b64, iv_b64, tag_b64, aad FROM secret_store WHERE id = ?"
    ).get(id) as { ciphertext_b64: string; iv_b64: string; tag_b64: string; aad: string } | undefined;
    return row;
  }

  deleteSecret(id: string): void {
    this.db.prepare("DELETE FROM secret_store WHERE id = ?").run(id);
  }

  listSecrets(): Array<{ id: string; purpose: string; ciphertext_b64: string; iv_b64: string; tag_b64: string; aad: string }> {
    return this.db.prepare(
      "SELECT id, purpose, ciphertext_b64, iv_b64, tag_b64, aad FROM secret_store ORDER BY id ASC"
    ).all() as Array<{ id: string; purpose: string; ciphertext_b64: string; iv_b64: string; tag_b64: string; aad: string }>;
  }
}
