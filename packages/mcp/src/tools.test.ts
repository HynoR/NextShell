import { describe, expect, test } from "vitest";
import { INSTRUCTIONS, toolDefinitions } from "./tools";

const EXPECTED = [
  "host_list",
  "session_list",
  "session_read",
  "session_history",
  "command_search",
  "session_open",
  "command_save",
  "exec",
  "session_send_text",
  "session_send_signal",
  "session_focus",
  "session_close",
  "agent_status",
  "request_cancel"
];

describe("tool metadata generated from the app", () => {
  test("lists every tool the app serves, in the app's order", () => {
    expect(toolDefinitions.map((tool) => tool.name)).toEqual(EXPECTED);
  });

  test("every tool has a real description, a closed schema and documented fields", () => {
    for (const tool of toolDefinitions) {
      expect(tool.description.length, tool.name).toBeGreaterThan(60);
      expect(tool.title, tool.name).toBeTruthy();
      expect(tool.inputSchema.type, tool.name).toBe("object");
      expect(tool.inputSchema.additionalProperties, tool.name).toBe(false);
      const properties = tool.inputSchema.properties as Record<string, { description?: string }>;
      for (const [name, property] of Object.entries(properties)) {
        expect(property.description, `${tool.name}.${name}`).toBeTruthy();
      }
      for (const required of tool.inputSchema.required ?? []) {
        expect(properties, `${tool.name} requires ${required}`).toHaveProperty(required);
      }
    }
    expect(INSTRUCTIONS).toContain("user_action_required");
  });

  test("required fields, enums and bounds are in the schema", () => {
    const by = Object.fromEntries(toolDefinitions.map((tool) => [tool.name, tool]));
    expect(by.exec!.inputSchema.required).toEqual(["target", "command"]);
    expect(by.session_send_signal!.inputSchema.required).toEqual(["target", "signal"]);
    expect(by.request_cancel!.inputSchema.required).toEqual(["requestId"]);
    const props = (name: string) => by[name]!.inputSchema.properties as Record<string, any>;
    expect(props("session_send_signal").signal.enum).toEqual([
      "interrupt",
      "eof",
      "suspend",
      "quit"
    ]);
    expect(props("exec").timeoutSec).toMatchObject({ minimum: 1, maximum: 120 });
    expect(props("host_list").limit).toMatchObject({ minimum: 1, maximum: 200 });
    expect(props("session_read").waitFor.properties.timeoutSec).toMatchObject({ maximum: 60 });
    expect(props("session_send_text").text.maxLength).toBe(4096);
  });

  test("annotations separate reads from approval-gated changes", () => {
    const gated = [
      "session_open",
      "command_save",
      "exec",
      "session_send_text",
      "session_send_signal",
      "session_focus",
      "session_close"
    ];
    for (const tool of toolDefinitions) {
      expect(tool.annotations.destructiveHint, tool.name).toBe(gated.includes(tool.name));
      if (gated.includes(tool.name)) expect(tool.annotations.readOnlyHint, tool.name).toBe(false);
    }
    for (const name of ["session_list", "session_read", "agent_status", "host_list"]) {
      expect(toolDefinitions.find((tool) => tool.name === name)!.annotations.readOnlyHint).toBe(
        true
      );
    }
  });
});
