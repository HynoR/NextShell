import { describe, expect, test, vi } from "vitest";
import {
  createSessionTabShortcutHandlers,
  resolveSessionTabShortcut,
  shouldIgnoreTabShortcutEvent
} from "./useSessionTabShortcuts";
import type {
  SessionSwitcherState,
  SessionTabShortcutCallbacks,
  TabShortcutKeyDownEvent,
  TabShortcutKeyEvent
} from "./useSessionTabShortcuts";

const event = (overrides: Partial<TabShortcutKeyEvent>): TabShortcutKeyEvent => ({
  key: "",
  code: "",
  ctrlKey: false,
  metaKey: false,
  shiftKey: false,
  altKey: false,
  ...overrides
});

describe("resolveSessionTabShortcut", () => {
  test("Ctrl+Tab cycles MRU forward, Shift reverses", () => {
    expect(resolveSessionTabShortcut(event({ key: "Tab", ctrlKey: true }), true)).toEqual({
      type: "mru-cycle",
      direction: 1
    });
    expect(
      resolveSessionTabShortcut(event({ key: "Tab", ctrlKey: true, shiftKey: true }), false)
    ).toEqual({ type: "mru-cycle", direction: -1 });
    expect(resolveSessionTabShortcut(event({ key: "Tab" }), true)).toBeUndefined();
  });

  test("mod+digit jumps to nth tab, 9 means last", () => {
    expect(resolveSessionTabShortcut(event({ code: "Digit1", metaKey: true }), true)).toEqual({
      type: "index",
      index: 0
    });
    expect(resolveSessionTabShortcut(event({ code: "Digit9", ctrlKey: true }), false)).toEqual({
      type: "index",
      index: "last"
    });
    // 数字键在非 mac 上要求 Ctrl,meta 不算
    expect(
      resolveSessionTabShortcut(event({ code: "Digit1", metaKey: true }), false)
    ).toBeUndefined();
    // Digit0 不是合法标签序号
    expect(
      resolveSessionTabShortcut(event({ code: "Digit0", metaKey: true }), true)
    ).toBeUndefined();
  });

  test("mod+shift+brackets and Ctrl+Page keys move to adjacent tabs", () => {
    expect(
      resolveSessionTabShortcut(
        event({ code: "BracketRight", metaKey: true, shiftKey: true }),
        true
      )
    ).toEqual({ type: "adjacent", delta: 1 });
    expect(
      resolveSessionTabShortcut(
        event({ code: "BracketLeft", ctrlKey: true, shiftKey: true }),
        false
      )
    ).toEqual({ type: "adjacent", delta: -1 });
    expect(resolveSessionTabShortcut(event({ code: "PageDown", ctrlKey: true }), false)).toEqual({
      type: "adjacent",
      delta: 1
    });
    expect(resolveSessionTabShortcut(event({ code: "PageUp", ctrlKey: true }), true)).toEqual({
      type: "adjacent",
      delta: -1
    });
  });

  test("close is Cmd+W on mac and Ctrl+Shift+W elsewhere — bare Ctrl+W stays with the shell", () => {
    expect(resolveSessionTabShortcut(event({ code: "KeyW", metaKey: true }), true)).toEqual({
      type: "close"
    });
    expect(
      resolveSessionTabShortcut(event({ code: "KeyW", ctrlKey: true, shiftKey: true }), false)
    ).toEqual({ type: "close" });
    expect(
      resolveSessionTabShortcut(event({ code: "KeyW", ctrlKey: true }), false)
    ).toBeUndefined();
  });

  test("mod+shift+D duplicates; alt combos never match", () => {
    expect(
      resolveSessionTabShortcut(event({ code: "KeyD", metaKey: true, shiftKey: true }), true)
    ).toEqual({ type: "duplicate" });
    expect(
      resolveSessionTabShortcut(event({ code: "KeyD", ctrlKey: true, shiftKey: true }), false)
    ).toEqual({ type: "duplicate" });
    expect(
      resolveSessionTabShortcut(
        event({ code: "KeyD", metaKey: true, shiftKey: true, altKey: true }),
        true
      )
    ).toBeUndefined();
  });
});

