import {
  parseCompoundOutput,
  parseNetworkInterfaceList,
  parseSystemProbeSections,
  sanitizeProbeText,
} from "./system-probe-parser";

const assertTrue = (value: unknown, message: string): void => {
  if (!value) {
    throw new Error(message);
  }
};

const assertEqual = <T>(actual: T, expected: T, message: string): void => {
  if (actual !== expected) {
    throw new Error(`${message}: expected ${String(expected)}, got ${String(actual)}`);
  }
};

(() => {
  const raw = "row\r\x1b[Kline\nnext";
  const sanitized = sanitizeProbeText(raw);
  assertTrue(!sanitized.includes("\x1b"), "sanitize should strip ansi");
  assertTrue(sanitized.includes("row"), "sanitize should preserve content");
})();

(() => {
  const raw = [
    "---NS_NETCOUNTERS---",
    "123",
    "456",
    "---NS_PROBE_END---",
  ].join("\n");

  const sections = parseCompoundOutput(raw);
  assertEqual(sections.get("NETCOUNTERS"), "123\n456", "parse NETCOUNTERS section");
})();

(() => {
  const sections = parseCompoundOutput([
    "---NS_CPUSTAT---",
    "cpu  1 2 3 4 5 6 7",
    "---NS_MEMINFO---",
    "MemTotal: 1000 kB",
    "MemAvailable: 500 kB",
    "---NS_PROBE_END---",
  ].join("\n"));

  const parsed = parseSystemProbeSections(sections, {
    collectCpuMemSwap: true,
    collectDisk: false,
    includeInterfaceMeta: false,
  });

  assertTrue(!parsed.ok, "missing NETCOUNTERS should fail");
})();

(() => {
  const sections = parseCompoundOutput([
    "---NS_NETCOUNTERS---",
    "invalid",
    "456",
    "---NS_PROBE_END---",
  ].join("\n"));

  const parsed = parseSystemProbeSections(sections, {
    collectCpuMemSwap: false,
    collectDisk: false,
    includeInterfaceMeta: false,
  });

  assertTrue(!parsed.ok, "invalid NETCOUNTERS should fail");
})();

(() => {
  const sections = parseCompoundOutput([
    "---NS_LOADAVG---",
    "0.10 0.20 0.30",
    "---NS_CPUSTAT---",
    "cpu  100 10 20 300 40 0 0 0 0 0",
    "---NS_MEMINFO---",
    "MemTotal: 1024000 kB",
    "MemAvailable: 512000 kB",
    "SwapTotal: 1024 kB",
    "SwapFree: 1024 kB",
    "---NS_DISK---",
    "/dev/vda1 102400 20480 81920 20% /",
    "---NS_NETIFACES---",
    "eth0",
    "ens5",
    "---NS_NETDEFAULT---",
    "ens5",
    "---NS_NETCOUNTER_IFACE---",
    "ens5",
    "---NS_NETCOUNTERS---",
    "1000",
    "2000",
    "---NS_PROBE_END---",
  ].join("\n"));

  const parsed = parseSystemProbeSections(sections, {
    collectCpuMemSwap: true,
    collectDisk: true,
    includeInterfaceMeta: true,
  });

  assertTrue(parsed.ok, "valid probe payload should pass");
  if (!parsed.ok) {
    return;
  }

  assertEqual(parsed.frame.networkCounters.rxBytes, 1000, "parse rx bytes");
  assertEqual(parsed.frame.networkCounters.txBytes, 2000, "parse tx bytes");
  assertEqual(parsed.frame.cpuTotal !== undefined, true, "parse cpu total");
  assertEqual(parsed.frame.disk?.diskUsedKb, 20480, "parse disk usage");
  assertEqual(parsed.frame.defaultNetworkInterface, "ens5", "parse default interface");
  assertEqual(parsed.frame.networkCounterInterface, "ens5", "parse counter interface");
})();

(() => {
  const parsed = parseNetworkInterfaceList(["lo", "eth0", "ens5", "eth0"].join("\n"));
  assertEqual(parsed.length, 2, "should remove lo and duplicates");
  assertEqual(parsed[0], "ens5", "should sort interfaces");
})();
