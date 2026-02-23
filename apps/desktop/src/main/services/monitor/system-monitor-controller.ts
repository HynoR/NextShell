import type { MonitorProcess, MonitorSnapshot } from "../../../../../../packages/core/src/index";
import type { SshConnection } from "../../../../../../packages/ssh/src/index";
import {
  buildDynamicSystemProbeCommand,
  MONITOR_NET_INTERFACES_COMMAND,
  normalizeNetworkInterfaceName,
} from "./system-probe-command";
import {
  parseCompoundOutput,
  parseNetworkInterfaceList,
  parseSystemProbeSections,
  type ParsedDiskTotals,
  type ParsedMemoryTotals,
  type ParsedNetworkCounters,
  type ParsedSystemProbeFrame,
} from "./system-probe-parser";
import { MonitorExecTimeoutError, runTimedExec } from "./monitor-runner";

const DEFAULT_POLL_INTERVAL_MS = 1000;
const DEFAULT_CPU_MEM_SWAP_INTERVAL_TICKS = 3;
const DEFAULT_DISK_INTERVAL_TICKS = 10;
const DEFAULT_INTERFACE_META_INTERVAL_TICKS = 30;
const DEFAULT_START_DELAY_MS = 300;
const DEFAULT_EXEC_TIMEOUT_MS = 20_000;
const DEFAULT_MAX_CONSECUTIVE_FAILURES = 3;

export type SystemMonitorControllerState = "IDLE" | "STARTING" | "RUNNING" | "STOPPING" | "STOPPED";

export interface MonitorSelectionState {
  selectedNetworkInterface?: string;
  networkInterfaceOptions?: string[];
}

export interface SystemMonitorLogger {
  info: (message: string, metadata?: Record<string, unknown>) => void;
  warn: (message: string, metadata?: Record<string, unknown>) => void;
  debug: (message: string, metadata?: Record<string, unknown>) => void;
}

export interface ProbeExecutionLog {
  command: string;
  stdout: string;
  exitCode: number;
  durationMs: number;
  ok: boolean;
  error?: string;
}

export interface SystemMonitorControllerOptions {
  connectionId: string;
  getConnection: () => Promise<SshConnection>;
  closeConnection: () => Promise<void>;
  isVisibleTerminalAlive: () => boolean;
  isReceiverAlive: () => boolean;
  emitSnapshot: (snapshot: MonitorSnapshot) => void;
  readSelection: () => MonitorSelectionState | undefined;
  writeSelection: (state: MonitorSelectionState) => void;
  logger: SystemMonitorLogger;
  onProbeExecution?: (entry: ProbeExecutionLog) => void;
  timing?: {
    pollIntervalMs?: number;
    cpuMemSwapIntervalTicks?: number;
    diskIntervalTicks?: number;
    interfaceMetaIntervalTicks?: number;
    startDelayMs?: number;
    execTimeoutMs?: number;
    maxConsecutiveFailures?: number;
  };
}

interface ProbeFlags {
  collectCpuMemSwap: boolean;
  collectDisk: boolean;
  includeInterfaceMeta: boolean;
}

const emptyMemory = (): ParsedMemoryTotals => ({
  memTotalKb: 0,
  memAvailableKb: 0,
  swapTotalKb: 0,
  swapFreeKb: 0,
});

const emptyDisk = (): ParsedDiskTotals => ({
  diskTotalKb: 0,
  diskUsedKb: 0,
});

