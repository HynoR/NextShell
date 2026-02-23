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

export const runTimedExec = async (
  connection: SshConnection,
  command: string,
  timeoutMs: number
): Promise<TimedExecResult> => {
  const startedAt = Date.now();

  let timeout: ReturnType<typeof setTimeout> | undefined;
  try {
    const result = await Promise.race([
      connection.exec(command),
      new Promise<never>((_resolve, reject) => {
        timeout = setTimeout(() => {
          reject(new MonitorExecTimeoutError());
        }, timeoutMs);
      })
    ]);

    return {
      stdout: result.stdout,
      stderr: result.stderr,
      exitCode: result.exitCode,
      durationMs: Date.now() - startedAt
    };
  } finally {
    if (timeout) {
      clearTimeout(timeout);
    }
  }
};
