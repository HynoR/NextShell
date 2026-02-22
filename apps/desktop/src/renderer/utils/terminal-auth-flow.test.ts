import {
  buildTerminalAuthIntro,
  consumeTerminalAuthInput,
  createTerminalAuthState,
  isAuthFailureReason,
  resetTerminalAuthForRetry
} from "./terminal-auth-flow";

const AUTH_REQUIRED_PREFIX = "AUTH_REQUIRED::";

const assertEqual = <T>(actual: T, expected: T, message: string): void => {
  if (actual !== expected) {
    throw new Error(`${message}: expected "${String(expected)}", got "${String(actual)}"`);
  }
};

const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

(() => {
  const state = createTerminalAuthState();
  const result = consumeTerminalAuthInput(state, "\r");
  assertEqual(result.nextState.stage, "username", "empty username keeps username stage");
  assertEqual(result.output, "\r\nlogin as: ", "empty username should prompt again");
  assertEqual(result.submit, undefined, "empty username should not submit");
})();

(() => {
  let state = createTerminalAuthState();
  const usernameStep = consumeTerminalAuthInput(state, "root\r");
  assertEqual(usernameStep.nextState.stage, "password", "username enter should switch to password stage");
  assertEqual(usernameStep.output, "root\r\npassword: ", "username input should echo and prompt password");

  state = usernameStep.nextState;
  const passwordTyping = consumeTerminalAuthInput(state, "secret");
  assertEqual(passwordTyping.output, "", "password typing should not echo");
  assertEqual(passwordTyping.nextState.passwordBuffer, "secret", "password should be buffered in memory");

  const submitStep = consumeTerminalAuthInput(passwordTyping.nextState, "\r");
  assertEqual(submitStep.nextState.stage, "submitting", "enter on password should submit");
  assert(submitStep.submit !== undefined, "submit payload should exist");
  assertEqual(submitStep.submit?.username, "root", "submit should carry username");
  assertEqual(submitStep.submit?.password, "secret", "submit should carry password");
  assertEqual(submitStep.submit?.nonce, 1, "submit should increment nonce");
  assertEqual(submitStep.nextState.passwordBuffer, "", "password buffer should be cleared after submit");
})();

(() => {
  const state = {
    ...createTerminalAuthState(),
    attempt: 1,
    nonce: 3,
    stage: "submitting" as const
  };
  const retried = resetTerminalAuthForRetry(state);
  assertEqual(retried.stage, "username", "retry should return to username stage");
  assertEqual(retried.attempt, 2, "retry should increase attempt");
  assertEqual(retried.nonce, 3, "retry should keep nonce");
})();

(() => {
  const reason = `${AUTH_REQUIRED_PREFIX}缺少密码，请输入密码后重试。`;
  assert(isAuthFailureReason(reason), "AUTH_REQUIRED prefix should be recognized");
  const intro = buildTerminalAuthIntro(reason);
  assert(intro.includes("Authentication required"), "intro should include auth required hint");
  assert(intro.endsWith("login as: "), "intro should end with login prompt");
})();

(() => {
  const wrappedReason = `Error invoking remote method: Error: ${AUTH_REQUIRED_PREFIX}缺少用户名，请输入用户名和认证信息后重试。`;
  assert(isAuthFailureReason(wrappedReason), "wrapped auth error should be recognized");
  const intro = buildTerminalAuthIntro(wrappedReason);
  assert(intro.includes("缺少用户名"), "wrapped auth error should be normalized");
})();
