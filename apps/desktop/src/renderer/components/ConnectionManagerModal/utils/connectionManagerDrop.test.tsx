import { describe, expect, test } from "bun:test";
import {
  canAcceptConnectionManagerExternalDrop,
  getConnectionManagerDropPathWarning
} from "./connectionManagerDrop";

describe("connection manager external drop guards", () => {
  test("only accepts external file drops while the connections tab is open", () => {
    expect(
      canAcceptConnectionManagerExternalDrop({
        open: true,
        activeTab: "connections",
        importingPreview: false
      })
    ).toBe(true);

    expect(
      canAcceptConnectionManagerExternalDrop({
        open: false,
        activeTab: "connections",
        importingPreview: false
      })
    ).toBe(false);

    expect(
      canAcceptConnectionManagerExternalDrop({
        open: true,
        activeTab: "keys",
        importingPreview: false
      })
    ).toBe(false);

    expect(
      canAcceptConnectionManagerExternalDrop({
        open: true,
        activeTab: "connections",
        importingPreview: true
      })
    ).toBe(false);
  });

  test("returns the correct warning message for unreadable or invalid drops", () => {
    expect(getConnectionManagerDropPathWarning({ allPathsEmpty: true })).toBe(
      "无法读取拖入文件的路径，请尝试使用导入按钮选择文件"
    );
    expect(getConnectionManagerDropPathWarning({ allPathsEmpty: false })).toBe(
      "当前仅支持拖入文件"
    );
  });
});
