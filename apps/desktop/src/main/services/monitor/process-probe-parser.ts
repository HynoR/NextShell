import type {
  MonitorProcess,
  ProcessDetailSnapshot,
  ProcessSnapshot,
} from "../../../../../../packages/core/src/index";

const parseFloatSafe = (value: string | undefined): number => {
  if (!value) {
    return 0;
  }

  const parsed = Number.parseFloat(value);
  return Number.isFinite(parsed) ? parsed : 0;
};

const parseIntSafe = (value: string | undefined): number => {
  if (!value) {
    return 0;
  }

  const parsed = Number.parseInt(value, 10);
  return Number.isFinite(parsed) ? parsed : 0;
};

export const firstNonEmptyLine = (value: string): string | undefined => {
  return value
    .split(/\r?\n/)
    .map((line) => line.trim())
    .find((line) => line.length > 0);
};

/**
 * Parse output of:
 *   LC_ALL=C ps -eo pid,ppid,user,stat,ni,pri,pcpu,pmem,rss,vsz,etimes,comm --no-headers --sort=-pcpu
 */
export const parseProcessSnapshot = (connectionId: string, stdout: string): ProcessSnapshot => {
  const lines = stdout.split(/\r?\n/);
  const processes: MonitorProcess[] = [];

  for (const rawLine of lines) {
    const line = rawLine.trim();
    if (!line || !/^\d/.test(line)) {
      continue;
    }

    const columns = line.split(/\s+/);
    if (columns.length < 12) {
      continue;
    }

    const pid = parseIntSafe(columns[0]);
    if (pid <= 0) {
      continue;
    }

    const ppid = parseIntSafe(columns[1]);
    const user = columns[2] ?? "unknown";
    const stat = columns[3] ?? "-";
    const nice = parseIntSafe(columns[4]);
    const priority = parseIntSafe(columns[5]);
    const cpuPercent = parseFloatSafe(columns[6]);
    const memoryPercent = parseFloatSafe(columns[7]);
    const rssKb = parseFloatSafe(columns[8]);
    const vszKb = parseFloatSafe(columns[9]);
    const elapsedSeconds = parseIntSafe(columns[10]);
    const command = columns.slice(11).join(" ") || "unknown";

    processes.push({
      pid,
      ppid,
      user,
      stat,
      nice,
      priority,
      cpuPercent: Number(cpuPercent.toFixed(1)),
      memoryPercent: Number(memoryPercent.toFixed(1)),
      memoryMb: Number((rssKb / 1024).toFixed(1)),
      vszMb: Number((vszKb / 1024).toFixed(1)),
      elapsedSeconds,
      command,
    });
  }

  return {
    connectionId,
    processes,
    capturedAt: new Date().toISOString(),
  };
};

export const parseProcessDetailPrimary = (
  connectionId: string,
  stdout: string
): Omit<ProcessDetailSnapshot, "commandLine" | "capturedAt"> | undefined => {
  const line = firstNonEmptyLine(stdout);
  if (!line) {
    return undefined;
  }

  const parts = line.replace(/\s+/g, " ").trim().split(" ");
  if (parts.length < 9) {
    return undefined;
  }

  const pid = parseIntSafe(parts[0]);
  const ppid = parseIntSafe(parts[1]);
  if (pid <= 0) {
    return undefined;
  }

  const user = parts[2] ?? "unknown";
  const state = parts[3] ?? "-";
  const cpuPercent = parseFloatSafe(parts[4]);
  const memoryPercent = parseFloatSafe(parts[5]);
  const rssKb = parseFloatSafe(parts[6]);
  const elapsed = parts[7] ?? "-";
  const command = parts.slice(8).join(" ") || "unknown";

  return {
    connectionId,
    pid,
    ppid,
    user,
    state,
    cpuPercent: Number(cpuPercent.toFixed(2)),
    memoryPercent: Number(memoryPercent.toFixed(2)),
    rssMb: Number((rssKb / 1024).toFixed(2)),
    elapsed,
    command,
  };
};
