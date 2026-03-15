import { describe, expect, test } from "bun:test";

describe("cloud sync navigation placement", () => {
  test("shows cloud sync in connection manager after proxies with green styling", async () => {
    (globalThis as Record<string, unknown>).__APP_VERSION__ = "test";
    (globalThis as Record<string, unknown>).__GITHUB_REPO__ = "owner/repo";

    const { MANAGER_TABS } = await import("./ConnectionManagerModal/constants");
    const tabKeys = MANAGER_TABS.map((tab) => tab.key);
    const proxiesIndex = tabKeys.indexOf("proxies");
    const cloudSyncIndex = tabKeys.indexOf("cloudSync");
    const cloudSyncTab = MANAGER_TABS.find((tab) => tab.key === "cloudSync");

    expect(proxiesIndex >= 0).toBe(true);
    expect(cloudSyncIndex).toBe(proxiesIndex + 1);
    expect(cloudSyncTab).toMatchObject({
      label: "云同步",
      icon: "ri-git-merge-line",
      labelClassName: "mgr-tab-label--success"
    });
  });

  test("removes cloud sync from settings center navigation", async () => {
    (globalThis as Record<string, unknown>).__APP_VERSION__ = "test";
    (globalThis as Record<string, unknown>).__GITHUB_REPO__ = "owner/repo";

    const { SECTIONS } = await import("./settings-center/constants");
    expect(SECTIONS.some((section) => section.label === "云同步")).toBe(false);
  });
});
