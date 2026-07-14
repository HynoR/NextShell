import { describe, expect, test } from "bun:test";
import { isTrustedRendererUrl } from "./navigation-security";

describe("isTrustedRendererUrl", () => {
  test("allows only the configured development origin", () => {
    expect(
      isTrustedRendererUrl(
        "http://localhost:5173/settings?tab=network",
        "/Applications/NextShell.app/Contents/Resources/app.asar",
        "http://localhost:5173"
      )
    ).toBe(true);
    expect(
      isTrustedRendererUrl(
        "http://localhost:5173.evil.example/settings",
        "/Applications/NextShell.app/Contents/Resources/app.asar",
        "http://localhost:5173"
      )
    ).toBe(false);
  });

  test("allows only the packaged renderer entry file", () => {
    const appPath = "/Applications/NextShell.app/Contents/Resources/app.asar";
    expect(
      isTrustedRendererUrl(
        "file:///Applications/NextShell.app/Contents/Resources/app.asar/dist/index.html#settings",
        appPath,
        undefined,
        "darwin"
      )
    ).toBe(true);
    expect(
      isTrustedRendererUrl(
        "file:///Applications/NextShell.app/Contents/Resources/app.asar/dist/other.html",
        appPath,
        undefined,
        "darwin"
      )
    ).toBe(false);
  });

  test("rejects malformed and remote production URLs", () => {
    expect(isTrustedRendererUrl("not a url", "/app", undefined)).toBe(false);
    expect(isTrustedRendererUrl("https://example.com", "/app", undefined)).toBe(false);
  });
});
