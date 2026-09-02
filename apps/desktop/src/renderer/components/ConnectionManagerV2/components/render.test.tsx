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
import { SshKeyPane } from "./SshKeyPane";
import { ConnectionTable } from "./ConnectionTable";
import { ConnectionGrid } from "./ConnectionGrid";
import { GridPathBar } from "./GridPathBar";
import { DetailCard } from "./DetailCard";
import { ManagerToolbar } from "./ManagerToolbar";
import { BulkBar } from "./BulkBar";
import { ConnectionEditor } from "./ConnectionEditor";
import { ScopeBar } from "./ScopeBar";
import { buildConnectionRow } from "../utils/connectionRows";
import { buildConnectionTableRows } from "../utils/connectionTableRows";
import { buildGridSections } from "../utils/gridItems";
import { buildBreadcrumb } from "../utils/folderNavigation";
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
  const treeProps = {
    onSelectFolder: noop,
    onCreateFolder: noop,
    onRenameFolder: noop,
    onDeleteFolder: noop,
    onMoveFolder: noop,
    onReorderFolder: noop,
    onDropConnections: noop,
    onBatchBindAuth: noop,
    recentActive: false,
    onSelectRecent: noop
  };

  test("renders the scope root and the top level of the hierarchy", () => {
    const html = renderToStaticMarkup(
      <FolderTree
        {...treeProps}
        rootLabel="本地"
        folders={folders}
        connections={connections}
        currentFolderId="prod"
      />
    );
    // 根节点默认展开，顶层目录及其子树计数可见。
    expect(html).toContain("本地");
    expect(html).toContain("prod");
    expect(html).toContain("cm2-tree-count");
  });

  // D18：网格是钻取式的,「含子目录」开关随平铺视图一并废止,底部那条 footer 必须消失。
  test("no longer offers an include-subfolders toggle", () => {
    const html = renderToStaticMarkup(
      <FolderTree {...treeProps} rootLabel="本地" folders={folders} connections={connections} />
    );
    expect(html).not.toContain("含子目录");
  });

  test("renders an empty scope without crashing", () => {
    const html = renderToStaticMarkup(
      <FolderTree {...treeProps} rootLabel="本地" folders={[]} connections={[]} />
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
    const html = renderToStaticMarkup(
      <ProxyPane proxies={[]} onReload={() => Promise.resolve()} />
    );
    expect(html).toContain("当前作用域没有代理");
  });
});

describe("SshKeyPane", () => {
  test("renders the key list with its parsed metadata", () => {
    const html = renderToStaticMarkup(
      <SshKeyPane sshKeys={[key("k1", "deploy")]} onReload={() => Promise.resolve()} />
    );
    expect(html).toContain("deploy");
    expect(html).toContain("SHA256:abc");
    expect(html).toContain("新建 / 生成密钥");
  });

  test("renders the empty state", () => {
    const html = renderToStaticMarkup(
      <SshKeyPane sshKeys={[]} onReload={() => Promise.resolve()} />
    );
    expect(html).toContain("当前作用域没有密钥");
    expect(html).toContain("单击一行查看密钥详情");
  });
});

describe("ConnectionTable", () => {
  const keys = [key("k1", "deploy")];
  const connections = [
    connection({ id: "c1", name: "prod-db", authType: "privateKey", sshKeyId: "k1" }),
    connection({ id: "c2", name: "orphan", authType: "privateKey" })
  ];
  const tableProps = {
    columns: DEFAULT_CONNECTION_COLUMNS,
    sort: { key: "name", direction: "asc" } as const,
    onSortChange: noop,
    selectedIds: [],
    onSelectionChange: noop,
    onFocus: noop,
    onConnect: noop,
    onRowContextMenu: noop,
    onEnterFolder: noop,
    onFolderContextMenu: noop,
    onDropConnections: noop
  };

  test("renders rows and flags the unbound private-key row", () => {
    const rows = buildConnectionTableRows({
      rows: connections.map((item) => buildConnectionRow(item, keys)),
      folders: [],
      sort: tableProps.sort,
      flat: true
    });
    const html = renderToStaticMarkup(<ConnectionTable {...tableProps} rows={rows} />);
    expect(html).toContain("prod-db");
    expect(html).toContain("deploy");
    expect(html).toContain("未绑定");
  });

  test("renders the empty state", () => {
    const html = renderToStaticMarkup(<ConnectionTable {...tableProps} rows={[]} />);
    expect(html).toContain("此处没有连接");
  });
});

