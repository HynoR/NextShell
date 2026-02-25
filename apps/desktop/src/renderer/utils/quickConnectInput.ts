import type { ConnectionProfile } from "@nextshell/core";
import type { ConnectionUpsertInput } from "@nextshell/shared";

export const DEFAULT_QUICK_CONNECT_PORT = 22;

export interface QuickConnectAddress {
  username: string;
  host: string;
  port: number;
}

export type QuickConnectParseErrorCode =
  | "empty"
  | "format"
  | "username"
  | "host"
  | "port";

export type QuickConnectParseResult =
  | { ok: true; value: QuickConnectAddress }
  | { ok: false; code: QuickConnectParseErrorCode; message: string };

const parseError = (
  code: QuickConnectParseErrorCode,
  message: string
): QuickConnectParseResult => ({
  ok: false,
  code,
  message
});

export const parseQuickConnectInput = (raw: string): QuickConnectParseResult => {
  const text = raw.trim();
  if (!text) {
    return parseError("empty", "请输入连接地址，格式：username@host[:port]。");
  }

  if (text.startsWith("ssh://")) {
    return parseError("format", "暂不支持 ssh:// 前缀，请输入 username@host[:port]。");
  }

  const atIndex = text.indexOf("@");
  if (atIndex <= 0 || atIndex !== text.lastIndexOf("@") || atIndex === text.length - 1) {
    return parseError("format", "格式错误，应为 username@host[:port]。");
  }

  const username = text.slice(0, atIndex).trim();
  const hostPort = text.slice(atIndex + 1).trim();

  if (!username || /\s/.test(username)) {
    return parseError("username", "用户名不能为空且不能包含空格。");
  }

  if (!hostPort) {
    return parseError("host", "主机不能为空。");
  }

  let host = hostPort;
  let port = DEFAULT_QUICK_CONNECT_PORT;

  const colonIndex = hostPort.indexOf(":");
  if (colonIndex >= 0) {
    if (hostPort.indexOf(":", colonIndex + 1) >= 0) {
      return parseError("format", "暂不支持 IPv6 地址，请使用 hostname 或 IPv4。");
    }

    host = hostPort.slice(0, colonIndex).trim();
    const rawPort = hostPort.slice(colonIndex + 1).trim();

    if (!rawPort) {
      return parseError("port", "端口不能为空。");
    }

    if (!/^\d+$/.test(rawPort)) {
      return parseError("port", "端口必须是 1-65535 的数字。");
    }

    port = Number(rawPort);
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      return parseError("port", "端口必须是 1-65535。");
    }
  }

  if (!host || /\s/.test(host)) {
    return parseError("host", "主机不能为空且不能包含空格。");
  }

  return {
    ok: true,
    value: {
      username,
      host,
      port
    }
  };
};

export const findExistingByAddress = (
  connections: ConnectionProfile[],
  target: QuickConnectAddress
): ConnectionProfile | undefined => {
  const expectedUsername = target.username.trim();
  const expectedHost = target.host.trim().toLowerCase();

  return connections.find((connection) =>
    connection.username.trim() === expectedUsername &&
    connection.host.trim().toLowerCase() === expectedHost &&
    connection.port === target.port
  );
};

export const buildQuickConnectUpsertInput = (
  target: QuickConnectAddress
): ConnectionUpsertInput => ({
  name: `${target.username}@${target.host}`,
  host: target.host,
  port: target.port,
  username: target.username,
  authType: "password",
  strictHostKeyChecking: false,
  portForwards: [],
  terminalEncoding: "utf-8",
  backspaceMode: "ascii-backspace",
  deleteMode: "vt220-delete",
  groupPath: "/server",
  tags: [],
  favorite: false,
  monitorSession: true
});
