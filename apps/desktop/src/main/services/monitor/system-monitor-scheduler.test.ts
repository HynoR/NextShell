import type { SshConnection } from "../../../../../../packages/ssh/src/index";
import { SystemMonitorController } from "./system-monitor-controller";

interface ExecResult {
  stdout: string;
  stderr: string;
  exitCode: number;
}

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

const buildProbeOutput = (
  command: string,
  sample: number,
  options?: { missingNetCounters?: boolean }
): string => {
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
  if (command.includes("---NS_NETIFACES---")) {
    parts.push("---NS_NETIFACES---", "eth0", "ens5");
  }
  if (command.includes("---NS_NETDEFAULT---")) {
    parts.push("---NS_NETDEFAULT---", "eth0");
  }
  if (command.includes("---NS_NETCOUNTERS---") && !options?.missingNetCounters) {
    parts.push("---NS_NETCOUNTERS---", String(sample * 100), String(sample * 120));
  }

  parts.push("---NS_PROBE_END---");
  return parts.join("\n");
};

const createController = (
  execImpl: (command: string) => Promise<ExecResult>,
  emitSnapshot: (snapshot: { networkInMbps: number; networkOutMbps: number }) => void,
  timing?: {
    pollIntervalMs?: number;
    cpuMemSwapIntervalTicks?: number;
    diskIntervalTicks?: number;
    interfaceMetaIntervalTicks?: number;
    startDelayMs?: number;
    execTimeoutMs?: number;
    maxConsecutiveFailures?: number;
  }
): SystemMonitorController => {
  const fakeConnection = {
    exec: (command: string) => execImpl(command),
  } as unknown as SshConnection;

  return new SystemMonitorController({
    connectionId: "conn-1",
    getConnection: async () => fakeConnection,
    closeConnection: async () => undefined,
    isVisibleTerminalAlive: () => true,
    isReceiverAlive: () => true,
    emitSnapshot,
    readSelection: () => ({ selectedNetworkInterface: "eth0", networkInterfaceOptions: ["eth0"] }),
    writeSelection: () => undefined,
    logger: {
      info: () => undefined,
      warn: () => undefined,
      debug: () => undefined,
    },
    timing,
  });
};

await (async () => {
  const commands: string[] = [];
  let sample = 0;

  const controller = createController(
    async (command) => {
      commands.push(command);
      sample += 1;
      return {
        stdout: buildProbeOutput(command, sample),
        stderr: "",
        exitCode: 0,
      };
    },
    () => undefined,
    {
      pollIntervalMs: 10,
      startDelayMs: 0,
      interfaceMetaIntervalTicks: 1000,
    }
  );

  await controller.start();
  await wait(140);
  await controller.stop();

  assertTrue(commands.length >= 11, "scheduler should execute at least 11 probes");
  const firstEleven = commands.slice(0, 11);
  const cpuFrames = firstEleven.filter((command) => command.includes("---NS_CPUSTAT---")).length;
  const diskFrames = firstEleven.filter((command) => command.includes("---NS_DISK---")).length;

  assertEqual(cpuFrames, 4, "cpu/mem probes should run every 3 ticks plus initial probe");
  assertEqual(diskFrames, 2, "disk probes should run every 10 ticks plus initial probe");
})();

await (async () => {
  const commands: string[] = [];
  let sample = 0;
  let activeExec = 0;
  let maxConcurrentExec = 0;

  const controller = createController(
    async (command) => {
      commands.push(command);
      sample += 1;
      activeExec += 1;
      maxConcurrentExec = Math.max(maxConcurrentExec, activeExec);
      await wait(35);
      activeExec -= 1;
      return {
        stdout: buildProbeOutput(command, sample),
        stderr: "",
        exitCode: 0,
      };
    },
    () => undefined,
    {
      pollIntervalMs: 10,
      startDelayMs: 0,
      interfaceMetaIntervalTicks: 1000,
    }
  );

  await controller.start();
  await wait(120);
  await controller.stop();

  assertEqual(maxConcurrentExec, 1, "probe execution should never overlap");
  assertTrue(commands.length < 8, "scheduler should drop ticks when previous probe is still running");
})();

await (async () => {
  const speeds: number[] = [];
  let sample = 0;

  const controller = createController(
    async (command) => {
      sample += 1;

      if (sample === 3) {
        return {
          stdout: buildProbeOutput(command, sample, { missingNetCounters: true }),
          stderr: "",
          exitCode: 0,
        };
      }

      return {
        stdout: buildProbeOutput(command, sample),
        stderr: "",
        exitCode: 0,
      };
    },
    (snapshot) => {
      speeds.push(snapshot.networkInMbps);
    },
    {
      pollIntervalMs: 30,
      cpuMemSwapIntervalTicks: 1,
      diskIntervalTicks: 1000,
      interfaceMetaIntervalTicks: 1000,
      startDelayMs: 0,
    }
  );

  await controller.start();
  await wait(125);
  await controller.stop();

  assertTrue(speeds.length >= 3, "valid probes should emit snapshots");
  const maxSpeed = Math.max(...speeds);
  assertTrue(maxSpeed < 50, "dropped frame should not reset baseline and cause speed spike");
})();