// D18：中栏换成 Finder 式图标网格。这些断言取代了原来的树形表格那几条(目录行、备注列、
// 默认展开)——它们描述的界面已经不存在了。
describe("ConnectionGrid", () => {
  const keys = [key("k1", "deploy")];
  const gridProps = {
    emptyText: "此目录为空",
    selectedIds: [],
    onFocus: noop,
    onToggleSelect: noop,
    onClearSelection: noop,
    onConnect: noop,
    onRowContextMenu: noop,
    onEnterFolder: noop,
    onFolderContextMenu: noop,
    onDropConnections: noop
  };

  const sectionsOf = (
    profiles: ConnectionProfile[],
    folderList: ConnectionFolder[],
    currentFolderId?: string
  ) =>
    buildGridSections({
      rows: profiles.map((item) => buildConnectionRow(item, keys)),
      folders: folderList,
      currentFolderId,
      searching: false,
      mode: "browse",
      sort: { key: "name", direction: "asc" }
    });

  test("根目录浏览态只渲染目录磁贴与根直属连接——最近连接是独立视图,不再混排", () => {
    const sections = sectionsOf(
      [
        connection({ id: "c1", name: "root-box" }),
        connection({
          id: "c2",
          name: "deep-box",
          folderId: "asia",
          lastConnectedAt: "2026-05-01T00:00:00.000Z"
        })
      ],
      [folder("asia", "asia")]
    );
    const html = renderToStaticMarkup(<ConnectionGrid {...gridProps} sections={sections} />);
    expect(html).toContain("cm2-tile--folder");
    expect(html).toContain("asia");
    // 目录磁贴带递归计数。
    expect(html).toContain("1 台");
    expect(html).toContain("root-box");
    // 子目录里的连接归目录磁贴,浏览态不铺开、也不混一段"最近"。
    expect(html).not.toContain("deep-box");
  });

  test("连接磁贴只显示 host,不带端口", () => {
    const sections = sectionsOf(
      [connection({ id: "c1", name: "gw", host: "10.1.2.3", port: 2222 })],
      []
    );
    const html = renderToStaticMarkup(<ConnectionGrid {...gridProps} sections={sections} />);
    expect(html).toContain("10.1.2.3");
    // 端口只在 hover 提示里给（D18）。
    expect(html).not.toContain("10.1.2.3:2222");
    expect(html).not.toContain("2222");
  });

  test("未绑定密钥的连接在图标上叠警告角标", () => {
    const sections = sectionsOf(
      [connection({ id: "c1", name: "orphan", authType: "privateKey" })],
      []
    );
    const html = renderToStaticMarkup(<ConnectionGrid {...gridProps} sections={sections} />);
    expect(html).toContain("cm2-tile-warn");
  });

  test("选中态落在磁贴本身上", () => {
    const sections = sectionsOf([connection({ id: "c1", name: "picked" })], []);
    const html = renderToStaticMarkup(
      <ConnectionGrid {...gridProps} sections={sections} selectedIds={["c1"]} focusedId="c1" />
    );
    expect(html).toContain("cm2-tile--selected");
    expect(html).toContain("cm2-tile--focused");
  });

  test("空目录给出空态文案", () => {
    const html = renderToStaticMarkup(<ConnectionGrid {...gridProps} sections={[]} />);
    expect(html).toContain("此目录为空");
  });
});

