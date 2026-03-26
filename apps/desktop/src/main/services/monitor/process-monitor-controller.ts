import type { ProcessSnapshot } from "../../../../../../packages/core/src/index";
import type { SshConnection } from "../../../../../../packages/ssh/src/index";
import { MonitorBackoff, MonitorExecTimeoutError, runTimedExec } from "./monitor-runner";
import { parseProcessSnapshot } from "./process-probe-parser";

export const PROCESS_MONITOR_PS_COMMAND =
  "LC_ALL=C ps -eo pid,ppid,user,stat,ni,pri,pcpu,pmem,rss,vsz,etimes,comm --no-headers --sort=-pcpu 2>/dev/null | head -n 200";
export const PROCESS_MONITOR_LINUX_CHECK_COMMAND = "uname -s 2>/dev/null";

const DEFAULT_POLL_INTERVAL_MS = 5000;
const DEFAULT_START_DELAY_MS = 200;
const DEFAULT_EXEC_TIMEOUT_MS = 10_000;
const DEFAULT_MAX_CONSECUTIVE_FAILURES = 3;

export type ProcessMonitorControllerState = "IDLE" | "STARTING" | "RUNNING" | "STOPPING" | "STOPPED";

export interface ProcessMonitorLogger {
  info: (message: string, metadata?: Record<string, unknown>) => void;
  warn: (message: string, metadata?: Record<string, unknown>) => void;
  debug: (message: string, metadata?: Record<string, unknown>) => void;
}

export interface ProcessProbeExecutionLog {
  command: string;
  stdout: string;
  exitCode: number;
  durationMs: number;
  ok: boolean;
  error?: string;
}

export interface ProcessMonitorControllerOptions {
  connectionId: string;
  getConnection: () => Promise<SshConnection>;
  closeConnection: () => Promise<void>;
  isVisibleTerminalAlive: () => boolean;
  isReceiverAlive: () => boolean;
  emitSnapshot: (snapshot: ProcessSnapshot) => void;
  logger: ProcessMonitorLogger;
  onProbeExecution?: (entry: ProcessProbeExecutionLog) => void;
  timing?: {
    pollIntervalMs?: number;
    startDelayMs?: number;
    execTimeoutMs?: number;
    maxConsecutiveFailures?: number;
  };
}

const wait = async (durationMs: number): Promise<void> => {
  if (durationMs <= 0) {
    return;
  }
  await new Promise<void>((resolve) => {
    setTimeout(resolve, durationMs);
  });
};

export class ProcessMonitorController {
  private readonly pollIntervalMs: number;
  private readonly startDelayMs: number;
  private readonly execTimeoutMs: number;
  private readonly maxConsecutiveFailures: number;

  private state: ProcessMonitorControllerState = "IDLE";
  private generation = 0;
  private timer: ReturnType<typeof setInterval> | undefined;
  private inFlight = false;
  private consecutiveFailures = 0;
  private readonly backoff = new MonitorBackoff();
  private lastProbeDurationMs = 0;
  private skipCount = 0;
  private suspended = false;

  constructor(private readonly options: ProcessMonitorControllerOptions) {
    this.pollIntervalMs = options.timing?.pollIntervalMs ?? DEFAULT_POLL_INTERVAL_MS;
    this.startDelayMs = options.timing?.startDelayMs ?? DEFAULT_START_DELAY_MS;
    this.execTimeoutMs = options.timing?.execTimeoutMs ?? DEFAULT_EXEC_TIMEOUT_MS;
    this.maxConsecutiveFailures =
      options.timing?.maxConsecutiveFailures ?? DEFAULT_MAX_CONSECUTIVE_FAILURES;
  }

  get currentState(): ProcessMonitorControllerState {
    return this.state;
  }

  clearSuspension(): void {
    this.suspended = false;
  }

