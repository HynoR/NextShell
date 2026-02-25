import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { App as AntdApp, message, Tabs, Tag } from "antd";
import { Group, Panel, Separator, usePanelRef } from "react-resizable-panels";
import type {
    ConnectionProfile,
    SessionDescriptor,
    SessionType,
    SshKeyProfile,
} from "@nextshell/core";
import type { ConnectionUpsertInput, SessionAuthOverrideInput } from "@nextshell/shared";
import { CommandCenterPane } from "./CommandCenterPane";
import { QuickConnectBar } from "./QuickConnectBar";
import { CommandInputBar } from "./CommandInputBar";
import { ConnectionTreePanel } from "./ConnectionTreePanel";
import { EditorPane } from "./EditorPane";
import { FileExplorerPane } from "./FileExplorerPane";
import { QuickTransferPane } from "./QuickTransferPane";
import { LiveEditPane } from "./LiveEditPane";
import { NetworkMonitorPane } from "./NetworkMonitorPane";
import { PortForwardPane } from "./PortForwardPane";
import { ProcessManagerPane } from "./ProcessManagerPane";
import { PingCard } from "./PingCard";
import { SystemInfoPanel } from "./SystemInfoPanel";
import { SystemStaticInfoPane } from "./SystemStaticInfoPane";
import { TerminalPane, type TerminalPaneHandle } from "./TerminalPane";
import { TransferQueuePanel } from "./TransferQueuePanel";
import { TraceroutePane } from "./TraceroutePane";
import { useCommandHistory } from "../hooks/useCommandHistory";
import type { TransferTask } from "../store/useTransferQueueStore";
import { formatErrorMessage } from "../utils/errorMessage";
import { promptModal } from "../utils/promptModal";

const SESSION_TYPE_ICON: Record<SessionType, string> = {
    terminal: "ri-terminal-line",
    processManager: "ri-cpu-line",
    networkMonitor: "ri-global-line",
    editor: "ri-file-code-line",
};

const isTerminalSession = (session: SessionDescriptor): boolean =>
    !session.type || session.type === "terminal";

interface WorkspaceLayoutProps {
    connections: ConnectionProfile[];
    sshKeys: SshKeyProfile[];
    sessions: SessionDescriptor[];
    activeConnectionId?: string;
    activeSessionId?: string;
    activeConnection?: ConnectionProfile;
    activeSession?: SessionDescriptor;
    activeSessionConnection?: ConnectionProfile;
    activeTerminalSession?: SessionDescriptor;
    activeTerminalConnection?: ConnectionProfile;
    activeConnectionConnectedTerminalSessionId?: string;
    terminalSessionIds: string[];
    isActiveConnectionTerminalConnected: boolean;
    monitor?: import("@nextshell/core").MonitorSnapshot;
    transferTasks: TransferTask[];
    transferPanelCollapsed: boolean;
    bottomTab: string;
    onLoadConnections: () => void;
    onOpenManager: () => void;
    onOpenSettings: () => void;
    onActivateConnection: (connectionId: string) => void;
    onTreeDoubleConnect: (connectionId: string) => void;
    onTreeConnect: (connectionId: string) => void;
    onTreeQuickSaveConnection: (payload: ConnectionUpsertInput) => Promise<void>;
    onTitlebarQuickConnect: (raw: string) => Promise<boolean>;
    onTreeEditServer: (connectionId: string) => void;
    onCloseSession: (sessionId: string) => void;
    onReconnectSession: (sessionId: string) => void;
    onRenameSession: (sessionId: string, title: string) => void;
    onOpenProcessManager: (connectionId: string) => void;
    onOpenNetworkMonitor: (connectionId: string) => void;
    onCloseMonitorTab: (sessionId: string) => void;
    onOpenEditorTab: (connectionId: string, remotePath: string) => Promise<void>;
    onRetrySessionAuth: (
        sessionId: string,
        authOverride: SessionAuthOverrideInput,
    ) => Promise<{ ok: true } | { ok: false; authRequired: boolean; reason: string }>;
    onSetActiveSession: (sessionId?: string) => void;
    onSetActiveConnection: (connectionId?: string) => void;
    onReorderSession: (sourceId: string, targetId: string) => void;
    onSelectNetworkInterface: (networkInterface: string) => void;
    onRetryTransfer: (taskId: string) => void;
    onClearFinishedTransfers: () => void;
    onOpenLocalFile: (task: TransferTask) => void;
    onTransferPanelToggle: () => void;
    onSetBottomTab: (tab: string) => void;
}

