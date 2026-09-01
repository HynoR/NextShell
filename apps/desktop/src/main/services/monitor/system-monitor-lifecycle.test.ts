import { SystemMonitorController } from "./system-monitor-controller";

const assertTrue = (value: unknown, message: string): void => {
  if (!value) throw new Error(message);
};

const assertEqual = <T>(actual: T, expected: T, message: string): void => {
  if (actual !== expected)
    throw new Error(`${message}: expected ${String(expected)}, got ${String(actual)}`);
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
  let stopCalls = 0;
  let sample = 0;
  const snapshots: number[] = [];

  const controller = new SystemMonitorController({
    connectionId: "conn-lifecycle",
    exec: async (command) => {
      sample += 1;
      return { stdout: buildProbeOutput(command, sample), stderr: "", exitCode: 0 };
    },
    stopMonitor: async () => {
      stopCalls += 1;
    },
    isVisibleTerminalAlive: () => visible,
    isReceiverAlive: () => true,
    emitSnapshot: (snapshot) => snapshots.push(snapshot.networkInMbps),
    readSelection: () => ({ selectedNetworkInterface: "eth0", networkInterfaceOptions: ["eth0"] }),
    writeSelection: () => undefined,
    logger: { info: () => undefined, warn: () => undefined, debug: () => undefined },
    timing: { pollIntervalMs: 10, startDelayMs: 0 }
  });

  await controller.start();
  await wait(30);
  assertTrue(snapshots.length > 0, "controller should emit snapshots while running");

  visible = false;
  await wait(30);
  assertEqual(controller.currentState, "STOPPED", "monitor should stop when terminal disappears");
  assertTrue(stopCalls > 0, "monitor should release its runtime when terminal disappears");
})();

await (async () => {
  const controller = new SystemMonitorController({
    connectionId: "conn-start-guard",
    exec: async () => {
      throw new Error("unexpected");
    },
    stopMonitor: async () => undefined,
    isVisibleTerminalAlive: () => false,
    isReceiverAlive: () => true,
    emitSnapshot: () => undefined,
    readSelection: () => undefined,
    writeSelection: () => undefined,
    logger: { info: () => undefined, warn: () => undefined, debug: () => undefined },
    timing: { startDelayMs: 0 }
  });

  let thrown = false;
  try {
    await controller.start();
  } catch {
    thrown = true;
  }
  assertTrue(thrown, "monitor should reject start without a visible terminal");
})();
