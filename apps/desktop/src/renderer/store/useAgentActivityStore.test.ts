import { beforeEach, describe, expect, test } from "vitest";
import { useAgentActivityStore } from "./useAgentActivityStore";

const event = (
  id: string,
  status: "running" | "succeeded" | "failed" | "unsettled" = "running"
) => ({
  id,
  clientName: "codex",
  tool: "exec",
  status,
  summary: `exec: ${status}`,
  createdAt: `2026-08-04T00:00:${id.padStart(2, "0")}Z`
});

describe("useAgentActivityStore", () => {
  beforeEach(() =>
    useAgentActivityStore.setState({
      activities: [],
      unseen: 0,
      controlledSessions: {},
      halted: false
    })
  );

  test("counts new calls as unseen until marked, not their status updates", () => {
    const store = useAgentActivityStore.getState();
    store.applyEvent(event("1"));
    store.applyEvent(event("1", "succeeded"));
    store.applyEvent(event("2"));
    expect(useAgentActivityStore.getState().unseen).toBe(2);
    store.markSeen();
    expect(useAgentActivityStore.getState().unseen).toBe(0);
  });

  test("updates an in-flight activity in place", () => {
    useAgentActivityStore.getState().applyEvent(event("1"));
    useAgentActivityStore.getState().applyEvent(event("1", "succeeded"));
    expect(useAgentActivityStore.getState().activities).toEqual([event("1", "succeeded")]);
  });

  test("keeps running entries when clearing finished activity", () => {
    useAgentActivityStore.getState().applyEvent(event("1", "failed"));
    useAgentActivityStore.getState().applyEvent(event("2"));
    useAgentActivityStore.getState().clearFinished();
    expect(useAgentActivityStore.getState().activities.map((item) => item.id)).toEqual(["2"]);
  });

  test("tracks which sessions an agent is driving, and releases them", () => {
    const store = useAgentActivityStore.getState();
    store.applySessionControl({ sessionId: "s1", clientName: "codex", controlled: true });
    store.applySessionControl({ sessionId: "s2", clientName: null, controlled: true });
    expect(useAgentActivityStore.getState().controlledSessions).toEqual({
      s1: "codex",
      s2: null
    });

    store.applySessionControl({ sessionId: "s1", clientName: null, controlled: false });
    expect(useAgentActivityStore.getState().controlledSessions).toEqual({ s2: null });
  });

  test("halting drops every badge — nothing is being driven any more", () => {
    const store = useAgentActivityStore.getState();
    store.applySessionControl({ sessionId: "s1", clientName: "codex", controlled: true });
    store.setHalted(true);

    expect(useAgentActivityStore.getState().halted).toBe(true);
    expect(useAgentActivityStore.getState().controlledSessions).toEqual({});

    // Resuming does not resurrect stale badges.
    store.setHalted(false);
    expect(useAgentActivityStore.getState().controlledSessions).toEqual({});
  });

  test("agent access starts disabled and mirrors the endpoint status", () => {
    // Opt-in default: the sidebar panel must render nothing until a real
    // endpoint status reporting enabled=true arrives.
    expect(useAgentActivityStore.getState().enabled).toBe(false);
    useAgentActivityStore.getState().setEnabled(true);
    expect(useAgentActivityStore.getState().enabled).toBe(true);
    useAgentActivityStore.getState().setEnabled(false);
    expect(useAgentActivityStore.getState().enabled).toBe(false);
  });
});