describe("shouldIgnoreTabShortcutEvent", () => {
  test("IME composition swallows the whole shortcut layer", () => {
    expect(
      shouldIgnoreTabShortcutEvent({
        isComposing: true,
        targetIsEditable: false,
        targetInsideTerminal: false
      })
    ).toBe(true);
  });

  test("an editable target outside the terminal keeps the key for itself", () => {
    expect(
      shouldIgnoreTabShortcutEvent({
        isComposing: false,
        targetIsEditable: true,
        targetInsideTerminal: false
      })
    ).toBe(true);
  });

  test("xterm's own hidden textarea still gets the shortcuts", () => {
    expect(
      shouldIgnoreTabShortcutEvent({
        isComposing: false,
        targetIsEditable: true,
        targetInsideTerminal: true
      })
    ).toBe(false);
  });

  test("a non-editable target is never skipped", () => {
    expect(
      shouldIgnoreTabShortcutEvent({
        isComposing: false,
        targetIsEditable: false,
        targetInsideTerminal: false
      })
    ).toBe(false);
  });
});

const keyDownEvent = (overrides: Partial<TabShortcutKeyDownEvent>): TabShortcutKeyDownEvent => ({
  ...event({}),
  isComposing: false,
  targetIsEditable: false,
  targetInsideTerminal: false,
  preventDefault: vi.fn(),
  stopPropagation: vi.fn(),
  ...overrides
});

/**
 * 迷你工作区:按真实 store 的语义维护 MRU(每次激活都提到队首),这样断言可以
 * 直接看激活次数与 MRU 结果,而不只是看调用参数。
 */
const createHarness = (displayOrder: string[], initialMru: string[]) => {
  const activations: string[] = [];
  const switcherStates: Array<SessionSwitcherState | null> = [];
  let mru = [...initialMru];
  let activeSessionId = initialMru[0];

  const callbacks: SessionTabShortcutCallbacks = {
    getSessionIds: () => [...displayOrder],
    getMruSessionIds: () => [...mru],
    getActiveSessionId: () => activeSessionId,
    activateSession: (sessionId) => {
      activations.push(sessionId);
      activeSessionId = sessionId;
      mru = [sessionId, ...mru.filter((id) => id !== sessionId)];
    },
    closeActiveSession: () => undefined,
    duplicateActiveSession: () => undefined,
    onSwitcherStateChange: (state) => {
      switcherStates.push(state);
    }
  };

  const handlers = createSessionTabShortcutHandlers(() => callbacks, false);

  return {
    handlers,
    activations,
    switcherStates,
    get switcherState(): SessionSwitcherState | null {
      return switcherStates.at(-1) ?? null;
    },
    get mru(): string[] {
      return [...mru];
    },
    get activeSessionId(): string | undefined {
      return activeSessionId;
    },
    pressCtrlTab: (times = 1): void => {
      for (let index = 0; index < times; index += 1) {
        handlers.onKeyDown(keyDownEvent({ key: "Tab", ctrlKey: true }));
      }
    },
    pressCtrlShiftTab: (): void => {
      handlers.onKeyDown(keyDownEvent({ key: "Tab", ctrlKey: true, shiftKey: true }));
    },
    pressKey: (key: string): void => {
      handlers.onKeyDown(keyDownEvent({ key, ctrlKey: true }));
    },
    releaseCtrl: (): void => {
      handlers.onKeyUp({ key: "Control" });
    }
  };
};

