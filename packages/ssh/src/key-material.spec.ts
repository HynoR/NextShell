import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, test } from "vitest";
import { generateSshKeyPair, parseSshKeyMaterial } from "./key-material";

describe("generateSshKeyPair", () => {
  test("produces an ed25519 key that parses back with its metadata", () => {
    const generated = generateSshKeyPair("ed25519", "nextshell@test");

    expect(generated.privateKey).toContain("BEGIN OPENSSH PRIVATE KEY");
    expect(generated.info.keyType).toBe("ssh-ed25519");
    expect(generated.info.bits).toBe(256);
    expect(generated.info.comment).toBe("nextshell@test");
    expect(generated.info.fingerprint).toMatch(/^SHA256:[A-Za-z0-9+/]{43}$/);
    expect(generated.info.publicKeyLine.startsWith("ssh-ed25519 AAAA")).toBe(true);
    expect(parseSshKeyMaterial(generated.privateKey)).toEqual(generated.info);
  });

  test("omits the comment when none was requested", () => {
    expect(generateSshKeyPair("ed25519").info.comment).toBeUndefined();
  });

  test("produces distinct keys on every call", () => {
    expect(generateSshKeyPair("ed25519").info.fingerprint).not.toBe(
      generateSshKeyPair("ed25519").info.fingerprint
    );
  });

  test("reports the real modulus size for RSA", () => {
    const generated = generateSshKeyPair("rsa-2048");
    expect(generated.info.keyType).toBe("ssh-rsa");
    expect(generated.info.bits).toBe(2048);
  });
});

describe("parseSshKeyMaterial", () => {
  test("rejects a public key handed over as if it were private", () => {
    const { info } = generateSshKeyPair("ed25519", "x@y");
    expect(() => parseSshKeyMaterial(info.publicKeyLine)).toThrow();
  });

  test("rejects truncated and non-key input", () => {
    const { privateKey } = generateSshKeyPair("ed25519");
    expect(() => parseSshKeyMaterial(privateKey.slice(0, 120))).toThrow("无法解析私钥");
    expect(() => parseSshKeyMaterial("hello world")).toThrow("无法解析私钥");
    expect(() => parseSshKeyMaterial("")).toThrow("无法解析私钥");
  });

  test("rejects a passphrase-protected key when the passphrase is wrong", () => {
    // Encrypted keys can only come from outside, so build one with the system ssh-keygen.
    const dir = mkdtempSync(join(tmpdir(), "nextshell-key-"));
    try {
      const keyPath = join(dir, "id_ed25519");
      execFileSync("ssh-keygen", [
        "-q",
        "-t",
        "ed25519",
        "-N",
        "correct-horse",
        "-C",
        "enc@test",
        "-f",
        keyPath
      ]);
      const encrypted = readFileSync(keyPath, "utf8");

      expect(parseSshKeyMaterial(encrypted, "correct-horse").keyType).toBe("ssh-ed25519");
      expect(() => parseSshKeyMaterial(encrypted, "wrong")).toThrow("无法解析私钥");
      expect(() => parseSshKeyMaterial(encrypted)).toThrow("无法解析私钥");
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  });
});

describe("fingerprint compatibility", () => {
  test("matches what ssh-keygen -lf reports for the same key", () => {
    const dir = mkdtempSync(join(tmpdir(), "nextshell-fp-"));
    try {
      const generated = generateSshKeyPair("ed25519", "fp@test");
      const keyPath = join(dir, "id_ed25519");
      writeFileSync(keyPath, generated.privateKey, { mode: 0o600 });

      // `ssh-keygen -lf <private key>` prints "<bits> SHA256:<hash> <comment> (ED25519)".
      const output = execFileSync("ssh-keygen", ["-lf", keyPath], { encoding: "utf8" });
      const reported = output.trim().split(/\s+/)[1];

      expect(reported).toBe(generated.info.fingerprint);
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  });
});
