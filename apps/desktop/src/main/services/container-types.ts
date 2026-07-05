import type { WebContents } from "electron";
import type {
  AppPreferences,
  BackspaceMode,
  DeleteMode,
  RecycleBinEntry,
  SessionDescriptor,
  TerminalEncoding,
} from "../../../../../packages/core/src/index";
import type { SshShellChannel, SshConnection } from "../../../../../packages/ssh/src/index";
import type { IPty } from "node-pty";
import type { SystemMonitorController } from "./monitor/system-monitor-controller";
import type { ProcessMonitorController } from "./monitor/process-monitor-controller";
import type { NetworkMonitorController } from "./monitor/network-monitor-controller";
import type { ConnectionService } from "./connection-service";
import type { ImportExportService } from "./import-export-service";
import type { SessionService } from "./session-service";
import type { MonitorService } from "./monitor-service";
import type { CommandService } from "./command-service";
import type { SftpService } from "./sftp-service";
import type { BackupPasswordService } from "./backup-password-service";
import type { NetworkToolService } from "./network-tool-service";
import type { PreferencesDialogService } from "./preferences-dialog-service";
import type { CloudSyncManager } from "./cloud-sync-manager";
import type { ResourceOperationsService } from "./resource-operations-service";

// ─── Active session types ──────────────────────────────────────────────────
export interface ActiveRemoteSession {
  kind: "remote";
  descriptor: SessionDescriptor;
  channel: SshShellChannel;
  sender: WebContents;
  connectionId: string;
  terminalEncoding: TerminalEncoding;
  backspaceMode: BackspaceMode;
  deleteMode: DeleteMode;
}

export interface ActiveLocalSession {
  kind: "local";
  descriptor: SessionDescriptor;
  pty: IPty;
  sender: WebContents;
  terminalEncoding: TerminalEncoding;
}

export type ActiveSession = ActiveRemoteSession | ActiveLocalSession;

// ─── Factory options ───────────────────────────────────────────────────────
export interface CreateServiceContainerOptions {
  dataDir: string;
  keytarServiceName?: string;
}

// ─── Monitor types ─────────────────────────────────────────────────────────
export interface MonitorState {
  selectedNetworkInterface?: string;
  networkInterfaceOptions?: string[];
}

export interface SystemMonitorRuntime {
  controller: SystemMonitorController;
  sender?: WebContents;
  disposed: boolean;
}

export interface ProcessMonitorRuntime {
  controller: ProcessMonitorController;
  sender?: WebContents;
  disposed: boolean;
}

export interface NetworkMonitorRuntime {
  controller: NetworkMonitorController;
  sender?: WebContents;
  disposed: boolean;
}

export interface AdhocSessionRuntime {
  connection: SshConnection;
  idleTimer?: ReturnType<typeof setTimeout>;
  lastUsedAt: number;
  disposed: boolean;
}

// ─── Public ServiceContainer interface ─────────────────────────────────────
/**
 * Facade over the main-process services.
 *
 * Single-service operations are reached through the exposed sub-services
 * (e.g. `services.sftp.listRemoteFiles(...)`). Only genuinely composed
 * orchestration — logic that spans multiple services or container-internal
 * state (repositories, timers, connection pool) — lives as methods here.
 */
export interface ServiceContainer {
  // Sub-services
  readonly connections: ConnectionService;
  readonly importExport: ImportExportService;
  readonly sessions: SessionService;
  readonly monitors: MonitorService;
  readonly commands: CommandService;
  readonly sftp: SftpService;
  readonly backupPassword: BackupPasswordService;
  readonly networkTools: NetworkToolService;
  readonly preferences: PreferencesDialogService;
  readonly cloudSync: CloudSyncManager;
  readonly resourceOps: ResourceOperationsService;

  // Orchestration
  /** Recycle-bin snapshot + tombstone (ResourceOperationsService) followed by
   *  record/runtime cleanup (ConnectionService). */
  removeConnection: (id: string) => Promise<{ ok: true }>;
  /** Recycle bin listing/clearing is backed by the connection repository,
   *  which is container-internal. */
  recycleBinList: () => RecycleBinEntry[];
  recycleBinClear: () => { ok: true; deleted: number };
  pauseMonitors: () => void;
  resumeMonitors: () => void;
  getAppPreferences: () => AppPreferences;
  dispose: () => Promise<void>;
}
