import { describe, expect, test } from "bun:test";
import { resolveAllowedAssetPath } from "./asset-protocol";

describe("resolveAllowedAssetPath", () => {
  test("allows only the configured POSIX background image", () => {
    expect(
      resolveAllowedAssetPath(
        "nextshell-asset://local/Users/example/Pictures/wallpaper.png",
        "/Users/example/Pictures/wallpaper.png",
        "darwin"
      )
    ).toBe("/Users/example/Pictures/wallpaper.png");
    expect(
      resolveAllowedAssetPath(
        "nextshell-asset://local/Users/example/.ssh/id_rsa",
        "/Users/example/Pictures/wallpaper.png",
        "darwin"
      )
    ).toBe(undefined);
  });

  test("normalizes Windows drive paths case-insensitively", () => {
    expect(
      resolveAllowedAssetPath(
        "nextshell-asset://local/C:/Users/Example/Pictures/wallpaper.png",
        "c:\\users\\example\\pictures\\wallpaper.png",
        "win32"
      )
    ).toBe("c:\\users\\example\\pictures\\wallpaper.png");
  });

  test("rejects empty configuration and untrusted hosts", () => {
    expect(resolveAllowedAssetPath("nextshell-asset://local/tmp/wallpaper.png", "", "linux")).toBe(
      undefined
    );
    expect(
      resolveAllowedAssetPath(
        "nextshell-asset://remote/tmp/wallpaper.png",
        "/tmp/wallpaper.png",
        "linux"
      )
    ).toBe(undefined);
  });
});
