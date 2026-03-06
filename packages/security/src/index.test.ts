import {
  __resetScryptImplForTesting,
  __setScryptImplForTesting,
  clearDerivedKeyCache,
  createMasterKeyMeta,
  decryptBackupPayload,
  deriveKey,
  encryptBackupPayload,
  verifyMasterPassword
} from "./index";

const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

const assertBufferEquals = (left: Buffer, right: Buffer, message: string): void => {
  assert(left.equals(right), message);
};

const createFakeDerivedKey = (fill: number): Buffer => Buffer.alloc(32, fill);

const withMockedScrypt = async (
  impl: (password: string, salt: Buffer, keylen: number, options: { N: number; r: number; p: number }) => Promise<Buffer>,
  run: () => Promise<void>
): Promise<void> => {
  clearDerivedKeyCache();
  __setScryptImplForTesting(impl);
  try {
    await run();
  } finally {
    clearDerivedKeyCache();
    __resetScryptImplForTesting();
  }
};

await (async () => {
  const metaPromise = createMasterKeyMeta("correct-password");
  assert(metaPromise instanceof Promise, "createMasterKeyMeta should return a Promise");
  const meta = await metaPromise;

  assert(typeof meta.salt === "string" && meta.salt.length > 0, "meta should include salt");
  assert(await verifyMasterPassword("correct-password", meta), "correct password should verify");
  assert(!(await verifyMasterPassword("wrong-password", meta)), "wrong password should fail verification");
})();

await (async () => {
  const payload = Buffer.from("backup-payload", "utf8");
  const encrypted = await encryptBackupPayload(payload, "backup-password");
  const decrypted = await decryptBackupPayload(encrypted, "backup-password");
  assertBufferEquals(decrypted, payload, "backup payload should round-trip");
})();

await withMockedScrypt(async () => createFakeDerivedKey(1), async () => {
  const salt = Buffer.from("same-salt");
  const first = await deriveKey("cached-password", salt);
  const second = await deriveKey("cached-password", salt);
  assertBufferEquals(first, second, "sequential calls should reuse cached derived key");
});

await (async () => {
  let calls = 0;
  await withMockedScrypt(async () => {
    calls += 1;
    return createFakeDerivedKey(calls);
  }, async () => {
    const salt = Buffer.from("concurrent-salt");
    const [first, second] = await Promise.all([
      deriveKey("parallel-password", salt),
      deriveKey("parallel-password", salt)
    ]);

    assert(calls === 1, "concurrent deriveKey calls should share one scrypt invocation");
    assertBufferEquals(first, second, "concurrent calls should resolve to the same derived key");
  });
})();

await (async () => {
  let calls = 0;
  await withMockedScrypt(async () => {
    calls += 1;
    return createFakeDerivedKey(calls);
  }, async () => {
    await deriveKey("scoped-password", Buffer.from("salt-a"));
    await deriveKey("scoped-password", Buffer.from("salt-b"));
    await deriveKey("scoped-password", Buffer.from("salt-a"), 32768);
    assert(calls === 3, "different salts or params should not share cache entries");
  });
})();

await (async () => {
  let calls = 0;
  await withMockedScrypt(async () => {
    calls += 1;
    return createFakeDerivedKey(calls);
  }, async () => {
    const salt = Buffer.from("clear-salt");
    await deriveKey("clearable-password", salt);
    clearDerivedKeyCache();
    await deriveKey("clearable-password", salt);
    assert(calls === 2, "clearing cache should force a new derivation");
  });
})();
