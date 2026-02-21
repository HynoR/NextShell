import type { SystemFilesystemEntry } from "@nextshell/core";
import type { DiskUsageEntry } from "./diskUsage";

export interface SharedDiskSnapshot {
  rows: DiskUsageEntry[];
  lastUpdatedAt?: string;
}

const sharedDiskCache = new Map<string, SharedDiskSnapshot>();

export const buildSystemDiskCacheKey = (
  connectionId?: string,
  connectedTerminalSessionId?: string
): string | undefined => {
  if (!connectionId || !connectedTerminalSessionId) {
    return undefined;
  }
  return `${connectionId}:${connectedTerminalSessionId}`;
};

export const getSharedDiskSnapshot = (key: string): SharedDiskSnapshot | undefined => {
  return sharedDiskCache.get(key);
};

export const setSharedDiskSnapshot = (key: string, snapshot: SharedDiskSnapshot): void => {
  sharedDiskCache.set(key, {
    rows: [...snapshot.rows].sort((a, b) => a.mountPoint.localeCompare(b.mountPoint)),
    lastUpdatedAt: snapshot.lastUpdatedAt
  });
};

export const systemFilesystemsToDiskRows = (filesystems: SystemFilesystemEntry[]): DiskUsageEntry[] => {
  return filesystems
    .map((item) => {
      const usedPercent = item.totalKb > 0 ? (item.usedKb / item.totalKb) * 100 : 0;
      return {
        filesystem: item.filesystem,
        mountPoint: item.mountPoint,
        totalKb: item.totalKb,
        usedKb: item.usedKb,
        availableKb: item.availableKb,
        usedPercent: Number(Math.max(0, Math.min(100, usedPercent)).toFixed(2))
      } satisfies DiskUsageEntry;
    })
    .sort((a, b) => a.mountPoint.localeCompare(b.mountPoint));
};

export const diskRowsToSystemFilesystems = (rows: DiskUsageEntry[]): SystemFilesystemEntry[] => {
  return rows
    .map((row) => ({
      filesystem: row.filesystem,
      totalKb: row.totalKb,
      usedKb: row.usedKb,
      availableKb: row.availableKb,
      mountPoint: row.mountPoint
    }))
    .sort((a, b) => a.mountPoint.localeCompare(b.mountPoint));
};