const toSnapshot = (
  connectionId: string,
  loadAverage: [number, number, number],
  cpuPercent: number,
  memory: ParsedMemoryTotals,
  disk: ParsedDiskTotals,
  networkInMbps: number,
  networkOutMbps: number,
  networkInterface: string,
  networkInterfaceOptions: string[],
  processes: MonitorProcess[]
): MonitorSnapshot => {
  const memoryUsedKb = Math.max(0, memory.memTotalKb - memory.memAvailableKb);
  const swapUsedKb = Math.max(0, memory.swapTotalKb - memory.swapFreeKb);

  const memoryPercent = memory.memTotalKb > 0 ? (memoryUsedKb / memory.memTotalKb) * 100 : 0;
  const swapPercent = memory.swapTotalKb > 0 ? (swapUsedKb / memory.swapTotalKb) * 100 : 0;
  const diskPercent = disk.diskTotalKb > 0 ? (disk.diskUsedKb / disk.diskTotalKb) * 100 : 0;

  return {
    connectionId,
    loadAverage,
    cpuPercent: Number(Math.max(0, cpuPercent).toFixed(2)),
    memoryPercent: Number(memoryPercent.toFixed(2)),
    memoryUsedMb: Number((memoryUsedKb / 1024).toFixed(2)),
    memoryTotalMb: Number((memory.memTotalKb / 1024).toFixed(2)),
    swapPercent: Number(swapPercent.toFixed(2)),
    swapUsedMb: Number((swapUsedKb / 1024).toFixed(2)),
    swapTotalMb: Number((memory.swapTotalKb / 1024).toFixed(2)),
    diskPercent: Number(diskPercent.toFixed(2)),
    diskUsedGb: Number((disk.diskUsedKb / (1024 * 1024)).toFixed(2)),
    diskTotalGb: Number((disk.diskTotalKb / (1024 * 1024)).toFixed(2)),
    networkInMbps: Number(Math.max(0, networkInMbps).toFixed(2)),
    networkOutMbps: Number(Math.max(0, networkOutMbps).toFixed(2)),
    networkInterface,
    networkInterfaceOptions,
    processes,
    capturedAt: new Date().toISOString(),
  };
};

const wait = async (durationMs: number): Promise<void> => {
  if (durationMs <= 0) {
    return;
  }

  await new Promise<void>((resolve) => {
    setTimeout(resolve, durationMs);
  });
};

export class SystemMonitorController {
  private readonly pollIntervalMs: number;
  private readonly cpuMemSwapIntervalTicks: number;
  private readonly diskIntervalTicks: number;
  private readonly interfaceMetaIntervalTicks: number;
  private readonly startDelayMs: number;
  private readonly execTimeoutMs: number;
  private readonly maxConsecutiveFailures: number;

  private state: SystemMonitorControllerState = "IDLE";
  private generation = 0;
  private tickCount = 0;
  private timer: ReturnType<typeof setInterval> | undefined;
  private inFlight = false;
  private consecutiveFailures = 0;

  private networkInterface = "eth0";
  private networkInterfaceOptions: string[] = [];

  private prevNetRx: number | undefined;
  private prevNetTx: number | undefined;
  private prevNetSampledAt: number | undefined;
  private cachedNetInMbps = 0;
  private cachedNetOutMbps = 0;

  private prevCpuTotal: number | undefined;
  private prevCpuIdle: number | undefined;
  private cachedCpuPercent = 0;

  private cachedLoadAvg: [number, number, number] = [0, 0, 0];
  private cachedMemory: ParsedMemoryTotals = emptyMemory();
  private cachedDisk: ParsedDiskTotals = emptyDisk();
  private cachedProcesses: MonitorProcess[] = [];

  constructor(private readonly options: SystemMonitorControllerOptions) {
    this.pollIntervalMs = options.timing?.pollIntervalMs ?? DEFAULT_POLL_INTERVAL_MS;
    this.cpuMemSwapIntervalTicks =
      options.timing?.cpuMemSwapIntervalTicks ?? DEFAULT_CPU_MEM_SWAP_INTERVAL_TICKS;
    this.diskIntervalTicks = options.timing?.diskIntervalTicks ?? DEFAULT_DISK_INTERVAL_TICKS;
    this.interfaceMetaIntervalTicks =
      options.timing?.interfaceMetaIntervalTicks ?? DEFAULT_INTERFACE_META_INTERVAL_TICKS;
    this.startDelayMs = options.timing?.startDelayMs ?? DEFAULT_START_DELAY_MS;
    this.execTimeoutMs = options.timing?.execTimeoutMs ?? DEFAULT_EXEC_TIMEOUT_MS;
    this.maxConsecutiveFailures =
      options.timing?.maxConsecutiveFailures ?? DEFAULT_MAX_CONSECUTIVE_FAILURES;
  }

  get currentState(): SystemMonitorControllerState {
    return this.state;
  }

