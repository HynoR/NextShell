import { existsSync } from "node:fs";
import {
  normalizeLocalShellPreference,
  resolveLocalShellLaunch,
  type LocalShellPreference
} from "./local-shell";

const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

const assertEqual = <T>(actual: T, expected: T, message: string): void => {
  if (actual !== expected) {
    throw new Error(`${message}: expected "${String(expected)}", got "${String(actual)}"`);
  }
};

(() => {
  const resolved = resolveLocalShellLaunch(
    {
      mode: "custom",
      preset: "system",
      customPath: "/bin/sh"
    } satisfies LocalShellPreference,
    "darwin"
  );

  assertEqual(resolved.command, "/bin/sh", "resolveLocalShellLaunch should prefer custom shell path");
  assertEqual(resolved.label, "sh", "resolveLocalShellLaunch should derive custom shell label");
})();

(() => {
  const resolved = resolveLocalShellLaunch(
    {
      mode: "preset",
      preset: "system",
      customPath: ""
    } satisfies LocalShellPreference,
    "win32"
  );

  assertEqual(
    resolved.command,
    "powershell.exe",
    "resolveLocalShellLaunch should default Windows system shell to PowerShell"
  );
  assertEqual(
    resolved.label,
    "PowerShell",
    "resolveLocalShellLaunch should label Windows system shell as PowerShell"
  );
})();

(() => {
  const resolved = resolveLocalShellLaunch(
    {
      mode: "preset",
      preset: "system",
      customPath: ""
    } satisfies LocalShellPreference,
    "linux"
  );

  assert(
    resolved.command === "/bin/bash" || resolved.command === "/bin/sh",
    "resolveLocalShellLaunch should resolve Linux system shell to bash or sh"
  );

  if (resolved.command === "/bin/bash") {
    assert(existsSync("/bin/bash"), "resolveLocalShellLaunch should only pick /bin/bash when it exists");
  }
})();

(() => {
  const normalized = normalizeLocalShellPreference(
    {
      mode: "preset",
      preset: "powershell",
      customPath: ""
    } satisfies LocalShellPreference,
    "darwin"
  );

  assertEqual(
    normalized.preset,
    "system",
    "normalizeLocalShellPreference should fallback unsupported presets to system"
  );
})();

(() => {
  const normalized = normalizeLocalShellPreference(
    {
      mode: "custom",
      preset: "powershell",
      customPath: "/opt/homebrew/bin/fish"
    } satisfies LocalShellPreference,
    "darwin"
  );

  assertEqual(
    normalized.mode,
    "custom",
    "normalizeLocalShellPreference should preserve custom shell mode across platforms"
  );
  assertEqual(
    normalized.customPath,
    "/opt/homebrew/bin/fish",
    "normalizeLocalShellPreference should preserve custom shell path when preset is unsupported"
  );
})();
