import { describe, expect, it } from "vitest";

import {
  coerceFlagValue,
  parseCliInvocation,
  renderCallResult,
  renderToolList
} from "./cli-core.js";
import { STATIC_TOOLS } from "./tools.js";

describe("parseCliInvocation", () => {
  it("maps --key value pairs onto tool arguments with JSON coercion", () => {
    const invocation = parseCliInvocation([
      "exec",
      "--target",
      "web-1",
      "--command",
      "uptime",
      "--timeoutSec",
      "30"
    ]);
    expect(invocation).toEqual({
      kind: "call",
      tool: "exec",
      args: { target: "web-1", command: "uptime", timeoutSec: 30 },
      full: false,
      timeoutSec: null
    });
  });

  it("supports --key=value, boolean flags and --json merge", () => {
    const invocation = parseCliInvocation([
      "session_read",
      "--target=sess-1",
      "--stripAnsi",
      "--json",
      '{"lines": 50}'
    ]);
    expect(invocation).toEqual({
      kind: "call",
      tool: "session_read",
      args: { target: "sess-1", stripAnsi: true, lines: 50 },
      full: false,
      timeoutSec: null
    });
  });

  it("keeps reserved flags out of tool arguments", () => {
    const invocation = parseCliInvocation(["session_list", "--full", "--timeout", "9"]);
    expect(invocation).toEqual({
      kind: "call",
      tool: "session_list",
      args: {},
      full: true,
      timeoutSec: 9
    });
  });

  it("rejects positional arguments, bad --json and bad --timeout", () => {
    expect(parseCliInvocation(["exec", "uptime"]).kind).toBe("usage-error");
    expect(parseCliInvocation(["exec", "--json", "not json"]).kind).toBe("usage-error");
    expect(parseCliInvocation(["exec", "--json", "[1]"]).kind).toBe("usage-error");
    expect(parseCliInvocation(["exec", "--timeout", "-5"]).kind).toBe("usage-error");
  });

  it("recognises built-in commands and help", () => {
    expect(parseCliInvocation([])).toEqual({ kind: "help" });
    expect(parseCliInvocation(["help"])).toEqual({ kind: "help" });
    expect(parseCliInvocation(["status"])).toEqual({ kind: "status" });
    expect(parseCliInvocation(["tools", "--full"])).toEqual({ kind: "tools", full: true });
  });
});

describe("coerceFlagValue", () => {
  it("parses JSON-shaped values and leaves plain strings alone", () => {
    expect(coerceFlagValue("42")).toBe(42);
    expect(coerceFlagValue("true")).toBe(true);
    expect(coerceFlagValue('{"a":1}')).toEqual({ a: 1 });
    expect(coerceFlagValue("/var/log/nginx")).toBe("/var/log/nginx");
    expect(coerceFlagValue("web-1")).toBe("web-1");
  });
});

describe("renderCallResult", () => {
  it("extracts text content and flags isError", () => {
    const rendered = renderCallResult(
      { content: [{ type: "text", text: "line1" }, { type: "text", text: "line2" }], isError: true },
      false
    );
    expect(rendered).toEqual({ text: "line1\nline2", isError: true });
  });

  it("falls back to JSON when there is no text content or --full is set", () => {
    expect(renderCallResult({ structuredContent: { a: 1 } }, false).text).toContain('"a": 1');
    const full = renderCallResult({ content: [{ type: "text", text: "hi" }] }, true);
    expect(full.text).toContain('"type": "text"');
  });
});

describe("renderToolList", () => {
  it("prints one aligned line per tool", () => {
    const lines = renderToolList(STATIC_TOOLS, false).split("\n");
    expect(lines).toHaveLength(STATIC_TOOLS.length);
    expect(lines.some((line) => line.startsWith("session_list"))).toBe(true);
  });
});
