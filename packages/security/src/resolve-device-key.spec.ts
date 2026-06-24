import { describe, expect, test } from "bun:test";
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
    },
    clearLegacy: () => {
      value = undefined;
    },
  };
};

describe("resolveDeviceKey", () => {
  test("migrates a legacy plaintext DB key into the keychain and purges the DB", async () => {
    const legacy = "b".repeat(64);
    const db = makeDb(legacy);
    let remembered: string | undefined;
    const store: DeviceKeyStore = {
      isAvailable: () => true,
      recall: async () => undefined,
      remember: async (key) => {
        remembered = key;
      },
    };

    const result = await resolveDeviceKey(store, db, generate);

    expect(result.deviceKeyHex).toBe(legacy); // reuse existing key so credentials still decrypt
    expect(result.storedIn).toBe("keychain");
    expect(result.migratedFromDatabase).toBe(true);
    expect(remembered).toBe(legacy); // now in keychain
    expect(db.value).toBeUndefined(); // plaintext purged
  });

  test("uses the keychain key when present and clears stale DB plaintext", async () => {
    const keychainKey = "c".repeat(64);
    const staleDb = "d".repeat(64);
    const db = makeDb(staleDb);
    const store: DeviceKeyStore = {
      isAvailable: () => true,
      recall: async () => keychainKey,
      remember: async () => {
        throw new Error("should not be called");
      },
    };

    const result = await resolveDeviceKey(store, db, generate);

    expect(result.deviceKeyHex).toBe(keychainKey);
    expect(result.storedIn).toBe("keychain");
    expect(result.migratedFromDatabase).toBe(false);
    expect(db.value).toBeUndefined();
  });

  test("mints a fresh key in the keychain on a clean install", async () => {
    const db = makeDb(undefined);
    let remembered: string | undefined;
    const store: DeviceKeyStore = {
      isAvailable: () => true,
      recall: async () => undefined,
      remember: async (key) => {
        remembered = key;
      },
    };

    const result = await resolveDeviceKey(store, db, generate);

    expect(result.deviceKeyHex).toBe(FIXED_KEY);
    expect(result.storedIn).toBe("keychain");
    expect(result.migratedFromDatabase).toBe(false);
    expect(remembered).toBe(FIXED_KEY);
    expect(db.value).toBeUndefined(); // never written to DB
  });

  test("degrades to DB storage when the keychain is unavailable", async () => {
    const db = makeDb(undefined);
    const store: DeviceKeyStore = {
      isAvailable: () => false,
      recall: async () => undefined,
      remember: async () => {
        throw new Error("should not be called");
      },
    };

    const result = await resolveDeviceKey(store, db, generate);

    expect(result.deviceKeyHex).toBe(FIXED_KEY);
    expect(result.storedIn).toBe("database");
    expect(db.value).toBe(FIXED_KEY); // persisted to DB as the only backing store
  });

  test("falls back to the existing DB key when a runtime keychain call throws", async () => {
    const legacy = "e".repeat(64);
    const db = makeDb(legacy);
    const store: DeviceKeyStore = {
      isAvailable: () => true,
      recall: async () => {
        throw new Error("keychain access denied");
      },
      remember: async () => {
        throw new Error("keychain access denied");
      },
    };

    const result = await resolveDeviceKey(store, db, generate);

    expect(result.deviceKeyHex).toBe(legacy); // existing credentials still decrypt
    expect(result.storedIn).toBe("database");
    expect(db.value).toBe(legacy); // legacy key left intact, not cleared
  });
});
