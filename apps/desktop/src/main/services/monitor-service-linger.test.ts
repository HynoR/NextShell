import type { WebContents } from "electron";
import type { ConnectionProfile } from "../../../../../packages/core/src/index";
import { SshConnection, type SshConnectOptions } from "../../../../../packages/ssh/src/index";
import type { CachedConnectionRepository } from "../../../../../packages/storage/src/index";
import type { ActiveSession } from "./container-types";
import { MonitorService } from "./monitor-service";

// Linger coverage for the system monitor: losing the last subscriber must NOT
// close the hidden SSH connection right away (tab flipping), while every
// teardown path still must. Written in the style of
// ./monitor/system-monitor-lifecycle.test.ts: bare assertions, real (short)
// timers, no test runner API — the linger delay is injected instead of faked so
// the real setTimeout/runExclusive interplay is what gets exercised.

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

const LINGER_MS = 150;
const CONNECTION_ID = "conn-linger";

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
  if (command.includes("---NS_NETIFACES---")) {
    parts.push("---NS_NETIFACES---", "eth0");
  }
  if (command.includes("---NS_NETDEFAULT---")) {
    parts.push("---NS_NETDEFAULT---", "eth0");
  }
  if (command.includes("---NS_NETCOUNTER_IFACE---")) {
    parts.push("---NS_NETCOUNTER_IFACE---", "eth0");
  }
  if (command.includes("---NS_NETCOUNTERS---")) {
    parts.push("---NS_NETCOUNTERS---", String(sample * 100), String(sample * 110));
  }
  parts.push("---NS_PROBE_END---");
  return parts.join("\n");
};

interface Harness {
  service: MonitorService;
  sender: WebContents;
  /** Handlers the service registered on the fake renderer (see watchSender). */
  senderEvents: Map<string, () => void>;
  dials: () => number;
  closes: () => number;
  dispose: () => void;
}

const createHarness = (): Harness => {
  let dials = 0;
  let closes = 0;
  let sample = 0;

  const fakeConnection = {
    exec: async (command: string) => {
      sample += 1;
      return { stdout: buildProbeOutput(command, sample), stderr: "", exitCode: 0 };
    },
    close: async () => {
      closes += 1;
    },
    onClose: () => undefined
  } as unknown as SshConnection;

  const connectHolder = SshConnection as unknown as {
    connect: (options: SshConnectOptions) => Promise<SshConnection>;
  };
  const originalConnect = connectHolder.connect;
  connectHolder.connect = async () => {
    dials += 1;
    return fakeConnection;
  };

  const senderEvents = new Map<string, () => void>();
  const sender = {
    isDestroyed: () => false,
    isCrashed: () => false,
    once: (event: string, handler: () => void) => {
      senderEvents.set(event, handler);
    },
    on: () => undefined
  } as unknown as WebContents;

  const profile = {
    id: CONNECTION_ID,
    name: "linger-host",
    host: "127.0.0.1",
    port: 22,
    username: "root",
    monitorSession: true
  } as unknown as ConnectionProfile;

  const activeSessions = new Map<string, ActiveSession>([
    [
      "term-1",
      {
        kind: "remote",
        connectionId: CONNECTION_ID,
        descriptor: { id: "term-1", type: "terminal", status: "connected" },
        sender
      } as unknown as ActiveSession
    ]
  ]);

  const service = new MonitorService({
    connections: {} as unknown as CachedConnectionRepository,
    getConnectionOrThrow: () => profile,
    resolveConnectOptions: async () => ({}) as unknown as SshConnectOptions,
    activeSessions,
    debugSenders: new Set<WebContents>(),
    emitDebugLog: () => undefined,
    emitSystemSnapshot: () => undefined,
    emitProcessSnapshot: () => undefined,
    emitNetworkSnapshot: () => undefined,
    systemMonitorLingerMs: LINGER_MS
  });

  return {
    service,
    sender,
    senderEvents,
    dials: () => dials,
    closes: () => closes,
    dispose: () => {
      connectHolder.connect = originalConnect;
    }
  };
};

// A→B→A tab flip: the stop schedules the teardown instead of performing it, and
// coming back inside the window reuses the live hidden SSH session.
await (async () => {
  const harness = createHarness();
  try {
    await harness.service.startSystemMonitor(CONNECTION_ID, harness.sender, "subscriber-1");
    assertEqual(harness.dials(), 1, "starting the system monitor should dial the hidden SSH once");

    await harness.service.stopSystemMonitor(CONNECTION_ID, "subscriber-1");
    await wait(LINGER_MS / 3);
    assertEqual(
      harness.closes(),
      0,
      "losing the last subscriber must not close the hidden SSH inside the linger window"
    );

    await harness.service.startSystemMonitor(CONNECTION_ID, harness.sender, "subscriber-1");
    assertEqual(harness.dials(), 1, "re-subscribing inside the linger window must not redial SSH");
    assertEqual(harness.closes(), 0, "re-subscribing must not have torn anything down");

    // Past the point where the cancelled timer would have fired.
    await wait(LINGER_MS * 2);
    assertEqual(harness.closes(), 0, "a cancelled linger timer must never stop a live monitor");
    assertEqual(harness.dials(), 1, "the monitor should still be running on the first dial");
  } finally {
    harness.dispose();
  }
})();

