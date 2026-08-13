import { describe, expect, test } from "vitest";
import { clampDialogSize, fitDialogToViewport } from "./dialogSize";

describe("clampDialogSize", () => {
  test("keeps a value inside the allowed range", () => {
    expect(clampDialogSize("width", 1180)).toBe(1180);
    expect(clampDialogSize("folderColumn", 240)).toBe(240);
  });

  test("clamps to the bounds instead of rejecting", () => {
    expect(clampDialogSize("width", 100)).toBe(900);
    expect(clampDialogSize("width", 99999)).toBe(3840);
    expect(clampDialogSize("detailColumn", 10)).toBe(260);
  });

  test("rounds fractional drag positions", () => {
    expect(clampDialogSize("height", 700.6)).toBe(701);
  });

  test("falls back to the minimum for a non-finite value", () => {
    expect(clampDialogSize("height", Number.NaN)).toBe(560);
    expect(clampDialogSize("height", Number.POSITIVE_INFINITY)).toBe(2160);
  });
});

describe("fitDialogToViewport", () => {
  test("leaves a size that fits alone", () => {
    expect(fitDialogToViewport({ width: 1180, height: 760 }, { width: 1920, height: 1080 })).toEqual(
      { width: 1180, height: 760 }
    );
  });

  test("shrinks a size remembered from a bigger screen", () => {
    // 2400×1400 saved on a desktop, reopened on a laptop.
    expect(fitDialogToViewport({ width: 2400, height: 1400 }, { width: 1440, height: 900 })).toEqual(
      { width: 1408, height: 836 }
    );
  });

  test("never collapses below a usable size on a tiny viewport", () => {
    expect(fitDialogToViewport({ width: 1180, height: 760 }, { width: 200, height: 200 })).toEqual({
      width: 320,
      height: 320
    });
  });
});
