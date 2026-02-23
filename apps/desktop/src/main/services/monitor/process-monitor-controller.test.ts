import type { SshConnection } from "../../../../../../packages/ssh/src/index";
import {
  PROCESS_MONITOR_LINUX_CHECK_COMMAND,
  PROCESS_MONITOR_PS_COMMAND,
  ProcessMonitorController,
} from "./process-monitor-controller";

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

const buildPsOutput = (): string => {
  return [
    "1 0 root S 0 20 15.5 0.1 1024 2048 60 systemd",
    "222 1 root S 0 20 3.2 0.0 768 1536 20 sshd",
  ].join("\n");
};

await (async () => {
  let runningExec = 0;
  let maxRunningExec = 0;
  let probeRuns = 0;
  let closeCalls = 0;
  const snapshots: number[] = [];

  const fakeConnection = {
    exec: async (command: string) => {
      if (command === PROCESS_MONITOR_LINUX_CHECK_COMMAND) {
        return { stdout: "Linux\n", stderr: "", exitCode: 0 };
      }

      if (command === PROCESS_MONITOR_PS_COMMAND) {
        probeRuns += 1;
        runningExec += 1;
        maxRunningExec = Math.max(maxRunningExec, runningExec);
        await wait(35);
        runningExec -= 1;
        return { stdout: buildPsOutput(), stderr: "", exitCode: 0 };
      }

      return { stdout: "", stderr: "", exitCode: 1 };
    },
  } as unknown as SshConnection;

  const controller = new ProcessMonitorController({
    connectionId: "conn-process-inflight",
    getConnection: async () => fakeConnection,
    closeConnection: async () => {
      closeCalls += 1;
    },
    isVisibleTerminalAlive: () => true,
    isReceiverAlive: () => true,
    emitSnapshot: (snapshot) => {
      snapshots.push(snapshot.processes.length);
    },
    logger: {
      info: () => undefined,
      warn: () => undefined,
      debug: () => undefined,
    },
    timing: {
      pollIntervalMs: 10,
      startDelayMs: 0,
      execTimeoutMs: 500,
    },
  });

  await controller.start();
  await wait(130);
  await controller.stop();

  assertEqual(maxRunningExec, 1, "process probe should not overlap");
  assertTrue(probeRuns < 8, "process monitor should drop frames when previous probe is running");
  assertTrue(snapshots.length > 0, "process monitor should emit snapshots when probe succeeds");
  assertTrue(closeCalls > 0, "process monitor stop should close hidden connection");
})();

await (async () => {
  let receiverAlive = true;
  let closeCalls = 0;

  const fakeConnection = {
    exec: async (command: string) => {
      if (command === PROCESS_MONITOR_LINUX_CHECK_COMMAND) {
        return { stdout: "Linux\n", stderr: "", exitCode: 0 };
      }
      if (command === PROCESS_MONITOR_PS_COMMAND) {
        return { stdout: buildPsOutput(), stderr: "", exitCode: 0 };
      }
      return { stdout: "", stderr: "", exitCode: 1 };
    },
  } as unknown as SshConnection;

  const controller = new ProcessMonitorController({
    connectionId: "conn-process-receiver",
    getConnection: async () => fakeConnection,
    closeConnection: async () => {
      closeCalls += 1;
    },
    isVisibleTerminalAlive: () => true,
    isReceiverAlive: () => receiverAlive,
    emitSnapshot: () => undefined,
    logger: {
      info: () => undefined,
      warn: () => undefined,
      debug: () => undefined,
    },
    timing: {
      pollIntervalMs: 20,
      startDelayMs: 0,
    },
  });

  await controller.start();
  await wait(35);
  receiverAlive = false;
  await wait(80);

  assertEqual(controller.currentState, "STOPPED", "process monitor should stop after receiver is gone");
  assertTrue(closeCalls > 0, "process monitor should close hidden connection after receiver is gone");
})();

await (async () => {
  let visibleTerminalAlive = true;
  let closeCalls = 0;

  const fakeConnection = {
    exec: async (command: string) => {
      if (command === PROCESS_MONITOR_LINUX_CHECK_COMMAND) {
        return { stdout: "Linux\n", stderr: "", exitCode: 0 };
      }
      if (command === PROCESS_MONITOR_PS_COMMAND) {
        return { stdout: buildPsOutput(), stderr: "", exitCode: 0 };
      }
      return { stdout: "", stderr: "", exitCode: 1 };
    },
  } as unknown as SshConnection;

  const controller = new ProcessMonitorController({
    connectionId: "conn-process-visible",
    getConnection: async () => fakeConnection,
    closeConnection: async () => {
      closeCalls += 1;
    },
    isVisibleTerminalAlive: () => visibleTerminalAlive,
    isReceiverAlive: () => true,
    emitSnapshot: () => undefined,
    logger: {
      info: () => undefined,
      warn: () => undefined,
      debug: () => undefined,
    },
    timing: {
      pollIntervalMs: 20,
      startDelayMs: 0,
    },
  });

  await controller.start();
  await wait(30);
  visibleTerminalAlive = false;
  await wait(80);

  assertEqual(
    controller.currentState,
    "STOPPED",
    "process monitor should stop when no visible terminal remains"
  );
  assertTrue(closeCalls > 0, "process monitor should close hidden connection when no terminal remains");
})();

await (async () => {
  const controller = new ProcessMonitorController({
    connectionId: "conn-process-start-guard",
    getConnection: async () => {
      throw new Error("unexpected");
    },
    closeConnection: async () => undefined,
    isVisibleTerminalAlive: () => false,
    isReceiverAlive: () => true,
    emitSnapshot: () => undefined,
    logger: {
      info: () => undefined,
      warn: () => undefined,
      debug: () => undefined,
    },
    timing: {
      startDelayMs: 0,
    },
  });

  let thrown = false;
  try {
    await controller.start();
  } catch {
    thrown = true;
  }

  assertTrue(thrown, "process monitor should reject start when no visible terminal is connected");
})();
