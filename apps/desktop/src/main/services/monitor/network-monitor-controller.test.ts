import type { SshConnection } from "../../../../../../packages/ssh/src/index";
import {
  NETWORK_PROBE_NETSTAT,
  NETWORK_PROBE_SS,
  NetworkMonitorController,
  type NetworkTool,
} from "./network-monitor-controller";

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

const buildSsProbeOutput = (): string => {
  return [
    "Netid State Recv-Q Send-Q Local Address:Port Peer Address:Port Process",
    'tcp LISTEN 0 128 0.0.0.0:22 0.0.0.0:* users:(("sshd",pid=101,fd=3))',
    'tcp ESTAB 0 0 10.0.0.1:22 10.0.0.2:51515 users:(("sshd",pid=101,fd=4))',
  ].join("\n");
};

const buildSsConnectionsOutput = (): string => {
  return [
    "State Recv-Q Send-Q Local Address:Port Peer Address:Port Process",
    'ESTAB 0 0 10.0.0.1:22 10.0.0.3:52000 users:(("sshd",pid=101,fd=9))',
  ].join("\n");
};

await (async () => {
  const commands: string[] = [];
  const snapshots: number[] = [];
  let cachedTool: NetworkTool | undefined;
  let openShellCalls = 0;
  let closeCalls = 0;

  const fakeConnection = {
    exec: async (command: string) => {
      commands.push(command);

      if (command.includes("command -v ss")) {
        return { stdout: "", stderr: "", exitCode: 0 };
      }
      if (command.includes("command -v netstat")) {
        return { stdout: "", stderr: "", exitCode: 1 };
      }
      if (command === NETWORK_PROBE_SS) {
        return { stdout: buildSsProbeOutput(), stderr: "", exitCode: 0 };
      }
      if (command.includes("ss -tnap")) {
        return { stdout: buildSsConnectionsOutput(), stderr: "", exitCode: 0 };
      }
      if (command === NETWORK_PROBE_NETSTAT) {
        throw new Error("unexpected netstat probe");
      }
      return { stdout: "", stderr: "", exitCode: 1 };
    },
    openShell: () => {
      openShellCalls += 1;
      throw new Error("network controller should not open interactive shell");
    },
  } as unknown as SshConnection;

  const controller = new NetworkMonitorController({
    connectionId: "conn-network-tool",
    getConnection: async () => fakeConnection,
    closeConnection: async () => {
      closeCalls += 1;
    },
    isVisibleTerminalAlive: () => true,
    isReceiverAlive: () => true,
    emitSnapshot: (snapshot) => {
      snapshots.push(snapshot.listeners.length);
    },
    readToolCache: () => cachedTool,
    writeToolCache: (tool) => {
      cachedTool = tool;
    },
    logger: {
      info: () => undefined,
      warn: () => undefined,
      debug: () => undefined,
    },
    timing: {
      pollIntervalMs: 20,
      startDelayMs: 0,
      execTimeoutMs: 500,
    },
  });

  await controller.start();
  await wait(90);
  const connections = await controller.getConnectionsByPort(22);
  await controller.stop();

  assertEqual(cachedTool, "ss", "network monitor should detect and cache ss as probe tool");
  assertTrue(
    commands.some((command) => command.includes("command -v ss")),
    "network monitor should probe ss availability"
  );
  assertTrue(snapshots.length > 0, "network monitor should emit snapshots after probe");
  assertTrue(connections.length > 0, "network monitor should support on-demand connection query");
  assertEqual(connections[0]?.localPort, 22, "on-demand query should parse local port");
  assertEqual(openShellCalls, 0, "network monitor should not use openShell path");
  assertTrue(closeCalls > 0, "network monitor stop should close hidden connection");
})();

await (async () => {
  let probeAttempts = 0;
  let closeCalls = 0;
  const snapshots: number[] = [];

  const fakeConnection = {
    exec: async (command: string) => {
      if (command.includes("command -v ss")) {
        return { stdout: "", stderr: "", exitCode: 0 };
      }
      if (command === NETWORK_PROBE_SS) {
        probeAttempts += 1;
        if (probeAttempts === 1) {
          return new Promise<{
            stdout: string;
            stderr: string;
            exitCode: number;
          }>(() => undefined);
        }
        return { stdout: buildSsProbeOutput(), stderr: "", exitCode: 0 };
      }
      return { stdout: "", stderr: "", exitCode: 1 };
    },
  } as unknown as SshConnection;

  const controller = new NetworkMonitorController({
    connectionId: "conn-network-timeout",
    getConnection: async () => fakeConnection,
    closeConnection: async () => {
      closeCalls += 1;
    },
    isVisibleTerminalAlive: () => true,
    isReceiverAlive: () => true,
    emitSnapshot: (snapshot) => {
      snapshots.push(snapshot.listeners.length);
    },
    readToolCache: () => "ss",
    writeToolCache: () => undefined,
    logger: {
      info: () => undefined,
      warn: () => undefined,
      debug: () => undefined,
    },
    timing: {
      pollIntervalMs: 20,
      startDelayMs: 0,
      execTimeoutMs: 20,
    },
  });

  await controller.start();
  await wait(130);
  await controller.stop();

  assertTrue(closeCalls > 0, "network monitor should close hidden connection on timeout");
  assertTrue(snapshots.length > 0, "network monitor should recover and emit snapshots after timeout");
})();

await (async () => {
  let probeAttempts = 0;
  let closeCalls = 0;
  const snapshots: number[] = [];

  const fakeConnection = {
    exec: async (command: string) => {
      if (command.includes("command -v ss")) {
        return { stdout: "", stderr: "", exitCode: 0 };
      }
      if (command === NETWORK_PROBE_SS) {
        probeAttempts += 1;
        if (probeAttempts <= 3) {
          return { stdout: "bad frame", stderr: "", exitCode: 1 };
        }
        return { stdout: buildSsProbeOutput(), stderr: "", exitCode: 0 };
      }
      return { stdout: "", stderr: "", exitCode: 1 };
    },
  } as unknown as SshConnection;

  const controller = new NetworkMonitorController({
    connectionId: "conn-network-failure-threshold",
    getConnection: async () => fakeConnection,
    closeConnection: async () => {
      closeCalls += 1;
    },
    isVisibleTerminalAlive: () => true,
    isReceiverAlive: () => true,
    emitSnapshot: (snapshot) => {
      snapshots.push(snapshot.listeners.length);
    },
    readToolCache: () => "ss",
    writeToolCache: () => undefined,
    logger: {
      info: () => undefined,
      warn: () => undefined,
      debug: () => undefined,
    },
    timing: {
      pollIntervalMs: 20,
      startDelayMs: 0,
      execTimeoutMs: 500,
      maxConsecutiveFailures: 3,
    },
  });

  await controller.start();
  await wait(150);
  await controller.stop();

  assertTrue(closeCalls > 0, "network monitor should reset hidden connection after repeated failures");
  assertTrue(
    snapshots.length > 0,
    "network monitor should continue polling and emit snapshots after failure threshold reset"
  );
})();
