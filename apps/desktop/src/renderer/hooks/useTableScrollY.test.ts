import { describe, expect, test } from "bun:test";
import { calculateTableScrollY } from "./useTableScrollY";

describe("calculateTableScrollY", () => {
  test("subtracts the table header from the available container height", () => {
    expect(calculateTableScrollY(320, 36)).toBe(284);
  });

  test("keeps a usable minimum body height in a collapsed panel", () => {
    expect(calculateTableScrollY(40, 36)).toBe(48);
  });
});
