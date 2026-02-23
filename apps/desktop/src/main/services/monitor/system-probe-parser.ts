import type { MonitorProcess } from "../../../../../../packages/core/src/index";
import { normalizeNetworkInterfaceName } from "./system-probe-command";

const ANSI_ESCAPE_PATTERN = /\x1B(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~])/g;

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

export interface ParsedMemoryTotals {
  memTotalKb: number;
  memAvailableKb: number;
  swapTotalKb: number;
  swapFreeKb: number;
}

export interface ParsedDiskTotals {
  diskTotalKb: number;
  diskUsedKb: number;
}

export interface ParsedNetworkCounters {
  rxBytes: number;
  txBytes: number;
}

export interface ParsedSystemProbeFrame {
  loadAverage?: [number, number, number];
  cpuTotal?: number;
  cpuIdle?: number;
  memory?: ParsedMemoryTotals;
  disk?: ParsedDiskTotals;
  networkCounters: ParsedNetworkCounters;
  processes?: MonitorProcess[];
  networkInterfaceOptions?: string[];
  defaultNetworkInterface?: string;
  networkCounterInterface?: string;
}

export interface ParseSystemProbeOptions {
  collectCpuMemSwap: boolean;
  collectDisk: boolean;
  includeInterfaceMeta: boolean;
}

export type ParseSystemProbeResult =
  | {
      ok: true;
      frame: ParsedSystemProbeFrame;
    }
  | {
      ok: false;
      reason: string;
      missingSections?: string[];
    };

export const sanitizeProbeText = (raw: string): string => {
  return raw
    .replace(/\r\n/g, "\n")
    .replace(/\r(?!\n)/g, "\n")
    .replace(ANSI_ESCAPE_PATTERN, "")
    .replace(/\u0000/g, "");
};

export const parseCompoundOutput = (stdout: string): Map<string, string> => {
  const sanitized = sanitizeProbeText(stdout);
  const sections = new Map<string, string>();
  const lines = sanitized.split("\n");
  let currentSection = "";
  let currentContent: string[] = [];

  for (const line of lines) {
    const match = line.match(/^---NS_(\w+)---$/);
    if (match?.[1]) {
      if (currentSection) {
        sections.set(currentSection, currentContent.join("\n"));
      }
      currentSection = match[1];
      currentContent = [];
      continue;
    }

    currentContent.push(line);
  }

  if (currentSection) {
    sections.set(currentSection, currentContent.join("\n"));
  }

  return sections;
};

export const parseLoadAverage = (raw: string): [number, number, number] => {
  if (!raw.trim()) {
    return [0, 0, 0];
  }

  const lower = raw.toLowerCase();
  const loadSegment = lower.includes("load average")
    ? raw.slice(lower.indexOf("load average"))
    : raw;

  const numbers = Array.from(loadSegment.matchAll(/-?\d+(?:\.\d+)?/g))
    .map((match) => parseFloatSafe(match[0]))
    .filter((value) => Number.isFinite(value));

  if (numbers.length < 3) {
    return [0, 0, 0];
  }

  return [numbers[0] ?? 0, numbers[1] ?? 0, numbers[2] ?? 0];
};

export const parseCpuTotals = (raw: string): { cpuTotal?: number; cpuIdle?: number } => {
  const cpuLine = raw
    .split(/\r?\n/)
    .map((line) => line.trim())
    .find((line) => line.startsWith("cpu "));

  if (!cpuLine) {
    return {};
  }

  const fields = cpuLine
    .replace(/\s+/g, " ")
    .trim()
    .split(" ")
    .slice(1)
    .map((value) => parseFloatSafe(value));

  if (fields.length < 4) {
    return {};
  }

  const cpuTotal = fields.reduce((sum, value) => sum + value, 0);
  const cpuIdle = (fields[3] ?? 0) + (fields[4] ?? 0);

  if (cpuTotal <= 0) {
    return {};
  }

  return { cpuTotal, cpuIdle };
};

