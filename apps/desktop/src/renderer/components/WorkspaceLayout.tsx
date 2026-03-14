import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { App as AntdApp, message, Tabs, Tag } from "antd";
import { Group, Panel, Separator, usePanelRef } from "react-resizable-panels";
import type {
    ConnectionProfile,
    SessionDescriptor,
    SessionType,
    SshKeyProfile,
} from "@nextshell/core";
import type { SessionAuthOverrideInput } from "@nextshell/shared";
import { CommandCenterPane } from "./CommandCenterPane";
import { QuickConnectBar } from "./QuickConnectBar";
import { CommandInputBar } from "./CommandInputBar";
import { EditorPane } from "./EditorPane";
import { FileExplorerPane } from "./FileExplorerPane";
import { QuickTransferPane } from "./QuickTransferPane";
import { LiveEditPane } from "./LiveEditPane";
import { NetworkMonitorPane } from "./NetworkMonitorPane";
import { ProcessManagerPane } from "./ProcessManagerPane";
import { PingCard } from "./PingCard";
import { SystemInfoPanel } from "./SystemInfoPanel";
import { SystemStaticInfoPane } from "./SystemStaticInfoPane";
import { TerminalPane, type TerminalPaneHandle } from "./TerminalPane";
import { TransferQueuePanel } from "./TransferQueuePanel";
import { TraceroutePane } from "./TraceroutePane";
import { useCommandHistory } from "../hooks/useCommandHistory";
import { usePreferencesStore } from "../store/usePreferencesStore";
import type { TransferTask } from "../store/useTransferQueueStore";
import { formatErrorMessage } from "../utils/errorMessage";
import type { QuickCreateConnectionInput } from "../utils/quickConnectInput";
import { promptModal } from "../utils/promptModal";
import {
    persistWorkspacePanelState,
    resolveWorkspacePanelState,
} from "../utils/workspaceLayoutState";

const SESSION_TYPE_ICON: Record<SessionType, string> = {
    terminal: "ri-terminal-line",
    processManager: "ri-cpu-line",
    networkMonitor: "ri-global-line",
    editor: "ri-file-code-line",
};

const isTerminalSession = (session: SessionDescriptor): boolean =>
    !session.type || session.type === "terminal";

const LEFT_SIDEBAR_STORAGE_KEY = "nextshell.workspace.leftSidebarCollapsed";
const LEFT_SIDEBAR_WIDTH_EXPANDED = 240;
const LEFT_SIDEBAR_WIDTH_COLLAPSED = 52;
const BOTTOM_WORKBENCH_STORAGE_KEY = "nextshell.workspace.bottomWorkbenchCollapsed";

const getWorkspaceLayoutStorage = (): Storage | undefined => {
    if (typeof window === "undefined") {
        return undefined;
    }

    try {
        return window.localStorage;
    } catch {
        return undefined;
    }
};

interface SessionTabContextMenuState {
    x: number;
    y: number;
    sessionId: string;
}

