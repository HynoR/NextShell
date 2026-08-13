import { useCallback, useEffect, useMemo, useState } from "react";
import type { MouseEvent } from "react";
import { App as AntdApp, Modal } from "antd";
import type { ConnectionFolder, ConnectionProfile, ProxyProfile, SshKeyProfile } from "@nextshell/core";
import { formatErrorMessage } from "../../utils/errorMessage";
import { promptModal } from "../../utils/promptModal";
import { usePreferencesStore } from "../../store/usePreferencesStore";
import { ScopeBar } from "./components/ScopeBar";
import { FolderColumn } from "./components/FolderColumn";
import { useManagerScope } from "./hooks/useManagerScope";
import { listVisibleConnections } from "./utils/folderNavigation";
import { resourceMatchesOriginScope } from "@nextshell/shared";
import { clampDialogSize, fitDialogToViewport } from "./utils/dialogSize";
import type { ResourceTab } from "./types";
import "./connection-manager-v2.css";

interface ConnectionManagerV2Props {
  open: boolean;
  connections: ConnectionProfile[];
  sshKeys: SshKeyProfile[];
  proxies: ProxyProfile[];
  onClose: () => void;
  onConnectConnection: (connectionId: string) => Promise<void>;
  onReloadConnections: () => Promise<void>;
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
 * 重构后的连接管理器。相对旧版的三处结构性改变:
 *  1. 作用域(本地 / 某个云 workspace)是最高层控制,一次只看一个隔离域;
 *  2. 用户目录是实体,导航方式为钻取 + 面包屑,不再靠 groupPath 字符串聚合出无限缩进的树;
 *  3. 对话框可缩放,尺寸与栏宽记住。
 * 云同步与回收站不在这里——它们是全局配置,归设置中心。
 */
export const ConnectionManagerV2 = ({
  open,
  connections,
  sshKeys,
  proxies,
  onClose,
  onConnectConnection,
  onReloadConnections
}: ConnectionManagerV2Props) => {
  const { message, modal } = AntdApp.useApp();
  const preferences = usePreferencesStore((state) => state.preferences.connectionManager);
  const updatePreferences = usePreferencesStore((state) => state.updatePreferences);
  const viewport = useViewport();

  const [resourceTab, setResourceTab] = useState<ResourceTab>("connections");
  const [includeSubfolders, setIncludeSubfolders] = useState(true);
  const [folderColumnWidth, setFolderColumnWidth] = useState(preferences.folderColumnWidth);

  const notifyError = useCallback((text: string) => message.error(text), [message]);
  const scope = useManagerScope({ open, onError: notifyError });

  useEffect(() => {
    setFolderColumnWidth(preferences.folderColumnWidth);
  }, [preferences.folderColumnWidth]);

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
        includeSubfolders
      }),
    [includeSubfolders, scope.currentFolderId, scope.folders, scopedConnections]
  );

  const persistFolderColumnWidth = useCallback(
    (width: number) => {
      const next = clampDialogSize("folderColumn", width);
      setFolderColumnWidth(next);
      void updatePreferences({ connectionManager: { folderColumnWidth: next } });
    },
    [updatePreferences]
  );

  const handleCreateFolder = useCallback(async () => {
    const name = await promptModal(modal, "新建目录", "请输入目录名称");
    if (!name) {
      return;
    }
    try {
      await window.nextshell.connectionFolder.create({
        scopeKey: scope.activeScope.key,
        name,
        parentId: scope.currentFolderId
      });
      await scope.reloadFolders();
    } catch (error) {
      message.error(`新建目录失败：${formatErrorMessage(error, "请稍后重试")}`);
    }
  }, [message, modal, scope]);

  const handleFolderContextMenu = useCallback(
    (event: MouseEvent, folder: ConnectionFolder) => {
      event.preventDefault();
      modal.confirm({
        title: `目录「${folder.name}」`,
        content: "重命名或删除该目录。删除只移除目录本身，里面的连接会回到上一层。",
        okText: "重命名",
        cancelText: "删除",
        okButtonProps: { type: "default" },
        cancelButtonProps: { danger: true },
        onOk: async () => {
          const name = await promptModal(modal, "重命名目录", "请输入新的目录名称");
          if (!name) {
            return;
          }
          try {
            await window.nextshell.connectionFolder.rename({ id: folder.id, name });
            await scope.reloadFolders();
          } catch (error) {
            message.error(`重命名失败：${formatErrorMessage(error, "请稍后重试")}`);
          }
        },
        onCancel: async () => {
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

        <div
          className="cm2-columns"
          style={{ gridTemplateColumns: `${folderColumnWidth}px 1fr ${preferences.detailColumnWidth}px` }}
        >
          <FolderColumn
            rootLabel={scope.activeScope.label}
            folders={scope.folders}
            connections={scopedConnections}
            currentFolderId={scope.currentFolderId}
            includeSubfolders={includeSubfolders}
            onToggleIncludeSubfolders={setIncludeSubfolders}
            onEnterFolder={scope.enterFolder}
            onCreateFolder={() => void handleCreateFolder()}
            onFolderContextMenu={handleFolderContextMenu}
            visibleConnectionCount={visibleConnections.length}
          />

          <ColumnResizer
            onResize={(delta) => setFolderColumnWidth((width) => clampDialogSize("folderColumn", width + delta))}
            onCommit={() => persistFolderColumnWidth(folderColumnWidth)}
          />

          {/* 完整表格与详情/编辑栏在下一步接入；先给出可用的最小列表。 */}
          <div className="cm2-list">
            {resourceTab !== "connections" ? (
              <p className="cm2-placeholder">
                {resourceTab === "keys"
                  ? `${scopedSshKeys.length} 个密钥`
                  : `${scopedProxies.length} 个代理`}
              </p>
            ) : visibleConnections.length === 0 ? (
              <p className="cm2-placeholder">此处没有连接</p>
            ) : (
              visibleConnections.map((connection) => (
                <button
                  key={connection.id}
                  type="button"
                  className="cm2-list-row"
                  // 双击=关闭对话框并直连，与右键编辑、单击查看分开。
                  onDoubleClick={() => {
                    void onConnectConnection(connection.id).then(onClose);
                  }}
                  title={`${connection.name} — 双击连接`}
                >
                  <span className="cm2-list-name">{connection.name}</span>
                  <span className="cm2-list-address">
                    {connection.host}:{connection.port}
                  </span>
                </button>
              ))
            )}
          </div>
          <div className="cm2-placeholder cm2-placeholder--detail">选中后显示详情</div>
        </div>
      </div>
    </Modal>
  );
};

/** 栏宽拖拽。指针捕获让拖出栏外也不丢事件。 */
const ColumnResizer = ({
  onResize,
  onCommit
}: {
  onResize: (delta: number) => void;
  onCommit: () => void;
}) => (
  <div
    className="cm2-resizer"
    role="separator"
    aria-orientation="vertical"
    onPointerDown={(event) => {
      event.currentTarget.setPointerCapture(event.pointerId);
      let last = event.clientX;
      const target = event.currentTarget;
      const move = (moveEvent: PointerEvent) => {
        onResize(moveEvent.clientX - last);
        last = moveEvent.clientX;
      };
      const up = () => {
        target.removeEventListener("pointermove", move);
        target.removeEventListener("pointerup", up);
        onCommit();
      };
      target.addEventListener("pointermove", move);
      target.addEventListener("pointerup", up);
    }}
  />
);
