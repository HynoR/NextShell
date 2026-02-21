import { decryptFinalShellPassword } from "./decrypt-password";

const assertEqual = <T>(actual: T, expected: T, message: string): void => {
  if (actual !== expected) {
    throw new Error(`${message}: expected "${String(expected)}", got "${String(actual)}"`);
  }
};

(() => {
  const encoded = "KjNmdxGqIrsm5Y0i05CTAAXHUcd4t0DkPoUzSRftZ8E=";
  const plain = decryptFinalShellPassword(encoded);
  assertEqual(plain, "pw-708011639-296362683", "should decrypt FinalShell password");
})();

(() => {
  assertEqual(decryptFinalShellPassword(undefined), undefined, "undefined input should return undefined");
  assertEqual(decryptFinalShellPassword("not-base64"), undefined, "invalid base64 should return undefined");
})();
