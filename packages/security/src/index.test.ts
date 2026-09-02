import { __resetScryptImplForTesting, __setScryptImplForTesting, deriveKey } from "./index";

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
  impl: (
    password: string,
    salt: Buffer,
    keylen: number,
    options: { N: number; r: number; p: number }
  ) => Promise<Buffer>,
  run: () => Promise<void>
): Promise<void> => {
  __setScryptImplForTesting(impl);
  try {
    await run();
  } finally {
    __resetScryptImplForTesting();
  }
};

await withMockedScrypt(
  async () => createFakeDerivedKey(1),
  async () => {
    const derived = await deriveKey("password", Buffer.from("salt"));
    assertBufferEquals(derived, createFakeDerivedKey(1), "deriveKey should return the scrypt key");
  }
);
