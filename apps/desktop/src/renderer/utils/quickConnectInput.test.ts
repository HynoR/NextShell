import { describe, expect, it } from "vitest";
import { parseQuickConnectInput } from "./quickConnectInput";

describe("parseQuickConnectInput", () => {
  it("解析 user@host，默认端口 22", () => {
    expect(parseQuickConnectInput("root@example.com")).toEqual({
      ok: true,
      value: { username: "root", host: "example.com", port: 22 }
    });
  });

  it("解析 user@host:2222", () => {
    expect(parseQuickConnectInput("deploy@192.168.1.10:2222")).toEqual({
      ok: true,
      value: { username: "deploy", host: "192.168.1.10", port: 2222 }
    });
  });

  it("解析 user@[::1]:22（IPv6）", () => {
    expect(parseQuickConnectInput("root@[::1]:22")).toEqual({
      ok: true,
      value: { username: "root", host: "::1", port: 22 }
    });
  });

  it("拒绝空输入与缺用户名的输入", () => {
    expect(parseQuickConnectInput("")).toMatchObject({ ok: false });
    expect(parseQuickConnectInput("example.com")).toMatchObject({ ok: false });
  });
});
