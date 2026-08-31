import { describe, expect, test } from "vitest";
import type { ConnectionProfile } from "@nextshell/core";
import {
  affectsDetailConnection,
  detailConnectionId,
  resolveCancelIntent,
  shouldConfirmDiscard
} from "./editGuard";

const connection = (id: string): ConnectionProfile => ({ id }) as ConnectionProfile;

describe("shouldConfirmDiscard", () => {
  test("只在编辑态且有改动时打断", () => {
    expect(shouldConfirmDiscard(true, true)).toBe(true);
  });

  test("只读态、或编辑态但没改过，都直接放行", () => {
    expect(shouldConfirmDiscard(true, false)).toBe(false);
    expect(shouldConfirmDiscard(false, true)).toBe(false);
    expect(shouldConfirmDiscard(false, false)).toBe(false);
  });
});

describe("resolveCancelIntent", () => {
  test("编辑态按 Esc 只退回只读详情", () => {
    expect(resolveCancelIntent("keydown", true)).toBe("leaveEdit");
  });

  test("只读态按 Esc 关闭弹窗", () => {
    expect(resolveCancelIntent("keydown", false)).toBe("closeDialog");
  });

  test("点 X 或遮罩（click）即使在编辑态也是关闭弹窗", () => {
    expect(resolveCancelIntent("click", true)).toBe("closeDialog");
    expect(resolveCancelIntent("click", false)).toBe("closeDialog");
  });

  test("拿不到事件类型时退化为关闭弹窗，不吞掉用户的关闭意图", () => {
    expect(resolveCancelIntent(undefined, true)).toBe("closeDialog");
  });
});

describe("detailConnectionId", () => {
  test("只读态与编辑态都取到右栏挂着的那一条", () => {
    expect(detailConnectionId({ kind: "view", connection: connection("a") })).toBe("a");
    expect(detailConnectionId({ kind: "edit", connection: connection("a") })).toBe("a");
  });

  test("空态与「新建」没有连接 id", () => {
    expect(detailConnectionId({ kind: "empty" })).toBeUndefined();
    expect(detailConnectionId({ kind: "edit", connection: undefined })).toBeUndefined();
  });
});

describe("affectsDetailConnection", () => {
  // 拖 A 到别的目录时正在编辑 A：不先离开编辑态，保存就会把 folderId 写回旧目录。
  test("被改动的 id 里有右栏那一条时判中", () => {
    expect(affectsDetailConnection("a", ["a", "b"])).toBe(true);
  });

  // 删 A 不该把正在编辑的 B 一起关掉。
  test("动的是别人时判否", () => {
    expect(affectsDetailConnection("b", ["a"])).toBe(false);
  });

  test("右栏是空态或新建（没有 id）时永远判否", () => {
    expect(affectsDetailConnection(undefined, ["a"])).toBe(false);
    expect(affectsDetailConnection(undefined, [])).toBe(false);
  });

  test("空的改动集判否", () => {
    expect(affectsDetailConnection("a", [])).toBe(false);
  });
});
