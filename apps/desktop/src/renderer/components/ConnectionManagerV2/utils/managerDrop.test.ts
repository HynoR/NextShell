import { describe, expect, test } from "vitest";
import {
  CONNECTION_DRAG_MIME,
  canAcceptManagerFileDrop,
  describeManagerDropWarning,
  isInternalConnectionDrag,
  parseConnectionDragIds,
  serializeConnectionDragIds,
  shouldInterceptFileDrag
} from "./managerDrop";
import { isExternalFileDrag } from "../../../utils/sftpFileDrop";

const base = {
  open: true,
  resourceTab: "connections" as const,
  importingPreview: false,
  scopeKind: "local" as const
};

describe("canAcceptManagerFileDrop", () => {
  test("accepts a drop on the connections tab of the local scope", () => {
    expect(canAcceptManagerFileDrop(base)).toBe(true);
  });

  test("rejects while the manager is closed", () => {
    expect(canAcceptManagerFileDrop({ ...base, open: false })).toBe(false);
  });

  test("rejects on the key and proxy tabs", () => {
    expect(canAcceptManagerFileDrop({ ...base, resourceTab: "keys" })).toBe(false);
    expect(canAcceptManagerFileDrop({ ...base, resourceTab: "proxies" })).toBe(false);
  });

  test("rejects while a preview is still loading", () => {
    expect(canAcceptManagerFileDrop({ ...base, importingPreview: true })).toBe(false);
  });

  // 导入执行链路只写本地：云作用域下接受拖入会静默把连接落到本地。
  test("rejects in a cloud scope", () => {
    expect(canAcceptManagerFileDrop({ ...base, scopeKind: "cloud" })).toBe(false);
  });
});

describe("内部拖拽与文件拖入互不干扰", () => {
  const internalDrag = { types: [CONNECTION_DRAG_MIME], items: [], files: [] };
  const fileDrag = {
    types: ["Files"],
    items: [{ kind: "file", getAsFile: () => ({ name: "a.json", path: "/tmp/a.json" }) }],
    files: []
  };

  // 拖连接行时 `.cm2-shell` 上的导入遮罩绝不能亮:它会盖住整张表，落点全部失灵。
  test("拖连接行不会被当成文件拖入", () => {
    expect(isExternalFileDrag(internalDrag)).toBe(false);
    expect(isInternalConnectionDrag(internalDrag)).toBe(true);
  });

  test("拖 JSON 文件不会被当成连接拖拽", () => {
    expect(isInternalConnectionDrag(fileDrag)).toBe(false);
    expect(isExternalFileDrag(fileDrag)).toBe(true);
  });

  test("没有 dataTransfer 时两边都判否", () => {
    expect(isInternalConnectionDrag(null)).toBe(false);
    expect(isInternalConnectionDrag({})).toBe(false);
  });
});

describe("shouldInterceptFileDrag", () => {
  const fileDrag = {
    types: ["Files"],
    items: [{ kind: "file", getAsFile: () => ({ name: "a.json", path: "/tmp/a.json" }) }],
    files: []
  };
  const connectionDrag = { types: [CONNECTION_DRAG_MIME], items: [], files: [] };
  // rc-tree 拖目录节点时只写 text/plain，不带 Files。
  const folderNodeDrag = { types: ["text/plain"], items: [], files: [] };

  // 松在目录节点上时 rc-tree 会先吞掉事件，所以文件拖入必须在捕获相截走。
  test("文件拖入且当前能接收时截走", () => {
    expect(shouldInterceptFileDrag(true, fileDrag)).toBe(true);
  });

  test("当前不能接收（云作用域、密钥分段、预览中）时不截——照旧交给下层", () => {
    expect(shouldInterceptFileDrag(false, fileDrag)).toBe(false);
  });

  // 截了这两类就等于把落点拿掉了：连接拖不进目录，目录也拖不动。
  test("内部连接拖拽与目录节点拖拽一概不截", () => {
    expect(shouldInterceptFileDrag(true, connectionDrag)).toBe(false);
    expect(shouldInterceptFileDrag(true, folderNodeDrag)).toBe(false);
  });

  test("没有 dataTransfer 时不截", () => {
    expect(shouldInterceptFileDrag(true, null)).toBe(false);
  });
});

describe("连接拖拽负载", () => {
  test("往返序列化保持 id 顺序", () => {
    expect(parseConnectionDragIds(serializeConnectionDragIds(["a", "b"]))).toEqual(["a", "b"]);
  });

  test("空负载、坏 JSON、非数组一律当成没有连接", () => {
    expect(parseConnectionDragIds("")).toEqual([]);
    expect(parseConnectionDragIds(null)).toEqual([]);
    expect(parseConnectionDragIds("{oops")).toEqual([]);
    expect(parseConnectionDragIds('{"a":1}')).toEqual([]);
  });

  test("过滤掉数组里的非字符串项", () => {
    expect(parseConnectionDragIds('["a",1,null,"","b"]')).toEqual(["a", "b"]);
  });
});

describe("describeManagerDropWarning", () => {
  test("points at the import button when no path could be read", () => {
    expect(describeManagerDropWarning({ allPathsEmpty: true })).toContain("导入");
  });

  test("explains that only files are supported otherwise", () => {
    expect(describeManagerDropWarning({ allPathsEmpty: false })).toBe("当前仅支持拖入文件");
  });
});
