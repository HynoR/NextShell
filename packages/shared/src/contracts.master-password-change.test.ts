import { masterPasswordChangeSchema } from "./contracts";

const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

(() => {
  const parsed = masterPasswordChangeSchema.parse({
    oldPassword: "old-password",
    newPassword: "new-password",
    confirmPassword: "new-password"
  });
  assert(parsed.oldPassword === "old-password", "should keep oldPassword");
  assert(parsed.newPassword === "new-password", "should keep newPassword");
})();

(() => {
  let thrown = false;
  try {
    masterPasswordChangeSchema.parse({
      oldPassword: "old-password",
      newPassword: "new-password",
      confirmPassword: "mismatch-password"
    });
  } catch {
    thrown = true;
  }
  assert(thrown, "should reject when confirm password mismatches");
})();

(() => {
  let thrown = false;
  try {
    masterPasswordChangeSchema.parse({
      oldPassword: "",
      newPassword: "short",
      confirmPassword: "short"
    });
  } catch {
    thrown = true;
  }
  assert(thrown, "should reject empty old password and too-short new password");
})();
