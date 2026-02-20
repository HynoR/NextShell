export interface DiskUsageEntry {
  filesystem: string;
  mountPoint: string;
  totalKb: number;
  usedKb: number;
  availableKb: number;
  usedPercent: number;
}

const parsePositiveInt = (value: string | undefined): number | undefined => {
  if (!value) {
    return undefined;
  }

  const parsed = Number.parseInt(value, 10);
  if (!Number.isFinite(parsed) || parsed < 0) {
    return undefined;
  }
  return parsed;
};

const parsePercent = (value: string | undefined): number | undefined => {
  if (!value) {
    return undefined;
  }

  const matched = value.match(/([0-9]+(?:\.[0-9]+)?)%/);
  if (!matched) {
    return undefined;
  }

  const parsed = Number.parseFloat(matched[1] ?? "");
  if (!Number.isFinite(parsed) || parsed < 0) {
    return undefined;
  }
  return parsed;
};

export const parseDfOutput = (output: string): DiskUsageEntry[] => {
  const lines = output
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line.length > 0);

  if (lines.length <= 1) {
    return [];
  }

  const rows: DiskUsageEntry[] = [];
  for (const line of lines.slice(1)) {
    const parts = line.split(/\s+/);
    if (parts.length < 6) {
      continue;
    }

    const filesystem = parts[0] ?? "";
    const totalKb = parsePositiveInt(parts[1]);
    const usedKb = parsePositiveInt(parts[2]);
    const availableKb = parsePositiveInt(parts[3]);
    const mountPoint = parts.slice(5).join(" ");
    if (!filesystem || !mountPoint) {
      continue;
    }
    if (totalKb === undefined || usedKb === undefined || availableKb === undefined || totalKb <= 0) {
      continue;
    }

    const parsedPercent = parsePercent(parts[4]);
    const usedPercent = parsedPercent ?? Math.min(100, Math.max(0, (usedKb / totalKb) * 100));

    rows.push({
      filesystem,
      mountPoint,
      totalKb,
      usedKb,
      availableKb,
      usedPercent
    });
  }

  rows.sort((a, b) => a.mountPoint.localeCompare(b.mountPoint));
  return rows;
};

export const formatDiskSize = (kilobytes: number): string => {
  const units = ["K", "M", "G", "T", "P"];
  let value = Math.max(0, kilobytes);
  let unitIndex = 0;

  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024;
    unitIndex += 1;
  }

  const precision = value >= 10 || unitIndex === 0 ? 0 : 1;
  return `${value.toFixed(precision)}${units[unitIndex]}`;
};

