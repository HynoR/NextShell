import { randomUUID } from "node:crypto";
import type { WebContents } from "electron";
import type {
  ConnectionProfile,
  MonitorSnapshot,
  NetworkConnection,
  NetworkSnapshot,
  ProcessDetailSnapshot,
  ProcessSnapshot,
  SystemInfoSnapshot
} from "../../../../../packages/core/src/index";
import type { SshConnection } from "../../../../../packages/ssh/src/index";
import type { DebugLogEntry } from "../../../../../packages/shared/src/index";
import {
  SystemMonitorController,
  type MonitorSelectionState,
  type ProbeExecutionLog
} from "./monitor/system-monitor-controller";
import {
  ProcessMonitorController,
  type ProcessProbeExecutionLog
} from "./monitor/process-monitor-controller";
import { firstNonEmptyLine, parseProcessDetailPrimary } from "./monitor/process-probe-parser";
import { runTimedExec } from "./monitor/monitor-runner";
import {
  NetworkMonitorController,
  type NetworkProbeExecutionLog,
  type NetworkTool
} from "./monitor/network-monitor-controller";
import {
  parseCpuInfo,
  parseFilesystemEntries,
  parseMeminfoTotals,
  parseNetworkInterfaceTotals,
  parseOsReleaseName
} from "./system-info-parser";
import {
  MONITOR_SYSTEM_INFO_KERNEL_NAME_COMMAND,
  MONITOR_MAX_CONSECUTIVE_FAILURES,
  MONITOR_COMMAND_TIMEOUT_MS,
  parseUptimeSeconds,
  parseCompoundOutput,
  buildSystemInfoCommand
} from "./container-utils";
import type {
  ActiveSession,
  MonitorState,
  SystemMonitorRuntime,
  ProcessMonitorRuntime,
  NetworkMonitorRuntime
} from "./container-types";
import { logger } from "../logger";

interface ProbeLog {
  command: string;
  stdout: string;
  exitCode: number;
  durationMs: number;
  ok: boolean;
  error?: string;
}

export interface MonitorServiceOptions {
  getConnectionOrThrow: (id: string) => ConnectionProfile;
  activeSessions: Map<string, ActiveSession>;
  retainConnection: (connectionId: string) => () => void;
  debugSenders: Set<WebContents>;
  emitDebugLog: (entry: DebugLogEntry) => void;
  emitSystemSnapshot: (sender: WebContents, snapshot: MonitorSnapshot) => void;
  emitProcessSnapshot: (sender: WebContents, snapshot: ProcessSnapshot) => void;
  emitNetworkSnapshot: (sender: WebContents, snapshot: NetworkSnapshot) => void;
}

export class MonitorService {
  private readonly systemMonitorRuntimes = new Map<string, SystemMonitorRuntime>();
  private readonly processMonitorRuntimes = new Map<string, ProcessMonitorRuntime>();
  private readonly networkMonitorRuntimes = new Map<string, NetworkMonitorRuntime>();

  readonly monitorStates = new Map<string, MonitorState>();
  private readonly networkToolCache = new Map<string, NetworkTool>();

  private readonly getConnectionOrThrow: (id: string) => ConnectionProfile;
  private readonly activeSessions: Map<string, ActiveSession>;
  private readonly retainConnection: (connectionId: string) => () => void;
  private readonly debugSenders: Set<WebContents>;
  private readonly emitDebugLog: (entry: DebugLogEntry) => void;
  private readonly emitSystemSnapshot: (sender: WebContents, snapshot: MonitorSnapshot) => void;
  private readonly emitProcessSnapshot: (sender: WebContents, snapshot: ProcessSnapshot) => void;
  private readonly emitNetworkSnapshot: (sender: WebContents, snapshot: NetworkSnapshot) => void;

  constructor(options: MonitorServiceOptions) {
    this.getConnectionOrThrow = options.getConnectionOrThrow;
    this.activeSessions = options.activeSessions;
    this.retainConnection = options.retainConnection;
    this.debugSenders = options.debugSenders;
    this.emitDebugLog = options.emitDebugLog;
    this.emitSystemSnapshot = options.emitSystemSnapshot;
    this.emitProcessSnapshot = options.emitProcessSnapshot;
    this.emitNetworkSnapshot = options.emitNetworkSnapshot;
  }

  private assertMonitorEnabled(connectionId: string): ConnectionProfile {
    const profile = this.getConnectionOrThrow(connectionId);
    if (!profile.monitorSession) {
      throw new Error("当前连接未启用 Monitor Session，请在连接配置中开启后重试。");
    }
    return profile;
  }

