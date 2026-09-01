import { describe, expect, test } from "vitest";
import { KeytarPasswordCache } from "./index";

interface FakeKeytar {
  module: {
    getPassword: (service: string, account: string) => Promise<string | null>;
    deletePassword: (service: string, account: string) => Promise<boolean>;
  };
  reads: string[];
  store: Map<string, string>;
}

const makeKeytar = (seed: Record<string, string> = {}): FakeKeytar => {
  const store = new Map<string, string>(Object.entries(seed));
  const reads: string[] = [];
  return {
    store,
    reads,
    module: {
      getPassword: async (service, account) => {
        const key = `${service}/${account}`;
        reads.push(key);
        return store.get(key) ?? null;
      },
      deletePassword: async (service, account) => store.delete(`${service}/${account}`)
    }
  };
};

describe("KeytarPasswordCache", () => {
  test("hits the keychain once and serves later reads from memory", async () => {
    const keytar = makeKeytar({ "NextShell/test-secret": "s3cret" });
    const cache = new KeytarPasswordCache("NextShell", "test-secret", {
      keytar: keytar.module
    });

    expect(await cache.recall()).toBe("s3cret");
    expect(await cache.recall()).toBe("s3cret");
    expect(keytar.reads).toEqual(["NextShell/test-secret"]);
  });

  test("memoizes a missing item so repeated misses do not re-prompt", async () => {
    const keytar = makeKeytar();
    const cache = new KeytarPasswordCache("NextShell", "test-secret", {
      keytar: keytar.module
    });

    expect(await cache.recall()).toBeUndefined();
    expect(await cache.recall()).toBeUndefined();
    expect(keytar.reads).toHaveLength(1);
  });

  test("collapses concurrent reads into a single keychain access", async () => {
    const keytar = makeKeytar({ "NextShell/device-key": "abc" });
    const cache = new KeytarPasswordCache("NextShell", "device-key", { keytar: keytar.module });

    const results = await Promise.all([cache.recall(), cache.recall(), cache.recall()]);

    expect(results).toEqual(["abc", "abc", "abc"]);
    expect(keytar.reads).toHaveLength(1);
  });

  test("does not memoize a failed read", async () => {
    let calls = 0;
    const cache = new KeytarPasswordCache("NextShell", "test-secret", {
      keytar: {
        getPassword: async () => {
          calls += 1;
          if (calls === 1) throw new Error("keychain denied");
          return "later";
        },
        deletePassword: async () => true
      }
    });

    await expect(cache.recall()).rejects.toThrow("keychain denied");
    expect(await cache.recall()).toBe("later");
  });

  test("deletes the keychain item and memoizes the empty result", async () => {
    const keytar = makeKeytar({ "NextShell/device-key": "old-key" });
    const cache = new KeytarPasswordCache("NextShell", "device-key", { keytar: keytar.module });

    expect(await cache.recall()).toBe("old-key");
    await cache.clear();

    expect(keytar.store.has("NextShell/device-key")).toBe(false);
    expect(await cache.recall()).toBeUndefined();
    expect(keytar.reads).toHaveLength(1);
  });
});
