import type {
  SystemCpuInfo,
  SystemFilesystemEntry,
  SystemNetworkInterfaceTotal
} from "../../../../../packages/core/src/index";

const parseIntSafe = (value: string | undefined): number => {
  if (!value) {
    return 0;
  }

  const parsed = Number.parseInt(value, 10);
  if (!Number.isFinite(parsed)) {
    return 0;
  }

  return parsed;
};

const parseFloatSafe = (value: string | undefined): number | undefined => {
  if (!value) {
    return undefined;
  }

  const parsed = Number.parseFloat(value);
  if (!Number.isFinite(parsed)) {
    return undefined;
  }

  return parsed;
};

const trimQuotedValue = (raw: string): string => {
  const trimmed = raw.trim();
  if (trimmed.length < 2) {
    return trimmed;
  }

  const startsWithQuote =
    (trimmed.startsWith("\"") && trimmed.endsWith("\"")) ||
    (trimmed.startsWith("'") && trimmed.endsWith("'"));

  if (!startsWithQuote) {
    return trimmed;
  }

  return trimmed.slice(1, -1).replace(/\\(["'])/g, "$1");
};

export const parseOsReleaseName = (raw: string): string => {
  const values = new Map<string, string>();
  for (const line of raw.split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("#")) {
      continue;
    }

    const separatorIndex = trimmed.indexOf("=");
    if (separatorIndex <= 0) {
      continue;
    }

    const key = trimmed.slice(0, separatorIndex).trim();
    const value = trimQuotedValue(trimmed.slice(separatorIndex + 1));
    if (key) {
      values.set(key, value);
    }
  }

  const prettyName = values.get("PRETTY_NAME");
  if (prettyName) {
    return prettyName;
  }

  const name = values.get("NAME");
  const version = values.get("VERSION");
  const joined = [name, version].filter(Boolean).join(" ").trim();
  if (joined) {
    return joined;
  }

  return "Unknown OS";
};

export const parseCpuInfo = (raw: string): SystemCpuInfo => {
  let modelName = "";
  let coreCount = 0;
  let fallbackCpuCores = 0;
  let frequencyMhz: number | undefined;
  let cacheSize: string | undefined;
  let bogoMips: number | undefined;

  for (const line of raw.split(/\r?\n/)) {
    const separatorIndex = line.indexOf(":");
    if (separatorIndex <= 0) {
      continue;
    }

    const key = line.slice(0, separatorIndex).trim().toLowerCase();
    const value = line.slice(separatorIndex + 1).trim();
    if (!key) {
      continue;
    }

    if (key === "processor") {
      coreCount += 1;
      continue;
    }

    if (!modelName && (key === "model name" || key === "hardware")) {
      modelName = value;
      continue;
    }

    if (frequencyMhz === undefined && key === "cpu mhz") {
      frequencyMhz = parseFloatSafe(value);
      continue;
    }

    if (!cacheSize && key === "cache size") {
      cacheSize = value;
      continue;
    }

    if (bogoMips === undefined && key === "bogomips") {
      bogoMips = parseFloatSafe(value);
      continue;
    }

    if (fallbackCpuCores <= 0 && key === "cpu cores") {
      fallbackCpuCores = parseIntSafe(value);
      continue;
    }
  }

  const normalizedCoreCount = coreCount > 0 ? coreCount : fallbackCpuCores > 0 ? fallbackCpuCores : 1;
  return {
    modelName: modelName || "Unknown CPU",
    coreCount: normalizedCoreCount,
    frequencyMhz: frequencyMhz !== undefined ? Number(frequencyMhz.toFixed(3)) : undefined,
    cacheSize,
    bogoMips: bogoMips !== undefined ? Number(bogoMips.toFixed(2)) : undefined
  };
};

export const parseMeminfoTotals = (raw: string): { memoryTotalKb: number; swapTotalKb: number } => {
  let memoryTotalKb = 0;
  let swapTotalKb = 0;

  for (const line of raw.split(/\r?\n/)) {
    const match = line.match(/^([A-Za-z_()]+):\s+(\d+)/);
    if (!match?.[1] || !match[2]) {
      continue;
    }

    const key = match[1];
    const value = parseIntSafe(match[2]);

    if (key === "MemTotal") {
      memoryTotalKb = value;
      continue;
    }

    if (key === "SwapTotal") {
      swapTotalKb = value;
    }
  }

  return { memoryTotalKb, swapTotalKb };
};

export const parseNetworkInterfaceTotals = (raw: string): SystemNetworkInterfaceTotal[] => {
  const rows: SystemNetworkInterfaceTotal[] = [];

  for (const line of raw.split(/\r?\n/)) {
    const separatorIndex = line.indexOf(":");
    if (separatorIndex <= 0) {
      continue;
    }

    const name = line.slice(0, separatorIndex).trim();
    const payload = line.slice(separatorIndex + 1).trim();
    if (!name || !payload) {
      continue;
    }

    const fields = payload.split(/\s+/);
    const rxBytes = parseIntSafe(fields[0]);
    const txBytes = parseIntSafe(fields[8]);
    rows.push({ name, rxBytes, txBytes });
  }

  rows.sort((a, b) => a.name.localeCompare(b.name));
  return rows;
};

export const parseFilesystemEntries = (raw: string): SystemFilesystemEntry[] => {
  const lines = raw
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);

  if (lines.length <= 1) {
    return [];
  }

  const rows: SystemFilesystemEntry[] = [];
  for (const line of lines.slice(1)) {
    const parts = line.split(/\s+/);
    if (parts.length < 6) {
      continue;
    }

    const filesystem = parts[0] ?? "";
    const totalKb = parseIntSafe(parts[1]);
    const usedKb = parseIntSafe(parts[2]);
    const availableKb = parseIntSafe(parts[3]);
    const mountPoint = parts.slice(5).join(" ");

    if (!filesystem || !mountPoint || totalKb <= 0) {
      continue;
    }

    rows.push({
      filesystem,
      totalKb,
      usedKb,
      availableKb,
      mountPoint
    });
  }

  rows.sort((a, b) => a.mountPoint.localeCompare(b.mountPoint));
  return rows;
};
