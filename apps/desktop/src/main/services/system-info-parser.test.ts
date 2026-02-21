import {
  parseCpuInfo,
  parseFilesystemEntries,
  parseNetworkInterfaceTotals,
  parseOsReleaseName
} from "./system-info-parser";

const assertEqual = <T>(actual: T, expected: T, message: string): void => {
  if (actual !== expected) {
    throw new Error(`${message}: expected "${String(expected)}", got "${String(actual)}"`);
  }
};

(() => {
  const raw = [
    "NAME=\"Debian GNU/Linux\"",
    "VERSION_ID=\"12\"",
    "VERSION=\"12 (bookworm)\"",
    "PRETTY_NAME=\"Debian GNU/Linux 12 (bookworm)\""
  ].join("\n");

  assertEqual(parseOsReleaseName(raw), "Debian GNU/Linux 12 (bookworm)", "parse PRETTY_NAME");
})();

(() => {
  const raw = [
    "NAME=\"Ubuntu\"",
    "VERSION=\"22.04.4 LTS (Jammy Jellyfish)\""
  ].join("\n");

  assertEqual(parseOsReleaseName(raw), "Ubuntu 22.04.4 LTS (Jammy Jellyfish)", "fallback to NAME + VERSION");
})();

(() => {
  const raw = [
    "processor\t: 0",
    "model name\t: AMD EPYC 7B13",
    "cpu MHz\t\t: 1996.249",
    "cache size\t: 512 KB",
    "bogomips\t: 3992.49",
    "processor\t: 1"
  ].join("\n");

  const cpu = parseCpuInfo(raw);
  assertEqual(cpu.modelName, "AMD EPYC 7B13", "cpu model");
  assertEqual(cpu.coreCount, 2, "cpu core count from processor lines");
  assertEqual(cpu.frequencyMhz, 1996.249, "cpu frequency");
  assertEqual(cpu.cacheSize, "512 KB", "cpu cache size");
  assertEqual(cpu.bogoMips, 3992.49, "cpu bogoMips");
})();

(() => {
  const raw = [
    "Inter-|   Receive                                                |  Transmit",
    " face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed",
    "    lo: 3560523    2453    0    0    0     0          0         0  3560523    2453    0    0    0     0       0          0",
    "  eth0: 4398046511104 0 0 0 0 0 0 0 2199023255552 0 0 0 0 0 0 0"
  ].join("\n");

  const interfaces = parseNetworkInterfaceTotals(raw);
  assertEqual(interfaces.length, 2, "parse all network interfaces");
  assertEqual(interfaces[0]?.name, "eth0", "sort interface names");
  assertEqual(interfaces[0]?.rxBytes, 4398046511104, "parse rx bytes");
  assertEqual(interfaces[1]?.name, "lo", "include loopback interface");
})();

(() => {
  const raw = [
    "Filesystem     1024-blocks      Used Available Capacity Mounted on",
    "/dev/vda1         10000000   2000000   8000000      20% /",
    "tmpfs              4096000         0   4096000       0% /dev/shm",
    "broken-line",
    "devtmpfs                0         0         0       0% /dev"
  ].join("\n");

  const rows = parseFilesystemEntries(raw);
  assertEqual(rows.length, 2, "parse valid filesystem rows and skip invalid rows");
  assertEqual(rows[0]?.mountPoint, "/", "sort by mount point");
  assertEqual(rows[1]?.mountPoint, "/dev/shm", "parse secondary mount point");
})();
