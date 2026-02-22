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
  reason?: string
): SessionDescriptor => ({
  id,
  connectionId,
  title: `${connectionId}#1`,
  type: "terminal",
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
