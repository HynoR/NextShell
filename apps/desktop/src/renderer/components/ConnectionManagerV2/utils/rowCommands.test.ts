import { describe, expect, test } from "vitest";
import type { ConnectionProfile } from "@nextshell/core";
import { describeAffected, planRowCommands } from "./rowCommands";

const conn = (id: string, name: string): ConnectionProfile =>
  ({ id, name }) as ConnectionProfile;

describe("planRowCommands", () => {
  test("offers the single-row commands with edit first", () => {
    const plan = planRowCommands({ targetId: "a", selectedIds: [] });
    expect(plan.isBulk).toBe(false);
    expect(plan.affectedIds).toEqual(["a"]);
    expect(plan.commands[0]).toBe("edit");
  });

  test("switches to bulk commands when right-clicking inside a multi-selection", () => {
    const plan = planRowCommands({ targetId: "a", selectedIds: ["a", "b", "c"] });
    expect(plan.isBulk).toBe(true);
    expect(plan.affectedIds).toEqual(["a", "b", "c"]);
    expect(plan.commands).toEqual(["bindAuth", "copyToScope", "export", "delete"]);
  });

  test("targets only the clicked row when it sits outside the selection", () => {
    // Otherwise right-clicking an unselected row would silently act on the selection.
    const plan = planRowCommands({ targetId: "z", selectedIds: ["a", "b"] });
    expect(plan.affectedIds).toEqual(["z"]);
    expect(plan.isBulk).toBe(false);
  });

  test("treats a single-row selection as single, not bulk", () => {
    const plan = planRowCommands({ targetId: "a", selectedIds: ["a"] });
    expect(plan.isBulk).toBe(false);
    expect(plan.commands).toContain("rename");
  });
});

describe("describeAffected", () => {
  const connections = [conn("a", "prod-db"), conn("b", "gateway")];

  test("names a single connection", () => {
    expect(describeAffected(["a"], connections)).toBe("「prod-db」");
  });

  test("counts a bulk selection", () => {
    expect(describeAffected(["a", "b"], connections)).toBe("2 个连接");
  });

  test("degrades gracefully when the connection is already gone", () => {
    expect(describeAffected(["missing"], connections)).toBe("该连接");
  });
});
