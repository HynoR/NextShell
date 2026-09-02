import { describe, expect, test } from "vitest";
import type { ScopedCommandItem } from "@nextshell/core";
import {
  DEFAULT_COMMAND_FOLDER,
  folderOf,
  getCommandsForTab,
  getCommandTabs
} from "./useCommandStore";

const command = (overrides: Partial<ScopedCommandItem> = {}): ScopedCommandItem => ({
  id: "1",
  name: "list",
  group: "  ",
  command: "ls",
  createdAt: "",
  updatedAt: "",
  scope: "local",
  ...overrides
});

describe("folderOf", () => {
  test("a whitespace-only group falls back to the default folder", () => {
    expect(folderOf(command())).toBe(DEFAULT_COMMAND_FOLDER);
  });

  test("getCommandTabs and getCommandsForTab agree with folderOf for a blank group", () => {
    const cmd = command();
    const tabs = getCommandTabs([cmd], [], []);
    const defaultTab = tabs.find((tab) => tab.group === DEFAULT_COMMAND_FOLDER);
    expect(defaultTab).toBeDefined();
    expect(getCommandsForTab([cmd], defaultTab)).toEqual([cmd]);
  });
});