  private getVisibleConnection(connectionId: string): SshConnection {
    const candidates = Array.from(this.activeSessions.values()).filter(
      (candidate): candidate is Extract<ActiveSession, { kind: "remote" }> =>
        candidate.kind === "remote" &&
        candidate.connectionId === connectionId &&
        candidate.descriptor.type === "terminal" &&
        candidate.descriptor.status === "connected" &&
        candidate.connection.isAlive
    );
    // Prefer a client with channel headroom so probe channels don't push a
    // fully loaded client past the server's MaxSessions.
    const session =
      candidates.find((candidate) => candidate.connection.hasChannelCapacity()) ?? candidates[0];
    if (!session) {
      throw new Error("请先连接 SSH 终端以启动 Monitor Session。");
    }
    return session.connection;
  }

  /**
   * One-shot exec on the terminal's shared connection. Always time-boxed: a hung
   * command here would otherwise pin a channel on the user's live terminal client
   * and leave the IPC call pending forever.
   */
  private execOnce(connectionId: string, command: string) {
    return runTimedExec(
      (cmd, options) => this.getVisibleConnection(connectionId).exec(cmd, options),
      command,
      MONITOR_COMMAND_TIMEOUT_MS
    );
  }

  private hasVisibleTerminalAlive(connectionId: string): boolean {
    return Array.from(this.activeSessions.values()).some(
      (session) =>
        session.kind === "remote" &&
        session.connectionId === connectionId &&
        session.descriptor.type === "terminal" &&
        session.descriptor.status === "connected" &&
        session.connection.isAlive
    );
  }

  private isSenderAlive(sender: WebContents | undefined): boolean {
    return Boolean(sender && !sender.isDestroyed() && !sender.isCrashed());
  }

  private emitProbeDebug(connectionId: string, entry: ProbeLog): void {
    if (this.debugSenders.size === 0) {
      return;
    }
    this.emitDebugLog({
      id: randomUUID(),
      timestamp: Date.now(),
      connectionId,
      command: entry.command,
      stdout: entry.stdout.slice(0, 4096),
      exitCode: entry.exitCode,
      durationMs: entry.durationMs,
      ok: entry.ok,
      error: entry.error
    });
  }

  private async disposeRuntime<
    T extends {
      controller: { stop: () => Promise<unknown> };
      sender?: WebContents;
      disposed: boolean;
      releaseConnection: () => void;
      stopPromise?: Promise<void>;
    }
  >(map: Map<string, T>, connectionId: string): Promise<void> {
    const runtime = map.get(connectionId);
    if (!runtime) {
      return;
    }
    if (runtime.stopPromise) {
      await runtime.stopPromise;
      return;
    }

    runtime.disposed = true;
    runtime.sender = undefined;
    const stopping = (async () => {
      try {
        await runtime.controller.stop();
      } finally {
        runtime.releaseConnection();
        if (map.get(connectionId) === runtime) {
          map.delete(connectionId);
        }
      }
    })();
    runtime.stopPromise = stopping;
    await stopping;
  }

  private async stopSystemFromController(connectionId: string, runtime: SystemMonitorRuntime) {
    if (!runtime.disposed && !runtime.stopPromise) {
      await this.disposeSystemMonitorRuntime(connectionId);
    }
  }

  private async stopProcessFromController(connectionId: string, runtime: ProcessMonitorRuntime) {
    if (!runtime.disposed && !runtime.stopPromise) {
      await this.disposeProcessMonitorRuntime(connectionId);
    }
  }

  private async stopNetworkFromController(connectionId: string, runtime: NetworkMonitorRuntime) {
    if (!runtime.disposed && !runtime.stopPromise) {
      await this.disposeNetworkMonitorRuntime(connectionId);
    }
  }

  async disposeSystemMonitorRuntime(connectionId: string): Promise<void> {
    await this.disposeRuntime(this.systemMonitorRuntimes, connectionId);
  }

  async disposeProcessMonitorRuntime(connectionId: string): Promise<void> {
    await this.disposeRuntime(this.processMonitorRuntimes, connectionId);
  }

  async disposeNetworkMonitorRuntime(connectionId: string): Promise<void> {
    await this.disposeRuntime(this.networkMonitorRuntimes, connectionId);
  }

  getAllConnectionIds(): string[] {
    return Array.from(
      new Set([
        ...this.systemMonitorRuntimes.keys(),
        ...this.processMonitorRuntimes.keys(),
        ...this.networkMonitorRuntimes.keys()
      ])
    );
  }

