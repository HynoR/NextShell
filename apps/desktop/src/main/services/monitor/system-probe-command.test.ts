import { buildDynamicSystemProbeCommand } from "./system-probe-command";

const assertTrue = (value: unknown, message: string): void => {
  if (!value) {
    throw new Error(message);
  }
};

(() => {
  const command = buildDynamicSystemProbeCommand("eth0", {
    collectCpuMemSwap: true,
    collectDisk: true,
    includeInterfaceMeta: true,
  });

  assertTrue(command.includes("---NS_NETIFACES---"), "includeInterfaceMeta should include NETIFACES section");
  assertTrue(command.includes("---NS_NETDEFAULT---"), "includeInterfaceMeta should include NETDEFAULT section");
  assertTrue(
    command.includes("---NS_NETCOUNTER_IFACE---"),
    "includeInterfaceMeta should include NETCOUNTER_IFACE section"
  );
  assertTrue(command.includes("__NS_MON_IF='eth0'"), "counter command should start from selected interface");
})();

(() => {
  const command = buildDynamicSystemProbeCommand("ens5", {
    collectCpuMemSwap: false,
    collectDisk: false,
    includeInterfaceMeta: false,
  });

  assertTrue(!command.includes("---NS_NETIFACES---"), "normal net tick should skip NETIFACES section");
  assertTrue(!command.includes("---NS_NETDEFAULT---"), "normal net tick should skip NETDEFAULT section");
  assertTrue(command.includes("---NS_NETCOUNTERS---"), "normal net tick should include NETCOUNTERS section");
  assertTrue(!command.includes("__NS_MON_IF='ens5'"), "normal net tick should avoid interface fallback shell logic");
})();
