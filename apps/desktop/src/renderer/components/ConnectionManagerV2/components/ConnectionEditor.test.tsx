import { describe, expect, test } from "vitest";
import {
  EDITOR_SECTION_BY_FIELD,
  resolveEditorErrorSection,
  withConnectionId
} from "./ConnectionEditor";

describe("resolveEditorErrorSection", () => {
  test("把折叠区内的出错字段映射到它所在的区块", () => {
    expect(resolveEditorErrorSection([{ name: ["keepAliveIntervalSec"] }])).toBe("network");
    expect(resolveEditorErrorSection([{ name: ["hostFingerprint"] }])).toBe("security");
    expect(resolveEditorErrorSection([{ name: ["deleteMode"] }])).toBe("terminal");
    expect(resolveEditorErrorSection([{ name: ["tags"] }])).toBe("meta");
  });

  test("只看第一个出错字段——展开一个区块再滚过去，多开只会晃眼", () => {
    expect(resolveEditorErrorSection([{ name: ["proxyId"] }, { name: ["hostFingerprint"] }])).toBe(
      "network"
    );
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

describe("withConnectionId", () => {
  test("编辑态补回未注册的当前连接 id", () => {
    const values = { name: "renamed" } as Parameters<typeof withConnectionId>[0];

    expect(withConnectionId(values, "11111111-1111-4111-8111-111111111111").id).toBe(
      "11111111-1111-4111-8111-111111111111"
    );
  });

  test("新建或已有 id 时不覆盖表单中的 id", () => {
    const id = "22222222-2222-4222-8222-222222222222";
    const values = { id } as Parameters<typeof withConnectionId>[0];

    expect(withConnectionId(values, "11111111-1111-4111-8111-111111111111").id).toBe(id);
    expect(withConnectionId(values).id).toBe(id);
  });
});