  async disposeAllMonitorSessions(connectionId: string): Promise<void> {
    await Promise.all([
      this.disposeSystemMonitorRuntime(connectionId),
      this.disposeProcessMonitorRuntime(connectionId),
      this.disposeNetworkMonitorRuntime(connectionId)
    ]);
    this.monitorStates.delete(connectionId);
    this.networkToolCache.delete(connectionId);
  }

  pauseAll(): void {
    for (const runtime of this.systemMonitorRuntimes.values()) runtime.controller.pause();
    for (const runtime of this.processMonitorRuntimes.values()) runtime.controller.pause();
    for (const runtime of this.networkMonitorRuntimes.values()) runtime.controller.pause();
  }

  resumeAll(): void {
    for (const runtime of this.systemMonitorRuntimes.values()) runtime.controller.resume();
    for (const runtime of this.processMonitorRuntimes.values()) runtime.controller.resume();
    for (const runtime of this.networkMonitorRuntimes.values()) runtime.controller.resume();
  }

  async ensureSystemMonitorRuntime(connectionId: string): Promise<SystemMonitorRuntime> {
    let existing = this.systemMonitorRuntimes.get(connectionId);
    while (existing?.stopPromise) {
      await existing.stopPromise;
      existing = this.systemMonitorRuntimes.get(connectionId);
    }
    if (existing && !existing.disposed) {
      return existing;
    }

    this.getVisibleConnection(connectionId);
    const releaseConnection = this.retainConnection(connectionId);
    let runtime: SystemMonitorRuntime;
    const controller = new SystemMonitorController({
      connectionId,
      exec: (command, options) => this.getVisibleConnection(connectionId).exec(command, options),
      stopMonitor: () => this.stopSystemFromController(connectionId, runtime),
      isVisibleTerminalAlive: () => this.hasVisibleTerminalAlive(connectionId),
      isReceiverAlive: () => this.isSenderAlive(runtime.sender),
      emitSnapshot: (snapshot) => {
        if (this.isSenderAlive(runtime.sender)) {
          this.emitSystemSnapshot(runtime.sender!, snapshot);
        }
      },
      readSelection: () => this.monitorStates.get(connectionId),
      writeSelection: (state: MonitorSelectionState) => {
        const previous = this.monitorStates.get(connectionId);
        this.monitorStates.set(connectionId, { ...previous, ...state });
      },
      logger,
      onProbeExecution: (entry: ProbeExecutionLog) => {
        this.emitProbeDebug(connectionId, entry);
        if (!entry.ok && entry.exitCode >= 0) {
          logger.debug("[SystemMonitor] command non-zero exit", {
            connectionId,
            command: entry.command,
            exitCode: entry.exitCode,
            output: entry.stdout.slice(0, 200)
          });
        }
      },
      timing: {
        pollIntervalMs: 2000,
        execTimeoutMs: MONITOR_COMMAND_TIMEOUT_MS,
        maxConsecutiveFailures: MONITOR_MAX_CONSECUTIVE_FAILURES
      }
    });
    runtime = { controller, sender: undefined, disposed: false, releaseConnection };
    this.systemMonitorRuntimes.set(connectionId, runtime);
    logger.info("[SystemMonitor] runtime ready", { connectionId });
    return runtime;
  }

  private async ensureProcessMonitorRuntime(connectionId: string): Promise<ProcessMonitorRuntime> {
    let existing = this.processMonitorRuntimes.get(connectionId);
    while (existing?.stopPromise) {
      await existing.stopPromise;
      existing = this.processMonitorRuntimes.get(connectionId);
    }
    if (existing && !existing.disposed) {
      return existing;
    }

    this.getVisibleConnection(connectionId);
    const releaseConnection = this.retainConnection(connectionId);
    let runtime: ProcessMonitorRuntime;
    const controller = new ProcessMonitorController({
      connectionId,
      exec: (command, options) => this.getVisibleConnection(connectionId).exec(command, options),
      stopMonitor: () => this.stopProcessFromController(connectionId, runtime),
      isVisibleTerminalAlive: () => this.hasVisibleTerminalAlive(connectionId),
      isReceiverAlive: () => this.isSenderAlive(runtime.sender),
      emitSnapshot: (snapshot) => {
        if (this.isSenderAlive(runtime.sender)) {
          this.emitProcessSnapshot(runtime.sender!, snapshot);
        }
      },
      logger,
      onProbeExecution: (entry: ProcessProbeExecutionLog) =>
        this.emitProbeDebug(connectionId, entry),
      timing: {
        pollIntervalMs: 2000,
        execTimeoutMs: MONITOR_COMMAND_TIMEOUT_MS,
        maxConsecutiveFailures: MONITOR_MAX_CONSECUTIVE_FAILURES
      }
    });
    runtime = { controller, sender: undefined, disposed: false, releaseConnection };
    this.processMonitorRuntimes.set(connectionId, runtime);
    logger.info("[ProcessMonitor] runtime ready", { connectionId });
    return runtime;
  }

