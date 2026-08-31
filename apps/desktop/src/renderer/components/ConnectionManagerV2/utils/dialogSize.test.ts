import { describe, expect, test } from "vitest";
import { clampDialogSize, fitDialogToViewport, resolveDialogResize } from "./dialogSize";

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
    // 2400×1400 saved on a desktop, reopened on a laptop. 高度扣的是 120 而不是 64——
    // 壳体上面还有 top:40 的偏移和标题栏，只扣 64 会把右下角的缩放手柄顶出屏幕。
    expect(fitDialogToViewport({ width: 2400, height: 1400 }, { width: 1440, height: 900 })).toEqual(
      { width: 1408, height: 780 }
    );
  });

  test("never collapses below a usable size on a tiny viewport", () => {
    expect(fitDialogToViewport({ width: 1180, height: 760 }, { width: 200, height: 200 })).toEqual({
      width: 320,
      height: 320
    });
  });
});

describe("resolveDialogResize", () => {
  const viewport = { width: 1920, height: 1080 };

  test("passes a drag that lands inside both the contract and the viewport", () => {
    expect(resolveDialogResize({ width: 1400, height: 820 }, viewport)).toEqual({
      width: 1400,
      height: 820
    });
  });

  test("stops at the persistable minimum when dragged smaller", () => {
    // 契约下限就是 900×560；再小写回偏好会被 Zod 拒收。
    expect(resolveDialogResize({ width: 300, height: 100 }, viewport)).toEqual({
      width: 900,
      height: 560
    });
  });

  test("stops at the viewport minus the margin when dragged bigger", () => {
    // 高度留 120：拖到最大时右下角的手柄必须还在屏幕里，否则拖大了就缩不回来。
    expect(resolveDialogResize({ width: 5000, height: 5000 }, viewport)).toEqual({
      width: 1888,
      height: 960
    });
  });

  // 视口比契约下限还矮时,旧口径会返回 560 并把它命令式写进 DOM:显示口径只认 640-120=520，
  // 于是那 40px 连同右下角的手柄一起留在屏幕外面，再也拖不回来。
  test("在矮视口里跟着视口走，不再留下一个顶出屏幕的高度", () => {
    const dragged = resolveDialogResize({ width: 1200, height: 800 }, { width: 1400, height: 640 });
    expect(dragged.height).toBeLessThanOrEqual(640 - 120);
    expect(dragged).toEqual({ width: 1200, height: 520 });
  });

  test("视口小到连最小可视尺寸都放不下时守住 320", () => {
    expect(resolveDialogResize({ width: 1200, height: 800 }, { width: 300, height: 300 })).toEqual({
      width: 320,
      height: 320
    });
  });

  // R5 的核心:写入口径与显示口径必须是同一套函数，拖出来的值再过一次 fit 不能变。
  test("与 fitDialogToViewport 同口径：拖出来的尺寸不会在松手后跳一下", () => {
    for (const viewport of [
      { width: 1920, height: 1080 },
      { width: 1400, height: 640 },
      { width: 700, height: 400 },
      { width: 300, height: 300 }
    ]) {
      const dragged = resolveDialogResize({ width: 1200, height: 800 }, viewport);
      expect(fitDialogToViewport(dragged, viewport)).toEqual(dragged);
    }
  });

  test("ignores an unreadable viewport instead of producing NaN", () => {
    expect(
      resolveDialogResize({ width: 1200, height: 800 }, { width: Number.NaN, height: Number.NaN })
    ).toEqual({ width: 1200, height: 800 });
  });

  // 拖出来的值可能低于契约下限(视口太小)，落偏好前必须再夹一次，否则主进程 Zod 直接拒收。
  test("落偏好前再夹一次就能回到契约范围内", () => {
    const dragged = resolveDialogResize({ width: 1200, height: 800 }, { width: 700, height: 400 });
    expect(clampDialogSize("width", dragged.width)).toBe(900);
    expect(clampDialogSize("height", dragged.height)).toBe(560);
  });
});