// Genuinely idle: the timer fires and really stops the monitor.
await (async () => {
  const harness = createHarness();
  try {
    await harness.service.startSystemMonitor(CONNECTION_ID, harness.sender, "subscriber-1");
    await harness.service.stopSystemMonitor(CONNECTION_ID, "subscriber-1");
    assertTrue(
      harness.service.getAllConnectionIds().includes(CONNECTION_ID),
      "a lingering monitor should still be reported as active state"
    );

    await wait(LINGER_MS * 2);
    assertTrue(
      harness.closes() > 0,
      "the linger timer must close the hidden SSH once the monitor stays idle"
    );

    // A second start after the real stop has to dial again — nothing is warm now.
    await harness.service.startSystemMonitor(CONNECTION_ID, harness.sender, "subscriber-1");
    assertEqual(harness.dials(), 2, "starting after a completed stop should dial a fresh session");
  } finally {
    harness.dispose();
  }
})();

// Teardown path: connection removal / terminal death (disposeAllMonitorSessions)
// must stop immediately and cancel the pending linger.
await (async () => {
  const harness = createHarness();
  try {
    await harness.service.startSystemMonitor(CONNECTION_ID, harness.sender, "subscriber-1");
    await harness.service.stopSystemMonitor(CONNECTION_ID, "subscriber-1");
    const closesWhileLingering = harness.closes();

    await harness.service.disposeAllMonitorSessions(CONNECTION_ID);
    assertTrue(
      harness.closes() > closesWhileLingering,
      "disposing monitor sessions must close the hidden SSH immediately"
    );
    assertEqual(
      harness.service.getAllConnectionIds().length,
      0,
      "dispose must cancel the pending linger timer, leaving no active monitor state"
    );

    const closesAfterDispose = harness.closes();
    await wait(LINGER_MS * 2);
    assertEqual(
      harness.closes(),
      closesAfterDispose,
      "a cancelled linger timer must not fire after teardown"
    );
  } finally {
    harness.dispose();
  }
})();

// Teardown path: the legacy connection-level stop (no subscriber id) keeps its
// immediate semantics.
await (async () => {
  const harness = createHarness();
  try {
    await harness.service.startSystemMonitor(CONNECTION_ID, harness.sender, "subscriber-1");
    await harness.service.stopSystemMonitor(CONNECTION_ID);
    assertTrue(
      harness.closes() > 0,
      "a stop without a subscriber id must close the hidden SSH immediately"
    );
  } finally {
    harness.dispose();
  }
})();

// Teardown path: the renderer is destroyed while a monitor lingers — nothing can
// come back to it, so the grace period is void.
await (async () => {
  const harness = createHarness();
  try {
    await harness.service.startSystemMonitor(CONNECTION_ID, harness.sender, "subscriber-1");
    await harness.service.stopSystemMonitor(CONNECTION_ID, "subscriber-1");
    assertEqual(harness.closes(), 0, "the monitor should be lingering at this point");

    const onDestroyed = harness.senderEvents.get("destroyed");
    assertTrue(onDestroyed !== undefined, "the service should watch the renderer for destruction");
    onDestroyed?.();
    await wait(20);

    assertTrue(
      harness.closes() > 0,
      "a destroyed renderer must stop the lingering monitor immediately"
    );
  } finally {
    harness.dispose();
  }
})();

// Failure path: no visible terminal left means a re-subscribe could not start
// anything, so there is nothing worth keeping warm.
await (async () => {
  const harness = createHarness();
  try {
    await harness.service.startSystemMonitor(CONNECTION_ID, harness.sender, "subscriber-1");
    const activeSessions = (
      harness.service as unknown as { activeSessions: Map<string, ActiveSession> }
    ).activeSessions;
    activeSessions.clear();

    await harness.service.stopSystemMonitor(CONNECTION_ID, "subscriber-1");
    assertTrue(
      harness.closes() > 0,
      "without a visible terminal the idle stop must be immediate, not lingering"
    );
  } finally {
    harness.dispose();
  }
})();

// Failure path: a stop for a connection that never had a runtime (monitor
// disabled, so nothing was ever started) must not leave a phantom linger timer.
await (async () => {
  const harness = createHarness();
  try {
    await harness.service.stopSystemMonitor(CONNECTION_ID, "subscriber-never-started");
    assertEqual(
      harness.service.getAllConnectionIds().length,
      0,
      "stopping a monitor that was never started must not schedule a linger"
    );
    assertEqual(harness.dials(), 0, "nothing should have been dialed");
  } finally {
    harness.dispose();
  }
})();
