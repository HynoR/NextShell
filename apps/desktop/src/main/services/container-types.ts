import type { WebContents } from "electron";
import type {
  AppPreferences,
  BackspaceMode,
  DeleteMode,
  RecycleBinEntry,
  SessionDescriptor,
  TerminalEncoding
} from "../../../../../packages/core/src/index";
import type { DeviceKeyNotice } from "../../../../../packages/shared/src/index";
import type { ConnectionFolderService } from "./connection-folder-service";
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
import type { NetworkToolService } from "./network-tool-service";
import type { PreferencesDialogService } from "./preferences-dialog-service";
import type { TerminalIntegrationService } from "./terminal-integration-service";
import type { CloudSyncManager } from "./cloud-sync-manager";
import type { ResourceOperationsService } from "./resource-operations-service";
import type { AgentMcpService } from "./mcp";

// ─── Active session types ──────────────────────────────────────────────────
export interface ActiveRemoteSession {
  kind: "remote";
  descriptor: SessionDescriptor;
  channel: SshShellChannel;
  connection: SshConnection;
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
  /**
   * Electron `app.getPath("userData")` — the profile root, not a subdirectory.
   * SQLite lives in `<userDataDir>/storage`, the MCP endpoint discovery file in
   * `<userDataDir>/mcp` (the path `@nextshell/mcp-bridge` probes). Passing the
   * storage subdirectory here would move the discovery file out of the bridge's
   * search path, so the container derives both locations from this one root.
   */
  userDataDir: string;
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
  releaseConnection: () => void;
  stopPromise?: Promise<void>;
}

export interface ProcessMonitorRuntime {
  controller: ProcessMonitorController;
  sender?: WebContents;
  disposed: boolean;
  releaseConnection: () => void;
  stopPromise?: Promise<void>;
}

export interface NetworkMonitorRuntime {
  controller: NetworkMonitorController;
  sender?: WebContents;
  disposed: boolean;
  releaseConnection: () => void;
  stopPromise?: Promise<void>;
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
  readonly networkTools: NetworkToolService;
  readonly preferences: PreferencesDialogService;
  readonly terminalIntegration: TerminalIntegrationService;
  readonly cloudSync: CloudSyncManager;
  readonly resourceOps: ResourceOperationsService;
  /**
   * 连接目录。仓储面之外还要编排一件事:rename/move/remove 之后重投影该作用域内所有连接的
   * `groupPath`(云同步线协议、MCP schema、导出文件都读这个投影),所以这里暴露的是服务而
   * 不是裸仓储。
   */
  readonly connectionFolders: ConnectionFolderService;
  /** MCP endpoint for agents. Dark until `preferences.agent.enabled` is true. */
  readonly agentMcp: AgentMcpService;

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
  /** One-time notice: the legacy keychain device key was unreadable and stored credentials are lost. */
  getDeviceKeyNotice: () => DeviceKeyNotice;
  acknowledgeDeviceKeyNotice: () => { ok: true };
  dispose: () => Promise<void>;
}
