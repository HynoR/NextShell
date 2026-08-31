import { describe, expect, it } from "vitest";
import { resolveNameColumnWidth } from "./ConnectionTable";
import { DEFAULT_CONNECTION_COLUMNS } from "../types";

describe("resolveNameColumnWidth", () => {
  it("名称列吃满容器剩余宽度(默认五列)", () => {
    // 默认列的定宽:address 200 + username 110 + auth 150 + notes 170 = 630,勾选列 36,滚动条 8。
    expect(resolveNameColumnWidth(1200, DEFAULT_CONNECTION_COLUMNS)).toBe(1200 - 630 - 36 - 8);
  });

  it("容器太窄时保底 220,宁可横向滚动也不再塌缩", () => {
    expect(resolveNameColumnWidth(600, DEFAULT_CONNECTION_COLUMNS)).toBe(220);
    // 量不到宽度的首帧(0)同样落在保底值上,名称列永远不会是 0 宽。
    expect(resolveNameColumnWidth(0, DEFAULT_CONNECTION_COLUMNS)).toBe(220);
  });

  it("列显隐变化会重新分配剩余宽度", () => {
    const wide = resolveNameColumnWidth(1200, ["name", "address"]);
    const narrow = resolveNameColumnWidth(1200, DEFAULT_CONNECTION_COLUMNS);
    expect(wide).toBeGreaterThan(narrow);
    expect(wide).toBe(1200 - 200 - 36 - 8);
  });
});