  async start(): Promise<{ ok: true }> {
    if (this.state === "RUNNING" || this.state === "STARTING") {
      return { ok: true };
    }

    if (!this.options.isVisibleTerminalAlive()) {
      throw new Error("请先连接 SSH 终端以启动 Monitor Session。");
    }

    this.state = "STARTING";
    const generation = this.bumpGeneration();
    this.tickCount = 0;
    this.consecutiveFailures = 0;
    this.inFlight = false;
    this.resetSamplingBaselines();
    this.syncSelectionState();

    try {
      await wait(this.startDelayMs);
      if (!this.isGenerationActive(generation)) {
        return { ok: true };
      }

      await this.options.getConnection();
      if (!this.isGenerationActive(generation)) {
        return { ok: true };
      }

      this.state = "RUNNING";
      await this.runProbe({
        collectCpuMemSwap: true,
        collectDisk: true,
        includeInterfaceMeta: true,
      }, generation);

      if (this.isGenerationActive(generation)) {
        this.startTicker(generation);
        this.options.logger.info("[SystemMonitor] started (net 1s, cpu/mem/swap 3s, disk 10s)", {
          connectionId: this.options.connectionId,
        });
      }

      return { ok: true };
    } catch (error) {
      if (this.generation === generation) {
        if (this.timer) {
          clearInterval(this.timer);
          this.timer = undefined;
        }
        try {
          await this.options.closeConnection();
        } finally {
          this.state = "STOPPED";
        }
      }
      throw error;
    }
  }

  async stop(): Promise<{ ok: true }> {
    if (this.state === "IDLE" || this.state === "STOPPED") {
      if (this.timer) {
        clearInterval(this.timer);
        this.timer = undefined;
      }
      await this.options.closeConnection();
      return { ok: true };
    }

    this.state = "STOPPING";
    this.bumpGeneration();
    if (this.timer) {
      clearInterval(this.timer);
      this.timer = undefined;
    }

    try {
      await this.options.closeConnection();
    } finally {
      this.inFlight = false;
      this.state = "STOPPED";
    }
    this.options.logger.info("[SystemMonitor] stopped", { connectionId: this.options.connectionId });

    return { ok: true };
  }

  async selectNetworkInterface(networkInterface: string): Promise<{ ok: true }> {
    if (!this.options.isVisibleTerminalAlive()) {
      throw new Error("请先连接 SSH 终端以启动 Monitor Session。");
    }

    const normalized = normalizeNetworkInterfaceName(networkInterface);
    if (!normalized) {
      throw new Error("无效网卡名称");
    }

    const connection = await this.options.getConnection();
    const result = await runTimedExec(connection, MONITOR_NET_INTERFACES_COMMAND, this.execTimeoutMs);
    if (result.exitCode !== 0) {
      throw new Error(`网卡列表读取失败 (exit ${result.exitCode})`);
    }

    const options = parseNetworkInterfaceList(result.stdout);
    if (!options.includes(normalized)) {
      throw new Error(`网卡不存在或不可用: ${normalized}`);
    }

    this.networkInterface = normalized;
    this.networkInterfaceOptions = options;
    this.prevNetRx = undefined;
    this.prevNetTx = undefined;
    this.prevNetSampledAt = undefined;
    this.cachedNetInMbps = 0;
    this.cachedNetOutMbps = 0;

    this.options.writeSelection({
      selectedNetworkInterface: normalized,
      networkInterfaceOptions: options,
    });

    return { ok: true };
  }

  private bumpGeneration(): number {
    this.generation += 1;
    return this.generation;
  }

  private isGenerationActive(generation: number): boolean {
    return generation === this.generation && (this.state === "STARTING" || this.state === "RUNNING");
  }

  private startTicker(generation: number): void {
    if (!this.isGenerationActive(generation) || this.state !== "RUNNING") {
      return;
    }

    if (this.timer) {
      clearInterval(this.timer);
    }

    this.timer = setInterval(() => {
      void this.poll(generation);
    }, this.pollIntervalMs);
  }

  private resetSamplingBaselines(): void {
    this.prevNetRx = undefined;
    this.prevNetTx = undefined;
    this.prevNetSampledAt = undefined;
    this.prevCpuTotal = undefined;
    this.prevCpuIdle = undefined;
    this.cachedNetInMbps = 0;
    this.cachedNetOutMbps = 0;
    this.cachedCpuPercent = 0;
  }

  private async poll(generation: number): Promise<void> {
    if (!this.isGenerationActive(generation) || this.state !== "RUNNING") {
      return;
    }

    if (!this.options.isVisibleTerminalAlive() || !this.options.isReceiverAlive()) {
      await this.stop();
      return;
    }

    this.tickCount += 1;

    if (this.inFlight) {
      this.options.logger.debug("[SystemMonitor] drop frame: previous probe still running", {
        connectionId: this.options.connectionId,
        tickCount: this.tickCount,
      });
      return;
    }

    const flags: ProbeFlags = {
      collectCpuMemSwap: this.tickCount % this.cpuMemSwapIntervalTicks === 0,
      collectDisk: this.tickCount % this.diskIntervalTicks === 0,
      includeInterfaceMeta: this.tickCount % this.interfaceMetaIntervalTicks === 0,
    };

    this.inFlight = true;
    try {
      await this.runProbe(flags, generation);
    } finally {
      this.inFlight = false;
    }
  }