const SessionTabContextMenu = ({
    state,
    session,
    displayTitle,
    onClose,
    onRename,
}: {
    state: SessionTabContextMenuState;
    session: SessionDescriptor;
    displayTitle: string;
    onClose: () => void;
    onRename: (session: SessionDescriptor) => void;
}) => {
    const menuRef = useRef<HTMLDivElement>(null);
    const [pos, setPos] = useState<{ left: number; top: number }>({
        left: state.x,
        top: state.y,
    });
    const [visible, setVisible] = useState(false);

    useLayoutEffect(() => {
        const element = menuRef.current;
        if (!element) {
            return;
        }

        const { offsetWidth: width, offsetHeight: height } = element;
        const viewportWidth = window.innerWidth;
        const viewportHeight = window.innerHeight;
        const gap = 4;

        let top = state.y - height - gap;
        if (top < gap) {
            top = state.y + gap;
        }
        if (top + height > viewportHeight - gap) {
            top = viewportHeight - height - gap;
        }

        let left = state.x;
        if (left + width > viewportWidth - gap) {
            left = state.x - width;
        }
        if (left < gap) {
            left = gap;
        }

        setPos({ left, top });
        setVisible(true);
    }, [state.x, state.y]);

    useEffect(() => {
        const handleWindowMouseDown = (event: MouseEvent) => {
            if (menuRef.current && !menuRef.current.contains(event.target as Node)) {
                onClose();
            }
        };
        const handleEscape = (event: KeyboardEvent) => {
            if (event.key === "Escape") {
                onClose();
            }
        };

        window.addEventListener("mousedown", handleWindowMouseDown);
        window.addEventListener("keydown", handleEscape);
        return () => {
            window.removeEventListener("mousedown", handleWindowMouseDown);
            window.removeEventListener("keydown", handleEscape);
        };
    }, [onClose]);

    return (
        <div
            ref={menuRef}
            className="session-tab-menu"
            style={{ left: pos.left, top: pos.top, visibility: visible ? "visible" : "hidden" }}
            onContextMenu={(event) => event.preventDefault()}
        >
            <button
                className="session-tab-menu-item"
                onClick={() => {
                    onRename(session);
                    onClose();
                }}
            >
                <span className="session-tab-menu-icon">
                    <i className="ri-edit-line" aria-hidden="true" />
                </span>
                重命名当前标签
            </button>
            <div className="session-tab-menu-hint" title={displayTitle}>
                {displayTitle}
            </div>
        </div>
    );
};

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
    followTerminalSessionId?: string;
    terminalSessionIds: string[];
    isActiveConnectionTerminalConnected: boolean;
    monitor?: import("@nextshell/core").MonitorSnapshot;
    transferTasks: TransferTask[];
    transferPanelCollapsed: boolean;
    liveEditPanelCollapsed: boolean;
    bottomTab: string;
    onLoadConnections: () => void;
    onOpenManager: () => void;
    onOpenSettings: () => void;
    onActivateConnection: (connectionId: string) => void;
    onTreeConnect: (connectionId: string) => void;
    onTitlebarQuickConnect: (raw: string) => Promise<boolean>;
    onTitlebarQuickCreateConnection: (input: QuickCreateConnectionInput) => Promise<boolean>;
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
    onLiveEditPanelToggle: () => void;
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
    followTerminalSessionId,
    terminalSessionIds,
    isActiveConnectionTerminalConnected,
    monitor,
    transferTasks,
    transferPanelCollapsed,
    liveEditPanelCollapsed,
    bottomTab,
    onLoadConnections,
    onOpenManager,
    onOpenSettings,
    onActivateConnection,
    onTreeConnect,
    onTitlebarQuickConnect,
    onTitlebarQuickCreateConnection,
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
    onLiveEditPanelToggle,
    onSetBottomTab,
}: WorkspaceLayoutProps) => {
    const { modal } = AntdApp.useApp();
    const windowPreferences = usePreferencesStore((state) => state.preferences.window);
    const [draggingSessionId, setDraggingSessionId] = useState<string>();
    const [leftSidebarCollapsed, setLeftSidebarCollapsed] = useState(() =>
        resolveWorkspacePanelState(
            getWorkspaceLayoutStorage(),
            LEFT_SIDEBAR_STORAGE_KEY,
            windowPreferences.leftSidebarDefaultCollapsed,
        ),
    );
    const [bottomCollapsed, setBottomCollapsed] = useState(() =>
        resolveWorkspacePanelState(
            getWorkspaceLayoutStorage(),
            BOTTOM_WORKBENCH_STORAGE_KEY,
            windowPreferences.bottomWorkbenchDefaultCollapsed,
        ),
    );
    const [terminalSearchMode, setTerminalSearchMode] = useState(false);
    const [terminalSearchTerm, setTerminalSearchTerm] = useState("");
    const [addressCopied, setAddressCopied] = useState(false);
    const [updateReleaseUrl, setUpdateReleaseUrl] = useState<string | null>(null);
    const [sessionContextMenu, setSessionContextMenu] =
        useState<SessionTabContextMenuState | null>(null);
    const bottomPanelRef = usePanelRef();
    const syncingBottomPanelRef = useRef(false);
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
        const baseLabel =
            activeSessionConnection?.name?.trim() ||
            activeSessionConnection?.host?.trim() ||
            activeSession.title ||
            "session";
        return `${activeSession.status} ${baseLabel}`;
    }, [activeSession, activeSessionConnection]);

    const headerSessionClass = activeSession?.status ?? "disconnected";

    const contextMenuSession = useMemo(
        () =>
            sessionContextMenu
                ? sessions.find(
                      (session) =>
                          session.id === sessionContextMenu.sessionId &&
                          isTerminalSession(session),
                  )
                : undefined,
        [sessionContextMenu, sessions],
    );
    const contextMenuSessionTitle = useMemo(
        () => contextMenuSession?.title,
        [contextMenuSession],
    );

    useEffect(() => {
        if (sessionContextMenu && !contextMenuSession) {
            setSessionContextMenu(null);
        }
    }, [contextMenuSession, sessionContextMenu]);

    const handlePromptRenameSession = useCallback(
        async (session: SessionDescriptor) => {
            const title = await promptModal(modal, "会话标题", undefined, session.title);
            if (title) {
                onRenameSession(session.id, title);
            }
        },
        [modal, onRenameSession],
    );

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

    const handleSessionTabContextMenu = useCallback(
        (event: React.MouseEvent<HTMLButtonElement>, session: SessionDescriptor) => {
            if (!isTerminalSession(session)) {
                return;
            }

            event.preventDefault();
            event.stopPropagation();
            onSetActiveSession(session.id);
            onSetActiveConnection(session.connectionId);
            setSessionContextMenu({
                x: event.clientX,
                y: event.clientY,
                sessionId: session.id,
            });
        },
        [onSetActiveConnection, onSetActiveSession],
    );

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

    useEffect(() => {
        const panel = bottomPanelRef.current;
        if (!panel) {
            return;
        }
        if (bottomCollapsed !== panel.isCollapsed()) {
            syncingBottomPanelRef.current = true;
            if (bottomCollapsed) {
                panel.collapse();
            } else {
                panel.expand();
            }
            const rafId = requestAnimationFrame(() => {
                syncingBottomPanelRef.current = false;
            });
            return () => cancelAnimationFrame(rafId);
        }
    }, [bottomCollapsed, bottomPanelRef]);

    useEffect(() => {
        cancelAnimationFrame(resizeFitRafRef.current);
        resizeFitRafRef.current = requestAnimationFrame(() => {
            terminalPaneRef.current?.fit();
        });
    }, [bottomCollapsed]);

    const syncBottomCollapsed = useCallback(
        (_panelSize?: unknown, _panelId?: string | number, prevPanelSize?: unknown) => {
        if (prevPanelSize === undefined) {
            return;
        }
        const collapsed = bottomPanelRef.current?.isCollapsed() ?? false;
        setBottomCollapsed(collapsed);
        if (syncingBottomPanelRef.current) {
            syncingBottomPanelRef.current = false;
        } else {
            persistWorkspacePanelState(
                getWorkspaceLayoutStorage(),
                BOTTOM_WORKBENCH_STORAGE_KEY,
                collapsed,
            );
        }
        cancelAnimationFrame(resizeFitRafRef.current);
        resizeFitRafRef.current = requestAnimationFrame(() => {
            terminalPaneRef.current?.fit();
        });
    }, [bottomPanelRef]);

    const setLeftSidebarCollapsedWithPersistence = useCallback((collapsed: boolean) => {
        persistWorkspacePanelState(
            getWorkspaceLayoutStorage(),
            LEFT_SIDEBAR_STORAGE_KEY,
            collapsed,
        );
        setLeftSidebarCollapsed(collapsed);
    }, []);

    const setBottomCollapsedWithPersistence = useCallback((collapsed: boolean) => {
        persistWorkspacePanelState(
            getWorkspaceLayoutStorage(),
            BOTTOM_WORKBENCH_STORAGE_KEY,
            collapsed,
        );
        setBottomCollapsed(collapsed);
    }, []);

    const handleToggleLeftSidebar = useCallback(() => {
        setLeftSidebarCollapsedWithPersistence(!leftSidebarCollapsed);
    }, [leftSidebarCollapsed, setLeftSidebarCollapsedWithPersistence]);

    const handleToggleBottomWorkbench = useCallback(() => {
        setBottomCollapsedWithPersistence(!bottomCollapsed);
    }, [bottomCollapsed, setBottomCollapsedWithPersistence]);

    const collapsedTransferCount = transferTasks.length > 99 ? "99+" : String(transferTasks.length);

    return (
        <div className="h-screen flex flex-col overflow-hidden">
            <header className="shell-header">
                <div className="titlebar-brand" />
                <div className="titlebar-center">
                    <QuickConnectBar
                        connections={connections}
                        sshKeys={sshKeys}
                        sessions={sessions}
                        onConnect={(connectionId) => void onTreeConnect(connectionId)}
                        onQuickConnectInput={onTitlebarQuickConnect}
                        onQuickCreateConnection={onTitlebarQuickCreateConnection}
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
                <aside
                    className="workspace-left-sidebar flex-shrink-0 flex flex-col h-full overflow-hidden transition-[width] duration-200 ease-out bg-[var(--bg-surface)] border-r border-[var(--border)]"
                    style={{
                        width: leftSidebarCollapsed
                            ? LEFT_SIDEBAR_WIDTH_COLLAPSED
                            : LEFT_SIDEBAR_WIDTH_EXPANDED,
                    }}
                >
                    {leftSidebarCollapsed ? (
                        <div className="sidebar-collapsed-shell w-full h-full flex flex-col items-center gap-2">
                            <button
                                type="button"
                                className="sidebar-collapsed-toggle"
                                onClick={handleToggleLeftSidebar}
                                title="展开侧栏"
                            >
                                <i className="ri-layout-left-line" aria-hidden="true" />
                            </button>
                            <div
                                className={`sidebar-collapsed-status ${headerSessionClass}`}
                                title={headerSessionText}
                            >
                                <span className="sidebar-session-dot" />
                            </div>
                            {transferTasks.length > 0 ? (
                                <div
                                    className="sidebar-collapsed-badge"
                                    title={`传输任务 ${transferTasks.length}`}
                                >
                                    {collapsedTransferCount}
                                </div>
                            ) : null}
                        </div>
                    ) : (
                        <div className="w-full h-full flex flex-col overflow-hidden">
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
                                        <button
                                            type="button"
                                            className="sidebar-collapse-btn"
                                            onClick={handleToggleLeftSidebar}
                                            title="折叠侧栏"
                                        >
                                            <i className="ri-layout-left-line" aria-hidden="true" />
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
                                        <span className="sidebar-session-addr empty">
                                            未选择服务器
                                        </span>
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
                                <LiveEditPane
                                    connections={connections}
                                    active={!liveEditPanelCollapsed}
                                    collapsed={liveEditPanelCollapsed}
                                    onToggle={onLiveEditPanelToggle}
                                />
                            </div>
                        )}
                </aside>
                <section className="flex-1 min-w-0 min-h-0 flex flex-col overflow-hidden">
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
                                                            setSessionContextMenu(null);
                                                            onSetActiveSession(session.id);
                                                            if (session.connectionId) {
                                                                onSetActiveConnection(
                                                                    session.connectionId,
                                                                );
                                                            }
                                                        }}
                                                        onContextMenu={(event) =>
                                                            handleSessionTabContextMenu(
                                                                event,
                                                                session,
                                                            )
                                                        }
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
                                                        session.status === "disconnected" ? (
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
                                                                title="拖拽重排 / 右键菜单"
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
                                        {sessionContextMenu && contextMenuSession ? (
                                            <SessionTabContextMenu
                                                state={sessionContextMenu}
                                                session={contextMenuSession}
                                                displayTitle={contextMenuSessionTitle ?? contextMenuSession.title}
                                                onClose={() => setSessionContextMenu(null)}
                                                onRename={(session) => {
                                                    void handlePromptRenameSession(session);
                                                }}
                                            />
                                        ) : null}
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
                                                onReconnectSession={onReconnectSession}
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
                                    defaultSize={bottomCollapsed ? "4%" : "32%"}
                                    minSize="16%"
                                    collapsible
                                    collapsedSize="4%"
                                    onResize={syncBottomCollapsed}
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
                                                        onClick={handleToggleBottomWorkbench}
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
                                                    key: "files",
                                                    label: "SFTP",
                                                    children: (
                                                        <FileExplorerPane
                                                            connection={activeConnection}
                                                            connected={
                                                                isActiveConnectionTerminalConnected
                                                            }
                                                            followSessionId={followTerminalSessionId}
                                                            active={bottomTab === "files"}
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
            </main>
        </div>
    );
};