  private async ensureNetworkMonitorRuntime(connectionId: string): Promise<NetworkMonitorRuntime> {
    let existing = this.networkMonitorRuntimes.get(connectionId);
    while (existing?.stopPromise) {
      await existing.stopPromise;
      existing = this.networkMonitorRuntimes.get(connectionId);
    }
    if (existing && !existing.disposed) {
      return existing;
    }

    this.getVisibleConnection(connectionId);
    const releaseConnection = this.retainConnection(connectionId);
    let runtime: NetworkMonitorRuntime;
    const controller = new NetworkMonitorController({
      connectionId,
      exec: (command, options) => this.getVisibleConnection(connectionId).exec(command, options),
      stopMonitor: () => this.stopNetworkFromController(connectionId, runtime),
      isVisibleTerminalAlive: () => this.hasVisibleTerminalAlive(connectionId),
      isReceiverAlive: () => this.isSenderAlive(runtime.sender),
      emitSnapshot: (snapshot) => {
        if (this.isSenderAlive(runtime.sender)) {
          this.emitNetworkSnapshot(runtime.sender!, snapshot);
        }
      },
      readToolCache: () => this.networkToolCache.get(connectionId),
      writeToolCache: (tool) => {
        if (tool) this.networkToolCache.set(connectionId, tool);
        else this.networkToolCache.delete(connectionId);
      },
      logger,
      onProbeExecution: (entry: NetworkProbeExecutionLog) =>
        this.emitProbeDebug(connectionId, entry),
      timing: {
        pollIntervalMs: 2000,
        execTimeoutMs: MONITOR_COMMAND_TIMEOUT_MS,
        maxConsecutiveFailures: MONITOR_MAX_CONSECUTIVE_FAILURES
      }
    });
    runtime = { controller, sender: undefined, disposed: false, releaseConnection };
    this.networkMonitorRuntimes.set(connectionId, runtime);
    logger.info("[NetworkMonitor] runtime ready", { connectionId });
    return runtime;
  }

  async startSystemMonitor(connectionId: string, sender: WebContents): Promise<{ ok: true }> {
    this.assertMonitorEnabled(connectionId);
    this.getVisibleConnection(connectionId);
    const runtime = await this.ensureSystemMonitorRuntime(connectionId);
    runtime.sender = sender;
    try {
      return await runtime.controller.start();
    } catch (error) {
      await this.disposeSystemMonitorRuntime(connectionId);
      throw error;
    }
  }

  async stopSystemMonitor(connectionId: string): Promise<{ ok: true }> {
    await this.disposeSystemMonitorRuntime(connectionId);
    return { ok: true };
  }

  async selectSystemNetworkInterface(
    connectionId: string,
    networkInterface: string
  ): Promise<{ ok: true }> {
    this.assertMonitorEnabled(connectionId);
    this.getVisibleConnection(connectionId);
    const runtime = await this.ensureSystemMonitorRuntime(connectionId);
    return runtime.controller.selectNetworkInterface(networkInterface);
  }

  async getSystemInfoSnapshot(connectionId: string): Promise<SystemInfoSnapshot> {
    this.assertMonitorEnabled(connectionId);
    const kernel = await this.execOnce(connectionId, MONITOR_SYSTEM_INFO_KERNEL_NAME_COMMAND);
    const platform = kernel.stdout.trim().split(/\s+/)[0] ?? "";
    if (kernel.exitCode !== 0 || platform !== "Linux") {
      throw new Error("系统信息标签页当前仅支持 Linux 主机");
    }

    const result = await this.execOnce(connectionId, buildSystemInfoCommand());
    const sections = parseCompoundOutput(result.stdout);
    const totals = parseMeminfoTotals(sections.get("MEMINFO") ?? "");
    return {
      connectionId,
      hostname: (sections.get("HOSTNAME") ?? "").trim() || "unknown",
      osName: parseOsReleaseName(sections.get("OSRELEASE") ?? ""),
      kernelName: (sections.get("KERNELNAME") ?? "").trim() || "Linux",
      kernelVersion: (sections.get("KERNELVER") ?? "").trim() || "unknown",
      architecture: (sections.get("ARCH") ?? "").trim() || "unknown",
      cpu: parseCpuInfo(sections.get("CPUINFO") ?? ""),
      memoryTotalKb: totals.memoryTotalKb,
      swapTotalKb: totals.swapTotalKb,
      networkInterfaces: parseNetworkInterfaceTotals(sections.get("NETDEV") ?? ""),
      filesystems: parseFilesystemEntries(sections.get("FILESYSTEMS") ?? ""),
      uptimeSeconds: parseUptimeSeconds(sections.get("UPTIME") ?? ""),
      capturedAt: new Date().toISOString()
    };
  }

