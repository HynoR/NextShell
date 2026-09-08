import { useState } from "react";
import { App as AntdApp, Alert, Button, Segmented, Tag } from "antd";
import { useAgentActivityStore } from "../store/useAgentActivityStore";
import { usePreferencesStore } from "../store/usePreferencesStore";
import { formatErrorMessage } from "../utils/errorMessage";

const STATUS = {
  running: { color: "processing", label: "执行中" },
  succeeded: { color: "success", label: "完成" },
  failed: { color: "error", label: "失败" },
  unsettled: { color: "warning", label: "未结束" }
} as const;

/** One section of the right sidebar, laid out like the transfer queue. */
export const AgentActivityPanel = () => {
  const { message } = AntdApp.useApp();
  const [collapsed, setCollapsed] = useState(false);
  const [toggling, setToggling] = useState(false);
  const enabled = useAgentActivityStore((state) => state.enabled);
  const activities = useAgentActivityStore((state) => state.activities);
  const clearFinished = useAgentActivityStore((state) => state.clearFinished);
  const halted = useAgentActivityStore((state) => state.halted);
  const setHalted = useAgentActivityStore((state) => state.setHalted);
  const execMode = usePreferencesStore((state) => state.preferences.agent.execMode);
  const execApproval = usePreferencesStore((state) => state.preferences.agent.execApproval);
  const updatePreferences = usePreferencesStore((state) => state.updatePreferences);
  const running = activities.filter((activity) => activity.status === "running").length;

  const savePrefs = (patch: {
    execMode?: "foreground" | "background";
    execApproval?: "auto" | "permission";
  }): void => {
    updatePreferences({ agent: patch }).catch((error) => {
      message.error(`切换失败：${formatErrorMessage(error, "请稍后重试")}`);
    });
  };

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

  // Agent access is opt-in: while it is off this section does not exist at all.
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
          {collapsed && (running > 0 || halted) ? (
            <span className="agent-panel-summary">{halted ? "已切断" : `${running} 项执行中`}</span>
          ) : null}
        </div>
      </div>
      {!collapsed ? (
        <div className="agent-panel-body">
          {/* How the agent's exec runs is the user's call, made here: foreground
              types into the tab in front of them, background stays out of it. */}
          <div className="agent-panel-row">
            <span className="agent-panel-label">命令执行</span>
            <Segmented
              size="small"
              value={execMode}
              options={[
                { label: "前台", value: "foreground" },
                { label: "后台", value: "background" }
              ]}
              onChange={(value) => savePrefs({ execMode: value as "foreground" | "background" })}
            />
          </div>
          {/* Auto = trust the agent and the harness's own approval; Permission =
              every command waits for a click here. Blacklist and .env gate stay. */}
          <div className="agent-panel-row">
            <span
              className="agent-panel-label"
              title="Auto：信任 agent 与 harness 的拦截，直接执行；Permission：每条命令先弹窗授权"
            >
              权限模式
            </span>
            <Segmented
              size="small"
              value={execApproval}
              options={[
                { label: "Auto", value: "auto" },
                { label: "Permission", value: "permission" }
              ]}
              onChange={(value) => savePrefs({ execApproval: value as "auto" | "permission" })}
            />
          </div>
          <div className="agent-panel-row">
            <Button
              type="text"
              size="small"
              className="agent-panel-action-btn"
              onClick={clearFinished}
              disabled={activities.length === running}
            >
              清理已完成
            </Button>
            {/* The breaker: taking control back must never be more than one click away. */}
            <Button
              size="small"
              className="agent-panel-action-btn"
              danger={!halted}
              loading={toggling}
              onClick={() => void toggleHalted(!halted)}
            >
              {halted ? "恢复调用" : "全部中止"}
            </Button>
          </div>
          {halted ? (
            <Alert
              className="mb-2"
              type="warning"
              showIcon
              message="Agent 调用已被切断"
              description="所有工具调用会立即被拒绝。"
            />
          ) : null}
          {activities.length > 0 ? (
            <div className="agent-panel-list">
              {activities.slice(0, 50).map((activity) => {
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
