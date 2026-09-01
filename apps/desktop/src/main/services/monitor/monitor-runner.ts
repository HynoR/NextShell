export class MonitorExecTimeoutError extends Error {
  constructor(message = "monitor exec timeout") {
    super(message);
    this.name = "MonitorExecTimeoutError";
  }
}

export interface MonitorExecResult {
  stdout: string;
  stderr: string;
  exitCode: number;
  durationMs: number;
}

export type MonitorExec = (
  command: string,
  options?: { signal?: AbortSignal }
) => Promise<{ stdout: string; stderr: string; exitCode: number }>;

export const normalizeMonitorError = (error: unknown): string => {
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
};

// ─── Timed Exec ─────────────────────────────────────────────────────────────

export const runTimedExec = async (
  exec: MonitorExec,
  command: string,
  timeoutMs: number
): Promise<MonitorExecResult> => {
  const startedAt = Date.now();
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(new MonitorExecTimeoutError()), timeoutMs);

  try {
    const result = await exec(command, { signal: controller.signal });

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
