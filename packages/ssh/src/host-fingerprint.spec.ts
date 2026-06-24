import { describe, expect, test } from "bun:test";
import { createHash } from "node:crypto";
import { computeHostKeyFingerprint, matchHostFingerprint } from "./index";

// A deterministic stand-in for a raw SSH host key.
const key = Buffer.from("ssh-ed25519 AAAA-fake-host-key-bytes", "utf8");
const otherKey = Buffer.from("ssh-ed25519 AAAA-different-host-key", "utf8");

const sha256Base64 = createHash("sha256").update(key).digest("base64");
const sha256NoPad = sha256Base64.replace(/=+$/, "");
const sha256Hex = createHash("sha256").update(key).digest("hex");
const md5Hex = createHash("md5").update(key).digest("hex");
const md5Colon = md5Hex.match(/.{2}/g)!.join(":");

describe("computeHostKeyFingerprint", () => {
  test("produces the canonical OpenSSH SHA256 form without padding", () => {
    expect(computeHostKeyFingerprint(key)).toBe(`SHA256:${sha256NoPad}`);
    expect(computeHostKeyFingerprint(key)).not.toMatch(/=$/);
  });

  test("accepts a binary string key the same as a Buffer", () => {
    const asString = key.toString("binary");
    expect(computeHostKeyFingerprint(asString)).toBe(computeHostKeyFingerprint(key));
  });
});

describe("matchHostFingerprint", () => {
  test("round-trips the canonical pinned form (case-sensitive)", () => {
    const pinned = computeHostKeyFingerprint(key);
    expect(matchHostFingerprint(pinned, key)).toBe(true);
  });

  test("accepts the legacy sha256:<padded base64> form", () => {
    expect(matchHostFingerprint(`sha256:${sha256Base64}`, key)).toBe(true);
  });

  test("accepts colon-separated MD5 and bare hex forms", () => {
    expect(matchHostFingerprint(md5Colon, key)).toBe(true);
    expect(matchHostFingerprint(sha256Hex, key)).toBe(true);
    expect(matchHostFingerprint(md5Hex, key)).toBe(true);
  });

  test("tolerates surrounding whitespace in the pinned value", () => {
    expect(matchHostFingerprint(`  ${computeHostKeyFingerprint(key)}  `, key)).toBe(true);
  });

  test("rejects a fingerprint from a different host key (mismatch / MITM)", () => {
    const pinned = computeHostKeyFingerprint(key);
    expect(matchHostFingerprint(pinned, otherKey)).toBe(false);
    expect(matchHostFingerprint(`sha256:${sha256Base64}`, otherKey)).toBe(false);
  });

  test("rejects an empty expected fingerprint", () => {
    expect(matchHostFingerprint("", key)).toBe(false);
    expect(matchHostFingerprint("   ", key)).toBe(false);
  });
});
