import type { SessionDescriptor } from "@nextshell/core";
import { useWorkspaceStore } from "./useWorkspaceStore";

const assertEqual = <T>(actual: T, expected: T, message: string): void => {
  if (actual !== expected) {
    throw new Error(`${message}: expected "${String(expected)}", got "${String(actual)}"`);
  }
};

const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

const createSession = (
  id: string,
  connectionId: string,
  status: SessionDescriptor["status"],
  reason?: string,
  type: SessionDescriptor["type"] = "terminal"
): SessionDescriptor => ({
  id,
  connectionId,
  title: `${connectionId}#1`,
  type,
  status,
  createdAt: "2026-01-01T00:00:00.000Z",
  reconnectable: true,
  ...(reason !== undefined ? { reason } : {})
});

const resetStore = (): void => {
  useWorkspaceStore.setState({
    connections: [],
    sshKeys: [],
    proxies: [],
    sessions: [],
    activeConnectionId: undefined,
    activeSessionId: undefined,
    monitor: undefined,
    processSnapshots: {},
    networkSnapshots: {},
    networkRateHistory: {},
    bottomTab: "connections"
  });
};

(() => {
  resetStore();
  useWorkspaceStore.setState({
    sessions: [createSession("s1", "c1", "failed", "previous failure")],
    activeSessionId: "s1",
    activeConnectionId: "c1"
  });
  useWorkspaceStore.getState().setSessionStatus("s1", "connected");
  const session = useWorkspaceStore.getState().sessions[0];
  assert(session !== undefined, "session should exist");
  assertEqual(session?.reason, undefined, "reason should clear on non-failed status when reason omitted");
})();

(() => {
  resetStore();
  useWorkspaceStore.setState({
    sessions: [
      createSession("s1", "c1", "connected"),
      createSession("s2", "c2", "connected")
    ],
    activeSessionId: "s1",
    activeConnectionId: "c1"
  });
  useWorkspaceStore.getState().removeSession("s1");
  const state = useWorkspaceStore.getState();
  assertEqual(state.activeSessionId, "s2", "active session should switch to remaining session");
  assertEqual(state.activeConnectionId, "c2", "active connection should align with active session");
})();

(() => {
  resetStore();
  useWorkspaceStore.setState({
    sessions: [
      createSession("s1", "c1", "connected"),
      createSession("s2", "c2", "connected")
    ],
    activeSessionId: "s1",
    activeConnectionId: "c1",
    networkRateHistory: {
      "c1:eth0": [{ inMbps: 1, outMbps: 1, capturedAt: "1" }],
      "c2:eth0": [{ inMbps: 2, outMbps: 2, capturedAt: "2" }]
    }
  });
  useWorkspaceStore.getState().removeSessionsByConnection("c1");
  const state = useWorkspaceStore.getState();
  assertEqual(state.sessions.length, 1, "sessions on removed connection should be removed");
  assertEqual(state.sessions[0]?.id, "s2", "remaining session should be from other connection");
  assertEqual(state.activeSessionId, "s2", "active session should align after bulk remove");
  assertEqual(state.activeConnectionId, "c2", "active connection should align after bulk remove");
  assertEqual(state.networkRateHistory["c1:eth0"], undefined, "removed connection history should be pruned");
})();

(() => {
  resetStore();
  useWorkspaceStore.setState({
    sessions: [
      createSession("pm1", "c1", "connected", undefined, "processManager"),
      createSession("nm1", "c1", "connected", undefined, "networkMonitor")
    ],
    processSnapshots: {
      c1: { connectionId: "c1", capturedAt: "2026-01-01T00:00:00.000Z", processes: [] }
    },
    networkSnapshots: {
      c1: { connectionId: "c1", capturedAt: "2026-01-01T00:00:00.000Z", listeners: [], connections: [] }
    }
  });
  useWorkspaceStore.getState().removeSession("pm1");
  const state = useWorkspaceStore.getState();
  assertEqual(state.processSnapshots.c1, undefined, "closing process manager should clear process snapshot");
  assert(state.networkSnapshots.c1 !== undefined, "closing process manager should keep network snapshot");
})();

