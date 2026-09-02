import { useCallback, useDeferredValue, useEffect, useMemo, useRef, useState } from "react";
import type { DragEvent as ReactDragEvent, KeyboardEvent, MouseEvent } from "react";
import { App as AntdApp, Modal } from "antd";
import type { ConnectionFolder, ConnectionProfile, ProxyProfile, SshKeyProfile } from "@nextshell/core";
import { formatErrorMessage } from "../../utils/errorMessage";
import { promptModal } from "../../utils/promptModal";
import { usePreferencesStore } from "../../store/usePreferencesStore";
import { ScopeBar } from "./components/ScopeBar";
import { FolderTree } from "./components/FolderTree";
import { ConnectionGrid, type ConnectionGridHandle } from "./components/ConnectionGrid";
import { GridPathBar } from "./components/GridPathBar";
import { DetailCard } from "./components/DetailCard";
import {
  ConnectionEditor,
  type ConnectionEditorHandle,
  type ConnectionEditorSubmitIntent,
  type ConnectionEditorValues
} from "./components/ConnectionEditor";
import { ManagerToolbar } from "./components/ManagerToolbar";
import { BulkBar } from "./components/BulkBar";
import { PointerMenu, type PointerMenuItem } from "./components/PointerMenu";
import { CopyToScopeModal } from "./components/CopyToScopeModal";
import { SshKeyPane } from "./components/SshKeyPane";
import { InlineSshKeyModal } from "./components/InlineSshKeyModal";
import { ProxyPane } from "./components/ProxyPane";
import { describeAffected, planRowCommands } from "./utils/rowCommands";
import { useConnectionExportActions } from "./hooks/useConnectionExportActions";
import { useConnectionImportFlow } from "./hooks/useConnectionImportFlow";
import { useConnectionPasswordReveal } from "./hooks/useConnectionPasswordReveal";
import { ConnectionBatchAuthModal } from "./components/ConnectionBatchAuthModal";
import { ConnectionImportModal } from "../ConnectionImportModal";
// 只借类型（`import type` 会被完全擦除），不会把设置中心拖进管理器这个懒加载 chunk。
import type { SettingsSection } from "../settings-center/types";

import { useManagerScope } from "./hooks/useManagerScope";
import { buildBreadcrumb, collectDescendantIds } from "./utils/folderNavigation";
import { buildConnectionRow, filterConnectionRows } from "./utils/connectionRows";
import { buildGridSections, collectGridConnectionIds, type GridViewMode } from "./utils/gridItems";
import {
  describeMoveOutcome,
  planConnectionMove,
  profileToUpsertPayload
} from "./utils/connectionMove";
import { planSiblingReorder } from "./utils/folderTree";
import { resolveOriginScopeKey, resourceMatchesOriginScope } from "@nextshell/shared";
import { clampDialogSize, fitDialogToViewport, resolveDialogResize } from "./utils/dialogSize";
import {
  affectsDetailConnection,
  detailConnectionId,
  resolveCancelIntent,
  shouldConfirmDiscard
} from "./utils/editGuard";
import {
  canAcceptManagerFileDrop,
  describeManagerDropWarning,
  shouldInterceptFileDrag
} from "./utils/managerDrop";
import { extractDroppedFilePaths } from "../../utils/sftpFileDrop";
import {
  type BatchAuthTarget,
  type ConnectionSort,
  type DetailMode,
  type ResourceTab
} from "./types";
import "./connection-manager-v2.css";

interface ConnectionManagerV2Props {
  open: boolean;
  connections: ConnectionProfile[];
  sshKeys: SshKeyProfile[];
  proxies: ProxyProfile[];
  /** 从外部(会话树/标签页)定位到某一条连接:切作用域、进目录、选中并展开到它。 */
  focusConnectionId?: string;
  /** 从外部(⌘K 面板「添加新服务器」)打开时直接进入新建态。 */
  initialAction?: "create";
  onClose: () => void;
  onConnectConnection: (connectionId: string) => Promise<void>;
  /** 打开一个本机 shell(A1)。调用方负责关掉管理器——它持有管理器的开关状态。 */
  onOpenLocalTerminal: () => void;
  /** 关掉管理器并把设置中心停在指定节(C4):云同步与回收站住在那边。 */
  onOpenSettingsSection: (section: SettingsSection) => void;
  onReloadConnections: () => Promise<void>;
  onReloadSshKeys: () => Promise<void>;
  onReloadProxies: () => Promise<void>;
}

