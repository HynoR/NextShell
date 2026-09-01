import { describe, expect, test } from "vitest";

// 这条曾经钉的是相反的位置(云同步在连接管理器里、回收站不在设置里)。决策已反转:
// 云同步是账号/服务配置,回收站是全局删除历史,都属于低频全局设置。
describe("cloud sync and recycle bin placement", () => {
  test("settings center exposes cloud sync before recycle bin", async () => {
    (globalThis as Record<string, unknown>).__APP_VERSION__ = "test";
    (globalThis as Record<string, unknown>).__GITHUB_REPO__ = "owner/repo";

    const { SECTIONS } = await import("./settings-center/constants");
    const keys = SECTIONS.map((section) => section.key);

    expect(keys).toContain("cloudSync");
    expect(keys).toContain("recycleBin");
    expect(keys.indexOf("recycleBin")).toBe(keys.indexOf("cloudSync") + 1);
  });
});