export const parseMemoryFromMeminfo = (raw: string): ParsedMemoryTotals | undefined => {
  const values = new Map<string, number>();
  for (const line of raw.split(/\r?\n/)) {
    const match = line.match(/^([A-Za-z_()]+):\s+(\d+)/);
    if (!match?.[1] || !match[2]) {
      continue;
    }
    values.set(match[1], parseIntSafe(match[2]));
  }

  const memTotalKb = values.get("MemTotal") ?? 0;
  if (memTotalKb <= 0) {
    return undefined;
  }

  const memAvailableKb = values.get("MemAvailable") ??
    ((values.get("MemFree") ?? 0) + (values.get("Buffers") ?? 0) + (values.get("Cached") ?? 0));

  return {
    memTotalKb,
    memAvailableKb,
    swapTotalKb: values.get("SwapTotal") ?? 0,
    swapFreeKb: values.get("SwapFree") ?? 0
  };
};

export const parseMemoryFromFree = (raw: string): ParsedMemoryTotals | undefined => {
  const lines = raw.split(/\r?\n/).map((line) => line.trim()).filter(Boolean);
  const memLine = lines.find((line) => /^mem:/i.test(line));
  if (!memLine) {
    return undefined;
  }

  const memParts = memLine.split(/\s+/);
  const memTotalKb = parseIntSafe(memParts[1]);
  if (memTotalKb <= 0) {
    return undefined;
  }

  const memAvailableKb = parseIntSafe(memParts[6]) || parseIntSafe(memParts[3]);
  const swapLine = lines.find((line) => /^swap:/i.test(line));
  const swapParts = swapLine?.split(/\s+/) ?? [];
  const swapTotalKb = parseIntSafe(swapParts[1]);
  const swapFreeKb = parseIntSafe(swapParts[3]) || Math.max(0, swapTotalKb - parseIntSafe(swapParts[2]));

  return {
    memTotalKb,
    memAvailableKb,
    swapTotalKb,
    swapFreeKb
  };
};

export const parseDiskUsage = (raw: string): ParsedDiskTotals | undefined => {
  const line = raw
    .split(/\r?\n/)
    .map((value) => value.trim())
    .filter(Boolean)
    .at(-1) ?? "";
  const parts = line.replace(/\s+/g, " ").trim().split(" ");
  const diskTotalKb = parseIntSafe(parts[1]);
  const diskUsedKb = parseIntSafe(parts[2]);

  if (diskTotalKb <= 0 || diskUsedKb < 0) {
    return undefined;
  }

  return { diskTotalKb, diskUsedKb };
};

export const parseNetworkInterfaceList = (raw: string): string[] => {
  const interfaces = raw
    .split(/\r?\n/)
    .map((line) => normalizeNetworkInterfaceName(line))
    .filter((line): line is string => Boolean(line) && line !== "lo");

  if (interfaces.length === 0) {
    return [];
  }

  return Array.from(new Set(interfaces)).sort((a, b) => a.localeCompare(b));
};

export const parseNetworkCounters = (raw: string): ParsedNetworkCounters | undefined => {
  const values = raw
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => Number.parseFloat(line))
    .filter((value) => Number.isFinite(value));

  const rxBytes = values[0];
  const txBytes = values[1];
  if (rxBytes === undefined || txBytes === undefined || rxBytes < 0 || txBytes < 0) {
    return undefined;
  }

  return { rxBytes, txBytes };
};

