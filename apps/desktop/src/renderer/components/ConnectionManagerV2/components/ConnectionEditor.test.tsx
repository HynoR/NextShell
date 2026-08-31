import { describe, expect, test } from "vitest";
import { EDITOR_SECTION_BY_FIELD, resolveEditorErrorSection } from "./ConnectionEditor";

describe("resolveEditorErrorSection", () => {
  test("把折叠区内的出错字段映射到它所在的区块", () => {
    expect(resolveEditorErrorSection([{ name: ["keepAliveIntervalSec"] }])).toBe("network");
    expect(resolveEditorErrorSection([{ name: ["hostFingerprint"] }])).toBe("security");
    expect(resolveEditorErrorSection([{ name: ["deleteMode"] }])).toBe("terminal");
    expect(resolveEditorErrorSection([{ name: ["agentAccess"] }])).toBe("meta");
  });

  test("只看第一个出错字段——展开一个区块再滚过去，多开只会晃眼", () => {
    expect(
      resolveEditorErrorSection([{ name: ["proxyId"] }, { name: ["hostFingerprint"] }])
    ).toBe("network");
  });

  test("常驻字段不需要展开任何区块", () => {
    expect(resolveEditorErrorSection([{ name: ["host"] }])).toBeUndefined();
    expect(resolveEditorErrorSection([{ name: ["sshKeyId"] }])).toBeUndefined();
  });

  test("未知字段、数字路径与空错误列表都安全退化", () => {
    expect(resolveEditorErrorSection([{ name: ["somethingNew"] }])).toBeUndefined();
    expect(resolveEditorErrorSection([{ name: [0, "host"] }])).toBeUndefined();
    expect(resolveEditorErrorSection([{ name: [] }])).toBeUndefined();
    expect(resolveEditorErrorSection([])).toBeUndefined();
  });

  test("映射表只登记折叠区内的字段", () => {
    expect(Object.keys(EDITOR_SECTION_BY_FIELD)).not.toContain("name");
    expect(new Set(Object.values(EDITOR_SECTION_BY_FIELD))).toEqual(
      new Set(["security", "network", "terminal", "meta"])
    );
  });
});
