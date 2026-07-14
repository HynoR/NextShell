import { describe, expect, test } from "bun:test";
import {
  consumeTerminalQueryReplyChunk,
  createTerminalQueryReplyFilterState,
  installTerminalQueryCompatibilityGuards
} from "./terminalControlSequenceCompat";

const ESC = "\u001b";
const ST = `${ESC}\\`;

describe("terminal control sequence compatibility", () => {
  test("strips concatenated xterm query replies before they can be written upstream", () => {
    const state = createTerminalQueryReplyFilterState();
    const chunk = [
      `${ESC}[>0;276;0c`,
      `${ESC}]10;rgb:d8d8/eaea/ffff${ST}`,
      `${ESC}]11;rgb:0000/0000/0000${ST}`,
      `${ESC}]12;rgb:ffff/ffff/ffff${ST}`,
      `${ESC}[?12;2$y`,
      `${ESC}P1$r2 q${ST}`
    ].join("");

    const result = consumeTerminalQueryReplyChunk(state, chunk);

    expect(result.text).toBe("");
    expect(result.state.pending).toBe("");
  });

  test("keeps ordinary user input untouched", () => {
    const state = createTerminalQueryReplyFilterState();

    const result = consumeTerminalQueryReplyChunk(state, "ls -la\r");

    expect(result.text).toBe("ls -la\r");
    expect(result.state.pending).toBe("");
  });

  test("handles partial query replies across chunks", () => {
    const state = createTerminalQueryReplyFilterState();

    const first = consumeTerminalQueryReplyChunk(state, `${ESC}]10;rgb:d8d8/eaea`);
    expect(first.text).toBe("");
    expect(first.state.pending).toBe(`${ESC}]10;rgb:d8d8/eaea`);

    const second = consumeTerminalQueryReplyChunk(first.state, `/ffff${ST}pwd\r`);
    expect(second.text).toBe("pwd\r");
    expect(second.state.pending).toBe("");
  });

  test("registers guards that only suppress query sequences", () => {
    const csiHandlers = new Map<
      string,
      (params: (number | number[])[]) => boolean | Promise<boolean>
    >();
    const oscHandlers = new Map<number, (data: string) => boolean | Promise<boolean>>();
    const dcsHandlers = new Map<
      string,
      (data: string, params: (number | number[])[]) => boolean | Promise<boolean>
    >();
    const suppressed: string[] = [];

    const disposer = installTerminalQueryCompatibilityGuards(
      {
        parser: {
          registerCsiHandler(id, callback) {
            csiHandlers.set(`${id.prefix ?? ""}|${id.intermediates ?? ""}|${id.final}`, callback);
            return { dispose() {} };
          },
          registerOscHandler(ident, callback) {
            oscHandlers.set(ident, callback);
            return { dispose() {} };
          },
          registerDcsHandler(id, callback) {
            dcsHandlers.set(`${id.prefix ?? ""}|${id.intermediates ?? ""}|${id.final}`, callback);
            return { dispose() {} };
          }
        }
      },
      {
        isEnabled: () => true,
        onSuppressed: (kind) => suppressed.push(kind)
      }
    );

    const secondaryDa = csiHandlers.get(">||c");
    const modeRequest = csiHandlers.get("|$|p");
    const privateModeRequest = csiHandlers.get("?|$|p");
    const osc10 = oscHandlers.get(10);
    const osc11 = oscHandlers.get(11);
    const osc12 = oscHandlers.get(12);
    const dcsQ = dcsHandlers.get("|$|q");

    expect(secondaryDa?.([])).toBe(true);
    expect(modeRequest?.([12])).toBe(true);
    expect(privateModeRequest?.([12])).toBe(true);
    expect(osc10?.("?")).toBe(true);
    expect(osc11?.("rgb:d8d8/eaea/ffff")).toBe(false);
    expect(osc12?.("?")).toBe(true);
    expect(dcsQ?.(" q", [])).toBe(true);
    expect(suppressed).toEqual([
      "device-attributes",
      "ansi-mode-request",
      "private-mode-request",
      "osc-color-query",
      "osc-color-query",
      "status-string-request"
    ]);

    disposer.dispose();
  });
});
