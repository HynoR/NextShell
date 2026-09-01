import { describe, expect, test, vi } from "vitest";
import { DeviceKeyProvider } from "./device-key-provider";
import type { DeviceKeyDbAccess, DeviceKeyStore } from "../../../../../packages/security/src/index";

vi.mock("../logger", () => ({
  logger: { info: () => {}, warn: () => {}, error: () => {}, debug: () => {} }
}));

const KEY = "a".repeat(64);

const makeDb = (initial?: string): DeviceKeyDbAccess & { value: string | undefined } => {
  let value = initial;
  return {
    get value() {
      return value;
    },
    getLegacy: () => value,
    saveLegacy: (key) => {
      value = key;
    }
  };
};

describe("DeviceKeyProvider", () => {
  test("does not touch the keychain until the key is asked for", async () => {
    const db = makeDb(KEY);
    let recalls = 0;
    const store: DeviceKeyStore = {
      isAvailable: () => true,
      recall: async () => {
        recalls += 1;
        return undefined;
      },
      clear: async () => {}
    };
    const provider = new DeviceKeyProvider({ store, db });

    expect(recalls).toBe(0);
    expect(provider.getStatus()).toBe("unresolved");
    expect((await provider.get()).toString("hex")).toBe(KEY);
    expect(recalls).toBe(0);
    expect(provider.getStatus()).toBe("database");
  });

  test("drains a legacy keychain key into the database once", async () => {
    const db = makeDb();
    let recalls = 0;
    let clears = 0;
    const store: DeviceKeyStore = {
      isAvailable: () => true,
      recall: async () => {
        recalls += 1;
        return KEY;
      },
      clear: async () => {
        clears += 1;
      }
    };
    const provider = new DeviceKeyProvider({ store, db });

    await provider.get();
    await provider.get();

    expect(db.value).toBe(KEY);
    expect(recalls).toBe(1);
    expect(clears).toBe(1);
    expect(provider.getStatus()).toBe("database");
  });

  test("creates a database key when the legacy keychain read is denied", async () => {
    const db = makeDb();
    let recalls = 0;
    const store: DeviceKeyStore = {
      isAvailable: () => true,
      recall: async () => {
        recalls += 1;
        throw new Error("User denied keychain access");
      },
      clear: async () => {}
    };
    const provider = new DeviceKeyProvider({ store, db });

    expect((await provider.get()).toString("hex")).toMatch(/^[0-9a-f]{64}$/);
    await provider.get();

    expect(db.value).toMatch(/^[0-9a-f]{64}$/);
    expect(recalls).toBe(1);
    expect(provider.getStatus()).toBe("database");
  });
});