  private syncSelectionState(): void {
    const selection = this.options.readSelection();
    const selected = normalizeNetworkInterfaceName(selection?.selectedNetworkInterface ?? "");

    if (selection?.networkInterfaceOptions && selection.networkInterfaceOptions.length > 0) {
      this.networkInterfaceOptions = selection.networkInterfaceOptions;
    }

    if (selected) {
      this.networkInterface = selected;
    } else if (this.networkInterfaceOptions.length > 0 && !this.networkInterfaceOptions.includes(this.networkInterface)) {
      this.networkInterface = this.networkInterfaceOptions[0] ?? "eth0";
    }
  }

  private resolveInterface(frame: ParsedSystemProbeFrame): void {
    const selection = this.options.readSelection();
    const selected = normalizeNetworkInterfaceName(selection?.selectedNetworkInterface ?? "");
    const counterInterface = normalizeNetworkInterfaceName(frame.networkCounterInterface ?? "");
    const previousInterface = this.networkInterface;

    if (frame.networkInterfaceOptions && frame.networkInterfaceOptions.length > 0) {
      this.networkInterfaceOptions = frame.networkInterfaceOptions;
    }

    if (selected && this.networkInterfaceOptions.includes(selected)) {
      this.networkInterface = selected;
    } else if (counterInterface && this.networkInterfaceOptions.includes(counterInterface)) {
      this.networkInterface = counterInterface;
    } else if (frame.defaultNetworkInterface && this.networkInterfaceOptions.includes(frame.defaultNetworkInterface)) {
      this.networkInterface = frame.defaultNetworkInterface;
    } else if (counterInterface) {
      this.networkInterface = counterInterface;
    } else if (!this.networkInterfaceOptions.includes(this.networkInterface) && this.networkInterfaceOptions.length > 0) {
      this.networkInterface = this.networkInterfaceOptions[0] ?? this.networkInterface;
    }

    const effectiveSelectedInterface =
      selected && this.networkInterfaceOptions.includes(selected)
        ? selected
        : this.networkInterface;

    if (previousInterface !== this.networkInterface) {
      this.prevNetRx = undefined;
      this.prevNetTx = undefined;
      this.prevNetSampledAt = undefined;
      this.cachedNetInMbps = 0;
      this.cachedNetOutMbps = 0;
    }

    this.options.writeSelection({
      selectedNetworkInterface: effectiveSelectedInterface,
      networkInterfaceOptions: this.networkInterfaceOptions,
    });
  }

  private updateNetwork(counters: ParsedNetworkCounters): void {
    const now = Date.now();

    if (
      this.prevNetRx !== undefined &&
      this.prevNetTx !== undefined &&
      this.prevNetSampledAt !== undefined
    ) {
      const elapsed = (now - this.prevNetSampledAt) / 1000;
      const deltaRx = counters.rxBytes - this.prevNetRx;
      const deltaTx = counters.txBytes - this.prevNetTx;

      if (elapsed > 0 && deltaRx >= 0 && deltaTx >= 0) {
        this.cachedNetInMbps = (deltaRx * 8) / (elapsed * 1e6);
        this.cachedNetOutMbps = (deltaTx * 8) / (elapsed * 1e6);
      }
    }

    this.prevNetRx = counters.rxBytes;
    this.prevNetTx = counters.txBytes;
    this.prevNetSampledAt = now;
  }

  private updateCpuMemSwap(frame: ParsedSystemProbeFrame): void {
    if (frame.cpuTotal !== undefined && frame.cpuIdle !== undefined) {
      if (this.prevCpuTotal !== undefined && this.prevCpuIdle !== undefined) {
        const deltaTotal = frame.cpuTotal - this.prevCpuTotal;
        const deltaIdle = frame.cpuIdle - this.prevCpuIdle;
        if (deltaTotal > 0) {
          this.cachedCpuPercent = ((deltaTotal - deltaIdle) / deltaTotal) * 100;
        }
      }

      this.prevCpuTotal = frame.cpuTotal;
      this.prevCpuIdle = frame.cpuIdle;
    }

    if (frame.memory) {
      this.cachedMemory = frame.memory;
    }

    if (frame.loadAverage) {
      this.cachedLoadAvg = frame.loadAverage;
    }

    if (frame.processes) {
      this.cachedProcesses = frame.processes;
    }
  }

