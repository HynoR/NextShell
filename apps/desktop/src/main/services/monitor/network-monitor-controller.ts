import type { NetworkConnection, NetworkSnapshot } from "../../../../../../packages/core/src/index";
import type { SshConnection } from "../../../../../../packages/ssh/src/index";
import { MonitorExecTimeoutError, runTimedExec } from "./monitor-runner";
import { parseNetstatOutput, parseSsOutput } from "./network-probe-parser";

export type NetworkTool = "ss" | "netstat";

export const NETWORK_PROBE_SS =
  "export LANG=en_US.UTF-8 LANGUAGE=en_US LC_ALL=en_US.UTF-8; ss -ltnup 2>/dev/null";
export const NETWORK_PROBE_NETSTAT =
  "export LANG=en_US.UTF-8 LANGUAGE=en_US LC_ALL=en_US.UTF-8; netstat -ltnup 2>/dev/null";

const NETWORK_TOOL_DETECT_SS = "command -v ss >/dev/null 2>&1";
const NETWORK_TOOL_DETECT_NETSTAT = "command -v netstat >/dev/null 2>&1";

const DEFAULT_POLL_INTERVAL_MS = 5000;
const DEFAULT_START_DELAY_MS = 200;
const DEFAULT_EXEC_TIMEOUT_MS = 20_000;
const DEFAULT_MAX_CONSECUTIVE_FAILURES = 3;

export type NetworkMonitorControllerState = "IDLE" | "STARTING" | "RUNNING" | "STOPPING" | "STOPPED";

export interface NetworkMonitorLogger {
  info: (message: string, metadata?: Record<string, unknown>) => void;
  warn: (message: string, metadata?: Record<string, unknown>) => void;
  debug: (message: string, metadata?: Record<string, unknown>) => void;
}

export interface NetworkProbeExecutionLog {
  command: string;
  stdout: string;
  exitCode: number;
  durationMs: number;
  ok: boolean;
  error?: string;
}

export interface NetworkMonitorControllerOptions {
  connectionId: string;
  getConnection: () => Promise<SshConnection>;
  closeConnection: () => Promise<void>;
  isVisibleTerminalAlive: () => boolean;
  isReceiverAlive: () => boolean;
  emitSnapshot: (snapshot: NetworkSnapshot) => void;
  readToolCache: () => NetworkTool | undefined;
  writeToolCache: (tool: NetworkTool | undefined) => void;
  logger: NetworkMonitorLogger;
  onProbeExecution?: (entry: NetworkProbeExecutionLog) => void;
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

const buildConnectionsQueryCommand = (tool: NetworkTool, port: number): string => {
  if (tool === "ss") {
    return `export LANG=en_US.UTF-8; ss -tnap '( sport = :${port} )' 2>/dev/null`;
  }

  return (
    "export LANG=en_US.UTF-8; netstat -tnp 2>/dev/null | " +
    `awk 'NR>2 {split($4,a,":"); p=a[length(a)]; if (p=="${port}") print $0}'`
  );
};

export class NetworkMonitorController {
  private readonly pollIntervalMs: number;
  private readonly startDelayMs: number;
  private readonly execTimeoutMs: number;
  private readonly maxConsecutiveFailures: number;

  private state: NetworkMonitorControllerState = "IDLE";
  private generation = 0;
  private timer: ReturnType<typeof setInterval> | undefined;
  private inFlight = false;
  private consecutiveFailures = 0;
  private tool: NetworkTool | undefined;

  constructor(private readonly options: NetworkMonitorControllerOptions) {
    this.pollIntervalMs = options.timing?.pollIntervalMs ?? DEFAULT_POLL_INTERVAL_MS;
    this.startDelayMs = options.timing?.startDelayMs ?? DEFAULT_START_DELAY_MS;
    this.execTimeoutMs = options.timing?.execTimeoutMs ?? DEFAULT_EXEC_TIMEOUT_MS;
    this.maxConsecutiveFailures =
      options.timing?.maxConsecutiveFailures ?? DEFAULT_MAX_CONSECUTIVE_FAILURES;
  }

  get currentState(): NetworkMonitorControllerState {
    return this.state;
  }

