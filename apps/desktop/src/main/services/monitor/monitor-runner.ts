import type { SshConnection } from "../../../../../../packages/ssh/src/index";

export class MonitorExecTimeoutError extends Error {
  constructor(message = "monitor exec timeout") {
    super(message);
    this.name = "MonitorExecTimeoutError";
  }
}

export interface TimedExecResult {
  stdout: string;
  stderr: string;
  exitCode: number;
  durationMs: number;
}

export const normalizeMonitorError = (error: unknown): string => {
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
};

// ─── Exponential Backoff ────────────────────────────────────────────────────

const DEFAULT_BACKOFF_BASE_MS = 5_000;
const DEFAULT_BACKOFF_MAX_MS = 120_000;
const DEFAULT_BACKOFF_MULTIPLIER_CAP = 5;

export class MonitorBackoff {
  private backoffUntil = 0;
  private multiplier = 0;
  private _exhausted = false;

  constructor(
    private readonly baseMs = DEFAULT_BACKOFF_BASE_MS,
    private readonly maxMs = DEFAULT_BACKOFF_MAX_MS,
    private readonly maxMultiplier = DEFAULT_BACKOFF_MULTIPLIER_CAP,
  ) {}

  isActive(): boolean {
    return this._exhausted || Date.now() < this.backoffUntil;
  }

  /** Backoff has reached the cap — the monitor should stop entirely. */
  isExhausted(): boolean {
    return this._exhausted;
  }

  remainingMs(): number {
    if (this._exhausted) return Infinity;
    return Math.max(0, this.backoffUntil - Date.now());
  }

  apply(): number {
    this.multiplier = Math.min(this.multiplier + 1, this.maxMultiplier);
    if (this.multiplier >= this.maxMultiplier) {
      this._exhausted = true;
      return Infinity;
    }
    const delayMs = Math.min(this.baseMs * Math.pow(2, this.multiplier - 1), this.maxMs);
    this.backoffUntil = Date.now() + delayMs;
    return delayMs;
  }

  reset(): void {
    this.multiplier = 0;
    this.backoffUntil = 0;
    this._exhausted = false;
  }
}

// ─── Timed Exec ─────────────────────────────────────────────────────────────

export const runTimedExec = async (
  connection: SshConnection,
  command: string,
  timeoutMs: number
): Promise<TimedExecResult> => {
  const startedAt = Date.now();
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(new MonitorExecTimeoutError()), timeoutMs);

  try {
    const result = await connection.exec(command, { signal: controller.signal });

    return {
      stdout: result.stdout,
      stderr: result.stderr,
      exitCode: result.exitCode,
      durationMs: Date.now() - startedAt
    };
  } catch (error) {
    if (error instanceof MonitorExecTimeoutError) {
      throw error;
    }
    const reason = controller.signal.reason;
    if (reason instanceof MonitorExecTimeoutError) {
      throw reason;
    }
    throw error;
  } finally {
    clearTimeout(timeout);
  }
};
