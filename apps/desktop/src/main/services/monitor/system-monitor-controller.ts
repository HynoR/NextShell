import type { MonitorSnapshot } from "../../../../../../packages/core/src/index";
import {
  buildDynamicSystemProbeCommand,
  MONITOR_NET_INTERFACES_COMMAND,
  normalizeNetworkInterfaceName
} from "./system-probe-command";
import {
  parseCompoundOutput,
  parseNetworkInterfaceList,
  parseSystemProbeSections,
  type ParsedSystemProbeFrame
} from "./system-probe-parser";
import { MonitorExecTimeoutError, runTimedExec, type MonitorExec } from "./monitor-runner";

const DEFAULT_POLL_INTERVAL_MS = 2000;
const DEFAULT_START_DELAY_MS = 300;
const DEFAULT_EXEC_TIMEOUT_MS = 10_000;
const DEFAULT_MAX_CONSECUTIVE_FAILURES = 3;

// One merged probe per tick: every section is collected on every frame.
const PROBE_OPTIONS = { collectCpuMemSwap: true, collectDisk: true, includeInterfaceMeta: true };

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
  exec: MonitorExec;
  stopMonitor: () => Promise<void>;
  isVisibleTerminalAlive: () => boolean;
  isReceiverAlive: () => boolean;
  emitSnapshot: (snapshot: MonitorSnapshot) => void;
  readSelection: () => MonitorSelectionState | undefined;
  writeSelection: (state: MonitorSelectionState) => void;
  logger: SystemMonitorLogger;
  onProbeExecution?: (entry: ProbeExecutionLog) => void;
  timing?: {
    pollIntervalMs?: number;
    startDelayMs?: number;
    execTimeoutMs?: number;
    maxConsecutiveFailures?: number;
  };
}

const round2 = (value: number): number => Number(value.toFixed(2));

const toSnapshot = (
  connectionId: string,
  frame: ParsedSystemProbeFrame,
  cpuPercent: number,
  networkInMbps: number,
  networkOutMbps: number,
  networkInterface: string,
  networkInterfaceOptions: string[]
): MonitorSnapshot => {
  // With PROBE_OPTIONS all on, the parser always fills these; defaults only satisfy the types.
  const memory = frame.memory ?? { memTotalKb: 0, memAvailableKb: 0, swapTotalKb: 0, swapFreeKb: 0 };
  const disk = frame.disk ?? { diskTotalKb: 0, diskUsedKb: 0 };
  const memoryUsedKb = Math.max(0, memory.memTotalKb - memory.memAvailableKb);
  const swapUsedKb = Math.max(0, memory.swapTotalKb - memory.swapFreeKb);
  const pct = (used: number, total: number): number => (total > 0 ? (used / total) * 100 : 0);

  return {
    connectionId,
    loadAverage: frame.loadAverage ?? [0, 0, 0],
    cpuPercent: round2(Math.max(0, cpuPercent)),
    memoryPercent: round2(pct(memoryUsedKb, memory.memTotalKb)),
    memoryUsedMb: round2(memoryUsedKb / 1024),
    memoryTotalMb: round2(memory.memTotalKb / 1024),
    swapPercent: round2(pct(swapUsedKb, memory.swapTotalKb)),
    swapUsedMb: round2(swapUsedKb / 1024),
    swapTotalMb: round2(memory.swapTotalKb / 1024),
    diskPercent: round2(pct(disk.diskUsedKb, disk.diskTotalKb)),
    diskUsedGb: round2(disk.diskUsedKb / (1024 * 1024)),
    diskTotalGb: round2(disk.diskTotalKb / (1024 * 1024)),
    networkInMbps: round2(Math.max(0, networkInMbps)),
    networkOutMbps: round2(Math.max(0, networkOutMbps)),
    networkInterface,
    networkInterfaceOptions,
    processes: frame.processes ?? [],
    capturedAt: new Date().toISOString()
  };
};