export const WorkspaceLayout = ({
    connections,
    sshKeys,
    sessions,
    activeConnectionId,
    activeSessionId,
    activeConnection,
    activeSession,
    activeSessionConnection,
    activeTerminalSession,
    activeTerminalConnection,
    activeConnectionConnectedTerminalSessionId,
    terminalSessionIds,
    isActiveConnectionTerminalConnected,
    monitor,
    transferTasks,
    transferPanelCollapsed,
    bottomTab,
    onLoadConnections,
    onOpenManager,
    onOpenSettings,
    onActivateConnection,
    onTreeDoubleConnect,
    onTreeConnect,
    onTreeQuickSaveConnection,
    onTitlebarQuickConnect,
    onTreeEditServer,
    onCloseSession,
    onReconnectSession,
    onRenameSession,
    onOpenProcessManager,
    onOpenNetworkMonitor,
    onCloseMonitorTab,
    onOpenEditorTab,
    onRetrySessionAuth,
    onSetActiveSession,
    onSetActiveConnection,
    onReorderSession,
    onSelectNetworkInterface,
    onRetryTransfer,
    onClearFinishedTransfers,
    onOpenLocalFile,
    onTransferPanelToggle,
    onSetBottomTab,
}: WorkspaceLayoutProps) => {
    const { modal } = AntdApp.useApp();
    const [draggingSessionId, setDraggingSessionId] = useState<string>();
    const [bottomCollapsed, setBottomCollapsed] = useState(false);
    const [terminalSearchMode, setTerminalSearchMode] = useState(false);
    const [terminalSearchTerm, setTerminalSearchTerm] = useState("");
    const [addressCopied, setAddressCopied] = useState(false);
    const [updateReleaseUrl, setUpdateReleaseUrl] = useState<string | null>(null);
    const bottomPanelRef = usePanelRef();
    const terminalPaneRef = useRef<TerminalPaneHandle | null>(null);
    const resizeFitRafRef = useRef(0);
    const commandHistory = useCommandHistory();

    const handleExecuteCommand = useCallback(
        (command: string) => {
            if (!activeTerminalSession || activeTerminalSession.status !== "connected") {
                return;
            }
            window.nextshell.session
                .write({ sessionId: activeTerminalSession.id, data: `${command}\r` })
                .catch(() => message.error("发送命令失败"));
            void commandHistory.push(command);
        },
        [activeTerminalSession, commandHistory],
    );

    const headerSessionText = useMemo(() => {
        if (!activeSession) return "no session";
        const sessionIndex = activeSession.title.match(/#\d+$/)?.[0];
        const baseLabel =
            activeSessionConnection?.name?.trim() ||
            activeSessionConnection?.host?.trim() ||
            activeSession.title.replace(/\s+#\d+$/, "").trim() ||
            "session";
        return `${activeSession.status} ${baseLabel}${sessionIndex ? ` ${sessionIndex}` : ""}`;
    }, [activeSession, activeSessionConnection]);

    const headerSessionClass = activeSession?.status ?? "disconnected";

    const handleTerminalSearchTermChange = useCallback((value: string) => {
        setTerminalSearchTerm(value);
        terminalPaneRef.current?.setSearchTerm(value);
    }, []);

    const handleTerminalSearchNext = useCallback(() => {
        terminalPaneRef.current?.setSearchTerm(terminalSearchTerm);
        terminalPaneRef.current?.findNext();
    }, [terminalSearchTerm]);

    const handleTerminalSearchPrevious = useCallback(() => {
        terminalPaneRef.current?.setSearchTerm(terminalSearchTerm);
        terminalPaneRef.current?.findPrevious();
    }, [terminalSearchTerm]);

    const handleRequestTerminalSearchMode = useCallback(() => {
        setTerminalSearchMode(true);
    }, []);

    const handleOpenProcessManagerFromMonitor = useCallback(() => {
        if (!activeConnectionId) return;
        onOpenProcessManager(activeConnectionId);
    }, [activeConnectionId, onOpenProcessManager]);

    const handleOpenNetworkMonitorFromMonitor = useCallback(() => {
        if (!activeConnectionId) return;
        onOpenNetworkMonitor(activeConnectionId);
    }, [activeConnectionId, onOpenNetworkMonitor]);

    const sidebarAddress = activeSessionConnection
        ? `${activeSessionConnection.host}:${activeSessionConnection.port}`
        : null;

    const handleCopyAddress = useCallback(() => {
        if (!sidebarAddress) return;
        navigator.clipboard
            .writeText(sidebarAddress)
            .then(() => {
                setAddressCopied(true);
                setTimeout(() => setAddressCopied(false), 1500);
            })
            .catch(() => undefined);
    }, [sidebarAddress]);

    useEffect(() => {
        let disposed = false;
        void (async () => {
            try {
                const result = await window.nextshell.about.checkUpdate();
                if (disposed) return;
                setUpdateReleaseUrl(
                    result.hasUpdate && result.releaseUrl ? result.releaseUrl : null,
                );
            } catch {
                if (!disposed) {
                    setUpdateReleaseUrl(null);
                }
            }
        })();
        return () => {
            disposed = true;
        };
    }, []);

    const handleOpenReleasePage = useCallback(async () => {
        if (!updateReleaseUrl) return;
        const result = await window.nextshell.dialog.openPath({
            path: updateReleaseUrl,
            revealInFolder: false,
        });
        if (!result.ok) {
            void message.error(`打开链接失败：${formatErrorMessage(result.error, "请稍后重试")}`);
        }
    }, [updateReleaseUrl]);

    return (
        <div className="h-screen flex flex-col overflow-hidden">
            <header className="shell-header">
                <div className="titlebar-brand" />
                <div className="titlebar-center">
                    <QuickConnectBar
                        connections={connections}
                        sessions={sessions}
                        onConnect={(connectionId) => void onTreeConnect(connectionId)}
                        onQuickConnectInput={onTitlebarQuickConnect}
                    />
                </div>
                <div className="header-actions">
                    {updateReleaseUrl ? (
                        <>
                            <a
                                href={updateReleaseUrl}
                                target="_blank"
                                rel="noopener noreferrer"
                                onClick={(event) => {
                                    event.preventDefault();
                                    void handleOpenReleasePage();
                                }}
                            >
                                <Tag color="green">有更新</Tag>
                            </a>
                            <span className="hdr-sep" />
                        </>
                    ) : null}
                    <button className="hdr-btn" onClick={onOpenManager} title="管理连接">
                        <i className="ri-links-line" aria-hidden="true" />
                        服务器
                    </button>
                    <button className="hdr-btn" onClick={onOpenSettings} title="打开设置中心">
                        <i className="ri-settings-3-line" aria-hidden="true" />
                        设置
                    </button>
                </div>
            </header>

            <main className="flex flex-1 min-w-0 min-h-0 overflow-hidden">
                <Group orientation="horizontal" className="w-full h-full min-w-0 min-h-0">
                    <Panel defaultSize="18%" minSize="14%" maxSize="36%">
                        <aside className="w-full h-full flex flex-col bg-[var(--bg-surface)] border-r border-[var(--border)] overflow-hidden">
                            <div className={`sidebar-session-card ${headerSessionClass}`}>
                                <div className="sidebar-session-row">
                                    <span className="sidebar-session-dot" />
                                    <span className="sidebar-session-status">
                                        {activeSession?.status ?? "disconnected"}
                                    </span>
                                    <button
                                        type="button"
                                        className="sidebar-refresh-btn"
                                        onClick={() => void onLoadConnections()}
                                        title="刷新连接列表"
                                    >
                                        <i className="ri-refresh-line" aria-hidden="true" />
                                    </button>
                                </div>
                                {sidebarAddress ? (
                                    <button
                                        type="button"
                                        className="sidebar-session-addr"
                                        title={addressCopied ? "已复制" : "点击复制地址"}
                                        onClick={handleCopyAddress}
                                    >
                                        {addressCopied ? (
                                            <>
                                                <i className="ri-check-line" aria-hidden="true" />{" "}
                                                已复制
                                            </>
                                        ) : (
                                            <>
                                                <i
                                                    className="ri-clipboard-line"
                                                    aria-hidden="true"
                                                />{" "}
                                                {sidebarAddress}
                                            </>
                                        )}
                                    </button>
                                ) : (
                                    <span className="sidebar-session-addr empty">未选择服务器</span>
                                )}
                            </div>
                            {activeConnection?.monitorSession ? (
                                <SystemInfoPanel
                                    monitorSessionEnabled
                                    hasVisibleTerminal={isActiveConnectionTerminalConnected}
                                    snapshot={monitor}
                                    onSelectNetworkInterface={onSelectNetworkInterface}
                                    onOpenProcessManager={handleOpenProcessManagerFromMonitor}
                                    onOpenNetworkMonitor={handleOpenNetworkMonitorFromMonitor}
                                    monitorActionsDisabled={
                                        !activeConnectionId || !isActiveConnectionTerminalConnected
                                    }
                                />
                            ) : null}
                            <PingCard host={activeConnection?.host} />
                            <TransferQueuePanel
                                tasks={transferTasks}
                                collapsed={transferPanelCollapsed}
                                onToggle={onTransferPanelToggle}
                                onRetry={(taskId) => void onRetryTransfer(taskId)}
                                onClearFinished={onClearFinishedTransfers}
                                onOpenLocalFile={(task) => {
                                    if (
                                        task.direction === "download" &&
                                        task.status === "success"
                                    ) {
                                        onOpenLocalFile(task);
                                    }
                                }}
                            />
                        </aside>
                    </Panel>
                    <Separator className="panel-resize-handle horizontal" />
                    <Panel minSize="48%">
                        <section className="w-full h-full min-w-0 min-h-0 flex flex-col overflow-hidden">
                            <Group orientation="vertical" className="w-full h-full min-w-0 min-h-0">
                                <Panel defaultSize="68%" minSize="38%">
                                    <div className="terminal-shell">
                                        <div className="session-tabs">
                                            {sessions.map((session) => {
                                                const isTerminal = isTerminalSession(session);
                                                const iconClass =
                                                    SESSION_TYPE_ICON[session.type ?? "terminal"];
                                                return (
                                                    <button
                                                        key={session.id}
                                                        type="button"
                                                        className={[
                                                            "session-tab",
                                                            session.id === activeSessionId
                                                                ? "active"
                                                                : "",
                                                            session.id === draggingSessionId
                                                                ? "dragging"
                                                                : "",
                                                        ]
                                                            .filter(Boolean)
                                                            .join(" ")}
                                                        onClick={() => {
                                                            onSetActiveSession(session.id);
                                                            onSetActiveConnection(
                                                                session.connectionId,
                                                            );
                                                        }}
                                                        onDoubleClick={() => {
                                                            if (!isTerminal) return;
                                                            void (async () => {
                                                                const title = await promptModal(
                                                                    modal,
                                                                    "会话标题",
                                                                    undefined,
                                                                    session.title,
                                                                );
                                                                if (title)
                                                                    onRenameSession(
                                                                        session.id,
                                                                        title,
                                                                    );
                                                            })();
                                                        }}
                                                        draggable={isTerminal}
                                                        onDragStart={() => {
                                                            if (isTerminal)
                                                                setDraggingSessionId(session.id);
                                                        }}
                                                        onDragEnd={() =>
                                                            setDraggingSessionId(undefined)
                                                        }
                                                        onDragOver={(event) =>
                                                            event.preventDefault()
                                                        }
                                                        onDrop={(event) => {
                                                            event.preventDefault();
                                                            if (!draggingSessionId) return;
                                                            onReorderSession(
                                                                draggingSessionId,
                                                                session.id,
                                                            );
                                                            setDraggingSessionId(undefined);
                                                        }}
                                                    >
                                                        <i
                                                            className={`tab-type-icon ${iconClass}`}
                                                            aria-hidden="true"
                                                        />
                                                        <span className="session-title">
                                                            {session.title}
                                                        </span>
                                                        {isTerminal &&
                                                        session.status !== "connected" ? (
                                                            <span
                                                                className="tab-action tab-reconnect"
                                                                title="重新连接"
                                                                onClick={(event) => {
                                                                    event.stopPropagation();
                                                                    void onReconnectSession(
                                                                        session.id,
                                                                    );
                                                                }}
                                                                role="button"
                                                                tabIndex={0}
                                                                onKeyDown={(event) => {
                                                                    if (
                                                                        event.key === "Enter" ||
                                                                        event.key === " "
                                                                    ) {
                                                                        event.preventDefault();
                                                                        void onReconnectSession(
                                                                            session.id,
                                                                        );
                                                                    }
                                                                }}
                                                            >
                                                                <i
                                                                    className="ri-refresh-line"
                                                                    aria-hidden="true"
                                                                />
                                                            </span>
                                                        ) : null}
                                                        {isTerminal ? (
                                                            <span
                                                                className="tab-action tab-drag"
                                                                title="拖拽重排 / 双击重命名"
                                                            >
                                                                <i
                                                                    className="ri-drag-move-2-line"
                                                                    aria-hidden="true"
                                                                />
                                                            </span>
                                                        ) : null}
                                                        <span
                                                            className="tab-action tab-close"
                                                            title="关闭会话"
                                                            onClick={(event) => {
                                                                event.stopPropagation();
                                                                if (isTerminal) {
                                                                    void onCloseSession(session.id);
                                                                } else {
                                                                    onCloseMonitorTab(session.id);
                                                                }
                                                            }}
                                                            role="button"
                                                            tabIndex={0}
                                                            onKeyDown={(event) => {
                                                                if (
                                                                    event.key === "Enter" ||
                                                                    event.key === " "
                                                                ) {
                                                                    event.preventDefault();
                                                                    if (isTerminal) {
                                                                        void onCloseSession(
                                                                            session.id,
                                                                        );
                                                                    } else {
                                                                        onCloseMonitorTab(
                                                                            session.id,
                                                                        );
                                                                    }
                                                                }
                                                            }}
                                                        >
                                                            <i
                                                                className="ri-close-line"
                                                                aria-hidden="true"
                                                            />
                                                        </span>
                                                    </button>
                                                );
                                            })}
                                        </div>
                                        <div
                                            className={
                                                activeSession?.type === "processManager" ||
                                                activeSession?.type === "networkMonitor" ||
                                                activeSession?.type === "editor"
                                                    ? "hidden"
                                                    : "flex-1 min-h-0 flex flex-col"
                                            }
                                        >
                                            <TerminalPane
                                                ref={terminalPaneRef}
                                                session={activeTerminalSession}
                                                connection={activeTerminalConnection}
                                                sessionIds={terminalSessionIds}
                                                onRetrySessionAuth={onRetrySessionAuth}
                                                onRequestSearchMode={
                                                    handleRequestTerminalSearchMode
                                                }
                                            />
                                            <CommandInputBar
                                                session={activeTerminalSession}
                                                commandHistory={commandHistory}
                                                searchMode={terminalSearchMode}
                                                onSearchModeChange={setTerminalSearchMode}
                                                terminalSearchTerm={terminalSearchTerm}
                                                onTerminalSearchTermChange={
                                                    handleTerminalSearchTermChange
                                                }
                                                onTerminalSearchNext={handleTerminalSearchNext}
                                                onTerminalSearchPrevious={
                                                    handleTerminalSearchPrevious
                                                }
                                            />
                                        </div>
                                        {activeSession?.type === "processManager" ? (
                                            <ProcessManagerPane session={activeSession} />
                                        ) : null}
                                        {activeSession?.type === "networkMonitor" ? (
                                            <NetworkMonitorPane session={activeSession} />
                                        ) : null}
                                        {activeSession?.type === "editor" ? (
                                            <EditorPane session={activeSession} />
                                        ) : null}
                                    </div>
                                </Panel>
                                <Separator className="panel-resize-handle vertical" />
                                <Panel
                                    panelRef={bottomPanelRef}
                                    defaultSize="32%"
                                    minSize="16%"
                                    collapsible
                                    collapsedSize="4%"
                                    onResize={() => {
                                        setBottomCollapsed(
                                            bottomPanelRef.current?.isCollapsed() ?? false,
                                        );
                                        cancelAnimationFrame(resizeFitRafRef.current);
                                        resizeFitRafRef.current = requestAnimationFrame(() => {
                                            terminalPaneRef.current?.fit();
                                        });
                                    }}
                                >
                                    <div className="bottom-workbench">
                                        <Tabs
                                            activeKey={bottomTab}
                                            onChange={onSetBottomTab}
                                            tabBarExtraContent={{
                                                right: (
                                                    <button
                                                        type="button"
                                                        className="bottom-collapse-btn"
                                                        title={
                                                            bottomCollapsed
                                                                ? "展开面板"
                                                                : "折叠面板"
                                                        }
                                                        onClick={() => {
                                                            if (bottomCollapsed) {
                                                                bottomPanelRef.current?.expand();
                                                            } else {
                                                                bottomPanelRef.current?.collapse();
                                                            }
                                                        }}
                                                    >
                                                        <i
                                                            className={
                                                                bottomCollapsed
                                                                    ? "ri-arrow-up-s-line"
                                                                    : "ri-arrow-down-s-line"
                                                            }
                                                            aria-hidden="true"
                                                        />
                                                    </button>
                                                ),
                                            }}
                                            items={[
                                                {
                                                    key: "connections",
                                                    label: "连接中心",
                                                    children: (
                                                        <ConnectionTreePanel
                                                            connections={connections}
                                                            sshKeys={sshKeys}
                                                            sessions={sessions}
                                                            activeConnectionId={activeConnectionId}
                                                            onSelect={onActivateConnection}
                                                            onConnectByDoubleClick={(
                                                                connectionId,
                                                            ) => {
                                                                void onTreeDoubleConnect(
                                                                    connectionId,
                                                                );
                                                            }}
                                                            onConnect={(connectionId) => {
                                                                void onTreeConnect(connectionId);
                                                            }}
                                                            onQuickSave={(payload) =>
                                                                onTreeQuickSaveConnection(payload)
                                                            }
                                                            onOpenManagerForConnection={
                                                                onTreeEditServer
                                                            }
                                                        />
                                                    ),
                                                },
                                                {
                                                    key: "files",
                                                    label: "SFTP",
                                                    children: (
                                                        <FileExplorerPane
                                                            connection={activeConnection}
                                                            connected={
                                                                isActiveConnectionTerminalConnected
                                                            }
                                                            onOpenSettings={onOpenSettings}
                                                            onOpenEditorTab={onOpenEditorTab}
                                                        />
                                                    ),
                                                },
                                                {
                                                    key: "quick-transfer",
                                                    label: "文件快传",
                                                    children: bottomTab === "quick-transfer" ? (
                                                        <QuickTransferPane
                                                            sourceConnection={activeConnection}
                                                            connected={
                                                                isActiveConnectionTerminalConnected
                                                            }
                                                            active
                                                            connections={connections}
                                                            sessions={sessions}
                                                        />
                                                    ) : null,
                                                },
                                                {
                                                    key: "live-edit",
                                                    label: "实时编辑",
                                                    children: (
                                                        <LiveEditPane connections={connections} />
                                                    ),
                                                },
                                                {
                                                    key: "port-forward",
                                                    label: "端口转发",
                                                    children: (
                                                        <PortForwardPane
                                                            connection={activeConnection}
                                                            connected={isActiveConnectionTerminalConnected}
                                                            onSaveConnection={onTreeQuickSaveConnection}
                                                        />
                                                    ),
                                                },
                                                {
                                                    key: "commands",
                                                    label: "命令",
                                                    children: (
                                                        <CommandCenterPane
                                                            connection={activeConnection}
                                                            connected={
                                                                isActiveConnectionTerminalConnected
                                                            }
                                                            connections={connections}
                                                            sessions={sessions}
                                                            onExecuteCommand={handleExecuteCommand}
                                                        />
                                                    ),
                                                },
                                                {
                                                    key: "system-info",
                                                    label: "系统信息",
                                                    children: (
                                                        <SystemStaticInfoPane
                                                            connection={activeConnection}
                                                            connected={
                                                                isActiveConnectionTerminalConnected
                                                            }
                                                            active={bottomTab === "system-info"}
                                                            connectedTerminalSessionId={
                                                                activeConnectionConnectedTerminalSessionId
                                                            }
                                                            onOpenSettings={onOpenSettings}
                                                        />
                                                    ),
                                                },
                                                {
                                                    key: "traceroute",
                                                    label: "路由追踪",
                                                    children: (
                                                        <TraceroutePane
                                                            connection={activeConnection}
                                                            connected={
                                                                isActiveConnectionTerminalConnected
                                                            }
                                                        />
                                                    ),
                                                },
                                            ]}
                                        />
                                    </div>
                                </Panel>
                            </Group>
                        </section>
                    </Panel>
                </Group>
            </main>
        </div>
    );
};
