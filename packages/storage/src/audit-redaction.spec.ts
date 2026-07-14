import { describe, expect, test } from "bun:test";
import { redactAuditMetadata } from "./index";

const redactCommand = (command: string): string => {
  const out = redactAuditMetadata({ command });
  return String(out?.command ?? "");
};

describe("redactAuditMetadata", () => {
  test("masks an inline mysql -p password", () => {
    const masked = redactCommand("mysql -uroot -pSup3rSecret -e 'select 1'");
    expect(masked).not.toContain("Sup3rSecret");
    expect(masked).toContain("mysql -uroot");
    expect(masked).toContain("«redacted»");
  });

  test("masks an Authorization Bearer token", () => {
    const masked = redactCommand("curl -H 'Authorization: Bearer abc.DEF-123_token' https://x");
    expect(masked).not.toContain("abc.DEF-123_token");
    expect(masked).toContain("Bearer «redacted»");
    expect(masked).toContain("https://x");
  });

  test("masks key=value and key: value secrets", () => {
    expect(redactCommand("export TOKEN=ghp_abc123")).not.toContain("ghp_abc123");
    expect(redactCommand("password = hunter2")).not.toContain("hunter2");
    expect(redactCommand("api_key:AKIA9999")).not.toContain("AKIA9999");
  });

  test("masks --password= and --token forms", () => {
    expect(redactCommand("psql --password=topsecret")).not.toContain("topsecret");
    expect(redactCommand("gh auth login --token ghp_zzz")).not.toContain("ghp_zzz");
  });

  test("masks an entire PEM private key block", () => {
    const masked = redactCommand(
      "echo '-----BEGIN OPENSSH PRIVATE KEY-----\nMIIEvbody\n-----END OPENSSH PRIVATE KEY-----'"
    );
    expect(masked).not.toContain("MIIEvbody");
    expect(masked).toContain("«redacted»");
  });

  test("redacts a value under a sensitive key name", () => {
    const out = redactAuditMetadata({ password: "plainsecret", host: "10.0.0.1" });
    expect(out?.password).toBe("«redacted»");
    expect(out?.host).toBe("10.0.0.1");
  });

  test("recurses into nested objects and arrays", () => {
    const out = redactAuditMetadata({
      steps: [{ command: "mysql -pNESTED" }],
      ctx: { token: "deep-secret" }
    });
    expect(JSON.stringify(out)).not.toContain("NESTED");
    expect(JSON.stringify(out)).not.toContain("deep-secret");
  });

  test("leaves non-sensitive metadata untouched", () => {
    const out = redactAuditMetadata({ command: "ls -la /var/log", exitCode: 0, durationMs: 12 });
    expect(out).toEqual({ command: "ls -la /var/log", exitCode: 0, durationMs: 12 });
  });

  test("returns undefined for undefined input", () => {
    expect(redactAuditMetadata(undefined)).toBeUndefined();
  });
});
