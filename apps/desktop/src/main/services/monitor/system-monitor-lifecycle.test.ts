import type { SshConnection } from "../../../../../../packages/ssh/src/index";
import { SystemMonitorController } from "./system-monitor-controller";

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

const wait = async (ms: number): Promise<void> => {
  await new Promise<void>((resolve) => setTimeout(resolve, ms));
};

const buildProbeOutput = (command: string, sample: number): string => {
  const parts: string[] = [];
  if (command.includes("---NS_LOADAVG---")) {
    parts.push("---NS_LOADAVG---", `0.${sample} 0.${sample} 0.${sample}`);
  }
  if (command.includes("---NS_CPUSTAT---")) {
    parts.push("---NS_CPUSTAT---", `cpu  ${100 + sample} 0 0 ${200 + sample} 0 0 0 0 0 0`);
  }
  if (command.includes("---NS_MEMINFO---")) {
    parts.push(
      "---NS_MEMINFO---",
      "MemTotal: 1024000 kB",
      "MemAvailable: 512000 kB",
      "SwapTotal: 1024 kB",
      "SwapFree: 1024 kB"
    );
  }
  if (command.includes("---NS_FREE---")) {
    parts.push("---NS_FREE---", "Mem: 1024000 512000 512000 0 0 512000");
  }
  if (command.includes("---NS_PROCESSES---")) {
    parts.push("---NS_PROCESSES---", "1 init 0.1 1024");
  }
  if (command.includes("---NS_DISK---")) {
    parts.push("---NS_DISK---", "/dev/vda1 102400 20480 81920 20% /");
  }
  if (command.includes("---NS_NETCOUNTERS---")) {
    parts.push("---NS_NETCOUNTERS---", String(sample * 100), String(sample * 110));
  }
  parts.push("---NS_PROBE_END---");
  return parts.join("\n");
};

await (async () => {
  let visible = true;
  let receiver = true;
  let closeCalls = 0;
  let getConnectionCalls = 0;
  let sample = 0;
  const snapshots: number[] = [];

  const fakeConnection = {
    exec: async (command: string) => {
      sample += 1;
      return {
        stdout: buildProbeOutput(command, sample),
        stderr: "",
        exitCode: 0,
      };
    },
  } as unknown as SshConnection;

  const controller = new SystemMonitorController({
    connectionId: "conn-lifecycle",
    getConnection: async () => {
      getConnectionCalls += 1;
      return fakeConnection;
    },
    closeConnection: async () => {
      closeCalls += 1;
    },
    isVisibleTerminalAlive: () => visible,
    isReceiverAlive: () => receiver,
    emitSnapshot: (snapshot) => {
      snapshots.push(snapshot.networkInMbps);
    },
    readSelection: () => ({ selectedNetworkInterface: "eth0", networkInterfaceOptions: ["eth0"] }),
    writeSelection: () => undefined,
    logger: {
      info: () => undefined,
      warn: () => undefined,
      debug: () => undefined,
    },
    timing: {
      pollIntervalMs: 20,
      cpuMemSwapIntervalTicks: 1,
      diskIntervalTicks: 1000,
      interfaceMetaIntervalTicks: 1000,
      startDelayMs: 0,
    },
  });

  await controller.start();
  await wait(60);
  assertTrue(getConnectionCalls > 0, "controller should acquire hidden monitor connection after start");
  assertTrue(snapshots.length > 0, "controller should emit snapshots while running");

  visible = false;
  await wait(80);

  assertEqual(controller.currentState, "STOPPED", "controller should stop when last visible terminal disappears");
  assertTrue(closeCalls > 0, "controller should close hidden monitor connection during stop");

  receiver = false;
  await controller.stop();

  let thrown = false;
  try {
    await controller.start();
  } catch {
    thrown = true;
  }
  assertTrue(thrown, "controller should reject start when no visible terminal is connected");
})();

await (async () => {
  const writes: Array<{ selectedNetworkInterface?: string; networkInterfaceOptions?: string[] }> = [];
  let selection = {
    selectedNetworkInterface: "eth0",
    networkInterfaceOptions: ["ens5"],
  };

  const fakeConnection = {
    exec: async () => {
      return {
        stdout: [
          "---NS_LOADAVG---",
          "0.10 0.20 0.30",
          "---NS_CPUSTAT---",
          "cpu  100 0 0 200 0 0 0 0 0 0",
          "---NS_MEMINFO---",
          "MemTotal: 1024000 kB",
          "MemAvailable: 512000 kB",
          "SwapTotal: 1024 kB",
          "SwapFree: 1024 kB",
          "---NS_FREE---",
          "Mem: 1024000 512000 512000 0 0 512000",
          "---NS_PROCESSES---",
          "1 init 0.1 1024",
          "---NS_DISK---",
          "/dev/vda1 102400 20480 81920 20% /",
          "---NS_NETIFACES---",
          "ens5",
          "---NS_NETDEFAULT---",
          "ens5",
          "---NS_NETCOUNTER_IFACE---",
          "ens5",
          "---NS_NETCOUNTERS---",
          "1000",
          "1100",
          "---NS_PROBE_END---",
        ].join("\n"),
        stderr: "",
        exitCode: 0,
      };
    },
  } as unknown as SshConnection;

  const controller = new SystemMonitorController({
    connectionId: "conn-selection",
    getConnection: async () => fakeConnection,
    closeConnection: async () => undefined,
    isVisibleTerminalAlive: () => true,
    isReceiverAlive: () => true,
    emitSnapshot: () => undefined,
    readSelection: () => selection,
    writeSelection: (state) => {
      writes.push(state);
      selection = { ...selection, ...state };
    },
    logger: {
      info: () => undefined,
      warn: () => undefined,
      debug: () => undefined,
    },
    timing: {
      pollIntervalMs: 100,
      startDelayMs: 0,
    },
  });

  await controller.start();
  await wait(30);
  await controller.stop();

  assertTrue(
    writes.some((state) => state.selectedNetworkInterface === "ens5"),
    "controller should correct invalid selected interface to effective interface"
  );
})();