  async start(): Promise<{ ok: true }> {
    if (this.state === "RUNNING" || this.state === "STARTING") {
      return { ok: true };
    }

    if (!this.options.isVisibleTerminalAlive()) {
      throw new Error("请先连接 SSH 终端以启动 Network Monitor。");
    }

    this.state = "STARTING";
    const generation = this.bumpGeneration();
    this.consecutiveFailures = 0;
    this.inFlight = false;

    try {
      await wait(this.startDelayMs);
      if (!this.isGenerationActive(generation)) {
        return { ok: true };
      }

      await this.options.getConnection();
      if (!this.isGenerationActive(generation)) {
        return { ok: true };
      }

      await this.ensureTool(generation);

      this.state = "RUNNING";
      await this.runProbe(generation);

      if (this.isGenerationActive(generation)) {
        this.startTicker(generation);
        this.options.logger.info("[NetworkMonitor] started", {
          connectionId: this.options.connectionId,
          tool: this.tool,
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

    this.options.logger.info("[NetworkMonitor] stopped", { connectionId: this.options.connectionId });
    return { ok: true };
  }

  async getConnectionsByPort(port: number): Promise<NetworkConnection[]> {
    const normalizedPort = Math.trunc(port);
    if (normalizedPort < 1 || normalizedPort > 65535) {
      throw new Error("无效端口号");
    }

    if (!this.options.isVisibleTerminalAlive()) {
      throw new Error("请先连接 SSH 终端以查询网络连接。");
    }

    const tool = await this.ensureTool();
    const command = buildConnectionsQueryCommand(tool, normalizedPort);
    const connection = await this.options.getConnection();
    const result = await runTimedExec(connection, command, this.execTimeoutMs);

    if (result.exitCode !== 0) {
      throw new Error(`网络连接查询失败 (exit ${result.exitCode})`);
    }

    const parsed = tool === "ss" ? parseSsOutput(result.stdout) : parseNetstatOutput(result.stdout);
    return parsed.connections;
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

    if (this.inFlight) {
      this.options.logger.debug("[NetworkMonitor] drop frame: previous probe still running", {
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

  private async ensureTool(generation?: number): Promise<NetworkTool> {
    if (generation !== undefined && !this.isGenerationActive(generation)) {
      throw new Error("Network monitor state changed");
    }

    if (this.tool) {
      return this.tool;
    }

    const cached = this.options.readToolCache();
    if (cached) {
      this.tool = cached;
      return cached;
    }

    const connection = await this.options.getConnection();
    if (generation !== undefined && !this.isGenerationActive(generation)) {
      throw new Error("Network monitor state changed");
    }

    const ssProbe = await runTimedExec(connection, NETWORK_TOOL_DETECT_SS, this.execTimeoutMs);
    if (ssProbe.exitCode === 0) {
      this.tool = "ss";
      this.options.writeToolCache("ss");
      return "ss";
    }

    const netstatProbe = await runTimedExec(connection, NETWORK_TOOL_DETECT_NETSTAT, this.execTimeoutMs);
    if (netstatProbe.exitCode === 0) {
      this.tool = "netstat";
      this.options.writeToolCache("netstat");
      return "netstat";
    }

    throw new Error("未找到 ss 或 netstat 命令，无法启动网络监控。");
  }

  private async runProbe(generation: number): Promise<void> {
    if (!this.isGenerationActive(generation)) {
      return;
    }

    let command = "";
    let stdout = "";

    try {
      const tool = await this.ensureTool(generation);
      if (!this.isGenerationActive(generation) || this.state !== "RUNNING") {
        return;
      }

      command = tool === "ss" ? NETWORK_PROBE_SS : NETWORK_PROBE_NETSTAT;
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
        this.options.logger.debug("[NetworkMonitor] drop frame: command non-zero exit", {
          connectionId: this.options.connectionId,
          exitCode: result.exitCode,
        });
        if (this.consecutiveFailures >= this.maxConsecutiveFailures) {
          await this.options.closeConnection();
          this.consecutiveFailures = 0;
        }
        return;
      }

      this.consecutiveFailures = 0;

      if (!this.options.isReceiverAlive() || !this.isGenerationActive(generation)) {
        return;
      }

      const parsed = tool === "ss" ? parseSsOutput(result.stdout) : parseNetstatOutput(result.stdout);
      const snapshot: NetworkSnapshot = {
        connectionId: this.options.connectionId,
        listeners: parsed.listeners,
        connections: [],
        capturedAt: new Date().toISOString(),
      };

      this.options.emitSnapshot(snapshot);
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

      this.options.logger.warn("[NetworkMonitor] drop frame: probe execution failed", {
        connectionId: this.options.connectionId,
        reason: errorMessage,
      });
    }
  }
}
