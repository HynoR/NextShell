import { describe, expect, test } from "vitest";
import { formatClientCount, formatRunningState } from "./agent-section";

describe("formatClientCount", () => {
  test("reports the connected count when clients are present", () => {
    expect(formatClientCount(2)).toBe("2 个客户端已连接");
  });

  test("falls back to an empty-state message when no clients are connected", () => {
    expect(formatClientCount(0)).toBe("暂无客户端连接");
  });
});

describe("formatRunningState", () => {
  test("reports disabled regardless of the listening flag", () => {
    expect(formatRunningState(false, true)).toEqual({ status: "default", text: "未启用" });
    expect(formatRunningState(false, false)).toEqual({ status: "default", text: "未启用" });
  });

  test("reports listening when enabled and the endpoint is actually up", () => {
    expect(formatRunningState(true, true)).toEqual({ status: "success", text: "监听中" });
  });

  test("reports a stalled state when enabled but not listening (e.g. bind failure)", () => {
    expect(formatRunningState(true, false)).toEqual({ status: "error", text: "已启用但未监听" });
  });

  test("a halted endpoint is not reported as plain 监听中 — it still listens but refuses", () => {
    expect(formatRunningState(true, true, true)).toEqual({
      status: "warning",
      text: "监听中（调用已被切断）"
    });
    expect(formatRunningState(true, false, true)).toEqual({
      status: "error",
      text: "已启用但未监听"
    });
  });
});

describe("client config lines", () => {
  test("every snippet points at the loopback url for the configured port", async () => {
    const { buildEndpointUrl, buildClaudeAddCommand, buildMcpJson } =
      await import("./agent-section");
    expect(buildEndpointUrl(41777)).toBe("http://127.0.0.1:41777/mcp");
    expect(buildClaudeAddCommand(5000, "http")).toBe(
      "claude mcp add --transport http nextshell http://127.0.0.1:5000/mcp"
    );
    expect(JSON.parse(buildMcpJson(41777, "http"))).toEqual({
      mcpServers: { nextshell: { type: "http", url: "http://127.0.0.1:41777/mcp" } }
    });
  });
});

test("stdio config is versioned and includes the configured port", async () => {
  const { buildClaudeAddCommand, buildMcpJson } = await import("./agent-section");
  expect(JSON.parse(buildMcpJson(5000))).toEqual({
    mcpServers: {
      nextshell: {
        command: "npx",
        args: ["-y", "@nextshell/mcp@0.1.0", "--port", "5000"]
      }
    }
  });
  expect(buildClaudeAddCommand(5000)).toBe(
    "claude mcp add --transport stdio nextshell -- npx -y @nextshell/mcp@0.1.0 --port 5000"
  );
});
