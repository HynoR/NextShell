import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { MouseEvent } from "react";
import { App as AntdApp, Modal } from "antd";
import type { ConnectionFolder, ConnectionProfile, ProxyProfile, SshKeyProfile } from "@nextshell/core";
import { formatErrorMessage } from "../../utils/errorMessage";
import { promptModal } from "../../utils/promptModal";
import { usePreferencesStore } from "../../store/usePreferencesStore";
import { ScopeBar } from "./components/ScopeBar";
import { FolderTree } from "./components/FolderTree";
import { ConnectionTable } from "./components/ConnectionTable";
import { DetailCard } from "./components/DetailCard";
import { ConnectionEditor, type ConnectionEditorValues } from "./components/ConnectionEditor";
import { ManagerToolbar } from "./components/ManagerToolbar";
import { BulkBar } from "./components/BulkBar";
import { PointerMenu, type PointerMenuItem } from "./components/PointerMenu";
import { CopyToScopeModal } from "./components/CopyToScopeModal";
import { SshKeyPane } from "./components/SshKeyPane";
import { ProxyPane } from "./components/ProxyPane";
import { describeAffected, planRowCommands } from "./utils/rowCommands";
import { useConnectionExportActions } from "./hooks/useConnectionExportActions";
import { useConnectionImportFlow } from "./hooks/useConnectionImportFlow";
import { useConnectionPasswordReveal } from "./hooks/useConnectionPasswordReveal";
import { ConnectionBatchAuthModal } from "./components/ConnectionBatchAuthModal";
import { ConnectionImportModal } from "../ConnectionImportModal";