const useViewport = (): { width: number; height: number } => {
  const [viewport, setViewport] = useState(() => ({
    width: window.innerWidth,
    height: window.innerHeight
  }));
  useEffect(() => {
    const onResize = () => setViewport({ width: window.innerWidth, height: window.innerHeight });
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);
  return viewport;
};

/**
 * 连接管理器。结构:作用域(本地/某个云 workspace)是最高层隔离,其下按资源分连接/密钥/代理
 * 三个态。连接态是 目录树|表格|详情 三栏;密钥与代理态是 列表|详情 两栏。
 * 云同步与回收站不在这里——它们是全局配置,归设置中心。
 */
export const ConnectionManagerV2 = ({
  open,
  connections,
  sshKeys,
  proxies,
  focusConnectionId,
  initialAction,
  onClose,
  onConnectConnection,
  onOpenLocalTerminal,
  onOpenSettingsSection,
  onReloadConnections,
  onReloadSshKeys,
  onReloadProxies
}: ConnectionManagerV2Props) => {
  const { message, modal } = AntdApp.useApp();
  const preferences = usePreferencesStore((state) => state.preferences.connectionManager);
  const updatePreferences = usePreferencesStore((state) => state.updatePreferences);
  const viewport = useViewport();

  const [resourceTab, setResourceTab] = useState<ResourceTab>("connections");
  const [keyword, setKeyword] = useState("");
  const [sort, setSort] = useState<ConnectionSort>({ key: "name", direction: "asc" });
  const [selectedIds, setSelectedIds] = useState<string[]>([]);
  const [detail, setDetail] = useState<DetailMode>({ kind: "empty" });
  const [saving, setSaving] = useState(false);
  const [contextMenu, setContextMenu] = useState<{
    x: number;
    y: number;
    items: PointerMenuItem[];
  } | null>(null);
  const [copyTarget, setCopyTarget] = useState<string[] | null>(null);
  const [batchAuthTarget, setBatchAuthTarget] = useState<BatchAuthTarget | null>(null);
  const [inlineKeyOpen, setInlineKeyOpen] = useState(false);
  const editorRef = useRef<ConnectionEditorHandle | null>(null);
  const gridHandleRef = useRef<ConnectionGridHandle | null>(null);
  const editing = detail.kind === "edit";
  const editingConnection = detail.kind === "edit" ? detail.connection : undefined;

  // 编辑器是否有未保存改动。用 ref 而不是 state：守卫在事件回调里同步读它，state 会读到旧值。
  const dirtyRef = useRef(false);
  /**
   * 右栏形态的镜像。守卫是异步的（要等用户点确认框），等它 resolve 的这段时间里右栏可能
   * 已经被别的路径换掉了；拖拽回调同理，闭包里的 detail 可能是上一轮的。凡是"等一会儿再
   * 决定"的地方都读这个 ref，不读闭包里的 state。
   */
  const detailRef = useRef<DetailMode>(detail);
  detailRef.current = detail;
  // 保存中。同上：Esc 的处理要在事件回调里同步判断，读 state 会慢一拍。
  const savingRef = useRef(false);
  const startSaving = useCallback((next: boolean) => {
    savingRef.current = next;
    setSaving(next);
  }, []);
  /** 切换右栏形态的唯一入口——顺手复位脏标记，避免漏掉某条路径。 */
  const openDetail = useCallback((next: DetailMode) => {
    dirtyRef.current = false;
    detailRef.current = next;
    setDetail(next);
  }, []);

  /**
   * 离开编辑态前的统一守卫。非编辑态或没改过直接放行；改过则问一次。
   * 所有会让编辑器卸载/换目标的路径都必须先过它——否则填了一半的表单就静默没了。
   */
  const confirmDiscardEdits = useCallback(async (): Promise<boolean> => {
    if (!shouldConfirmDiscard(editing, dirtyRef.current)) {
      return true;
    }
    return new Promise<boolean>((resolve) => {
      modal.confirm({
        title: "放弃未保存的修改？",
        content: "当前编辑的连接还没保存，离开后这些修改会丢失。",
        okText: "放弃修改",
        cancelText: "继续编辑",
        okButtonProps: { danger: true },
        onOk: () => resolve(true),
        onCancel: () => resolve(false)
      });
    });
  }, [editing, modal]);

  const handleEditorDirtyChange = useCallback((dirty: boolean) => {
    dirtyRef.current = dirty;
  }, []);

  /** 编辑态退回只读详情（新建时没有可退回的连接，落到空态）。 */
  const leaveEdit = useCallback(() => {
    // 读 ref 而不是闭包：守卫 resolve 时右栏可能已经不是编辑态了（保存成功会切到空态），
    // 这时再"退回只读详情"等于把一张过期的卡片弹回来。
    const current = detailRef.current;
    if (current.kind !== "edit") {
      return;
    }
    openDetail(
      current.connection ? { kind: "view", connection: current.connection } : { kind: "empty" }
    );
  }, [openDetail]);

  /** 守卫通过后执行 action；用于「点了别的地方」这一类会顶掉编辑器的操作。 */
  const runGuarded = useCallback(
    (action: () => void) => {
      void confirmDiscardEdits().then((ok) => {
        if (ok) {
          action();
        }
      });
    },
    [confirmDiscardEdits]
  );

  /**
   * 改动某几条连接之前，先处理"其中一条正被编辑"的情况。编辑器里存的是打开那一刻的快照，
   * 保存时整份写回——不先离开编辑态的话，这次移动/重命名会在用户按保存时被静默回滚，
   * 而拖拽和右键菜单都不产生 click，右栏那套守卫根本不会自己触发。
   *
   * 返回 false = 用户选择继续编辑，调用方必须把整个操作放弃掉（不移动、不重命名）。
   */
  const leaveEditBeforeMutating = useCallback(
    async (affectedIds: readonly string[]): Promise<boolean> => {
      const current = detailRef.current;
      if (
        current.kind !== "edit" ||
        !affectsDetailConnection(current.connection?.id, affectedIds)
      ) {
        return true;
      }
      if (!(await confirmDiscardEdits())) {
        return false;
      }
      openDetail({ kind: "empty" });
      return true;
    },
    [confirmDiscardEdits, openDetail]
  );

  const {
    handleRevealConnectionPassword,
    revealedLoginPassword,
    revealingLoginPassword
  } = useConnectionPasswordReveal({
    activeAuthType: editingConnection?.authType,
    modal,
    message,
    primarySelectedId: editingConnection?.id,
    selectedConnection: editingConnection
  });

  const notifyError = useCallback((text: string) => message.error(text), [message]);
  const importFlow = useConnectionImportFlow({ modal, message, onConnectionsImported: onReloadConnections });
  const onOpenBatchAuth = useCallback(
    (connectionIds: string[]) => {
      setBatchAuthTarget({
        type: "connections",
        connectionIds,
        label: `选中的 ${connectionIds.length} 个连接`
      });
    },
    []
  );
  const scope = useManagerScope({ open, onError: notifyError });

  const dialogSize = useMemo(
    () =>
      fitDialogToViewport(
        { width: preferences.dialogWidth, height: preferences.dialogHeight },
        viewport
      ),
    [preferences.dialogHeight, preferences.dialogWidth, viewport]
  );

  // 只显示当前作用域的资源。作用域是隔离边界，跨域引用在这里结构上就不可能发生。
  const scopedConnections = useMemo(
    () => connections.filter((item) => resourceMatchesOriginScope(item, scope.activeScope.key)),
    [connections, scope.activeScope.key]
  );
  const scopedSshKeys = useMemo(
    () => sshKeys.filter((item) => resourceMatchesOriginScope(item, scope.activeScope.key)),
    [scope.activeScope.key, sshKeys]
  );
  const scopedProxies = useMemo(
    () => proxies.filter((item) => resourceMatchesOriginScope(item, scope.activeScope.key)),
    [proxies, scope.activeScope.key]
  );

  // 过滤管道读延后值,输入框仍受控于 keyword 本身:1000+ 连接时逐键重算整条管道会把
  // 输入卡成一顿一顿的,而搜索结果晚一帧出现没有人会察觉。
  const deferredKeyword = useDeferredValue(keyword);
  // 搜索时无视目录圈定,直接搜整个作用域——找一台机器时没人记得它在哪个目录里。
  const searching = deferredKeyword.trim().length > 0;

  /**
   * 派生与过滤分成两层 memo。派生要为每条连接反查一次密钥,只有连接/密钥真的变了才值得重做;
   * 搭在一起的话每敲一个字都会把整个作用域重新派生一遍。
   */
  const allRows = useMemo(
    () => scopedConnections.map((connection) => buildConnectionRow(connection, scopedSshKeys)),
    [scopedConnections, scopedSshKeys]
  );

  /** 作用域内的全部连接行(已按关键词过滤)。分层交给 `buildGridSections`。 */
  const rows = useMemo(
    () => filterConnectionRows(allRows, deferredKeyword),
    [allRows, deferredKeyword]
  );

  /**
   * 中栏的磁贴分段(D18)。根目录只出「顶层目录 + 根直属 + 最近 30」,所以打开管理器时的
   * 渲染量与连接总数无关——这正是上一轮"整个界面卡卡的"里最大的一块。
   */
  // 默认视图是「最近连接」本身,不是根目录——根下直属几百台时浏览态照样重,但那是用户
  // 主动点进去的;打开管理器的第一屏渲染量必须恒定(≤30 磁贴)。
  const [gridView, setGridView] = useState<GridViewMode>("recent");
  useEffect(() => {
    setGridView("recent");
  }, [open, scope.activeScope.key]);

  /** 左树/面包屑/磁贴钻取共用:任何目录导航都把视图切回浏览态。 */
  const handleBrowseFolder = useCallback(
    (folderId: string | undefined) => {
      setGridView("browse");
      scope.enterFolder(folderId);
    },
    [scope]
  );
  const handleSelectRecent = useCallback(() => setGridView("recent"), []);

  const sections = useMemo(
    () =>
      buildGridSections({
        rows,
        folders: scope.folders,
        currentFolderId: scope.currentFolderId,
        searching,
        mode: gridView,
        sort
      }),
    [gridView, rows, scope.currentFolderId, scope.folders, searching, sort]
  );

  /** 网格上真正露出来的连接 id。选中/详情的剪枝以它为准,不是"作用域里存在"。 */
  const gridConnectionIds = useMemo(() => collectGridConnectionIds(sections), [sections]);

  // 目录/作用域切换后，选中与详情里的连接可能已经不在网格上，留着会显示一张查不到的卡片。
  useEffect(() => {
    const visibleIds = new Set(gridConnectionIds);
    setSelectedIds((previous) => {
      const next = previous.filter((id) => visibleIds.has(id));
      // 没剪掉任何东西就把原数组原样交回去:`filter` 每次都产出新引用，React 认引用，
      // 于是这条对账每跑一次就白白重渲一遍中栏——而它跟着每次分段变化都要跑。
      return next.length === previous.length ? previous : next;
    });
    setDetail((previous) =>
      previous.kind === "view" && !visibleIds.has(previous.connection.id)
        ? { kind: "empty" }
        : previous
    );
  }, [gridConnectionIds]);

  const folderLabel = useMemo(() => {
    const target =
      detail.kind === "view" ? detail.connection.folderId : undefined;
    return buildBreadcrumb(target, scope.folders, scope.activeScope.label)
      .map((segment) => segment.label)
      .join(" / ");
  }, [detail, scope.activeScope.label, scope.folders]);

  /** 路径栏的面包屑:钻取式导航的回溯路径,每段可点。 */
  const pathSegments = useMemo(
    () => buildBreadcrumb(scope.currentFolderId, scope.folders, scope.activeScope.label),
    [scope.activeScope.label, scope.currentFolderId, scope.folders]
  );

  /** 开会话并关掉管理器。不带守卫——只给"已经确认过要走"的路径用。 */
  const connectAndClose = useCallback(
    (connectionId: string) => {
      void onConnectConnection(connectionId).then(onClose);
    },
    [onClose, onConnectConnection]
  );

  /**
   * 双击行、右键「连接」、详情卡的「连接」都会开会话并把管理器整个关掉——编辑器跟着卸载。
   * 所以和点遮罩关弹窗是同一类"离开"动作，必须过守卫，否则填了一半的表单会静默没掉。
   */
  const handleConnect = useCallback(
    (connectionId: string) => {
      runGuarded(() => connectAndClose(connectionId));
    },
    [connectAndClose, runGuarded]
  );

  /**
   * 本地终端(A1)与设置中心深链(C4)都会把管理器关掉,所以编辑态下要先过守卫——
   * 和点遮罩关弹窗是同一类"离开"动作。
   */
  const handleOpenLocalTerminal = useCallback(() => {
    runGuarded(onOpenLocalTerminal);
  }, [onOpenLocalTerminal, runGuarded]);

  const handleOpenSettingsSection = useCallback(
    (section: SettingsSection) => {
      runGuarded(() => onOpenSettingsSection(section));
    },
    [onOpenSettingsSection, runGuarded]
  );

  const handleSubmit = useCallback(
    async (values: ConnectionEditorValues, intent: ConnectionEditorSubmitIntent) => {
      startSaving(true);
      try {
        const host = (values.host ?? "").trim();
        const saved = await window.nextshell.connection.upsert({
          ...values,
          name: (values.name ?? "").trim() || `${host}:${values.port}`,
          host,
          username: (values.username ?? "").trim(),
          workspaceId: scope.activeScope.workspaceId,
          // 下拉清空 = 移到顶层。传 undefined 会被主进程当成"没提到目录"从而沿用旧目录。
          folderId: values.folderId ?? null,
          // groupPath 由主进程按 folderId 派生；这里给出的值只是旧调用方的兜底。
          groupPath: "/server"
        });
        await onReloadConnections();
        message.success(values.id ? "连接已更新" : "连接已创建");
        openDetail({ kind: "empty" });
        if (intent === "saveAndConnect") {
          // 新建时 id 由主进程生成，表单里的是 undefined——只能用返回值。
          // 走不带守卫的那条：改动刚存下去，没有可丢的东西，再问一次纯属打断。
          connectAndClose(saved.id);
        }
      } catch (error) {
        // 主进程拒绝（如私钥认证没选密钥）时既不连接也不关弹窗，编辑器留在原地。
        message.error(`保存连接失败：${formatErrorMessage(error, "请检查输入内容")}`);
      } finally {
        startSaving(false);
      }
    },
    [
      connectAndClose,
      message,
      onReloadConnections,
      openDetail,
      scope.activeScope.workspaceId,
      startSaving
    ]
  );

  const handleDelete = useCallback(
    (ids: string[]) => {
      modal.confirm({
        title: "确认删除",
        content: `删除${describeAffected(ids, scopedConnections)}后会关闭相关会话。删除的连接会进入回收站。`,
        okText: "删除",
        cancelText: "取消",
        okButtonProps: { danger: true },
        onOk: async () => {
          try {
            for (const id of ids) {
              await window.nextshell.connection.remove({ id });
            }
            await onReloadConnections();
            // 只摘掉被删的那几个：整体清空会把"删 A 时正好选中/正在编辑 B"一起清掉，
            // B 的编辑器就这么没了，而用户只是删了另一条。
            const removed = new Set(ids);
            setSelectedIds((previous) => previous.filter((id) => !removed.has(id)));
            if (affectsDetailConnection(detailConnectionId(detailRef.current), ids)) {
              openDetail({ kind: "empty" });
            }
          } catch (error) {
            message.error(`删除失败：${formatErrorMessage(error, "请稍后重试")}`);
          }
        }
      });
    },
    [message, modal, onReloadConnections, openDetail, scopedConnections]
  );

  // 导出的目录选择、明文/加密选项与批量结果汇总沿用既有 hook，不重复一套。
  const [exportIds, setExportIds] = useState<string[]>([]);
  const exportSelection = useMemo(() => new Set(exportIds), [exportIds]);
  const { handleExportAll, handleExportSelected } = useConnectionExportActions({
    connections: scopedConnections,
    selectedIds: exportSelection,
    modal,
    message
  });

  const handleExport = useCallback(
    (ids: string[]) => {
      setExportIds(ids);
    },
    []
  );

  // hook 读的是 selectedIds 快照，所以要等 state 落地后再触发。
  // 导出流程中间有三个 await 的弹窗（选模式、输密码、选目录），期间云同步的 onApplied 会
  // setConnections 换掉数组 → handleExportSelected 换引用 → 这个 effect 重跑 → 导出跑两遍。
  // 用 ref 守住"这一轮还在跑"，它是同步的，effect 重跑时立刻能读到。
  const exportRunningRef = useRef(false);
  useEffect(() => {
    if (exportIds.length === 0 || exportRunningRef.current) {
      return;
    }
    exportRunningRef.current = true;
    void handleExportSelected().finally(() => {
      setExportIds([]);
      exportRunningRef.current = false;
    });
  }, [exportIds, handleExportSelected]);

  const handleRowContextMenu = useCallback(
    (event: MouseEvent, connectionId: string) => {
      event.preventDefault();
      const plan = planRowCommands({ targetId: connectionId, selectedIds });
      const target = scopedConnections.find((item) => item.id === connectionId);
      const label = describeAffected(plan.affectedIds, scopedConnections);

      const byCommand: Record<string, PointerMenuItem> = {
        edit: {
          key: "edit",
          label: "编辑",
          icon: "ri-edit-line",
          // 已经在编辑另一条时，先问一次再换目标。
          onSelect: () => runGuarded(() => openDetail({ kind: "edit", connection: target }))
        },
        connect: {
          key: "connect",
          label: "连接",
          icon: "ri-terminal-box-line",
          onSelect: () => handleConnect(connectionId)
        },
        rename: {
          key: "rename",
          label: "重命名",
          icon: "ri-pencil-line",
          onSelect: () => {
            void (async () => {
              if (!target) {
                return;
              }
              const name = await promptModal(modal, "重命名连接", "请输入新的名称");
              if (!name || name === target.name) {
                return;
              }
              // 改的就是正在编辑的那一条：编辑器里那份快照会在保存时把名字写回旧值。
              // 先问一次要不要放弃编辑；用户选择继续编辑就整个不改名。
              if (!(await leaveEditBeforeMutating([connectionId]))) {
                return;
              }
              try {
                await window.nextshell.connection.upsert(profileToUpsertPayload(target, { name }));
                await onReloadConnections();
              } catch (error) {
                message.error(`重命名失败：${formatErrorMessage(error, "请稍后重试")}`);
              }
            })();
          }
        },
        copyAddress: {
          key: "copyAddress",
          label: "复制地址",
          icon: "ri-link-m",
          onSelect: () => {
            if (!target) {
              return;
            }
            void navigator.clipboard.writeText(`${target.host}:${target.port}`);
            message.success("已复制地址");
          }
        },
        copyToScope: {
          key: "copyToScope",
          label: "复制到作用域…",
          icon: "ri-file-copy-line",
          onSelect: () => setCopyTarget(plan.affectedIds)
        },
        bindAuth: {
          key: "bindAuth",
          label: `批量绑定认证（${label}）`,
          icon: "ri-key-2-line",
          onSelect: () => onOpenBatchAuth(plan.affectedIds)
        },
        export: {
          key: "export",
          label: "导出",
          icon: "ri-download-2-line",
          onSelect: () => handleExport(plan.affectedIds)
        },
        delete: {
          key: "delete",
          label: plan.isBulk ? `删除 ${plan.affectedIds.length} 个` : "删除",
          icon: "ri-delete-bin-line",
          danger: true,
          onSelect: () => handleDelete(plan.affectedIds)
        }
      };

      setContextMenu({
        x: event.clientX,
        y: event.clientY,
        items: plan.commands
          .map((command) => byCommand[command])
          .filter((item): item is PointerMenuItem => Boolean(item))
      });
    },
    [
      handleConnect,
      handleDelete,
      handleExport,
      leaveEditBeforeMutating,
      message,
      modal,
      onOpenBatchAuth,
      onReloadConnections,
      openDetail,
      runGuarded,
      scopedConnections,
      selectedIds
    ]
  );

  // ── 目录 CRUD ──────────────────────────────────────────────

  const handleCreateFolder = useCallback(
    async (parentId: string | undefined) => {
      const name = await promptModal(modal, "新建目录", "请输入目录名称");
      if (!name) {
        return;
      }
      try {
        await window.nextshell.connectionFolder.create({
          scopeKey: scope.activeScope.key,
          name,
          parentId
        });
        await scope.reloadFolders();
      } catch (error) {
        message.error(`新建目录失败：${formatErrorMessage(error, "请稍后重试")}`);
      }
    },
    // 依赖收窄到真正用到的两片而不是整个 scope:scope 每次翻目录都换引用，挂在它上面的
    // 回调会跟着换，下游 memo 组件(目录树/网格)就永远命中不了。
    [message, modal, scope.activeScope.key, scope.reloadFolders]
  );

  const handleRenameFolder = useCallback(
    async (folder: ConnectionFolder) => {
      const name = await promptModal(modal, `重命名目录「${folder.name}」`, "请输入新的目录名称");
      if (!name || name === folder.name) {
        return;
      }
      try {
        await window.nextshell.connectionFolder.rename({ id: folder.id, name });
        await scope.reloadFolders();
      } catch (error) {
        message.error(`重命名失败：${formatErrorMessage(error, "请稍后重试")}`);
      }
    },
    [message, modal, scope.reloadFolders]
  );

  const handleDeleteFolder = useCallback(
    (folder: ConnectionFolder) => {
      modal.confirm({
        title: `删除目录「${folder.name}」`,
        content: "只移除目录本身，里面的连接和子目录会回到上一层。",
        okText: "删除",
        cancelText: "取消",
        okButtonProps: { danger: true },
        onOk: async () => {
          try {
            await window.nextshell.connectionFolder.remove({ id: folder.id });
            await scope.reloadFolders();
            await onReloadConnections();
          } catch (error) {
            message.error(`删除目录失败：${formatErrorMessage(error, "请稍后重试")}`);
          }
        }
      });
    },
    [message, modal, onReloadConnections, scope.reloadFolders]
  );

  const handleMoveFolder = useCallback(
    async (folderId: string, parentId: string | undefined) => {
      try {
        await window.nextshell.connectionFolder.move({ id: folderId, parentId });
        await scope.reloadFolders();
        await onReloadConnections();
      } catch (error) {
        message.error(`移动目录失败：${formatErrorMessage(error, "请稍后重试")}`);
      }
    },
    [message, onReloadConnections, scope.reloadFolders]
  );

  /**
   * 同级排序。落在缝隙里可能同时意味着"换爹"(跨层拖到别人的缝隙),所以先 move 再整层重编号——
   * 只发 reorder 的话跨层拖会看起来完全没反应。
   */
  const handleReorderFolder = useCallback(
    async (dragId: string, dropId: string, placeAfter: boolean) => {
      const dragged = scope.folders.find((folder) => folder.id === dragId);
      const dropTarget = scope.folders.find((folder) => folder.id === dropId);
      if (!dragged || !dropTarget) {
        return;
      }
      const steps = planSiblingReorder({ folders: scope.folders, dragId, dropId, placeAfter });
      const needsReparent = (dragged.parentId ?? undefined) !== (dropTarget.parentId ?? undefined);
      if (!needsReparent && steps.length === 0) {
        return;
      }
      try {
        if (needsReparent) {
          await window.nextshell.connectionFolder.move({
            id: dragId,
            parentId: dropTarget.parentId
          });
        }
        for (const step of steps) {
          await window.nextshell.connectionFolder.reorder(step);
        }
        await scope.reloadFolders();
        if (needsReparent) {
          await onReloadConnections();
        }
      } catch (error) {
        message.error(`调整目录顺序失败：${formatErrorMessage(error, "请稍后重试")}`);
        await scope.reloadFolders();
      }
    },
    [message, onReloadConnections, scope.folders, scope.reloadFolders]
  );

  // ── 拖拽移动连接 ───────────────────────────────────────────
  // 没有轻量 move 通道，只能逐条 upsert 全量 payload。逐条意味着可能"成功一半"，
  // 所以失败时不回滚已成功的，只如实报数并重新拉一次列表。
  const handleMoveConnections = useCallback(
    async (connectionIds: string[], targetFolderId: string | undefined) => {
      const pending = planConnectionMove(scopedConnections, connectionIds, targetFolderId);
      if (pending.length === 0) {
        return;
      }
      // 拖的正是编辑中的那一条：不先离开编辑态的话，用户按保存时会把 folderId 写回旧目录，
      // 这次移动被静默回滚。空计划(拖回原地)不必打断，所以判定放在 plan 之后。
      if (!(await leaveEditBeforeMutating(pending.map((item) => item.id)))) {
        return;
      }
      const targetLabel = targetFolderId
        ? (scope.folders.find((folder) => folder.id === targetFolderId)?.name ?? "目标目录")
        : scope.activeScope.label;

      let moved = 0;
      let failure: unknown;
      for (const connection of pending) {
        try {
          await window.nextshell.connection.upsert(
            // null 而不是 undefined：省略这个字段主进程会理解成"别动目录"，回不了顶层。
            profileToUpsertPayload(connection, { folderId: targetFolderId ?? null })
          );
          moved += 1;
        } catch (error) {
          failure = error;
        }
      }
      const failed = pending.length - moved;
      await onReloadConnections();
      if (failed === 0) {
        message.success(describeMoveOutcome({ moved, failed, targetLabel }));
        return;
      }
      message.error(
        `${describeMoveOutcome({ moved, failed, targetLabel })}：${formatErrorMessage(
          failure,
          "请稍后重试"
        )}`
      );
    },
    [
      leaveEditBeforeMutating,
      message,
      onReloadConnections,
      scope.activeScope.label,
      scope.folders,
      scopedConnections
    ]
  );

  /** 目录 → 递归收集其下全部连接 id → 复用既有的批量绑定弹窗(ids 形态)。 */
  const handleFolderBatchAuth = useCallback(
    (folder: ConnectionFolder) => {
      const subtree = collectDescendantIds(folder.id, scope.folders);
      subtree.add(folder.id);
      const ids = scopedConnections
        .filter((connection) => connection.folderId && subtree.has(connection.folderId))
        .map((connection) => connection.id);
      if (ids.length === 0) {
        message.info(`目录「${folder.name}」下没有连接`);
        return;
      }
      setBatchAuthTarget({
        type: "connections",
        connectionIds: ids,
        label: `目录「${folder.name}」下的 ${ids.length} 个连接`
      });
    },
    [message, scope.folders, scopedConnections]
  );

  const handleFolderContextMenu = useCallback(
    (event: MouseEvent, folder: ConnectionFolder) => {
      setContextMenu({
        x: event.clientX,
        y: event.clientY,
        items: [
          {
            key: "create",
            label: "新建子目录",
            icon: "ri-folder-add-line",
            onSelect: () => void handleCreateFolder(folder.id)
          },
          {
            key: "rename",
            label: "重命名",
            icon: "ri-pencil-line",
            onSelect: () => void handleRenameFolder(folder)
          },
          {
            key: "bindAuth",
            label: "批量绑定认证（含子目录）",
            icon: "ri-key-2-line",
            onSelect: () => handleFolderBatchAuth(folder)
          },
          {
            key: "delete",
            label: "删除目录",
            icon: "ri-delete-bin-line",
            danger: true,
            onSelect: () => handleDeleteFolder(folder)
          }
        ]
      });
    },
    [handleCreateFolder, handleDeleteFolder, handleFolderBatchAuth, handleRenameFolder]
  );

  // ── 中栏网格与目录树的回调 ─────────────────────────────────
  // 全部 useCallback 稳定化。这两个组件都包了 memo，而 memo 只比较引用:任何一个内联箭头
  // 都会让它每帧重渲整片磁贴/整棵树——上一轮"整个界面卡卡的"另一半就出在这里。

  /** 单击磁贴。选中与右栏详情必须在同一个守卫里:用户选择"继续编辑"时两者都不该动。 */
  const handleGridFocus = useCallback(
    (connectionId: string) => {
      const found = rows.find((row) => row.connection.id === connectionId);
      if (!found) {
        return;
      }
      runGuarded(() => {
        setSelectedIds([connectionId]);
        openDetail({ kind: "view", connection: found.connection });
      });
    },
    [openDetail, rows, runGuarded]
  );

  /** Ctrl/Cmd+单击:只动选区,右栏原样不动,所以不过守卫。 */
  const handleToggleSelect = useCallback((connectionId: string) => {
    setSelectedIds((previous) =>
      previous.includes(connectionId)
        ? previous.filter((id) => id !== connectionId)
        : [...previous, connectionId]
    );
  }, []);

  const handleClearSelection = useCallback(() => setSelectedIds([]), []);

  const handleDropConnections = useCallback(
    (ids: string[], folderId: string | undefined) => void handleMoveConnections(ids, folderId),
    [handleMoveConnections]
  );

  const handleCreateFolderCommand = useCallback(
    (parentId: string | undefined) => void handleCreateFolder(parentId),
    [handleCreateFolder]
  );

  const handleRenameFolderCommand = useCallback(
    (folder: ConnectionFolder) => void handleRenameFolder(folder),
    [handleRenameFolder]
  );

  const handleMoveFolderCommand = useCallback(
    (folderId: string, parentId: string | undefined) => void handleMoveFolder(folderId, parentId),
    [handleMoveFolder]
  );

  const handleReorderFolderCommand = useCallback(
    (dragId: string, dropId: string, placeAfter: boolean) =>
      void handleReorderFolder(dragId, dropId, placeAfter),
    [handleReorderFolder]
  );

  // ── 外部定位(A6) ───────────────────────────────────────────
  // 定位要跨三个异步台阶:作用域 → 目录列表 → 行。每一步都可能还没就绪，所以这个效应会跑
  // 很多次，用 ref 记住"这一条已经处理过了"来保证只生效一次(否则用户刚翻到别处又被拽回来)。
  const focusAppliedRef = useRef<string>(undefined);
  const focusScopeRequestedRef = useRef<string>(undefined);
  // 守卫要等用户点确认框。这期间效应会因为 connections/folders 变化重跑，不记一笔就会
  // 一口气弹出好几个一模一样的确认框。
  const focusGuardPendingRef = useRef<string>(undefined);
  // 从密钥/代理分段切回来时，网格是这一帧才挂上的，`gridHandleRef` 这一刻还是 null。
  // 记下来交给下面那个效应，等网格真的在了再滚过去。
  const pendingRevealRef = useRef<{ id: string; ancestorKeys: string[] } | null>(null);

  useEffect(() => {
    if (!open) {
      focusAppliedRef.current = undefined;
      focusScopeRequestedRef.current = undefined;
      focusGuardPendingRef.current = undefined;
      pendingRevealRef.current = null;
      return;
    }
    if (!focusConnectionId || focusAppliedRef.current === focusConnectionId) {
      return;
    }
    // 守卫还开着确认框，等它的结果。
    if (focusGuardPendingRef.current === focusConnectionId) {
      return;
    }
    const target = connections.find((item) => item.id === focusConnectionId);
    if (!target) {
      // 连接已经不在了：静默放弃，并记下来别再找。
      focusAppliedRef.current = focusConnectionId;
      return;
    }
    const targetScopeKey = resolveOriginScopeKey(target);
    if (scope.activeScope.key !== targetScopeKey) {
      // selectScope 会清空目录并重新拉，只能请求一次——每次渲染都请求会把自己锁在重渲循环里。
      if (focusScopeRequestedRef.current !== focusConnectionId) {
        focusScopeRequestedRef.current = focusConnectionId;
        scope.selectScope(targetScopeKey);
      }
      // 这里不烧 focusAppliedRef：作用域/目录还没就绪只是"还没轮到"，烧了这条深链就永久没了。
      return;
    }
    // 目录还没加载完就设 currentFolderId，会被 reconcile 立刻打回根。
    if (target.folderId && !scope.folders.some((folder) => folder.id === target.folderId)) {
      return;
    }
    // 深链会顶掉右栏的编辑器，和点别的行一样要先过守卫。
    focusGuardPendingRef.current = focusConnectionId;
    void confirmDiscardEdits().then((ok) => {
      focusGuardPendingRef.current = undefined;
      // 应用了、或者用户明确拒绝了，都算这条深链处理完毕——拒绝后不该再弹第二次。
      focusAppliedRef.current = focusConnectionId;
      if (!ok) {
        return;
      }
      // 管理器组件从不卸载：上次停在「密钥」分段时连表格都不渲染，定位会无声失效。
      setResourceTab("connections");
      // 残留的搜索词会让对账效应立刻把刚选中的行清掉（它不在过滤后的 rows 里）。
      setKeyword("");
      // 深链定位落在浏览态:「最近连接」视图未必包含目标,而浏览态"当前层的直属连接"
      // 从不截断,钻到它所在的目录就一定能看见它。
      setGridView("browse");
      scope.enterFolder(target.folderId);
      setSelectedIds([target.id]);
      openDetail({ kind: "view", connection: target });
      // 网格没有可折叠的层级,祖先目录 key 用不上,但签名与休眠的表格保持一致。
      pendingRevealRef.current = { id: target.id, ancestorKeys: [] };
    });
  }, [confirmDiscardEdits, connections, focusConnectionId, open, openDetail, scope]);

  // ── 外部动作:打开即新建(⌘K 面板「添加新服务器」)──────────────
  // 与 focusConnectionId 同款的一次性深链:本次 open 生效一次,关掉后重置。新建会顶掉
  // 右栏的编辑器,和深链定位一样先过未保存守卫;用户拒绝也算处理完毕,不再弹第二次。
  const initialActionAppliedRef = useRef(false);
  useEffect(() => {
    if (!open) {
      initialActionAppliedRef.current = false;
      return;
    }
    if (initialAction !== "create" || initialActionAppliedRef.current) {
      return;
    }
    initialActionAppliedRef.current = true;
    void confirmDiscardEdits().then((ok) => {
      if (!ok) {
        return;
      }
      // 管理器组件从不卸载:上次停在「密钥」分段时右栏不在,新建会无声落空。
      setResourceTab("connections");
      openDetail({ kind: "edit", connection: undefined });
    });
  }, [confirmDiscardEdits, initialAction, open, openDetail]);

  // 网格挂上之后再把磁贴滚进视野。不给依赖数组：要的是"任意一次渲染之后"，
  // 而触发它的那次 setState 恰好也是让网格出现的那一次。
  useEffect(() => {
    const pending = pendingRevealRef.current;
    if (!pending || resourceTab !== "connections" || !gridHandleRef.current) {
      return;
    }
    pendingRevealRef.current = null;
    gridHandleRef.current.revealConnection(pending.id, pending.ancestorKeys);
  });

  // ── 栏宽拖拽:命令式改 CSS 变量,不触发 React 重渲;松手才落 state 和偏好。──
  const columnsRef = useRef<HTMLDivElement>(null);
  const shellRef = useRef<HTMLDivElement>(null);
  const folderWidthRef = useRef(preferences.folderColumnWidth);
  const detailWidthRef = useRef(preferences.detailColumnWidth);

  useEffect(() => {
    folderWidthRef.current = preferences.folderColumnWidth;
    columnsRef.current?.style.setProperty("--cm2-folder-w", `${preferences.folderColumnWidth}px`);
  }, [preferences.folderColumnWidth, resourceTab]);

  useEffect(() => {
    detailWidthRef.current = preferences.detailColumnWidth;
    columnsRef.current?.style.setProperty("--cm2-detail-w", `${preferences.detailColumnWidth}px`);
  }, [preferences.detailColumnWidth, resourceTab]);

  /**
   * 竖向分隔条的公共骨架。两条分隔条只差三件事:改哪个 CSS 变量、夹在哪个范围、往回写哪个偏好;
   * 以及右栏在鼠标右侧,往右拖是变窄,所以要能反号。
   */
  const startColumnResize = useCallback(
    (
      event: React.PointerEvent<HTMLDivElement>,
      config: {
        widthRef: React.MutableRefObject<number>;
        dimension: "folderColumn" | "detailColumn";
        cssVar: string;
        sign: 1 | -1;
        commit: (width: number) => void;
      }
    ) => {
      event.preventDefault();
      const target = event.currentTarget;
      target.setPointerCapture(event.pointerId);
      let last = event.clientX;
      const move = (moveEvent: PointerEvent) => {
        config.widthRef.current = clampDialogSize(
          config.dimension,
          config.widthRef.current + config.sign * (moveEvent.clientX - last)
        );
        last = moveEvent.clientX;
        columnsRef.current?.style.setProperty(config.cssVar, `${config.widthRef.current}px`);
      };
      const up = () => {
        target.removeEventListener("pointermove", move);
        target.removeEventListener("pointerup", up);
        config.commit(config.widthRef.current);
      };
      target.addEventListener("pointermove", move);
      target.addEventListener("pointerup", up);
    },
    []
  );

  const startResize = useCallback(
    (event: React.PointerEvent<HTMLDivElement>) =>
      startColumnResize(event, {
        widthRef: folderWidthRef,
        dimension: "folderColumn",
        cssVar: "--cm2-folder-w",
        sign: 1,
        commit: (width) =>
          void updatePreferences({ connectionManager: { folderColumnWidth: width } })
      }),
    [startColumnResize, updatePreferences]
  );

  /** 5.4「右栏可拖宽(宽度持久化)」——想一屏看全整张表单就把它拉宽。 */
  const startDetailResize = useCallback(
    (event: React.PointerEvent<HTMLDivElement>) =>
      startColumnResize(event, {
        widthRef: detailWidthRef,
        dimension: "detailColumn",
        cssVar: "--cm2-detail-w",
        // 分隔条在右栏左边：指针往右走，右栏变窄。
        sign: -1,
        commit: (width) =>
          void updatePreferences({ connectionManager: { detailColumnWidth: width } })
      }),
    [startColumnResize, updatePreferences]
  );

  // ── 对话框缩放(D4) ─────────────────────────────────────────
  // Modal 的宽是 antd 写在 `.ant-modal` 内联 style 上的,拖动期间直接改那个元素;高度是本壳自己的。
  // 同样只在松手时写偏好——每帧一次 IPC 会把主进程刷爆。
  const dialogDraftRef = useRef({ width: dialogSize.width, height: dialogSize.height });
  const dialogResizingRef = useRef(false);
  // 把偏好这个唯一真相同步回 DOM。刻意不给依赖数组:拖拽期间宽高是命令式写进 DOM 的，
  // React 并不知道自己那份值和 DOM 已经不一致了——只要新值恰好等于旧值(写偏好被契约拒收、
  // 或者小视口下夹回了同一个数)，它就不会重写内联样式，DOM 会永远停在那个尺寸上。
  // 每次渲染后把宽和高一起重申一遍，无论提交成功、失败还是回滚都能收敛。
  useEffect(() => {
    if (dialogResizingRef.current) {
      // 正在拖：这段时间 DOM 归上面那条命令式路径管，别跟它抢。
      return;
    }
    dialogDraftRef.current = { width: dialogSize.width, height: dialogSize.height };
    const shell = shellRef.current;
    const dialog = shell?.closest<HTMLElement>(".ant-modal");
    if (dialog) {
      dialog.style.width = `${dialogSize.width}px`;
    }
    if (shell) {
      shell.style.height = `${dialogSize.height}px`;
    }
  });

  const startDialogResize = useCallback(
    (event: React.PointerEvent<HTMLDivElement>) => {
      event.preventDefault();
      const target = event.currentTarget;
      target.setPointerCapture(event.pointerId);
      const shell = shellRef.current;
      const dialog = shell?.closest<HTMLElement>(".ant-modal") ?? null;
      dialogResizingRef.current = true;
      const startX = event.clientX;
      const startY = event.clientY;
      const origin = { ...dialogDraftRef.current };
      const move = (moveEvent: PointerEvent) => {
        const next = resolveDialogResize(
          {
            width: origin.width + (moveEvent.clientX - startX),
            height: origin.height + (moveEvent.clientY - startY)
          },
          { width: window.innerWidth, height: window.innerHeight }
        );
        dialogDraftRef.current = next;
        if (dialog) {
          dialog.style.width = `${next.width}px`;
        }
        if (shell) {
          shell.style.height = `${next.height}px`;
        }
      };
      const up = () => {
        target.removeEventListener("pointermove", move);
        target.removeEventListener("pointerup", up);
        target.removeEventListener("pointercancel", up);
        dialogResizingRef.current = false;
        // 拖出来的值可能被视口压到契约下限以下(矮屏)，那样主进程会整条拒收。
        // 存夹回范围内的值，显示口径会照旧把它压回视口。
        const persisted = {
          width: clampDialogSize("width", dialogDraftRef.current.width),
          height: clampDialogSize("height", dialogDraftRef.current.height)
        };
        // 命令式那条路径到此为止：把 DOM 直接对齐到"React 下一帧会认的那个值"，之后完全
        // 交给上面那个镜像 effect。不能只是把内联样式删掉——写偏好落地前要等一次渲染，
        // 这中间对话框会塌成 auto 宽度闪一下。
        const settled = fitDialogToViewport(persisted, {
          width: window.innerWidth,
          height: window.innerHeight
        });
        dialogDraftRef.current = settled;
        if (dialog) {
          dialog.style.width = `${settled.width}px`;
        }
        if (shell) {
          shell.style.height = `${settled.height}px`;
        }
        void updatePreferences({
          connectionManager: {
            dialogWidth: persisted.width,
            dialogHeight: persisted.height
          }
        });
      };
      target.addEventListener("pointermove", move);
      target.addEventListener("pointerup", up);
      // 拖到一半被系统打断(右键、切窗口)时也要收尾，否则镜像 effect 会一直被那面旗子挡着。
      target.addEventListener("pointercancel", up);
    },
    [updatePreferences]
  );

  // ── 拖文件导入 ────────────────────────────────────────────
  // 导入执行链路只写本地作用域，云作用域下入口整体禁用，否则用户会以为导进了云端。
  const importLocked = scope.activeScope.kind !== "local";
  const [dropActive, setDropActive] = useState(false);
  // dragenter/dragleave 会在子元素之间反复触发；用深度计数才不会在跨越边框时闪掉遮罩。
  const dropDepthRef = useRef(0);
  const canAcceptDrop = canAcceptManagerFileDrop({
    open,
    resourceTab,
    importingPreview: importFlow.importingPreview,
    scopeKind: scope.activeScope.kind
  });

  useEffect(() => {
    if (!canAcceptDrop) {
      dropDepthRef.current = 0;
      setDropActive(false);
    }
  }, [canAcceptDrop]);

  /**
   * 四个监听全部挂在**捕获相**(见 JSX 里的 `on*Capture`)。原因是 rc-tree 给每个可拖节点都挂了
   * 无条件 `preventDefault + stopPropagation` 的 dragover/drop:文件松在目录节点上时事件冒泡
   * 不上来，导入被静默吞掉;松在根节点或空白区却正常——表现成"有时候能导入"。
   *
   * 捕获相里只截文件拖入(`shouldInterceptFileDrag`)，截住的就 stopPropagation，让 rc-tree
   * 根本看不到它;内部连接拖拽与目录自身的拖拽一律放行，走它们原来的冒泡路径。
   */
  const handleDragEnter = useCallback(
    (event: ReactDragEvent<HTMLDivElement>) => {
      if (!shouldInterceptFileDrag(canAcceptDrop, event.dataTransfer)) {
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      dropDepthRef.current += 1;
      setDropActive(true);
    },
    [canAcceptDrop]
  );

  const handleDragOver = useCallback(
    (event: ReactDragEvent<HTMLDivElement>) => {
      if (!shouldInterceptFileDrag(canAcceptDrop, event.dataTransfer)) {
        return;
      }
      // 不 preventDefault 的话浏览器默认会用这个文件导航掉整个渲染进程。
      event.preventDefault();
      event.stopPropagation();
      event.dataTransfer.dropEffect = "copy";
      setDropActive(true);
    },
    [canAcceptDrop]
  );

  const handleDragLeave = useCallback(
    (event: ReactDragEvent<HTMLDivElement>) => {
      if (!shouldInterceptFileDrag(canAcceptDrop, event.dataTransfer)) {
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      dropDepthRef.current = Math.max(0, dropDepthRef.current - 1);
      if (dropDepthRef.current === 0) {
        setDropActive(false);
      }
    },
    [canAcceptDrop]
  );

  const handleDrop = useCallback(
    (event: ReactDragEvent<HTMLDivElement>) => {
      // 遮罩无论如何都要收掉，哪怕这次 drop 不归我们管。
      dropDepthRef.current = 0;
      setDropActive(false);
      if (!shouldInterceptFileDrag(canAcceptDrop, event.dataTransfer)) {
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      // 渲染进程拿不到 File.path（sandbox），真实路径只能问 preload 要。
      const extracted = extractDroppedFilePaths(
        event.dataTransfer,
        window.nextshell.getFilePathForDrop
      );
      if (extracted.paths.length === 0) {
        message.warning(describeManagerDropWarning({ allPathsEmpty: extracted.allPathsEmpty }));
        return;
      }
      void importFlow.handleImportDroppedNextShellFiles(extracted.paths);
    },
    [canAcceptDrop, importFlow, message]
  );

  // 不挂全局 keydown——rc-portal 的 Esc 栈只把事件交给最顶层弹窗，挂全局会跟嵌套弹窗抢。
  const handleModalCancel = useCallback(
    (event: MouseEvent<HTMLButtonElement> | KeyboardEvent<HTMLElement>) => {
      const intent = resolveCancelIntent(event?.type, editing);
      if (intent === "leaveEdit" && savingRef.current) {
        // 保存进行中按 Esc 直接忽略：守卫的确认框要等用户点，等它 resolve 时保存多半
        // 已经成功、右栏也换成空态了，这时再退回只读详情等于弹回一张过期的卡片。
        return;
      }
      runGuarded(intent === "leaveEdit" ? leaveEdit : onClose);
    },
    [editing, leaveEdit, onClose, runGuarded]
  );

  return (
    <Modal
      open={open}
      onCancel={handleModalCancel}
      footer={null}
      width={dialogSize.width}
      style={{ top: 40 }}
      styles={{
        header: { padding: "12px 18px", marginBottom: 0, borderBottom: "1px solid var(--border)" },
        body: { padding: 0, overflow: "hidden" }
      }}
      title={<span className="cm2-title">连接管理器</span>}
      destroyOnHidden
    >
      <div
        ref={shellRef}
        className={`cm2-shell${dropActive ? " cm2-shell--drop-target" : ""}`}
        style={{ height: dialogSize.height }}
        // 捕获相:rc-tree 的目录节点会无条件吞掉 dragover/drop，冒泡相收不到落在节点上的文件。
        onDragEnterCapture={handleDragEnter}
        onDragOverCapture={handleDragOver}
        onDragLeaveCapture={handleDragLeave}
        onDropCapture={handleDrop}
      >
        <ScopeBar
          scopes={scope.scopes}
          activeScope={scope.activeScope}
          // 切作用域与切资源分段都会顶掉右栏的编辑器，先过守卫。
          onSelectScope={(key) =>
            runGuarded(() => {
              openDetail({ kind: "empty" });
              scope.selectScope(key);
            })
          }
          resourceTab={resourceTab}
          onSelectResource={(tab) =>
            runGuarded(() => {
              // 编辑器与资源面板互斥渲染，切走就是卸载；已经确认放弃了就别把编辑态留着，
              // 否则切回「连接」还会再问一次。
              if (editing) {
                openDetail({ kind: "empty" });
              }
              setResourceTab(tab);
            })
          }
        />

        {resourceTab === "connections" ? (
          <div
            ref={columnsRef}
            className="cm2-columns"
            style={{
              // 没选中任何行时右栏整个不渲染,中栏吃满宽度——空态占着 1/3 宽只放一句
              // 提示语,是用户点名难看的设计。
              gridTemplateColumns:
                detail.kind === "empty"
                  ? `var(--cm2-folder-w, ${preferences.folderColumnWidth}px) 6px minmax(0, 1fr)`
                  : `var(--cm2-folder-w, ${preferences.folderColumnWidth}px) 6px minmax(0, 1fr) 6px var(--cm2-detail-w, ${preferences.detailColumnWidth}px)`
            }}
          >
            <FolderTree
              rootLabel={scope.activeScope.label}
              recentActive={gridView === "recent"}
              onSelectRecent={handleSelectRecent}
              folders={scope.folders}
              connections={scopedConnections}
              currentFolderId={scope.currentFolderId}
              onSelectFolder={handleBrowseFolder}
              onCreateFolder={handleCreateFolderCommand}
              onRenameFolder={handleRenameFolderCommand}
              onDeleteFolder={handleDeleteFolder}
              onMoveFolder={handleMoveFolderCommand}
              onReorderFolder={handleReorderFolderCommand}
              onDropConnections={handleDropConnections}
              onBatchBindAuth={handleFolderBatchAuth}
            />

            <div
              className="cm2-resizer"
              role="separator"
              aria-orientation="vertical"
              onPointerDown={startResize}
            />

            <div className="cm2-main">
              <ManagerToolbar
                keyword={keyword}
                onKeywordChange={setKeyword}
                onNewConnection={() =>
                  runGuarded(() => openDetail({ kind: "edit", connection: undefined }))
                }
                onOpenLocalTerminal={handleOpenLocalTerminal}
                onImportNextShellFile={() => void importFlow.handleImportNextShell()}
                onImportNextShellDirectory={() =>
                  void importFlow.handleImportNextShellDirectory()
                }
                onImportFinalShellFile={() => void importFlow.handleImportFinalShell()}
                onImportFinalShellDirectory={() =>
                  void importFlow.handleImportFinalShellDirectory()
                }
                importDisabled={importLocked}
                importDisabledReason="导入目前仅支持本地作用域，请先切换"
                onExportAllToFile={() => void handleExportAll()}
                onExportAllToDirectory={() =>
                  handleExport(scopedConnections.map((item) => item.id))
                }
                onOpenCloudSync={() => handleOpenSettingsSection("cloudSync")}
                onOpenRecycleBin={() => handleOpenSettingsSection("recycleBin")}
              />
              {/*
                网格里单击就等于选中(没有勾选框了)，所以批量条的门槛提到 2:否则每点一个
                磁贴都会多出一条横栏、把下面的网格顶一下。单选的那几条命令右键菜单里都有。
              */}
              {selectedIds.length > 1 ? (
                <BulkBar
                  count={selectedIds.length}
                  onClear={handleClearSelection}
                  onBindAuth={() => onOpenBatchAuth(selectedIds)}
                  onCopyToScope={() => setCopyTarget(selectedIds)}
                  onExport={() => handleExport(selectedIds)}
                  onDelete={() => handleDelete(selectedIds)}
                />
              ) : null}
              <GridPathBar
                segments={pathSegments}
                searching={searching}
                recent={gridView === "recent"}
                resultCount={searching ? rows.length : gridConnectionIds.length}
                sort={sort}
                onSortChange={setSort}
                onSelectFolder={handleBrowseFolder}
              />
              <ConnectionGrid
                gridRef={gridHandleRef}
                sections={sections}
                emptyText={
                  searching
                    ? "没有匹配的连接"
                    : gridView === "recent"
                      ? "还没有可显示的连接"
                      : "此目录为空"
                }
                selectedIds={selectedIds}
                focusedId={detail.kind === "view" ? detail.connection.id : undefined}
                onFocus={handleGridFocus}
                onToggleSelect={handleToggleSelect}
                onClearSelection={handleClearSelection}
                onConnect={handleConnect}
                onRowContextMenu={handleRowContextMenu}
                // 钻取不会卸载右栏的编辑器，和左侧树点目录一样不需要过守卫。
                onEnterFolder={handleBrowseFolder}
                onFolderContextMenu={handleFolderContextMenu}
                onDropConnections={handleDropConnections}
              />
            </div>

            {detail.kind !== "empty" ? (
              <>
            <div
              className="cm2-resizer cm2-resizer--flush"
              role="separator"
              aria-orientation="vertical"
              aria-label="调整详情栏宽度"
              onPointerDown={startDetailResize}
            />

            <div className="cm2-detail-col">
              {detail.kind === "view" ? (
                <DetailCard
                  connection={detail.connection}
                  sshKeys={scopedSshKeys}
                  folderLabel={folderLabel}
                  onEdit={() => openDetail({ kind: "edit", connection: detail.connection })}
                  onConnect={() => handleConnect(detail.connection.id)}
                />
              ) : detail.kind === "edit" ? (
                <ConnectionEditor
                  // 换连接时整体重挂载，让 initialValues 生效。换目标前的未保存改动由守卫兜。
                  key={detail.connection?.id ?? "__new__"}
                  editorRef={editorRef}
                  connection={detail.connection}
                  folders={scope.folders}
                  currentFolderId={scope.currentFolderId}
                  sshKeys={scopedSshKeys}
                  proxies={scopedProxies}
                  saving={saving}
                  revealedPassword={revealedLoginPassword}
                  revealingPassword={revealingLoginPassword}
                  onRevealPassword={() => void handleRevealConnectionPassword()}
                  onSubmit={(values, intent) => void handleSubmit(values, intent)}
                  onCancel={() => runGuarded(leaveEdit)}
                  onDirtyChange={handleEditorDirtyChange}
                  onCreateKey={() => setInlineKeyOpen(true)}
                />
              ) : null}
            </div>
              </>
            ) : null}
          </div>
        ) : (
          <div
            className="cm2-columns"
            style={{
              gridTemplateColumns: `minmax(0, 1fr) ${preferences.detailColumnWidth}px`
            }}
          >
            {resourceTab === "keys" ? (
              <SshKeyPane
                sshKeys={scopedSshKeys}
                workspaceId={scope.activeScope.workspaceId}
                onReload={onReloadSshKeys}
              />
            ) : (
              <ProxyPane
                proxies={scopedProxies}
                workspaceId={scope.activeScope.workspaceId}
                onReload={onReloadProxies}
              />
            )}
          </div>
        )}

        {dropActive ? (
          <div className="cm2-drop-overlay">
            <div className="cm2-drop-overlay-card">
              <i className="ri-upload-cloud-2-line" aria-hidden="true" />
              <strong>释放以导入连接</strong>
              <span>支持 NextShell 导出的 .json 文件，可一次拖入多个</span>
            </div>
          </div>
        ) : null}

        {/* D4：右下角拖角缩放。放在 shell 里而不是 Modal 里，才能盖在内容之上又跟着壳走。 */}
        <div
          className="cm2-dialog-resize"
          role="separator"
          aria-label="调整对话框大小"
          onPointerDown={startDialogResize}
        />
      </div>

      {contextMenu ? (
        <PointerMenu
          x={contextMenu.x}
          y={contextMenu.y}
          items={contextMenu.items}
          onClose={() => setContextMenu(null)}
        />
      ) : null}

      <ConnectionBatchAuthModal
        open={Boolean(batchAuthTarget)}
        target={batchAuthTarget}
        connections={connections}
        sshKeys={sshKeys}
        onClose={() => setBatchAuthTarget(null)}
        onUpdated={onReloadConnections}
      />

      <ConnectionImportModal
        open={importFlow.importModalOpen}
        entries={importFlow.currentImportBatch?.entries ?? []}
        existingConnections={connections}
        sshKeys={sshKeys}
        // 导入只写本地，目标目录也只能从本地目录里选（云作用域下入口已禁用）。
        folders={importLocked ? [] : scope.folders}
        defaultTargetFolderId={importLocked ? undefined : scope.currentFolderId}
        sourceName={importFlow.currentImportBatch?.fileName}
        // 目录扫描与单文件对 groupPath 的解释不同，这个标记要一路带到 execute payload。
        sourceKind={importFlow.currentImportBatch?.sourceKind}
        onClose={importFlow.resetImportFlow}
        onImported={importFlow.handleImportBatchImported}
      />

      <InlineSshKeyModal
        open={inlineKeyOpen}
        workspaceId={scope.activeScope.workspaceId}
        onClose={() => setInlineKeyOpen(false)}
        onCreated={async (key) => {
          await onReloadSshKeys();
          // 编辑器全程没卸载，直接把新密钥写回它的 sshKeyId。
          editorRef.current?.selectSshKey(key.id);
        }}
      />

      <CopyToScopeModal
        open={copyTarget !== null}
        connectionIds={copyTarget ?? []}
        label={describeAffected(copyTarget ?? [], scopedConnections)}
        scopes={scope.scopes}
        currentScopeKey={scope.activeScope.key}
        onClose={() => setCopyTarget(null)}
        onCopied={onReloadConnections}
      />
    </Modal>
  );
};
