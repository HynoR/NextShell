import { describe, expect, test } from "vitest";
import { resolveDeviceKey, type DeviceKeyDbAccess, type DeviceKeyStore } from "./index";

const FIXED_KEY = "a".repeat(64);
const generate = () => FIXED_KEY;

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

describe("resolveDeviceKey", () => {
  test("uses the database key without touching the keychain", async () => {
    const legacy = "b".repeat(64);
    const db = makeDb(legacy);
    let recalls = 0;
    const store: DeviceKeyStore = {
      isAvailable: () => true,
      recall: async () => {
        recalls += 1;
        return "c".repeat(64);
      },
      clear: async () => {}
    };

    const result = await resolveDeviceKey(store, db, generate);

    expect(result).toEqual({ deviceKeyHex: legacy, storedIn: "database" });
    expect(result.credentialsUnrecoverable).toBeUndefined();
    expect(recalls).toBe(0);
  });

  test("drains a legacy keychain key into the database and deletes it", async () => {
    const db = makeDb();
    let cleared = 0;
    const store: DeviceKeyStore = {
      isAvailable: () => true,
      recall: async () => FIXED_KEY,
      clear: async () => {
        cleared += 1;
      }
    };

    const result = await resolveDeviceKey(store, db, generate);

    expect(result).toEqual({ deviceKeyHex: FIXED_KEY, storedIn: "database" });
    expect(result.credentialsUnrecoverable).toBeUndefined();
    expect(db.value).toBe(FIXED_KEY);
    expect(cleared).toBe(1);
  });

  test("creates a database key when the legacy keychain read is denied", async () => {
    const db = makeDb();
    const store: DeviceKeyStore = {
      isAvailable: () => true,
      recall: async () => {
        throw new Error("User denied keychain access");
      },
      clear: async () => {}
    };

    const result = await resolveDeviceKey(store, db, generate);

    expect(result).toEqual({
      deviceKeyHex: FIXED_KEY,
      storedIn: "database",
      credentialsUnrecoverable: true
    });
    expect(db.value).toBe(FIXED_KEY);
  });

  test("does not flag credentials when the keychain is unavailable or empty", async () => {
    const empty: DeviceKeyStore = {
      isAvailable: () => true,
      recall: async () => undefined,
      clear: async () => {}
    };
    const unavailable: DeviceKeyStore = { ...empty, isAvailable: () => false };

    for (const store of [empty, unavailable]) {
      const result = await resolveDeviceKey(store, makeDb(), generate);
      expect(result).toEqual({ deviceKeyHex: FIXED_KEY, storedIn: "database" });
      expect(result.credentialsUnrecoverable).toBeUndefined();
    }
  });
});
