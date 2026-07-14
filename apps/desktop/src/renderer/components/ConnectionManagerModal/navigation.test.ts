import { expect, test } from "bun:test";
import { MANAGER_TABS } from "./constants";

test("connection manager exposes the expected tabs in order", () => {
  expect(MANAGER_TABS.map((tab) => tab.key)).toEqual([
    "connections",
    "keys",
    "proxies",
    "cloudSync",
    "recycleBin",
    "import"
  ]);

  const recycleBinTab = MANAGER_TABS.find((tab) => tab.key === "recycleBin");
  expect(recycleBinTab).toMatchObject({
    label: "回收站"
  });
});

test("settings center source no longer includes recycle bin section", async () => {
  const settingsConstantsSource = await Bun.file(
    new URL("../settings-center/constants.ts", import.meta.url)
  ).text();
  const settingsTypesSource = await Bun.file(
    new URL("../settings-center/types.ts", import.meta.url)
  ).text();

  expect(settingsConstantsSource.includes('"recycleBin"')).toBe(false);
  expect(settingsTypesSource.includes('| "recycleBin"')).toBe(false);
});
