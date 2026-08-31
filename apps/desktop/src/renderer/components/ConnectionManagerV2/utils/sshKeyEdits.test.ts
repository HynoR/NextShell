import { describe, expect, test } from "vitest";
import { planSshKeyRename, planSshKeyReplace, suggestKeyNameFromFile } from "./sshKeyEdits";

const key = { id: "11111111-1111-4111-8111-111111111111", name: "deploy" };

describe("planSshKeyRename", () => {
  test("sends only the new name so the stored private key is left alone", () => {
    const payload = planSshKeyRename({ key, name: "deploy-prod" });
    expect(payload).toEqual({ id: key.id, name: "deploy-prod", workspaceId: undefined });
    // 契约允许省略 keyContent；带上原文才是危险的（等于重新解析一次已经好的密钥）。
    expect(payload).not.toHaveProperty("keyContent");
    // passphrase 一旦出现在 payload 里（哪怕是 undefined 之外的空串）主进程就会把已存的口令删掉。
    expect(payload?.passphrase).toBeUndefined();
  });

  test("keeps the key inside its scope", () => {
    expect(planSshKeyRename({ key, name: "renamed", workspaceId: "ws-1" })?.workspaceId).toBe(
      "ws-1"
    );
  });

  test("trims before comparing so whitespace alone is not a rename", () => {
    expect(planSshKeyRename({ key, name: "  deploy  " })).toBeUndefined();
  });

  test("refuses a blank or unchanged name instead of writing", () => {
    expect(planSshKeyRename({ key, name: "" })).toBeUndefined();
    expect(planSshKeyRename({ key, name: "   " })).toBeUndefined();
    expect(planSshKeyRename({ key, name: "deploy" })).toBeUndefined();
  });
});

describe("planSshKeyReplace", () => {
  test("carries the new content and keeps the existing name", () => {
    const payload = planSshKeyReplace({
      key,
      keyContent: "-----BEGIN OPENSSH PRIVATE KEY-----\nx\n",
      workspaceId: "ws-1"
    });
    expect(payload).toEqual({
      id: key.id,
      name: "deploy",
      keyContent: "-----BEGIN OPENSSH PRIVATE KEY-----\nx",
      passphrase: undefined,
      workspaceId: "ws-1"
    });
  });

  test("passes a passphrase through when one was typed", () => {
    expect(
      planSshKeyReplace({ key, keyContent: "-----BEGIN-----", passphrase: " hunter2 " })?.passphrase
    ).toBe("hunter2");
  });

  test("treats a blank passphrase as 'do not touch the stored one'", () => {
    expect(
      planSshKeyReplace({ key, keyContent: "-----BEGIN-----", passphrase: "   " })?.passphrase
    ).toBeUndefined();
  });

  test("treats blank content as 'do not update' rather than as an empty key", () => {
    expect(planSshKeyReplace({ key, keyContent: "" })).toBeUndefined();
    expect(planSshKeyReplace({ key, keyContent: "   \n " })).toBeUndefined();
    expect(planSshKeyReplace({ key, keyContent: undefined })).toBeUndefined();
  });
});

describe("suggestKeyNameFromFile", () => {
  test("drops the extension", () => {
    expect(suggestKeyNameFromFile("id_rsa.pem")).toBe("id_rsa");
  });

  test("keeps a name that has no extension", () => {
    expect(suggestKeyNameFromFile("id_ed25519")).toBe("id_ed25519");
  });

  test("keeps dotfiles usable instead of returning an empty name", () => {
    expect(suggestKeyNameFromFile(".ssh-key")).toBe(".ssh-key");
  });

  test("uses only the basename when the browser hands back a relative path", () => {
    expect(suggestKeyNameFromFile("keys/prod/id_rsa.pem")).toBe("id_rsa");
  });
});