import { useManagerScope } from "./hooks/useManagerScope";
import { buildBreadcrumb, listVisibleConnections } from "./utils/folderNavigation";
import { buildConnectionRow, filterConnectionRows, sortConnectionRows } from "./utils/connectionRows";
import { resourceMatchesOriginScope } from "@nextshell/shared";
import { clampDialogSize, fitDialogToViewport } from "./utils/dialogSize";
import {
  DEFAULT_CONNECTION_COLUMNS,
  type BatchAuthTarget,
  type ConnectionColumnKey,
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
  onClose: () => void;
  onConnectConnection: (connectionId: string) => Promise<void>;
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
  onClose,
  onConnectConnection,
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
  const [columns, setColumns] = useState<ConnectionColumnKey[]>(DEFAULT_CONNECTION_COLUMNS);
  const [contextMenu, setContextMenu] = useState<{
    x: number;
    y: number;
    items: PointerMenuItem[];
  } | null>(null);
  const [copyTarget, setCopyTarget] = useState<string[] | null>(null);
  const [batchAuthTarget, setBatchAuthTarget] = useState<BatchAuthTarget | null>(null);
  const editingConnection = detail.kind === "edit" ? detail.connection : undefined;
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

  const visibleConnections = useMemo(
    () =>
      listVisibleConnections({
        folderId: scope.currentFolderId,
        folders: scope.folders,
        connections: scopedConnections,
        includeSubfolders: true
      }),
    [scope.currentFolderId, scope.folders, scopedConnections]
  );

  // 搜索时无视目录圈定,直接搜整个作用域——找一台机器时没人记得它在哪个目录里。
  const searching = keyword.trim().length > 0;
  const rows = useMemo(() => {
    const base = searching ? scopedConnections : visibleConnections;
    const built = base.map((connection) => buildConnectionRow(connection, scopedSshKeys));
    return sortConnectionRows(filterConnectionRows(built, keyword), sort);
  }, [keyword, scopedSshKeys, searching, scopedConnections, sort, visibleConnections]);

  // 目录/作用域切换后，选中与详情里的连接可能已经不在列表里，留着会显示一张查不到的卡片。
  useEffect(() => {
    const visibleIds = new Set(rows.map((row) => row.connection.id));
    setSelectedIds((previous) => previous.filter((id) => visibleIds.has(id)));
    setDetail((previous) =>
      previous.kind === "view" && !visibleIds.has(previous.connection.id)
        ? { kind: "empty" }
        : previous
    );
  }, [rows]);

  const folderLabel = useMemo(() => {
    const target =
      detail.kind === "view" ? detail.connection.folderId : undefined;
    return buildBreadcrumb(target, scope.folders, scope.activeScope.label)
      .map((segment) => segment.label)
      .join(" / ");
  }, [detail, scope.activeScope.label, scope.folders]);

  const handleConnect = useCallback(
    (connectionId: string) => {
      void onConnectConnection(connectionId).then(onClose);
    },
    [onClose, onConnectConnection]
  );

  const handleSubmit = useCallback(
    async (values: ConnectionEditorValues) => {
      setSaving(true);
      try {
        const host = (values.host ?? "").trim();
        await window.nextshell.connection.upsert({
          ...values,
          name: (values.name ?? "").trim() || `${host}:${values.port}`,
          host,
          username: (values.username ?? "").trim(),
          workspaceId: scope.activeScope.workspaceId,
          // groupPath 由主进程按 folderId 派生；这里给出的值只是旧调用方的兜底。
          groupPath: "/server"
        });
        await onReloadConnections();
        message.success(values.id ? "连接已更新" : "连接已创建");
        setDetail({ kind: "empty" });
      } catch (error) {
        message.error(`保存连接失败：${formatErrorMessage(error, "请检查输入内容")}`);
      } finally {
        setSaving(false);
      }
    },
    [message, onReloadConnections, scope.activeScope.workspaceId]
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
            setSelectedIds([]);
            setDetail({ kind: "empty" });
          } catch (error) {
            message.error(`删除失败：${formatErrorMessage(error, "请稍后重试")}`);
          }
        }
      });
    },
    [message, modal, onReloadConnections, scopedConnections]
  );

  // 导出的目录选择、明文/加密选项与批量结果汇总沿用既有 hook，不重复一套。
  const [exportIds, setExportIds] = useState<string[]>([]);
  const exportSelection = useMemo(() => new Set(exportIds), [exportIds]);
  const { handleExportSelected } = useConnectionExportActions({
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
  useEffect(() => {
    if (exportIds.length === 0) {
      return;
    }
    void handleExportSelected().finally(() => setExportIds([]));
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
          onSelect: () => setDetail({ kind: "edit", connection: target })
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
              try {
                await window.nextshell.connection.upsert({ ...target, name });
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
      message,
      modal,
      onOpenBatchAuth,
      onReloadConnections,
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
    [message, modal, scope]
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
    [message, modal, scope]
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
    [message, modal, onReloadConnections, scope]
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
    [message, onReloadConnections, scope]
  );

  // ── 栏宽拖拽:命令式改 CSS 变量,不触发 React 重渲;松手才落 state 和偏好。──
  const columnsRef = useRef<HTMLDivElement>(null);
  const folderWidthRef = useRef(preferences.folderColumnWidth);

  useEffect(() => {
    folderWidthRef.current = preferences.folderColumnWidth;
    columnsRef.current?.style.setProperty("--cm2-folder-w", `${preferences.folderColumnWidth}px`);
  }, [preferences.folderColumnWidth, resourceTab]);

  const startResize = useCallback(
    (event: React.PointerEvent<HTMLDivElement>) => {
      event.preventDefault();
      const target = event.currentTarget;
      target.setPointerCapture(event.pointerId);
      let last = event.clientX;
      const move = (moveEvent: PointerEvent) => {
        folderWidthRef.current = clampDialogSize(
          "folderColumn",
          folderWidthRef.current + (moveEvent.clientX - last)
        );
        last = moveEvent.clientX;
        columnsRef.current?.style.setProperty("--cm2-folder-w", `${folderWidthRef.current}px`);
      };
      const up = () => {
        target.removeEventListener("pointermove", move);
        target.removeEventListener("pointerup", up);
        void updatePreferences({ connectionManager: { folderColumnWidth: folderWidthRef.current } });
      };
      target.addEventListener("pointermove", move);
      target.addEventListener("pointerup", up);
    },
    [updatePreferences]
  );

  return (
    <Modal
      open={open}
      onCancel={onClose}
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
      <div className="cm2-shell" style={{ height: dialogSize.height }}>
        <ScopeBar
          scopes={scope.scopes}
          activeScope={scope.activeScope}
          onSelectScope={scope.selectScope}
          resourceTab={resourceTab}
          onSelectResource={setResourceTab}
        />

        {resourceTab === "connections" ? (
          <div
            ref={columnsRef}
            className="cm2-columns"
            style={{
              gridTemplateColumns: `var(--cm2-folder-w, ${preferences.folderColumnWidth}px) 6px minmax(0, 1fr) ${preferences.detailColumnWidth}px`
            }}
          >
            <FolderTree
              rootLabel={scope.activeScope.label}
              folders={scope.folders}
              connections={scopedConnections}
              currentFolderId={scope.currentFolderId}
              onSelectFolder={scope.enterFolder}
              onCreateFolder={(parentId) => void handleCreateFolder(parentId)}
              onRenameFolder={(folder) => void handleRenameFolder(folder)}
              onDeleteFolder={handleDeleteFolder}
              onMoveFolder={(id, parentId) => void handleMoveFolder(id, parentId)}
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
                columns={columns}
                onColumnsChange={setColumns}
                onNewConnection={() => setDetail({ kind: "edit", connection: undefined })}
                onImport={() => void importFlow.handleImportNextShell()}
                onExportAll={() => handleExport(scopedConnections.map((item) => item.id))}
              />
              {selectedIds.length > 0 ? (
                <BulkBar
                  count={selectedIds.length}
                  onClear={() => setSelectedIds([])}
                  onBindAuth={() => onOpenBatchAuth(selectedIds)}
                  onCopyToScope={() => setCopyTarget(selectedIds)}
                  onExport={() => handleExport(selectedIds)}
                  onDelete={() => handleDelete(selectedIds)}
                />
              ) : null}
              <ConnectionTable
                rows={rows}
                columns={columns}
                sort={sort}
                onSortChange={setSort}
                selectedIds={selectedIds}
                onSelectionChange={setSelectedIds}
                focusedId={detail.kind === "view" ? detail.connection.id : undefined}
                onFocus={(id) => {
                  const found = rows.find((row) => row.connection.id === id);
                  if (found) {
                    setDetail({ kind: "view", connection: found.connection });
                  }
                }}
                onConnect={handleConnect}
                onRowContextMenu={handleRowContextMenu}
              />
            </div>

            <div className="cm2-detail-col">
              {detail.kind === "view" ? (
                <DetailCard
                  connection={detail.connection}
                  sshKeys={scopedSshKeys}
                  folderLabel={folderLabel}
                  onEdit={() => setDetail({ kind: "edit", connection: detail.connection })}
                  onConnect={() => handleConnect(detail.connection.id)}
                />
              ) : detail.kind === "edit" ? (
                <ConnectionEditor
                  // 换连接时整体重挂载，让 initialValues 生效，也天然清掉上一条的未保存输入。
                  key={detail.connection?.id ?? "__new__"}
                  connection={detail.connection}
                  folders={scope.folders}
                  currentFolderId={scope.currentFolderId}
                  sshKeys={scopedSshKeys}
                  proxies={scopedProxies}
                  saving={saving}
                  revealedPassword={revealedLoginPassword}
                  revealingPassword={revealingLoginPassword}
                  onRevealPassword={() => void handleRevealConnectionPassword()}
                  onSubmit={(values) => void handleSubmit(values)}
                  onCancel={() =>
                    setDetail(
                      detail.connection
                        ? { kind: "view", connection: detail.connection }
                        : { kind: "empty" }
                    )
                  }
                  onCreateKey={() => setResourceTab("keys")}
                />
              ) : (
                <p className="cm2-placeholder">单击一行查看详情，双击直接连接</p>
              )}
            </div>
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
        sourceName={importFlow.currentImportBatch?.fileName}
        onClose={importFlow.resetImportFlow}
        onImported={importFlow.handleImportBatchImported}
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
