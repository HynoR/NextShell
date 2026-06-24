const BYTE_UNITS = ["B", "KB", "MB", "GB", "TB", "PB"] as const;

/**
 * Format a byte count into a compact human-readable string (binary units).
 * e.g. 0 -> "0 B", 1536 -> "1.5 KB", 1048576 -> "1.0 MB".
 */
export const formatBytes = (bytes: number): string => {
  if (!Number.isFinite(bytes) || bytes <= 0) {
    return "0 B";
  }
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < BYTE_UNITS.length - 1) {
    value /= 1024;
    unit += 1;
  }
  const label = BYTE_UNITS[unit] ?? "B";
  const rounded = value >= 100 || unit === 0 ? Math.round(value) : Number(value.toFixed(1));
  return `${rounded} ${label}`;
};

/** Format a transfer rate (bytes/second) as "X/s". */
export const formatSpeed = (bytesPerSec: number): string => `${formatBytes(bytesPerSec)}/s`;