  async startProcessMonitor(connectionId: string, sender: WebContents): Promise<{ ok: true }> {
    this.assertMonitorEnabled(connectionId);
    this.getVisibleConnection(connectionId);
    const runtime = await this.ensureProcessMonitorRuntime(connectionId);
    runtime.sender = sender;
    try {
      return await runtime.controller.start();
    } catch (error) {
      await this.disposeProcessMonitorRuntime(connectionId);
      throw error;
    }
  }

  async stopProcessMonitor(connectionId: string): Promise<{ ok: true }> {
    await this.disposeProcessMonitorRuntime(connectionId);
    return { ok: true };
  }

  async getProcessDetail(connectionId: string, pid: number): Promise<ProcessDetailSnapshot> {
    this.assertMonitorEnabled(connectionId);
    const normalizedPid = Math.trunc(pid);
    if (normalizedPid < 1) {
      throw new Error("无效进程 PID");
    }
    const primaryCommand = `ps -p ${normalizedPid} -o pid=,ppid=,user=,state=,%cpu=,%mem=,rss=,etime=,comm=`;
    const primary = await this.execOnce(connectionId, primaryCommand);
    if (primary.exitCode !== 0) {
      throw new Error("进程不存在或已结束");
    }

    const parsed = parseProcessDetailPrimary(connectionId, primary.stdout);
    if (!parsed) {
      throw new Error("进程不存在或已结束");
    }

    const args = await this.execOnce(connectionId, `ps -p ${normalizedPid} -o args=`);
    return {
      ...parsed,
      commandLine:
        args.exitCode === 0 ? (firstNonEmptyLine(args.stdout) ?? parsed.command) : parsed.command,
      capturedAt: new Date().toISOString()
    };
  }

  async killRemoteProcess(
    connectionId: string,
    pid: number,
    signal: "SIGTERM" | "SIGKILL"
  ): Promise<{ ok: true }> {
    this.assertMonitorEnabled(connectionId);
    if (signal !== "SIGTERM" && signal !== "SIGKILL") {
      throw new Error("Invalid signal");
    }
    const normalizedPid = Math.trunc(pid);
    if (normalizedPid < 1) {
      throw new Error("无效进程 PID");
    }
    const result = await this.execOnce(connectionId, `kill -${signal} ${normalizedPid} 2>&1`);
    if (result.exitCode !== 0) {
      throw new Error(
        `kill 失败 (exit ${result.exitCode}): ${result.stdout.trim() || "unknown error"}`
      );
    }
    return { ok: true };
  }

  async startNetworkMonitor(connectionId: string, sender: WebContents): Promise<{ ok: true }> {
    this.assertMonitorEnabled(connectionId);
    this.getVisibleConnection(connectionId);
    const runtime = await this.ensureNetworkMonitorRuntime(connectionId);
    runtime.sender = sender;
    try {
      return await runtime.controller.start();
    } catch (error) {
      await this.disposeNetworkMonitorRuntime(connectionId);
      throw error;
    }
  }

  async stopNetworkMonitor(connectionId: string): Promise<{ ok: true }> {
    await this.disposeNetworkMonitorRuntime(connectionId);
    return { ok: true };
  }

  async getNetworkConnections(connectionId: string, port: number): Promise<NetworkConnection[]> {
    this.assertMonitorEnabled(connectionId);
    this.getVisibleConnection(connectionId);
    const runtime = await this.ensureNetworkMonitorRuntime(connectionId);
    try {
      return await runtime.controller.getConnectionsByPort(port);
    } finally {
      if (!runtime.sender && runtime.controller.currentState === "IDLE") {
        await this.disposeNetworkMonitorRuntime(connectionId);
      }
    }
  }
}