describe("createSessionTabShortcutHandlers MRU cycling", () => {
  test("the first Ctrl+Tab opens the switcher on MRU[1] without activating anything", () => {
    const harness = createHarness(["a", "b", "c", "d"], ["a", "b", "c", "d"]);

    harness.pressCtrlTab(1);

    expect(harness.switcherState).toEqual({ ids: ["a", "b", "c", "d"], index: 1 });
    expect(harness.activations).toEqual([]);
    expect(harness.activeSessionId).toBe("a");
    expect(harness.mru).toEqual(["a", "b", "c", "d"]);
  });

  test("repeated presses walk the snapshot and wrap around, still without activating", () => {
    const harness = createHarness(["a", "b", "c"], ["a", "b", "c"]);

    harness.pressCtrlTab(4);

    expect(harness.switcherStates.map((state) => state?.index)).toEqual([1, 2, 0, 1]);
    expect(harness.activations).toEqual([]);
  });

  test("Shift reverses the walk", () => {
    const harness = createHarness(["a", "b", "c", "d"], ["a", "b", "c", "d"]);

    harness.pressCtrlShiftTab();
    expect(harness.switcherState).toEqual({ ids: ["a", "b", "c", "d"], index: 3 });

    harness.pressCtrlShiftTab();
    expect(harness.switcherState?.index).toBe(2);
  });

  test("arrow keys move the selection while the switcher is open", () => {
    const harness = createHarness(["a", "b", "c"], ["a", "b", "c"]);
    const arrowDown = keyDownEvent({ key: "ArrowDown" });

    // 面板没开时方向键必须原样落进终端
    harness.handlers.onKeyDown(arrowDown);
    expect(harness.switcherStates).toEqual([]);
    expect(arrowDown.preventDefault).not.toHaveBeenCalled();

    harness.pressCtrlTab(1);
    harness.pressKey("ArrowDown");
    expect(harness.switcherState?.index).toBe(2);
    harness.pressKey("ArrowUp");
    expect(harness.switcherState?.index).toBe(1);
    expect(harness.activations).toEqual([]);
  });

  test("Escape cancels the cycle without switching anything", () => {
    const harness = createHarness(["a", "b", "c"], ["a", "b", "c"]);

    harness.pressCtrlTab(2);
    const escape = keyDownEvent({ key: "Escape", ctrlKey: true });
    harness.handlers.onKeyDown(escape);

    expect(escape.preventDefault).toHaveBeenCalled();
    expect(harness.switcherState).toBeNull();
    expect(harness.activations).toEqual([]);
    expect(harness.activeSessionId).toBe("a");
    expect(harness.mru).toEqual(["a", "b", "c"]);

    // 取消之后下一次 Ctrl+Tab 重新起步,不接着走旧快照
    harness.pressCtrlTab(1);
    expect(harness.switcherState).toEqual({ ids: ["a", "b", "c"], index: 1 });
  });

  test("releasing Ctrl activates the selected tab exactly once and closes the switcher", () => {
    const harness = createHarness(["a", "b", "c", "d"], ["a", "b", "c", "d"]);

    harness.pressCtrlTab(3);
    harness.releaseCtrl();

    expect(harness.activations).toEqual(["d"]);
    expect(harness.switcherState).toBeNull();
    // Alt-Tab 语义:途经的 b、c 保持原有相对顺序
    expect(harness.mru).toEqual(["d", "a", "b", "c"]);

    // 同一次松手不会落定两遍
    harness.releaseCtrl();
    expect(harness.activations).toEqual(["d"]);
  });

  test("landing back on the current tab closes the switcher without a redundant activation", () => {
    const harness = createHarness(["a", "b", "c"], ["a", "b", "c"]);

    // 走满一圈回到 index 0,也就是当前会话
    harness.pressCtrlTab(3);
    expect(harness.switcherState?.index).toBe(0);
    harness.releaseCtrl();

    expect(harness.activations).toEqual([]);
    expect(harness.switcherState).toBeNull();
    expect(harness.mru).toEqual(["a", "b", "c"]);
  });

  test("a quick tap and release lands on MRU[1]", () => {
    const harness = createHarness(["a", "b", "c"], ["a", "b", "c"]);

    harness.pressCtrlTab(1);
    harness.releaseCtrl();

    expect(harness.activations).toEqual(["b"]);
    expect(harness.mru).toEqual(["b", "a", "c"]);
  });

  test("window blur cancels the cycle instead of switching behind the user's back", () => {
    const harness = createHarness(["a", "b", "c", "d"], ["a", "b", "c", "d"]);

    harness.pressCtrlTab(2);
    harness.handlers.onBlur();

    expect(harness.switcherState).toBeNull();
    expect(harness.activations).toEqual([]);
    expect(harness.activeSessionId).toBe("a");
    expect(harness.mru).toEqual(["a", "b", "c", "d"]);

    // 失焦已经结束这一轮:随后的 keyup 不该补一次切换
    harness.releaseCtrl();
    expect(harness.activations).toEqual([]);
  });

  test("releasing Ctrl without a cycle in progress does nothing", () => {
    const harness = createHarness(["a", "b", "c"], ["a", "b", "c"]);

    harness.releaseCtrl();
    harness.releaseCtrl();

    expect(harness.activations).toEqual([]);
    expect(harness.switcherStates).toEqual([]);
    expect(harness.mru).toEqual(["a", "b", "c"]);
  });

  test("an external close (mouse commit) makes the trailing Ctrl keyup a no-op", () => {
    const harness = createHarness(["a", "b", "c"], ["a", "b", "c"]);

    harness.pressCtrlTab(1);
    harness.handlers.onExternalClose();
    expect(harness.switcherState).toBeNull();

    harness.releaseCtrl();
    expect(harness.activations).toEqual([]);
  });

  test("other tab shortcuts are ignored while the switcher is open", () => {
    const harness = createHarness(["a", "b", "c"], ["a", "b", "c"]);

    harness.pressCtrlTab(1);
    const digit = keyDownEvent({ code: "Digit3", ctrlKey: true });
    harness.handlers.onKeyDown(digit);

    expect(harness.activations).toEqual([]);
    expect(harness.switcherState?.index).toBe(1);
    // 不处理的键也不吞:让它照常走下去
    expect(digit.preventDefault).not.toHaveBeenCalled();
  });

  test("non-cycle jumps activate immediately and promote the MRU", () => {
    const harness = createHarness(["a", "b", "c"], ["a", "b", "c"]);

    harness.handlers.onKeyDown(keyDownEvent({ code: "PageDown", ctrlKey: true }));
    expect(harness.activations).toEqual(["b"]);
    expect(harness.mru).toEqual(["b", "a", "c"]);

    harness.handlers.onKeyDown(keyDownEvent({ code: "Digit3", ctrlKey: true }));
    expect(harness.activations).toEqual(["b", "c"]);
    expect(harness.mru).toEqual(["c", "b", "a"]);

    // 这些动作不开切换器,松开 Ctrl 也就没有落定要提交
    harness.releaseCtrl();
    expect(harness.activations).toEqual(["b", "c"]);
    expect(harness.switcherStates).toEqual([]);
  });

  test("a single-tab workspace never opens the switcher", () => {
    const harness = createHarness(["a"], ["a"]);

    harness.pressCtrlTab(2);
    harness.releaseCtrl();

    expect(harness.switcherStates).toEqual([]);
    expect(harness.activations).toEqual([]);
  });

  test("composing and foreign editable targets skip the handler entirely", () => {
    const harness = createHarness(["a", "b", "c"], ["a", "b", "c"]);

    harness.handlers.onKeyDown(keyDownEvent({ key: "Tab", ctrlKey: true, isComposing: true }));
    harness.handlers.onKeyDown(keyDownEvent({ key: "Tab", ctrlKey: true, targetIsEditable: true }));
    expect(harness.switcherStates).toEqual([]);

    // xterm 自己的 helper textarea 是可编辑的,但快捷键照旧生效
    harness.handlers.onKeyDown(
      keyDownEvent({
        key: "Tab",
        ctrlKey: true,
        targetIsEditable: true,
        targetInsideTerminal: true
      })
    );
    expect(harness.switcherState).toEqual({ ids: ["a", "b", "c"], index: 1 });
  });
});
