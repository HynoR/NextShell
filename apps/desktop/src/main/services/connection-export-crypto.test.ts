import {
  decryptConnectionExportPayload,
  encryptConnectionExportPayload
} from "./connection-export-crypto";

const assertEqual = <T>(actual: T, expected: T, message: string): void => {
  if (actual !== expected) {
    throw new Error(`${message}: expected "${String(expected)}", got "${String(actual)}"`);
  }
};

const assertThrows = (fn: () => void, message: string): void => {
  let didThrow = false;
  try {
    fn();
  } catch {
    didThrow = true;
  }

  if (!didThrow) {
    throw new Error(`${message}: expected function to throw`);
  }
};

(() => {
  const plaintext = JSON.stringify({ hello: "world", count: 2 });
  const password = "example-password";
  const encrypted = encryptConnectionExportPayload(plaintext, password);
  const decrypted = decryptConnectionExportPayload(encrypted, password);
  assertEqual(decrypted, plaintext, "should decrypt encrypted export payload");
})();

(() => {
  const plaintext = "{\"format\":\"nextshell-connections\"}";
  const encrypted = encryptConnectionExportPayload(plaintext, "correct-password");
  assertThrows(() => {
    decryptConnectionExportPayload(encrypted, "wrong-password");
  }, "wrong password should fail decryption");
})();
