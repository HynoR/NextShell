import { formatDiskSize, parseDfOutput } from "./diskUsage";

const assertEqual = <T>(actual: T, expected: T, message: string): void => {
  if (actual !== expected) {
    throw new Error(`${message}: expected "${String(expected)}", got "${String(actual)}"`);
  }
};

(() => {
  const raw = [
    "Filesystem     1024-blocks      Used Available Capacity Mounted on",
    "/dev/vda1         10000000   2000000   8000000      20% /",
    "tmpfs              4096000         0   4096000       0% /dev/shm",
    "broken-line",
    "devtmpfs                0         0         0       0% /dev"
  ].join("\n");

  const rows = parseDfOutput(raw);
  assertEqual(rows.length, 2, "parse valid df rows and skip invalid rows");
  assertEqual(rows[0]?.mountPoint, "/", "sorted mount points");
  assertEqual(rows[0]?.availableKb, 8000000, "parse available blocks");
  assertEqual(rows[1]?.mountPoint, "/dev/shm", "parse secondary row");
})();

(() => {
  const raw = [
    "Filesystem 1024-blocks Used Available Use% Mounted on",
    "overlay 52428800 20971520 31457280 unknown /var/lib/docker"
  ].join("\n");

  const rows = parseDfOutput(raw);
  assertEqual(rows.length, 1, "fallback to computed percent when Use% is missing");
  assertEqual(rows[0]?.usedPercent, 40, "computed used percent");
})();

(() => {
  assertEqual(formatDiskSize(795 * 1024), "795M", "format megabytes");
  assertEqual(formatDiskSize(5 * 1024 * 1024), "5.0G", "format gigabytes");
})();