describe("GridPathBar", () => {
  const pathProps = {
    searching: false,
    recent: false,
    resultCount: 0,
    sort: { key: "name", direction: "asc" } as const,
    onSortChange: noop,
    onSelectFolder: noop
  };

  test("面包屑逐段可点,末段是当前目录", () => {
    const segments = buildBreadcrumb(
      "asia",
      [folder("prod", "prod"), folder("asia", "asia", "prod")],
      "本地"
    );
    const html = renderToStaticMarkup(<GridPathBar {...pathProps} segments={segments} />);
    expect(html).toContain("本地");
    expect(html).toContain("prod");
    expect(html).toContain("cm2-pathbar-link--current");
    expect(html.indexOf("本地")).toBeLessThan(html.indexOf("asia"));
  });

  test("搜索中用命中数取代面包屑", () => {
    const html = renderToStaticMarkup(
      <GridPathBar
        {...pathProps}
        segments={buildBreadcrumb(undefined, [], "本地")}
        searching
        resultCount={12}
      />
    );
    expect(html).toContain("搜索结果 12 台");
    expect(html).not.toContain("cm2-pathbar-link");
  });

  test("排序收在路径栏右侧", () => {
    const html = renderToStaticMarkup(
      <GridPathBar {...pathProps} segments={buildBreadcrumb(undefined, [], "本地")} />
    );
    expect(html).toContain("cm2-pathbar-sort");
    expect(html).toContain("名称");
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

  test("footer offers save-and-connect alongside save and cancel", () => {
    const html = renderToStaticMarkup(
      <ConnectionEditor
        connection={connection({ id: "c1", name: "prod-db" })}
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
    expect(html).toContain("保存并连接");
    // antd 会给两字按钮插一个空格，这里按松散匹配断言。
    expect(html).toMatch(/<span>保\s*存<\/span>/);
    expect(html).toMatch(/<span>取\s*消<\/span>/);
    // 回车提交仍要落在「只保存」上，所以原生 submit 按钮必须还在。
    expect(html).toContain('type="submit"');
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
    expect(html).toContain("查看明文密码");

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
    expect(forNew).not.toContain("查看明文密码");
  });
});

describe("toolbars", () => {
  const toolbarProps = {
    keyword: "",
    onKeywordChange: noop,
    onNewConnection: noop,
    onOpenLocalTerminal: noop,
    onImportNextShellFile: noop,
    onImportNextShellDirectory: noop,
    onImportFinalShellFile: noop,
    onImportFinalShellDirectory: noop,
    onExportAllToFile: noop,
    onExportAllToDirectory: noop,
    onOpenCloudSync: noop,
    onOpenRecycleBin: noop
  };

  test("ManagerToolbar renders every global command plus the search box", () => {
    const html = renderToStaticMarkup(<ManagerToolbar {...toolbarProps} />);
    expect(html).toContain("新建连接");
    expect(html).toContain("导入");
    expect(html).toContain("导出全部");
    expect(html).toContain("搜索整个作用域");
  });

  // D18：网格没有列的概念，「列…」下拉必须一起消失，否则点开是个对界面毫无作用的菜单。
  test("ManagerToolbar no longer offers the column picker", () => {
    const html = renderToStaticMarkup(<ManagerToolbar {...toolbarProps} />);
    expect(html).not.toContain("列设置");
  });

  // A1：删掉旧管理器后全应用没有任何「开一个本地终端」的入口，这条断言就是那次回归的守门人。
  test("ManagerToolbar keeps a local-terminal entry", () => {
    const html = renderToStaticMarkup(<ManagerToolbar {...toolbarProps} />);
    expect(html).toContain("本地终端");
  });

  // C4：云同步/回收站迁进设置中心后必须有跳转，否则用户「找不到回收站」。
  test("ManagerToolbar exposes a more menu for the settings deep links", () => {
    const html = renderToStaticMarkup(<ManagerToolbar {...toolbarProps} />);
    expect(html).toContain('aria-label="更多"');
  });

  // 云作用域下导入执行链路只写本地，入口必须整体禁用而不是静默落本地。
  test("ManagerToolbar disables the import entry when the scope cannot be imported into", () => {
    const html = renderToStaticMarkup(
      <ManagerToolbar
        {...toolbarProps}
        importDisabled
        importDisabledReason="导入目前仅支持本地作用域，请先切换"
      />
    );
    expect(html).toContain("disabled");

    const enabled = renderToStaticMarkup(<ManagerToolbar {...toolbarProps} />);
    // 工具条上没有任何天生禁用的控件，所以未禁用时整条工具条不应出现 disabled 属性。
    expect(enabled).not.toContain("disabled");
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
