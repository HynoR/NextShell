import { describe, expect, test } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import type {
  ConnectionFolder,
  ConnectionProfile,
  ProxyProfile,
  SshKeyProfile
} from "@nextshell/core";
import { FolderTree } from "./FolderTree";
import { ProxyPane } from "./ProxyPane";
import { ConnectionTable } from "./ConnectionTable";
import { DetailCard } from "./DetailCard";
import { ManagerToolbar } from "./ManagerToolbar";
import { BulkBar } from "./BulkBar";
import { ConnectionEditor } from "./ConnectionEditor";
import { ScopeBar } from "./ScopeBar";
import { buildConnectionRow } from "../utils/connectionRows";
import { buildManagerScopes, LOCAL_SCOPE } from "../utils/scopes";
import { DEFAULT_CONNECTION_COLUMNS } from "../types";

// 这些面板没有 portal，可以直接静态渲染。目的不是断言像素，而是保证整棵树在真实 props 下
// 不会在 render 期抛异常——渲染层崩溃在过去是完全无痕的。

const folder = (id: string, name: string, parentId?: string): ConnectionFolder => ({
  id,
  scopeKey: "local-default",
  parentId,
  name,
  sortIndex: 0,
  createdAt: "2026-01-01T00:00:00.000Z",
  updatedAt: "2026-01-01T00:00:00.000Z"
});

const connection = (patch: Partial<ConnectionProfile> & { id: string }): ConnectionProfile =>
  ({
    name: patch.id,
    host: "10.0.0.1",
    port: 22,
    username: "root",
    authType: "password",
    strictHostKeyChecking: false,
    groupPath: "/server",
    tags: [],
    favorite: false,
    monitorSession: false,
    terminalEncoding: "utf-8",
    backspaceMode: "ascii-backspace",
    deleteMode: "vt220-delete",
    createdAt: "2026-01-01T00:00:00.000Z",
    updatedAt: "2026-01-01T00:00:00.000Z",
    ...patch
  }) as ConnectionProfile;

const key = (id: string, name: string): SshKeyProfile =>
  ({
    id,
    name,
    keyContentRef: "secret://x",
    keyType: "ssh-ed25519",
    fingerprint: "SHA256:abc",
    createdAt: "2026-01-01T00:00:00.000Z",
    updatedAt: "2026-01-01T00:00:00.000Z"
  }) as SshKeyProfile;

const noop = () => undefined;

describe("FolderTree", () => {
  const folders = [folder("prod", "prod"), folder("asia", "asia", "prod")];
  const connections = [connection({ id: "c1", folderId: "asia" })];

  test("renders the scope root and the top level of the hierarchy", () => {
    const html = renderToStaticMarkup(
      <FolderTree
        rootLabel="本地"
        folders={folders}
        connections={connections}
        currentFolderId="prod"
        onSelectFolder={noop}
        onCreateFolder={noop}
        onRenameFolder={noop}
        onDeleteFolder={noop}
        onMoveFolder={noop}
      />
    );
    // 根节点默认展开，顶层目录及其子树计数可见。
    expect(html).toContain("本地");
    expect(html).toContain("prod");
    expect(html).toContain("cm2-tree-count");
  });

  test("renders an empty scope without crashing", () => {
    const html = renderToStaticMarkup(
      <FolderTree
        rootLabel="本地"
        folders={[]}
        connections={[]}
        onSelectFolder={noop}
        onCreateFolder={noop}
        onRenameFolder={noop}
        onDeleteFolder={noop}
        onMoveFolder={noop}
      />
    );
    expect(html).toContain("本地");
  });
});

describe("ProxyPane", () => {
  const proxy: ProxyProfile = {
    id: "p1",
    name: "office",
    proxyType: "socks5",
    host: "127.0.0.1",
    port: 1080,
    username: "u",
    createdAt: "2026-01-01T00:00:00.000Z",
    updatedAt: "2026-01-01T00:00:00.000Z"
  } as ProxyProfile;

  test("renders the proxy list", () => {
    const html = renderToStaticMarkup(
      <ProxyPane proxies={[proxy]} onReload={() => Promise.resolve()} />
    );
    expect(html).toContain("office");
    expect(html).toContain("SOCKS5");
    expect(html).toContain("127.0.0.1:1080");
    expect(html).toContain("新建代理");
  });

  test("renders the empty state", () => {
    const html = renderToStaticMarkup(<ProxyPane proxies={[]} onReload={() => Promise.resolve()} />);
    expect(html).toContain("当前作用域没有代理");
  });
});

