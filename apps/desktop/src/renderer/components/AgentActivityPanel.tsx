import { useState } from "react";
import { App as AntdApp, Alert, Button, Tag } from "antd";
import { useAgentActivityStore } from "../store/useAgentActivityStore";
import { formatErrorMessage } from "../utils/errorMessage";

const STATUS = {
  running: { color: "processing", label: "执行中" },
  succeeded: { color: "success", label: "完成" },
  failed: { color: "error", label: "失败" }
} as const;

export const AgentActivityPanel = () => {
  const { message } = AntdApp.useApp();
  const [collapsed, setCollapsed] = useState(false);
  const [toggling, setToggling] = useState(false);
  const enabled = useAgentActivityStore((state) => state.enabled);
  const activities = useAgentActivityStore((state) => state.activities);
  const clearFinished = useAgentActivityStore((state) => state.clearFinished);
  const halted = useAgentActivityStore((state) => state.halted);
  const setHalted = useAgentActivityStore((state) => state.setHalted);
  const running = activities.filter((activity) => activity.status === "running").length;

  const toggleHalted = async (next: boolean): Promise<void> => {
    setToggling(true);
    try {
      const status = await window.nextshell.agent.setHalted({ halted: next });
      setHalted(status.halted);
      message.success(next ? "已切断 Agent 的所有调用" : "已恢复 Agent 调用");
    } catch (error) {
      message.error(`操作失败：${formatErrorMessage(error, "请稍后重试")}`);
    } finally {
      setToggling(false);
    }
  };

  // Agent access is opt-in: while it is off this panel does not exist at all.
  if (!enabled) return null;

  return (
    <section className="agent-panel" aria-label="Agent 活动">
      <div className="agent-panel-header" onClick={() => setCollapsed((value) => !value)}>
        <i
          className={collapsed ? "ri-arrow-right-s-line" : "ri-arrow-down-s-line"}
          aria-hidden="true"
        />
        <span className="agent-panel-title">Agent 活动</span>
        <div className="agent-panel-header-right" onClick={(e) => e.stopPropagation()}>
          {collapsed ? (
            running > 0 || halted ? (
              <span className="agent-panel-summary">
                {halted ? "已切断" : `${running} 项执行中`}
              </span>
            ) : null
          ) : (
            <Button
              type="text"
              size="small"
              className="agent-panel-action-btn"
              onClick={clearFinished}
              disabled={activities.length === running}
            >
              清理
            </Button>
          )}
          {/* The breaker stays reachable even when the list is collapsed: taking
              control back must never be more than one click away. */}
          <Button
            type="text"
            size="small"
            className="agent-panel-action-btn"
            danger={!halted}
            loading={toggling}
            onClick={() => void toggleHalted(!halted)}
          >
            {halted ? "恢复" : "全部中止"}
          </Button>
        </div>
      </div>
      {!collapsed ? (
        <div className="agent-panel-body">
          {halted ? (
            <Alert
              className="mb-2"
              type="warning"
              showIcon
              message="Agent 调用已被切断"
              description="所有工具调用会立即被拒绝，「本会话始终允许」也已全部作废。"
            />
          ) : null}
          {activities.length > 0 ? (
            <div className="agent-panel-list">
              {activities.slice(0, 20).map((activity) => {
                const status = STATUS[activity.status];
                return (
                  <div
                    key={activity.id}
                    className="rounded border border-[var(--border)] px-2 py-1.5 text-xs"
                  >
                    <div className="flex items-center justify-between gap-2">
                      <span className="truncate font-medium">
                        {activity.clientName ?? "未知客户端"} · {activity.tool}
                      </span>
                      <Tag color={status.color} className="m-0">
                        {status.label}
                      </Tag>
                    </div>
                    <div
                      className="mt-1 truncate text-[var(--text-secondary)]"
                      title={activity.summary}
                    >
                      {activity.summary}
                    </div>
                  </div>
                );
              })}
            </div>
          ) : (
            <div className="agent-panel-empty">
              <i className="ri-robot-2-line agent-panel-empty-icon" aria-hidden="true" />
              <span className="agent-panel-empty-text">暂无 Agent 活动</span>
            </div>
          )}
        </div>
      ) : null}
    </section>
  );
};