  private updateDisk(frame: ParsedSystemProbeFrame): void {
    if (frame.disk) {
      this.cachedDisk = frame.disk;
    }
  }

  private async runProbe(flags: ProbeFlags, generation: number): Promise<void> {
    if (!this.isGenerationActive(generation)) {
      return;
    }

    this.syncSelectionState();

    const includeInterfaceMeta =
      flags.includeInterfaceMeta ||
      this.networkInterfaceOptions.length === 0 ||
      !this.networkInterfaceOptions.includes(this.networkInterface);

    const command = buildDynamicSystemProbeCommand(this.networkInterface, {
      collectCpuMemSwap: flags.collectCpuMemSwap,
      collectDisk: flags.collectDisk,
      includeInterfaceMeta,
    });

    let stdout = "";
    try {
      const connection = await this.options.getConnection();
      const result = await runTimedExec(connection, command, this.execTimeoutMs);
      if (!this.isGenerationActive(generation) || this.state !== "RUNNING") {
        return;
      }

      stdout = result.stdout;
      this.options.onProbeExecution?.({
        command,
        stdout: result.stdout.slice(0, 4096),
        exitCode: result.exitCode,
        durationMs: result.durationMs,
        ok: result.exitCode === 0,
      });

      if (result.exitCode !== 0) {
        this.consecutiveFailures += 1;
        this.options.logger.debug("[SystemMonitor] drop frame: command non-zero exit", {
          connectionId: this.options.connectionId,
          exitCode: result.exitCode,
          tickCount: this.tickCount,
        });
        if (this.consecutiveFailures >= this.maxConsecutiveFailures) {
          await this.options.closeConnection();
          this.consecutiveFailures = 0;
        }
        return;
      }

      this.consecutiveFailures = 0;
    } catch (error) {
      if (!this.isGenerationActive(generation) || this.state !== "RUNNING") {
        return;
      }
      const errorMessage = error instanceof Error ? error.message : String(error);
      this.options.onProbeExecution?.({
        command,
        stdout: stdout.slice(0, 4096),
        exitCode: -1,
        durationMs: 0,
        ok: false,
        error: errorMessage,
      });

      this.consecutiveFailures += 1;
      if (error instanceof MonitorExecTimeoutError || this.consecutiveFailures >= this.maxConsecutiveFailures) {
        await this.options.closeConnection();
        this.consecutiveFailures = 0;
      }

      this.options.logger.warn("[SystemMonitor] drop frame: probe execution failed", {
        connectionId: this.options.connectionId,
        reason: errorMessage,
        tickCount: this.tickCount,
      });
      return;
    }

    if (!this.isGenerationActive(generation) || this.state !== "RUNNING") {
      return;
    }

    const sections = parseCompoundOutput(stdout);
    const parsed = parseSystemProbeSections(sections, {
      collectCpuMemSwap: flags.collectCpuMemSwap,
      collectDisk: flags.collectDisk,
      includeInterfaceMeta,
    });

    if (!parsed.ok) {
      this.options.logger.warn("[SystemMonitor] drop frame: invalid probe payload", {
        connectionId: this.options.connectionId,
        reason: parsed.reason,
        missingSections: parsed.missingSections,
        tickCount: this.tickCount,
      });
      if (parsed.reason === "invalid NETCOUNTERS") {
        this.networkInterfaceOptions = [];
      }
      return;
    }

    if (!this.isGenerationActive(generation) || this.state !== "RUNNING") {
      return;
    }

    this.resolveInterface(parsed.frame);
    this.updateNetwork(parsed.frame.networkCounters);

    if (flags.collectCpuMemSwap) {
      this.updateCpuMemSwap(parsed.frame);
    }

    if (flags.collectDisk) {
      this.updateDisk(parsed.frame);
    }

    if (!this.options.isReceiverAlive() || !this.isGenerationActive(generation)) {
      return;
    }

    const snapshot = toSnapshot(
      this.options.connectionId,
      this.cachedLoadAvg,
      this.cachedCpuPercent,
      this.cachedMemory,
      this.cachedDisk,
      this.cachedNetInMbps,
      this.cachedNetOutMbps,
      this.networkInterface,
      this.networkInterfaceOptions,
      this.cachedProcesses,
    );

    this.options.emitSnapshot(snapshot);
  }
}