(() => {
  resetStore();
  useWorkspaceStore.setState({
    sessions: [
      createSession("pm1", "c1", "connected", undefined, "processManager"),
      createSession("nm1", "c1", "connected", undefined, "networkMonitor")
    ],
    processSnapshots: {
      c1: { connectionId: "c1", capturedAt: "2026-01-01T00:00:00.000Z", processes: [] }
    },
    networkSnapshots: {
      c1: { connectionId: "c1", capturedAt: "2026-01-01T00:00:00.000Z", listeners: [], connections: [] }
    }
  });
  useWorkspaceStore.getState().removeSession("nm1");
  const state = useWorkspaceStore.getState();
  assertEqual(state.networkSnapshots.c1, undefined, "closing network monitor should clear network snapshot");
  assert(state.processSnapshots.c1 !== undefined, "closing network monitor should keep process snapshot");
})();

(() => {
  resetStore();
  useWorkspaceStore.setState({
    sessions: [
      createSession("s1", "c1", "connected"),
      createSession("pm1", "c1", "connected", undefined, "processManager")
    ],
    processSnapshots: {
      c1: { connectionId: "c1", capturedAt: "2026-01-01T00:00:00.000Z", processes: [] }
    },
    networkSnapshots: {
      c1: { connectionId: "c1", capturedAt: "2026-01-01T00:00:00.000Z", listeners: [], connections: [] }
    }
  });
  useWorkspaceStore.getState().removeSession("s1");
  const state = useWorkspaceStore.getState();
  assert(state.processSnapshots.c1 !== undefined, "closing terminal should not clear process snapshot");
  assert(state.networkSnapshots.c1 !== undefined, "closing terminal should not clear network snapshot");
})();

(() => {
  resetStore();
  useWorkspaceStore.setState({
    sessions: [
      createSession("s1", "c1", "connected"),
      createSession("pm1", "c1", "connected", undefined, "processManager"),
      createSession("nm1", "c1", "connected", undefined, "networkMonitor"),
      createSession("s2", "c2", "connected")
    ],
    activeSessionId: "pm1",
    activeConnectionId: "c1",
    processSnapshots: {
      c1: { connectionId: "c1", capturedAt: "2026-01-01T00:00:00.000Z", processes: [] },
      c2: { connectionId: "c2", capturedAt: "2026-01-01T00:00:00.000Z", processes: [] }
    },
    networkSnapshots: {
      c1: { connectionId: "c1", capturedAt: "2026-01-01T00:00:00.000Z", listeners: [], connections: [] },
      c2: { connectionId: "c2", capturedAt: "2026-01-01T00:00:00.000Z", listeners: [], connections: [] }
    },
    networkRateHistory: {
      "c1:eth0": [{ inMbps: 1, outMbps: 1, capturedAt: "1" }],
      "c2:eth0": [{ inMbps: 2, outMbps: 2, capturedAt: "2" }]
    }
  });
  useWorkspaceStore.getState().removeSessionsByConnection("c1");
  const state = useWorkspaceStore.getState();
  assertEqual(state.processSnapshots.c1, undefined, "bulk remove should clear process snapshot");
  assertEqual(state.networkSnapshots.c1, undefined, "bulk remove should clear network snapshot");
  assert(state.processSnapshots.c2 !== undefined, "bulk remove should keep other process snapshots");
  assert(state.networkSnapshots.c2 !== undefined, "bulk remove should keep other network snapshots");
  assertEqual(state.activeSessionId, "s2", "bulk remove should advance active session");
  assertEqual(state.activeConnectionId, "c2", "bulk remove should advance active connection");
})();

(() => {
  resetStore();
  useWorkspaceStore.setState({
    sessions: [
      createSession("s1", "c1", "connected"),
      createSession("s2", "c2", "connected")
    ],
    activeSessionId: "s1",
    activeConnectionId: "c1"
  });
  useWorkspaceStore.getState().setActiveSession("s2");
  const state = useWorkspaceStore.getState();
  assertEqual(state.activeSessionId, "s2", "setActiveSession should switch active session");
  assertEqual(state.activeConnectionId, "c2", "setActiveSession should align active connection");
})();

(() => {
  resetStore();
  useWorkspaceStore.getState().setBottomTab("quick-transfer");
  const state = useWorkspaceStore.getState();
  assertEqual(state.bottomTab, "quick-transfer", "setBottomTab should accept quick-transfer");
})();
