import { describe, expect, test } from "bun:test";
import { formatBytes, formatSpeed } from "./formatBytes";

describe("formatBytes", () => {
  test("formats zero and invalid input as 0 B", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(-5)).toBe("0 B");
    expect(formatBytes(Number.NaN)).toBe("0 B");
  });

  test("keeps bytes as whole numbers", () => {
    expect(formatBytes(1)).toBe("1 B");
    expect(formatBytes(512)).toBe("512 B");
  });

  test("scales into binary units with one decimal below 100", () => {
    expect(formatBytes(1024)).toBe("1 KB");
    expect(formatBytes(1536)).toBe("1.5 KB");
    expect(formatBytes(1024 * 1024)).toBe("1 MB");
    expect(formatBytes(5.5 * 1024 * 1024)).toBe("5.5 MB");
    expect(formatBytes(1024 * 1024 * 1024)).toBe("1 GB");
  });

  test("rounds to whole numbers at or above 100 in a unit", () => {
    expect(formatBytes(150 * 1024)).toBe("150 KB");
  });

  test("formatSpeed appends /s", () => {
    expect(formatSpeed(1024)).toBe("1 KB/s");
    expect(formatSpeed(0)).toBe("0 B/s");
  });
});
