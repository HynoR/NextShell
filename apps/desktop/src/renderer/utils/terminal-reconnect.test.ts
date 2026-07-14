import { isEnterInput, shouldReconnectOnInput } from "./terminal-reconnect";

const assertEqual = <T>(actual: T, expected: T, message: string): void => {
  if (actual !== expected) {
    throw new Error(`${message}: expected "${String(expected)}", got "${String(actual)}"`);
  }
};

(() => {
  assertEqual(isEnterInput("\r"), true, "carriage return should be enter input");
  assertEqual(isEnterInput("\n"), true, "line feed should be enter input");
  assertEqual(isEnterInput("a"), false, "plain text should not be enter input");
})();

(() => {
  assertEqual(
    shouldReconnectOnInput("disconnected", "\r"),
    true,
    "disconnected + enter should reconnect"
  );
  assertEqual(
    shouldReconnectOnInput("disconnected", "a"),
    false,
    "disconnected + normal text should not reconnect"
  );
  assertEqual(
    shouldReconnectOnInput("connected", "\r"),
    false,
    "connected + enter should not reconnect"
  );
  assertEqual(shouldReconnectOnInput("failed", "\r"), false, "failed + enter should not reconnect");
  assertEqual(
    shouldReconnectOnInput(undefined, "\r"),
    false,
    "unknown status + enter should not reconnect"
  );
})();
