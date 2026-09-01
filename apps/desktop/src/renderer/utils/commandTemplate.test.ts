import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import {
  clearParamsFromStorage,
  loadParamsFromStorage,
  saveParamsToStorage,
  substituteTemplate
} from "./commandTemplate";

const storageKey = "command-template-test";

beforeEach(() => {
  const values = new Map<string, string>();
  vi.stubGlobal("localStorage", {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
    removeItem: (key: string) => values.delete(key)
  });
});

afterEach(() => {
  clearParamsFromStorage(storageKey);
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("command parameter history", () => {
  test("keeps the newest ten values and reads legacy single values", () => {
    localStorage.setItem(
      "nextshell:cmdParams:" + storageKey,
      JSON.stringify({ host: "legacy", port: ["22", "2222"] })
    );

    expect(loadParamsFromStorage(storageKey)).toEqual({
      host: ["legacy"],
      port: ["22", "2222"]
    });

    saveParamsToStorage(storageKey, { host: "new-host" });
    for (let index = 0; index < 11; index += 1) {
      saveParamsToStorage(storageKey, { port: String(index) });
    }
    expect(loadParamsFromStorage(storageKey).host).toEqual(["new-host", "legacy"]);
    expect(loadParamsFromStorage(storageKey).port).toEqual(
      [10, 9, 8, 7, 6, 5, 4, 3, 2, 1].map(String)
    );
  });

  test("treats malformed storage and unavailable writes as harmless", () => {
    localStorage.setItem("nextshell:cmdParams:" + storageKey, "{");
    expect(loadParamsFromStorage(storageKey)).toEqual({});

    vi.spyOn(globalThis.localStorage, "setItem").mockImplementation(() => {
      throw new Error("storage unavailable");
    });
    expect(() => saveParamsToStorage(storageKey, { value: "x" })).not.toThrow();
    expect(substituteTemplate("echo [#value]", { value: "x" })).toBe("echo x");
  });
});
