import type { ConnectionProfile } from "@nextshell/core";
import type { ConnectionUpsertInput } from "@nextshell/shared";
import { CONNECTION_EDITOR_DEFAULT_VALUES } from "../components/ConnectionManagerV2/components/ConnectionEditor";

export const DEFAULT_QUICK_CONNECT_PORT = 22;

export interface QuickConnectAddress {
  username: string;
  host: string;
  port: number;
}

export type QuickConnectParseResult =
  { ok: true; value: QuickConnectAddress } | { ok: false; message: string };

export const parseQuickConnectInput = (raw: string): QuickConnectParseResult => {
  const text = raw.trim();
  if (!text) {
    return { ok: false, message: "请输入连接地址，格式：username@host[:port]。" };
  }

  try {
    const url = new URL(text.startsWith("ssh://") ? text : `ssh://${text}`);
    const username = decodeURIComponent(url.username);
    const host = url.hostname.replace(/^\[(.*)\]$/, "$1");
    const port = url.port ? Number(url.port) : DEFAULT_QUICK_CONNECT_PORT;
    if (!username || !host) {
      return { ok: false, message: "格式错误，应为 username@host[:port]。" };
    }
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      return { ok: false, message: "端口必须是 1-65535。" };
    }
    return { ok: true, value: { username, host, port } };
  } catch {
    return { ok: false, message: "格式错误，应为 username@host[:port]。" };
  }
};

export const findExistingByAddress = (
  connections: ConnectionProfile[],
  target: QuickConnectAddress
): ConnectionProfile | undefined => {
  const expectedUsername = target.username.trim();
  const expectedHost = target.host.trim().toLowerCase();

  return connections.find(
    (connection) =>
      connection.username.trim() === expectedUsername &&
      connection.host.trim().toLowerCase() === expectedHost &&
      connection.port === target.port
  );
};

export const buildQuickConnectUpsertInput = (
  target: QuickConnectAddress
): ConnectionUpsertInput => ({
  ...CONNECTION_EDITOR_DEFAULT_VALUES,
  name: `${target.username}@${target.host}`,
  host: target.host,
  port: target.port,
  username: target.username,
  groupPath: "/server"
});