describe("ConnectionTable", () => {
  const keys = [key("k1", "deploy")];
  const rows = [
    connection({ id: "c1", name: "prod-db", authType: "privateKey", sshKeyId: "k1" }),
    connection({ id: "c2", name: "orphan", authType: "privateKey" })
  ].map((item) => buildConnectionRow(item, keys));

  test("renders rows and flags the unbound private-key row", () => {
    const html = renderToStaticMarkup(
      <ConnectionTable
        rows={rows}
        columns={DEFAULT_CONNECTION_COLUMNS}
        sort={{ key: "name", direction: "asc" }}
        onSortChange={noop}
        selectedIds={[]}
        onSelectionChange={noop}
        onFocus={noop}
        onConnect={noop}
        onRowContextMenu={noop}
      />
    );
    expect(html).toContain("prod-db");
    expect(html).toContain("deploy");
    expect(html).toContain("未绑定");
  });

  test("renders the empty state", () => {
    const html = renderToStaticMarkup(
      <ConnectionTable
        rows={[]}
        columns={DEFAULT_CONNECTION_COLUMNS}
        sort={{ key: "name", direction: "asc" }}
        onSortChange={noop}
        selectedIds={[]}
        onSelectionChange={noop}
        onFocus={noop}
        onConnect={noop}
        onRowContextMenu={noop}
      />
    );
    expect(html).toContain("此处没有连接");
  });
});

describe("DetailCard", () => {
  test("shows the bound key and its fingerprint", () => {
    const html = renderToStaticMarkup(
      <DetailCard
        connection={connection({
          id: "c1",
          name: "prod-db",
          authType: "privateKey",
          sshKeyId: "k1"
        })}
        sshKeys={[key("k1", "deploy")]}
        folderLabel="本地 / prod"
        onEdit={noop}
        onConnect={noop}
      />
    );
    expect(html).toContain("prod-db");
    expect(html).toContain("deploy");
    expect(html).toContain("SHA256:abc");
    expect(html).toContain("从未连接");
  });

  test("renders a connection with no key, tags or notes", () => {
    const html = renderToStaticMarkup(
      <DetailCard
        connection={connection({ id: "c1" })}
        sshKeys={[]}
        folderLabel="本地"
        onEdit={noop}
        onConnect={noop}
      />
    );
    expect(html).toContain("密码");
  });
});

describe("ConnectionEditor", () => {
  test("renders the always-visible fields plus the collapsed sections", () => {
    const html = renderToStaticMarkup(
      <ConnectionEditor
        connection={connection({ id: "c1", name: "prod-db" })}
        folders={[folder("prod", "prod")]}
        sshKeys={[]}
        proxies={[]}
        saving={false}
        revealingPassword={false}
        onRevealPassword={noop}
        onSubmit={noop}
        onCancel={noop}
        onCreateKey={noop}
      />
    );
    expect(html).toContain("主机");
    expect(html).toContain("认证方式");
    expect(html).toContain("目录");
    // 四个折叠区取代旧表单的四个 tab
    expect(html).toContain("安全");
    expect(html).toContain("网络");
    expect(html).toContain("终端");
  });

  test("offers to reveal a stored password only when editing password auth", () => {
    const html = renderToStaticMarkup(
      <ConnectionEditor
        connection={connection({ id: "c1", authType: "password" })}
        folders={[]}
        sshKeys={[]}
        proxies={[]}
        saving={false}
        revealingPassword={false}
        onRevealPassword={noop}
        onSubmit={noop}
        onCancel={noop}
        onCreateKey={noop}
      />
    );
    expect(html).toContain("输入主密码查看");

    const forNew = renderToStaticMarkup(
      <ConnectionEditor
        folders={[]}
        sshKeys={[]}
        proxies={[]}
        saving={false}
        revealingPassword={false}
        onRevealPassword={noop}
        onSubmit={noop}
        onCancel={noop}
        onCreateKey={noop}
      />
    );
    expect(forNew).not.toContain("输入主密码查看");
  });
});

describe("toolbars", () => {
  test("ManagerToolbar renders every global command plus the search box", () => {
    const html = renderToStaticMarkup(
      <ManagerToolbar
        keyword=""
        onKeywordChange={noop}
        columns={DEFAULT_CONNECTION_COLUMNS}
        onColumnsChange={noop}
        onNewConnection={noop}
        onImport={noop}
        onExportAll={noop}
      />
    );
    expect(html).toContain("新建连接");
    expect(html).toContain("导入");
    expect(html).toContain("导出全部");
    expect(html).toContain("搜索整个作用域");
  });

  test("BulkBar surfaces batch auth binding", () => {
    const html = renderToStaticMarkup(
      <BulkBar
        count={3}
        onClear={noop}
        onBindAuth={noop}
        onCopyToScope={noop}
        onExport={noop}
        onDelete={noop}
      />
    );
    expect(html).toContain("已选 3");
    expect(html).toContain("绑定认证");
    expect(html).toContain("复制到作用域");
  });

  test("ScopeBar lists local plus every workspace", () => {
    const scopes = buildManagerScopes([
      {
        id: "ws-1",
        apiBaseUrl: "https://sync.example.com",
        workspaceName: "team-a",
        displayName: "Team A",
        pullIntervalSec: 60,
        ignoreTlsErrors: false,
        enabled: true,
        createdAt: "2026-01-01T00:00:00.000Z",
        updatedAt: "2026-01-01T00:00:00.000Z",
        lastSyncAt: null,
        lastError: null
      }
    ]);
    const html = renderToStaticMarkup(
      <ScopeBar
        scopes={scopes}
        activeScope={LOCAL_SCOPE}
        onSelectScope={noop}
        resourceTab="connections"
        onSelectResource={noop}
      />
    );
    expect(html).toContain("作用域");
    expect(html).toContain("本地");
  });
});
