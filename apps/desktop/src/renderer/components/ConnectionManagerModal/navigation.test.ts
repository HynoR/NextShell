import { expect, test } from "vitest";
import { MANAGER_TABS } from "./constants";
import settingsConstantsSource from "../settings-center/constants.ts?raw";
import settingsTypesSource from "../settings-center/types.ts?raw";

test("connection manager keeps only connection-scoped tabs", () => {
  // 云同步与回收站已迁往设置中心；剩下的四个都直接服务于「管理这台机器的连接」。
  expect(MANAGER_TABS.map((tab) => tab.key)).toEqual([
    "connections",
    "keys",
    "proxies",
    "import"
  ]);
});

test("settings center source declares the migrated sections", () => {
  expect(settingsConstantsSource.includes('"recycleBin"')).toBe(true);
  expect(settingsConstantsSource.includes('"cloudSync"')).toBe(true);
  expect(settingsTypesSource.includes('| "recycleBin"')).toBe(true);
  expect(settingsTypesSource.includes('| "cloudSync"')).toBe(true);
});