const wait = async (durationMs: number): Promise<void> => {
  if (durationMs > 0) {
    await new Promise<void>((resolve) => setTimeout(resolve, durationMs));
  }
};

export class SystemMonitorController {
  private readonly pollIntervalMs: number;
  private readonly startDelayMs: number;
  private readonly execTimeoutMs: number;
  private readonly maxConsecutiveFailures: number;

  private state: SystemMonitorControllerState = "IDLE";
  private generation = 0;
  private timer: ReturnType<typeof setInterval> | undefined;
  private inFlight = false;
  private consecutiveFailures = 0;
  private paused = false;

  private networkInterface = "eth0";
  private networkInterfaceOptions: string[] = [];

  private lastProbeDurationMs = 0;
  private skipCount = 0;

  // Previous samples for rate computation; the rates persist across frames that cannot be rated
  // (first frame, counter reset, zero elapsed).
  private prevNet: { rx: number; tx: number; at: number } | undefined;
  private netInMbps = 0;
  private netOutMbps = 0;
  private prevCpu: { total: number; idle: number } | undefined;
  private cpuPercent = 0;

  constructor(private readonly options: SystemMonitorControllerOptions) {
    this.pollIntervalMs = options.timing?.pollIntervalMs ?? DEFAULT_POLL_INTERVAL_MS;
    this.startDelayMs = options.timing?.startDelayMs ?? DEFAULT_START_DELAY_MS;
    this.execTimeoutMs = options.timing?.execTimeoutMs ?? DEFAULT_EXEC_TIMEOUT_MS;
    this.maxConsecutiveFailures =
      options.timing?.maxConsecutiveFailures ?? DEFAULT_MAX_CONSECUTIVE_FAILURES;
  }

  get currentState(): SystemMonitorControllerState {
    return this.state;
  }

  /** Stop the polling ticker without tearing down runtime state (OS suspend / window hidden). */
  pause(): void {
    if (this.paused) {
      return;
    }
    this.paused = true;
    this.clearTimer();
  }

  /** Restart the ticker paused by pause(); idempotent, only restarts while RUNNING. */
  resume(): void {
    if (!this.paused) {
      return;
    }
    this.paused = false;
    if (this.state === "RUNNING" && !this.timer) {
      // Counters sampled before a suspend are stale; start from fresh baselines.
      this.resetSamplingBaselines();
      this.startTicker(this.generation);
    }
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
    this.consecutiveFailures = 0;
    this.inFlight = false;
    this.lastProbeDurationMs = 0;
    this.skipCount = 0;
    this.resetSamplingBaselines();
    this.syncSelectionState();

    try {
      await wait(this.startDelayMs);
      if (!this.isGenerationActive(generation)) {
        return { ok: true };
      }

      this.state = "RUNNING";
      await this.runProbe(generation);

      if (this.isGenerationActive(generation)) {
        this.startTicker(generation);
        this.options.logger.info("[SystemMonitor] started (merged probe every 2s)", {
          connectionId: this.options.connectionId
        });
      }

      return { ok: true };
    } catch (error) {
      if (this.generation === generation) {
        this.clearTimer();
        await this.options.stopMonitor();
        this.state = "STOPPED";
      }
      throw error;
    }
  }

  async stop(): Promise<{ ok: true }> {
    if (this.state === "STOPPING") {
      // Re-entrant stop (self-stop → stopMonitor → dispose → stop): the outer call finishes it.
      // Awaiting here would deadlock, since the outer stop is waiting on our caller.
      return { ok: true };
    }

    if (this.state === "IDLE" || this.state === "STOPPED") {
      this.clearTimer();
      await this.options.stopMonitor();
      return { ok: true };
    }

    this.state = "STOPPING";
    this.bumpGeneration();
    this.clearTimer();

    try {
      await this.options.stopMonitor();
    } finally {
      this.inFlight = false;
      this.state = "STOPPED";
    }
    this.options.logger.info("[SystemMonitor] stopped", {
      connectionId: this.options.connectionId
    });

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

    const result = await runTimedExec(
      this.options.exec,
      MONITOR_NET_INTERFACES_COMMAND,
      this.execTimeoutMs
    );
    if (result.exitCode !== 0) {
      throw new Error(`网卡列表读取失败 (exit ${result.exitCode})`);
    }

    const options = parseNetworkInterfaceList(result.stdout);
    if (!options.includes(normalized)) {
      throw new Error(`网卡不存在或不可用: ${normalized}`);
    }

    this.networkInterface = normalized;
    this.networkInterfaceOptions = options;
    this.resetNetworkBaseline();

    this.options.writeSelection({
      selectedNetworkInterface: normalized,
      networkInterfaceOptions: options
    });

    return { ok: true };
  }

