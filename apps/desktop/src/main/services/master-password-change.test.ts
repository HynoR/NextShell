import {
  createMasterKeyMeta,
  verifyMasterPassword
} from "../../../../../packages/security/src/index";
import { changeMasterPassword } from "./master-password-change";

const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

const assertRejects = async (
  run: () => Promise<unknown>,
  expectedMessage: string
): Promise<void> => {
  try {
    await run();
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    assert(
      message.includes(expectedMessage),
      `should include "${expectedMessage}", got "${message}"`
    );
    return;
  }
  throw new Error("expected promise to reject");
};

await (async () => {
  const original = "old-password";
  const next = "new-password";
  let currentMeta = await createMasterKeyMeta(original);
  let unlockedPassword = original;

  await changeMasterPassword({
    oldPassword: original,
    newPassword: next,
    getMasterKeyMeta: () => currentMeta,
    saveMasterKeyMeta: (meta) => {
      currentMeta = meta;
    },
    setMasterPassword: (password) => {
      unlockedPassword = password;
    }
  });

  assert(unlockedPassword === next, "should keep runtime unlocked password as new password");
  assert(await verifyMasterPassword(next, currentMeta), "new password should verify updated meta");
  assert(
    !(await verifyMasterPassword(original, currentMeta)),
    "old password should not verify updated meta"
  );
})();

await (async () => {
  const password = "same-password";
  let currentMeta = await createMasterKeyMeta(password);
  let unlockedPassword = password;

  await changeMasterPassword({
    oldPassword: password,
    newPassword: password,
    getMasterKeyMeta: () => currentMeta,
    saveMasterKeyMeta: (meta) => {
      currentMeta = meta;
    },
    setMasterPassword: (value) => {
      unlockedPassword = value;
    }
  });

  assert(unlockedPassword === password, "same password update should still keep unlocked");
})();

await (async () => {
  const original = "correct-password";
  const currentMeta = await createMasterKeyMeta(original);
  await assertRejects(
    () =>
      changeMasterPassword({
        oldPassword: "wrong-password",
        newPassword: "next-password",
        getMasterKeyMeta: () => currentMeta,
        saveMasterKeyMeta: () => {},
        setMasterPassword: () => {}
      }),
    "原密码错误"
  );
})();
