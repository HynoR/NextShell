import { createMasterKeyMeta, verifyMasterPassword } from "../../../../../packages/security/src/index";
import { changeMasterPassword } from "./master-password-change";

const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

const assertRejects = async (run: () => Promise<unknown>, expectedMessage: string): Promise<void> => {
  try {
    await run();
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    assert(message.includes(expectedMessage), `should include "${expectedMessage}", got "${message}"`);
    return;
  }
  throw new Error("expected promise to reject");
};

await (async () => {
  const original = "old-password";
  const next = "new-password";
  let currentMeta = createMasterKeyMeta(original);
  let unlockedPassword = original;
  const rememberCalls: string[] = [];
  const auditRecords: Array<{ action: string; level: string; metadata?: Record<string, unknown> }> = [];

  await changeMasterPassword({
    oldPassword: original,
    newPassword: next,
    getMasterKeyMeta: () => currentMeta,
    saveMasterKeyMeta: (meta) => { currentMeta = meta; },
    setMasterPassword: (password) => { unlockedPassword = password; },
    rememberPasswordBestEffort: async (password, phase) => {
      rememberCalls.push(`${phase}:${password}`);
    },
    appendAuditLog: (record) => {
      auditRecords.push(record);
    }
  });

  assert(unlockedPassword === next, "should keep runtime unlocked password as new password");
  assert(verifyMasterPassword(next, currentMeta), "new password should verify updated meta");
  assert(!verifyMasterPassword(original, currentMeta), "old password should not verify updated meta");
  assert(rememberCalls[0] === `change:${next}`, "should remember new password with change phase");
  assert(auditRecords[0]?.action === "master_password.change", "should append change audit log");
  assert(auditRecords[0]?.metadata?.["sameAsOld"] === false, "audit should mark sameAsOld=false");
})();

await (async () => {
  const password = "same-password";
  let currentMeta = createMasterKeyMeta(password);
  let unlockedPassword = password;
  const auditRecords: Array<{ metadata?: Record<string, unknown> }> = [];

  await changeMasterPassword({
    oldPassword: password,
    newPassword: password,
    getMasterKeyMeta: () => currentMeta,
    saveMasterKeyMeta: (meta) => { currentMeta = meta; },
    setMasterPassword: (value) => { unlockedPassword = value; },
    rememberPasswordBestEffort: async () => {},
    appendAuditLog: (record) => {
      auditRecords.push(record);
    }
  });

  assert(unlockedPassword === password, "same password update should still keep unlocked");
  assert(auditRecords[0]?.metadata?.["sameAsOld"] === true, "audit should mark sameAsOld=true");
})();

await (async () => {
  const original = "correct-password";
  const currentMeta = createMasterKeyMeta(original);
  await assertRejects(
    () => changeMasterPassword({
      oldPassword: "wrong-password",
      newPassword: "next-password",
      getMasterKeyMeta: () => currentMeta,
      saveMasterKeyMeta: () => {},
      setMasterPassword: () => {},
      rememberPasswordBestEffort: async () => {},
      appendAuditLog: () => {}
    }),
    "原密码错误"
  );
})();
