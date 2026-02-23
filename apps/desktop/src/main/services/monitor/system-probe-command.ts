export const MONITOR_LOADAVG_COMMAND =
  "cat /proc/loadavg 2>/dev/null | awk '{print $1\" \"$2\" \"$3}' || uptime 2>/dev/null";
export const MONITOR_CPU_STAT_COMMAND = "grep '^cpu ' /proc/stat 2>/dev/null";
export const MONITOR_MEMINFO_COMMAND = "cat /proc/meminfo 2>/dev/null";
export const MONITOR_FREE_COMMAND = "free -k 2>/dev/null";
export const MONITOR_DISK_COMMAND = "df -kP / 2>/dev/null | tail -n 1";
export const MONITOR_NET_INTERFACES_COMMAND = "ls -1 /sys/class/net 2>/dev/null | grep -v '^lo$'";
export const MONITOR_NET_DEFAULT_INTERFACE_COMMAND =
  "ip route show default 2>/dev/null | awk 'NR==1 {for (i=1;i<=NF;i++) if ($i==\"dev\") {print $(i+1); exit}}'";
export const MONITOR_SYSTEM_PROCESS_COMMAND =
  "ps -eo pid=,comm=,%cpu=,rss= --sort=-%cpu 2>/dev/null | head -n 5";

export interface DynamicSystemProbeOptions {
  collectCpuMemSwap: boolean;
  collectDisk: boolean;
  includeInterfaceMeta: boolean;
}

export const normalizeNetworkInterfaceName = (value: string): string | undefined => {
  const trimmed = value.trim();
  if (!trimmed) {
    return undefined;
  }

  if (!/^[a-zA-Z0-9_.:-]+$/.test(trimmed)) {
    return undefined;
  }

  return trimmed;
};

export const buildNetCountersCommand = (networkInterface: string): string => {
  const normalized = normalizeNetworkInterfaceName(networkInterface);
  if (!normalized) {
    throw new Error("无效网卡名称");
  }

  return (
    `cat /sys/class/net/${normalized}/statistics/rx_bytes 2>/dev/null; ` +
    `cat /sys/class/net/${normalized}/statistics/tx_bytes 2>/dev/null`
  );
};

const buildSafeInterfaceCounterCommand = (networkInterface: string): string[] => {
  return [
    `__NS_MON_IF='${networkInterface}'`,
    `if [ ! -r "/sys/class/net/${networkInterface}/statistics/rx_bytes" ] || [ ! -r "/sys/class/net/${networkInterface}/statistics/tx_bytes" ]; then __NS_MON_IF="$(${MONITOR_NET_DEFAULT_INTERFACE_COMMAND})"; fi`,
    "if [ -z \"$__NS_MON_IF\" ] || [ ! -r \"/sys/class/net/$__NS_MON_IF/statistics/rx_bytes\" ] || [ ! -r \"/sys/class/net/$__NS_MON_IF/statistics/tx_bytes\" ]; then __NS_MON_IF=\"$(ls -1 /sys/class/net 2>/dev/null | grep -v '^lo$' | head -n 1)\"; fi",
    "echo '---NS_NETCOUNTER_IFACE---'",
    "printf '%s\\n' \"$__NS_MON_IF\"",
    "echo '---NS_NETCOUNTERS---'",
    "cat \"/sys/class/net/$__NS_MON_IF/statistics/rx_bytes\" 2>/dev/null; cat \"/sys/class/net/$__NS_MON_IF/statistics/tx_bytes\" 2>/dev/null"
  ];
};

export const buildDynamicSystemProbeCommand = (
  networkInterface: string,
  options: DynamicSystemProbeOptions
): string => {
  const normalized = normalizeNetworkInterfaceName(networkInterface) ?? "eth0";
  const parts: string[] = [];

  if (options.collectCpuMemSwap) {
    parts.push(
      "echo '---NS_LOADAVG---'",
      MONITOR_LOADAVG_COMMAND,
      "echo '---NS_CPUSTAT---'",
      MONITOR_CPU_STAT_COMMAND,
      "echo '---NS_MEMINFO---'",
      MONITOR_MEMINFO_COMMAND,
      "echo '---NS_FREE---'",
      MONITOR_FREE_COMMAND,
      "echo '---NS_PROCESSES---'",
      MONITOR_SYSTEM_PROCESS_COMMAND
    );
  }

  if (options.collectDisk) {
    parts.push("echo '---NS_DISK---'", MONITOR_DISK_COMMAND);
  }

  if (options.includeInterfaceMeta) {
    parts.push(
      "echo '---NS_NETIFACES---'",
      MONITOR_NET_INTERFACES_COMMAND,
      "echo '---NS_NETDEFAULT---'",
      MONITOR_NET_DEFAULT_INTERFACE_COMMAND,
      ...buildSafeInterfaceCounterCommand(normalized)
    );
  } else {
    parts.push(
      "echo '---NS_NETCOUNTERS---'",
      buildNetCountersCommand(normalized)
    );
  }

  parts.push(
    "echo '---NS_PROBE_END---'"
  );

  return parts.join("; ");
};
