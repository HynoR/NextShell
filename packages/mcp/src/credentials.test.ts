import { afterEach, beforeEach, describe, expect, test } from "vitest";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import {
  defaultConfigPath,
  MissingTokenError,
  resolveCredentials,
  SETUP_HINT,
  TOKEN_ENV
} from "./credentials";

describe("default mcp.json location mirrors Go's os.UserConfigDir", () => {
  test("macOS", () => {
    expect(defaultConfigPath({ platform: "darwin", env: {}, home: "/Users/me" })).toBe(
      "/Users/me/Library/Application Support/NextShell-v2/mcp.json"
    );
  });
  test("Linux honours an absolute XDG_CONFIG_HOME, otherwise ~/.config", () => {
    const home = "/home/me";
    expect(defaultConfigPath({ platform: "linux", env: { XDG_CONFIG_HOME: "/x/cfg" }, home })).toBe(
      "/x/cfg/NextShell-v2/mcp.json"
    );
    for (const xdg of [undefined, "", "relative/dir"])
      expect(defaultConfigPath({ platform: "linux", env: { XDG_CONFIG_HOME: xdg }, home })).toBe(
        "/home/me/.config/NextShell-v2/mcp.json"
      );
  });
  test("Windows uses %AppData%", () => {
    expect(
      defaultConfigPath({
        platform: "win32",
        env: { AppData: "C:\\Users\\me\\AppData\\Roaming" },
        home: "C:\\Users\\me"
      })
    ).toBe("C:\\Users\\me\\AppData\\Roaming\\NextShell-v2\\mcp.json");
  });
  test("no config directory is a clear error", () => {
    expect(() => defaultConfigPath({ platform: "win32", env: {}, home: "" })).toThrow(
      MissingTokenError
    );
  });
});

describe("credential resolution", () => {
  let dir: string;
  let home: string;
  const host = (env: Record<string, string | undefined> = {}) => ({
    platform: "linux" as const,
    env: { XDG_CONFIG_HOME: undefined, ...env },
    home
  });
  const write = async (file: string, value: unknown) => {
    await mkdir(path.dirname(file), { recursive: true });
    await writeFile(file, typeof value === "string" ? value : JSON.stringify(value));
  };
  const defaultFile = () => path.join(home, ".config", "NextShell-v2", "mcp.json");
  beforeEach(async () => {
    dir = await mkdtemp(path.join(tmpdir(), "nextshell-mcp-cred-"));
    home = path.join(dir, "home");
  });
  afterEach(() => rm(dir, { recursive: true, force: true }));

  test("--token beats the environment, --config and the default file", async () => {
    const config = path.join(dir, "c.json");
    await write(config, { port: 2000, token: "from-config" });
    await write(defaultFile(), { port: 3000, token: "from-default" });
    expect(
      await resolveCredentials({ token: "flag", config }, host({ [TOKEN_ENV]: "env" }))
    ).toEqual({ token: "flag", port: 2000 });
  });
  test("the environment beats --config and the default file", async () => {
    const config = path.join(dir, "c.json");
    await write(config, { port: 2000, token: "from-config" });
    await write(defaultFile(), { port: 3000, token: "from-default" });
    expect(await resolveCredentials({ config }, host({ [TOKEN_ENV]: "env" }))).toEqual({
      token: "env",
      port: 2000
    });
  });
  test("--config beats the default file", async () => {
    const config = path.join(dir, "c.json");
    await write(config, { port: 2000, token: "from-config" });
    await write(defaultFile(), { port: 3000, token: "from-default" });
    expect(await resolveCredentials({ config }, host())).toEqual({
      token: "from-config",
      port: 2000
    });
  });
  test("the default file is used with no options, and is re-read every call", async () => {
    await write(defaultFile(), { port: 3000, token: "first" });
    expect(await resolveCredentials({}, host())).toEqual({ token: "first", port: 3000 });
    await write(defaultFile(), { port: 3001, token: "rotated" });
    expect(await resolveCredentials({}, host())).toEqual({ token: "rotated", port: 3001 });
  });
  test("--port overrides the file's port but not its token", async () => {
    await write(defaultFile(), { port: 3000, token: "t" });
    expect(await resolveCredentials({ port: 4000 }, host())).toEqual({ token: "t", port: 4000 });
  });
  test("an explicit token without any file falls back to the default port", async () => {
    expect(await resolveCredentials({ token: "t" }, host())).toEqual({ token: "t", port: 41777 });
    expect(await resolveCredentials({}, host({ [TOKEN_ENV]: "t" }))).toEqual({
      token: "t",
      port: 41777
    });
    expect(await resolveCredentials({ token: "t", port: 5 }, host())).toEqual({
      token: "t",
      port: 5
    });
  });
  test("an explicit token still takes its port from the file", async () => {
    await write(defaultFile(), { port: 3000, token: "from-default" });
    expect(await resolveCredentials({ token: "t" }, host())).toEqual({ token: "t", port: 3000 });
  });

  test.each([
    ["missing file", undefined],
    ["not JSON", "{oops"],
    ["no token", { port: 1 }],
    ["empty token", { port: 1, token: "" }],
    ["no port", { token: "SECRET-TOKEN" }],
    ["bad port", { port: 70000, token: "SECRET-TOKEN" }],
    ["non-object", "[1]"]
  ])(
    "no usable token (%s) gives a setup hint and never echoes file contents",
    async (_, content) => {
      if (content !== undefined) await write(defaultFile(), content);
      const error = await resolveCredentials({}, host()).catch((e: unknown) => e);
      expect(error).toBeInstanceOf(MissingTokenError);
      const message = (error as Error).message;
      expect(message).toContain(SETUP_HINT);
      expect(message).not.toContain("SECRET-TOKEN");
    }
  );
  test("a broken --config does not silently fall back to the default file", async () => {
    await write(defaultFile(), { port: 3000, token: "t" });
    await expect(
      resolveCredentials({ config: path.join(dir, "missing.json") }, host())
    ).rejects.toBeInstanceOf(MissingTokenError);
  });
});
