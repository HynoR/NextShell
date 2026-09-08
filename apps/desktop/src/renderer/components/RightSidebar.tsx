import { useCallback, useEffect, useState } from "react";
import type { ConnectionProfile } from "@nextshell/core";
import { useAgentActivityStore } from "../store/useAgentActivityStore";
import { useTransferQueueStore, type TransferTask } from "../store/useTransferQueueStore";
import {
  persistWorkspacePanelState,
  resolveWorkspacePanelState
} from "../utils/workspaceLayoutState";
import { AgentActivityPanel } from "./AgentActivityPanel";
import { LiveEditPane } from "./LiveEditPane";
import { TransferQueuePanel } from "./TransferQueuePanel";

const STORAGE_KEY = "nextshell.workspace.rightSidebarCollapsed";

const storage = (): Storage | undefined => {
  try {
    return window.localStorage;
  } catch {
    return undefined;
  }
};

interface RightSidebarProps {
  connections: ConnectionProfile[];
  transferPanelCollapsed: boolean;
  liveEditPanelCollapsed: boolean;
  onTransferPanelToggle: () => void;
  onLiveEditPanelToggle: () => void;
  onRetryTransfer: (taskId: string) => void;
  onClearFinishedTransfers: () => void;
  onOpenLocalFile: (task: TransferTask) => void;
}

/** A notification dot: only things that happened *without* the user — agent calls, running transfers. */
const Dot = ({ count, title }: { count: number; title: string }) =>
  count > 0 ? (
    <div className="right-sidebar-dot" title={`${title} ${count}`}>
      {count > 99 ? "99+" : count}
    </div>
  ) : null;

const AgentDot = () => {
  const enabled = useAgentActivityStore((state) => state.enabled);
  const halted = useAgentActivityStore((state) => state.halted);
  const unseen = useAgentActivityStore((state) => state.unseen);
  if (!enabled) return null;
  if (halted) return <span className="right-sidebar-halted-dot" title="Agent 调用已被切断" />;
  return <Dot count={unseen} title="Agent 调用" />;
};

const TransferDot = () => {
  const count = useTransferQueueStore(
    (state) => state.tasks.filter((task) => task.status === "running").length
  );
  return <Dot count={count} title="传输中" />;
};

/**
 * The activity column to the right of the terminal: agent calls, transfers
 * and live edits — everything that happens *around* a session rather than in
 * it. Collapses to a strip of counters so nothing running is ever out of sight.
 */
export const RightSidebar = ({
  connections,
  transferPanelCollapsed,
  liveEditPanelCollapsed,
  onTransferPanelToggle,
  onLiveEditPanelToggle,
  onRetryTransfer,
  onClearFinishedTransfers,
  onOpenLocalFile
}: RightSidebarProps) => {
  // Collapsed by default: the column is a notification surface, not a dashboard.
  const [collapsed, setCollapsed] = useState(() =>
    resolveWorkspacePanelState(storage(), STORAGE_KEY, true)
  );
  const markSeen = useAgentActivityStore((state) => state.markSeen);
  const activityCount = useAgentActivityStore((state) => state.activities.length);

  // While the column is open the user sees every call as it lands, so nothing
  // is "unseen"; the dot only counts what arrives while it is collapsed.
  useEffect(() => {
    if (!collapsed) markSeen();
  }, [collapsed, activityCount, markSeen]);

  const toggle = useCallback(() => {
    setCollapsed((value) => {
      persistWorkspacePanelState(storage(), STORAGE_KEY, !value);
      return !value;
    });
  }, []);

  if (collapsed) {
    return (
      <aside className="right-sidebar collapsed" aria-label="活动面板">
        <button
          type="button"
          className="sidebar-collapsed-toggle"
          onClick={toggle}
          title="展开活动面板"
        >
          <i className="ri-layout-right-line" aria-hidden="true" />
        </button>
        <AgentDot />
        <TransferDot />
      </aside>
    );
  }

  return (
    <aside className="right-sidebar" aria-label="活动面板">
      <div className="right-sidebar-top">
        <button
          type="button"
          className="sidebar-collapse-btn"
          onClick={toggle}
          title="收起活动面板"
        >
          <i className="ri-layout-right-line" aria-hidden="true" />
        </button>
      </div>
      <div className="right-sidebar-sections">
        <AgentActivityPanel />
        <TransferQueuePanel
          collapsed={transferPanelCollapsed}
          onToggle={onTransferPanelToggle}
          onRetry={onRetryTransfer}
          onCancel={(taskId) => void window.nextshell.sftp.cancelTransfer({ taskId })}
          onClearFinished={onClearFinishedTransfers}
          onOpenLocalFile={(task) => {
            if (task.direction === "download" && task.status === "success") {
              onOpenLocalFile(task);
            }
          }}
        />
        <LiveEditPane
          connections={connections}
          collapsed={liveEditPanelCollapsed}
          onToggle={onLiveEditPanelToggle}
        />
      </div>
    </aside>
  );
};