  async start(): Promise<{ ok: true }> {
    if (this.suspended) {
      this.options.logger.info("[ProcessMonitor] start refused: suspended until reconnection", {
        connectionId: this.options.connectionId,
      });
      return { ok: true };
    }

    if (this.state === "RUNNING" || this.state === "STARTING") {
      return { ok: true };
    }

    if (!this.options.isVisibleTerminalAlive()) {
      throw new Error("请先连接 SSH 终端以启动 Process Monitor。");
    }

    this.state = "STARTING";
    const generation = this.bumpGeneration();
    this.consecutiveFailures = 0;
    this.inFlight = false;
    this.backoff.reset();
    this.lastProbeDurationMs = 0;
    this.skipCount = 0;

    try {
      await wait(this.startDelayMs);
      if (!this.isGenerationActive(generation)) {
        return { ok: true };
      }

      const connection = await this.options.getConnection();
      if (!this.isGenerationActive(generation)) {
        return { ok: true };
      }

      const linuxCheck = await runTimedExec(
        connection,
        PROCESS_MONITOR_LINUX_CHECK_COMMAND,
        this.execTimeoutMs
      );
      const platform = linuxCheck.stdout.trim().split(/\s+/)[0] ?? "";
      if (linuxCheck.exitCode !== 0 || platform !== "Linux") {
        throw new Error("当前模式仅支持 Linux");
      }

      this.state = "RUNNING";
      await this.runProbe(generation);

      if (this.isGenerationActive(generation)) {
        this.startTicker(generation);
        this.options.logger.info("[ProcessMonitor] started (ps polling)", {
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

    this.options.logger.info("[ProcessMonitor] stopped", { connectionId: this.options.connectionId });
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

  private async poll(generation: number): Promise<void> {
    if (!this.isGenerationActive(generation) || this.state !== "RUNNING") {
      return;
    }

    if (!this.options.isVisibleTerminalAlive() || !this.options.isReceiverAlive()) {
      await this.stop();
      return;
    }

    if (this.backoff.isActive()) {
      this.options.logger.debug("[ProcessMonitor] skipping poll: backoff active", {
        connectionId: this.options.connectionId,
        remainingMs: this.backoff.remainingMs(),
      });
      return;
    }

    if (this.lastProbeDurationMs > this.pollIntervalMs * 0.5) {
      this.skipCount += 1;
      if (this.skipCount % 2 !== 0) {
        this.options.logger.debug("[ProcessMonitor] throttling: slow probe detected, skipping frame", {
          connectionId: this.options.connectionId,
          lastProbeDurationMs: this.lastProbeDurationMs,
        });
        return;
      }
    } else {
      this.skipCount = 0;
    }

    if (this.inFlight) {
      this.options.logger.debug("[ProcessMonitor] drop frame: previous probe still running", {
        connectionId: this.options.connectionId,
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

  private async runProbe(generation: number): Promise<void> {
    if (!this.isGenerationActive(generation)) {
      return;
    }

    let stdout = "";
    try {
      const connection = await this.options.getConnection();
      const result = await runTimedExec(connection, PROCESS_MONITOR_PS_COMMAND, this.execTimeoutMs);
      if (!this.isGenerationActive(generation) || this.state !== "RUNNING") {
        return;
      }

      stdout = result.stdout;
      this.options.onProbeExecution?.({
        command: PROCESS_MONITOR_PS_COMMAND,
        stdout: result.stdout.slice(0, 4096),
        exitCode: result.exitCode,
        durationMs: result.durationMs,
        ok: result.exitCode === 0,
      });

      if (result.exitCode !== 0) {
        this.consecutiveFailures += 1;
        this.options.logger.debug("[ProcessMonitor] drop frame: command non-zero exit", {
          connectionId: this.options.connectionId,
          exitCode: result.exitCode,
        });
        if (this.consecutiveFailures >= this.maxConsecutiveFailures) {
          await this.options.closeConnection();
          this.backoff.apply();
          if (this.backoff.isExhausted()) {
            this.suspended = true;
            this.options.logger.warn("[ProcessMonitor] server unresponsive, suspending monitor until reconnection", {
              connectionId: this.options.connectionId,
            });
            await this.stop();
            return;
          }
          this.options.logger.warn("[ProcessMonitor] backing off after consecutive failures", {
            connectionId: this.options.connectionId,
            consecutiveFailures: this.consecutiveFailures,
          });
        }
        return;
      }

      this.lastProbeDurationMs = result.durationMs;
      this.consecutiveFailures = 0;
      this.backoff.reset();

      if (!this.options.isReceiverAlive() || !this.isGenerationActive(generation)) {
        return;
      }

      const snapshot = parseProcessSnapshot(this.options.connectionId, result.stdout);
      this.options.emitSnapshot(snapshot);
    } catch (error) {
      if (!this.isGenerationActive(generation) || this.state !== "RUNNING") {
        return;
      }

      const errorMessage = error instanceof Error ? error.message : String(error);
      this.options.onProbeExecution?.({
        command: PROCESS_MONITOR_PS_COMMAND,
        stdout: stdout.slice(0, 4096),
        exitCode: -1,
        durationMs: 0,
        ok: false,
        error: errorMessage,
      });

      this.consecutiveFailures += 1;
      if (error instanceof MonitorExecTimeoutError || this.consecutiveFailures >= this.maxConsecutiveFailures) {
        await this.options.closeConnection();
        this.backoff.apply();
        if (this.backoff.isExhausted()) {
          this.options.logger.warn("[ProcessMonitor] server unresponsive, stopping monitor", {
            connectionId: this.options.connectionId,
            reason: errorMessage,
          });
          await this.stop();
          return;
        }
        this.options.logger.warn("[ProcessMonitor] backing off after probe failure", {
          connectionId: this.options.connectionId,
          consecutiveFailures: this.consecutiveFailures,
          reason: errorMessage,
        });
      }

      this.options.logger.warn("[ProcessMonitor] drop frame: probe execution failed", {
        connectionId: this.options.connectionId,
        reason: errorMessage,
      });
    }
  }
}
