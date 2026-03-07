import {
  decryptConnectionExportPayload,
  encryptConnectionExportPayload
} from "./connection-export-crypto";

const assertEqual = <T>(actual: T, expected: T, message: string): void => {
  if (actual !== expected) {
    throw new Error(`${message}: expected "${String(expected)}", got "${String(actual)}"`);
  }
};

const assertRejects = async (fn: () => Promise<unknown>, message: string): Promise<void> => {
  let didThrow = false;
  try {
    await fn();
  } catch {
    didThrow = true;
  }

  if (!didThrow) {
    throw new Error(`${message}: expected function to throw`);
  }
};

await (async () => {
  const plaintext = JSON.stringify({ hello: "world", count: 2 });
  const password = "example-password";
  const encrypted = await encryptConnectionExportPayload(plaintext, password);
  const decrypted = await decryptConnectionExportPayload(encrypted, password);
  assertEqual(decrypted, plaintext, "should decrypt encrypted export payload");
})();

await (async () => {
  const plaintext = "{\"format\":\"nextshell-connections\"}";
  const encrypted = await encryptConnectionExportPayload(plaintext, "correct-password");
  await assertRejects(async () => {
    await decryptConnectionExportPayload(encrypted, "wrong-password");
  }, "wrong password should fail decryption");
})();