export const parseMonitorProcesses = (raw: string): MonitorProcess[] => {
  return raw
    .split(/\r?\n/)
    .map((line) => line.replace(/\s+/g, " ").trim())
    .filter(Boolean)
    .map((line) => {
      const parts = line.split(" ");
      const pid = parseIntSafe(parts[0]);
      const command = parts[1] ?? "unknown";
      const cpuPercent = parseFloatSafe(parts[2]);
      const rssKb = parseFloatSafe(parts[3]);

      return {
        pid,
        ppid: 0,
        command,
        cpuPercent,
        memoryPercent: 0,
        memoryMb: Number((rssKb / 1024).toFixed(2)),
        user: "-",
        stat: "-",
        nice: 0,
        priority: 0,
        vszMb: 0,
        elapsedSeconds: 0
      };
    })
    .filter((item) => item.pid > 0)
    .slice(0, 5);
};

const firstNonEmptyLine = (raw: string): string | undefined => {
  return raw
    .split(/\r?\n/)
    .map((line) => line.trim())
    .find((line) => line.length > 0);
};

const parseDefaultNetworkInterface = (raw: string): string | undefined => {
  const line = firstNonEmptyLine(raw);
  if (!line) {
    return undefined;
  }
  return normalizeNetworkInterfaceName(line);
};

const parseCounterNetworkInterface = (raw: string): string | undefined => {
  const line = firstNonEmptyLine(raw);
  if (!line) {
    return undefined;
  }
  return normalizeNetworkInterfaceName(line);
};

export const parseSystemProbeSections = (
  sections: Map<string, string>,
  options: ParseSystemProbeOptions
): ParseSystemProbeResult => {
  const missingSections: string[] = [];

  const netCountersRaw = sections.get("NETCOUNTERS");
  if (!netCountersRaw) {
    missingSections.push("NETCOUNTERS");
  }

  if (options.collectCpuMemSwap && !sections.get("CPUSTAT")) {
    missingSections.push("CPUSTAT");
  }

  if (options.collectDisk && !sections.get("DISK")) {
    missingSections.push("DISK");
  }

  if (missingSections.length > 0) {
    return {
      ok: false,
      reason: "missing required sections",
      missingSections
    };
  }

  const networkCounters = parseNetworkCounters(netCountersRaw ?? "");
  if (!networkCounters) {
    return {
      ok: false,
      reason: "invalid NETCOUNTERS"
    };
  }

  const frame: ParsedSystemProbeFrame = {
    networkCounters
  };

  if (options.includeInterfaceMeta) {
    const netIfacesRaw = sections.get("NETIFACES") ?? "";
    const defaultIfaceRaw = sections.get("NETDEFAULT") ?? "";
    const counterIfaceRaw = sections.get("NETCOUNTER_IFACE") ?? "";
    frame.networkInterfaceOptions = parseNetworkInterfaceList(netIfacesRaw);
    frame.defaultNetworkInterface = parseDefaultNetworkInterface(defaultIfaceRaw);
    frame.networkCounterInterface = parseCounterNetworkInterface(counterIfaceRaw);
  }

  if (options.collectCpuMemSwap) {
    const cpuTotals = parseCpuTotals(sections.get("CPUSTAT") ?? "");
    if (cpuTotals.cpuTotal === undefined || cpuTotals.cpuIdle === undefined) {
      return {
        ok: false,
        reason: "invalid CPUSTAT"
      };
    }

    const memory = parseMemoryFromMeminfo(sections.get("MEMINFO") ?? "") ??
      parseMemoryFromFree(sections.get("FREE") ?? "");
    if (!memory) {
      return {
        ok: false,
        reason: "invalid memory sections"
      };
    }

    frame.cpuTotal = cpuTotals.cpuTotal;
    frame.cpuIdle = cpuTotals.cpuIdle;
    frame.memory = memory;
    frame.loadAverage = parseLoadAverage(sections.get("LOADAVG") ?? "");
    frame.processes = parseMonitorProcesses(sections.get("PROCESSES") ?? "");
  }

  if (options.collectDisk) {
    const disk = parseDiskUsage(sections.get("DISK") ?? "");
    if (!disk) {
      return {
        ok: false,
        reason: "invalid DISK"
      };
    }
    frame.disk = disk;
  }

  return {
    ok: true,
    frame
  };
};
