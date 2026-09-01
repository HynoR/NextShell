import { describe, expect, test } from "vitest";
import { KeytarPasswordCache } from "./index";

interface FakeKeytar {
  module: {
    setPassword: (service: string, account: string, password: string) => Promise<void>;
    getPassword: (service: string, account: string) => Promise<string | null>;
    deletePassword: (service: string, account: string) => Promise<boolean>;
  };
  reads: string[];
  writes: string[];
  store: Map<string, string>;
}

const makeKeytar = (seed: Record<string, string> = {}): FakeKeytar => {
  const store = new Map<string, string>(Object.entries(seed));
  const reads: string[] = [];
  const writes: string[] = [];
  return {
    store,
    reads,
    writes,
    module: {
      getPassword: async (service, account) => {
        const key = `${service}/${account}`;
        reads.push(key);
        return store.get(key) ?? null;
      },
      setPassword: async (service, account, password) => {
        const key = `${service}/${account}`;
        writes.push(key);
        store.set(key, password);
      },
      deletePassword: async (service, account) => {
        return store.delete(`${service}/${account}`);
      }
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
        setPassword: async () => {},
        deletePassword: async () => true
      }
    });

    await expect(cache.recall()).rejects.toThrow("keychain denied");
    expect(await cache.recall()).toBe("later");
  });

  test("remember and clear keep the memo in sync without re-reading", async () => {
    const keytar = makeKeytar();
    const cache = new KeytarPasswordCache("NextShell", "test-secret", {
      keytar: keytar.module
    });

    await cache.remember("new-password");
    expect(await cache.recall()).toBe("new-password");
    expect(keytar.reads).toHaveLength(0);

    await cache.clear();
    expect(await cache.recall()).toBeUndefined();
    expect(keytar.reads).toHaveLength(0);
  });

  test("adopts the fallback service item and copies it into the primary", async () => {
    const keytar = makeKeytar({ "NextShell/device-key": "shared-key" });
    const cache = new KeytarPasswordCache("NextShell (Dev)", "device-key", {
      keytar: keytar.module,
      fallbackService: "NextShell"
    });

    expect(await cache.recall()).toBe("shared-key");
    // Copied over, so the next launch only touches the dev-owned item.
    expect(keytar.store.get("NextShell (Dev)/device-key")).toBe("shared-key");
    expect(keytar.writes).toEqual(["NextShell (Dev)/device-key"]);
  });

  test("prefers the primary item over the fallback", async () => {
    const keytar = makeKeytar({
      "NextShell (Dev)/device-key": "dev-key",
      "NextShell/device-key": "shared-key"
    });
    const cache = new KeytarPasswordCache("NextShell (Dev)", "device-key", {
      keytar: keytar.module,
      fallbackService: "NextShell"
    });

    expect(await cache.recall()).toBe("dev-key");
    expect(keytar.reads).toEqual(["NextShell (Dev)/device-key"]);
  });

  test("invalidate forces the next recall back to the keychain", async () => {
    const keytar = makeKeytar({ "NextShell/test-secret": "one" });
    const cache = new KeytarPasswordCache("NextShell", "test-secret", {
      keytar: keytar.module
    });

    expect(await cache.recall()).toBe("one");
    keytar.store.set("NextShell/test-secret", "two");
    expect(await cache.recall()).toBe("one");

    cache.invalidate();
    expect(await cache.recall()).toBe("two");
    expect(keytar.reads).toHaveLength(2);
  });
});