  private bumpGeneration(): number {
    this.generation += 1;
    return this.generation;
  }

  private isGenerationActive(generation: number): boolean {
    return (
      generation === this.generation && (this.state === "STARTING" || this.state === "RUNNING")
    );
  }

  private isRunning(generation: number): boolean {
    return this.isGenerationActive(generation) && this.state === "RUNNING";
  }

  private clearTimer(): void {
    if (this.timer) {
      clearInterval(this.timer);
      this.timer = undefined;
    }
  }

  private startTicker(generation: number): void {
    if (this.paused || !this.isRunning(generation)) {
      return;
    }

    this.clearTimer();
    this.timer = setInterval(() => {
      void this.poll(generation);
    }, this.pollIntervalMs);
  }

  private resetNetworkBaseline(): void {
    this.prevNet = undefined;
    this.netInMbps = 0;
    this.netOutMbps = 0;
  }

  private resetSamplingBaselines(): void {
    this.resetNetworkBaseline();
    this.prevCpu = undefined;
    this.cpuPercent = 0;
  }

  private async poll(generation: number): Promise<void> {
    if (!this.isRunning(generation)) {
      return;
    }

    if (!this.options.isVisibleTerminalAlive() || !this.options.isReceiverAlive()) {
      await this.stop();
      return;
    }

    if (this.lastProbeDurationMs > this.pollIntervalMs * 0.5) {
      this.skipCount += 1;
      if (this.skipCount % 2 !== 0) {
        this.options.logger.debug(
          "[SystemMonitor] throttling: slow probe detected, skipping frame",
          {
            connectionId: this.options.connectionId,
            lastProbeDurationMs: this.lastProbeDurationMs
          }
        );
        return;
      }
    } else {
      this.skipCount = 0;
    }

    if (this.inFlight) {
      this.options.logger.debug("[SystemMonitor] drop frame: previous probe still running", {
        connectionId: this.options.connectionId
      });
      return;
    }

    this.inFlight = true;
    try {
      await this.runProbe(generation);
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
    } else if (
      this.networkInterfaceOptions.length > 0 &&
      !this.networkInterfaceOptions.includes(this.networkInterface)
    ) {
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

    const known = (name: string | undefined): name is string =>
      !!name && this.networkInterfaceOptions.includes(name);

    if (known(selected)) {
      this.networkInterface = selected;
    } else if (known(counterInterface)) {
      this.networkInterface = counterInterface;
    } else if (known(frame.defaultNetworkInterface)) {
      this.networkInterface = frame.defaultNetworkInterface;
    } else if (counterInterface) {
      this.networkInterface = counterInterface;
    } else if (
      !this.networkInterfaceOptions.includes(this.networkInterface) &&
      this.networkInterfaceOptions.length > 0
    ) {
      this.networkInterface = this.networkInterfaceOptions[0] ?? this.networkInterface;
    }

    if (previousInterface !== this.networkInterface) {
      this.resetNetworkBaseline();
    }

    this.options.writeSelection({
      selectedNetworkInterface: known(selected) ? selected : this.networkInterface,
      networkInterfaceOptions: this.networkInterfaceOptions
    });
  }

  private updateRates(frame: ParsedSystemProbeFrame): void {
    const now = Date.now();
    const { rxBytes, txBytes } = frame.networkCounters;
    if (this.prevNet) {
      const elapsed = (now - this.prevNet.at) / 1000;
      const deltaRx = rxBytes - this.prevNet.rx;
      const deltaTx = txBytes - this.prevNet.tx;
      if (elapsed > 0 && deltaRx >= 0 && deltaTx >= 0) {
        this.netInMbps = (deltaRx * 8) / (elapsed * 1e6);
        this.netOutMbps = (deltaTx * 8) / (elapsed * 1e6);
      }
    }
    this.prevNet = { rx: rxBytes, tx: txBytes, at: now };

    if (frame.cpuTotal !== undefined && frame.cpuIdle !== undefined) {
      if (this.prevCpu) {
        const deltaTotal = frame.cpuTotal - this.prevCpu.total;
        const deltaIdle = frame.cpuIdle - this.prevCpu.idle;
        if (deltaTotal > 0) {
          this.cpuPercent = ((deltaTotal - deltaIdle) / deltaTotal) * 100;
        }
      }
      this.prevCpu = { total: frame.cpuTotal, idle: frame.cpuIdle };
    }
  }

  private async runProbe(generation: number): Promise<void> {
    if (!this.isGenerationActive(generation)) {
      return;
    }

    this.syncSelectionState();

    const command = buildDynamicSystemProbeCommand(this.networkInterface, PROBE_OPTIONS);

    let stdout = "";
    try {
      const result = await runTimedExec(this.options.exec, command, this.execTimeoutMs);
      if (!this.isRunning(generation)) {
        return;
      }

      stdout = result.stdout;
      this.options.onProbeExecution?.({
        command,
        stdout: result.stdout.slice(0, 4096),
        exitCode: result.exitCode,
        durationMs: result.durationMs,
        ok: result.exitCode === 0
      });

      if (result.exitCode !== 0) {
        this.consecutiveFailures += 1;
        this.options.logger.debug("[SystemMonitor] drop frame: command non-zero exit", {
          connectionId: this.options.connectionId,
          exitCode: result.exitCode
        });
        if (this.consecutiveFailures >= this.maxConsecutiveFailures) {
          await this.options.stopMonitor();
        }
        return;
      }

      this.lastProbeDurationMs = result.durationMs;
      this.consecutiveFailures = 0;
    } catch (error) {
      if (!this.isRunning(generation)) {
        return;
      }
      const errorMessage = error instanceof Error ? error.message : String(error);
      this.options.onProbeExecution?.({
        command,
        stdout: stdout.slice(0, 4096),
        exitCode: -1,
        durationMs: 0,
        ok: false,
        error: errorMessage
      });

      this.consecutiveFailures += 1;
      if (
        error instanceof MonitorExecTimeoutError ||
        this.consecutiveFailures >= this.maxConsecutiveFailures
      ) {
        await this.options.stopMonitor();
      }

      this.options.logger.warn("[SystemMonitor] drop frame: probe execution failed", {
        connectionId: this.options.connectionId,
        reason: errorMessage
      });
      return;
    }

    if (!this.isRunning(generation)) {
      return;
    }

    const parsed = parseSystemProbeSections(parseCompoundOutput(stdout), PROBE_OPTIONS);
    if (!parsed.ok) {
      this.options.logger.warn("[SystemMonitor] drop frame: invalid probe payload", {
        connectionId: this.options.connectionId,
        reason: parsed.reason,
        missingSections: parsed.missingSections
      });
      if (parsed.reason === "invalid NETCOUNTERS") {
        this.networkInterfaceOptions = [];
      }
      return;
    }

    this.resolveInterface(parsed.frame);
    this.updateRates(parsed.frame);

    if (!this.options.isReceiverAlive() || !this.isGenerationActive(generation)) {
      return;
    }

    this.options.emitSnapshot(
      toSnapshot(
        this.options.connectionId,
        parsed.frame,
        this.cpuPercent,
        this.netInMbps,
        this.netOutMbps,
        this.networkInterface,
        this.networkInterfaceOptions
      )
    );
  }
}
