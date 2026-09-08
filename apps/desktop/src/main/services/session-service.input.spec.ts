import { describe, expect, test } from "vitest";

import { isControlOnlyInput } from "./session-service";

const ESC = "\u001b";

describe("isControlOnlyInput", () => {
  test("terminal protocol replies are not keystrokes", () => {
    expect(isControlOnlyInput(`${ESC}[24;80R`)).toBe(true); // CPR
    expect(isControlOnlyInput(`${ESC}[?62;22c`)).toBe(true); // DA1
    expect(isControlOnlyInput(`${ESC}[I`)).toBe(true); // focus in
    expect(isControlOnlyInput(`${ESC}[O${ESC}[I`)).toBe(true); // focus flip
    expect(isControlOnlyInput(`${ESC}]10;rgb:ffff/ffff/ffff${ESC}\\`)).toBe(true); // OSC reply
    expect(isControlOnlyInput(`${ESC}[A`)).toBe(true); // arrow key
    expect(isControlOnlyInput(`${ESC}OA`)).toBe(true); // SS3 arrow key
  });

  test("anything printable counts as the human typing", () => {
    expect(isControlOnlyInput("ls")).toBe(false);
    expect(isControlOnlyInput("\r")).toBe(false);
    expect(isControlOnlyInput(`${ESC}[Ils`)).toBe(false);
    expect(isControlOnlyInput("")).toBe(false);
  });
});
