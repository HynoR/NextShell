import { useCallback, useEffect, useState } from "react";
import {
  App as AntdApp,
  Alert,
  Badge,
  Button,
  Input,
  InputNumber,
  Space,
  Switch,
  Tag,
  Typography
} from "antd";
import type { AgentEndpointStatus } from "@nextshell/shared";
import { useAgentActivityStore } from "../../store/useAgentActivityStore";
import { usePreferencesStore } from "../../store/usePreferencesStore";
import { formatErrorMessage } from "../../utils/errorMessage";
import { SettingsCard, SettingsRow } from "./shared-components";

/** Exported for unit testing — maps a connected-client count to its display copy. */
export const formatClientCount = (count: number): string =>
  count > 0 ? `${count} 个客户端已连接` : "暂无客户端连接";

/**
 * Exported for unit testing — maps live endpoint status to the running-state
 * badge copy. A halted endpoint is still listening, so reporting it as "监听中"
 * would tell the user the opposite of what is true.
 */
export const formatRunningState = (
  enabled: boolean,
  listening: boolean,
  halted = false
): { status: "success" | "error" | "warning" | "default"; text: string } => {
  if (!enabled) return { status: "default", text: "未启用" };
  if (!listening) return { status: "error", text: "已启用但未监听" };
  return halted
    ? { status: "warning", text: "监听中（调用已被切断）" }
    : { status: "success", text: "监听中" };
};

/** Exported for unit testing — the one line any MCP client needs. */
export const buildEndpointUrl = (port: number): string => `http://127.0.0.1:${port}/mcp`;
export const buildClaudeAddCommand = (port: number): string =>
  `claude mcp add --transport http nextshell ${buildEndpointUrl(port)}`;
export const buildMcpJson = (port: number): string =>
  JSON.stringify(
    { mcpServers: { nextshell: { type: "http", url: buildEndpointUrl(port) } } },
    null,
    2
  );

export const AgentSection = () => {
  const { message } = AntdApp.useApp();
  const preferences = usePreferencesStore((s) => s.preferences);
  const prefsLoading = usePreferencesStore((s) => s.loading);
  const updatePreferences = usePreferencesStore((s) => s.updatePreferences);
  const agentPrefs = preferences.agent;

  const [status, setStatus] = useState<AgentEndpointStatus | null>(null);
  const [statusLoading, setStatusLoading] = useState(false);
  const [togglingEnabled, setTogglingEnabled] = useState(false);
  const [blacklistDraft, setBlacklistDraft] = useState("");
  const setPanelEnabled = useAgentActivityStore((s) => s.setEnabled);

  // The sidebar's Agent panel hides itself while agent access is off; keep its
  // mirrored flag in step with every fresh status this section receives.
  const applyStatus = useCallback(
    (result: AgentEndpointStatus) => {
      setStatus(result);
      setPanelEnabled(result.enabled);
    },
    [setPanelEnabled]
  );

  const refreshStatus = useCallback(async (): Promise<void> => {
    setStatusLoading(true);
    try {
      applyStatus(await window.nextshell.agent.status());
    } catch (error) {
      message.error(`获取 Agent 状态失败：${formatErrorMessage(error, "请稍后重试")}`);
    } finally {
      setStatusLoading(false);
    }
  }, [message, applyStatus]);

  useEffect(() => {
    void refreshStatus();
    // Runs once on mount; the section unmounts when the sidebar tab changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const save = useCallback(
    (patch: Parameters<typeof updatePreferences>[0]) => {
      void updatePreferences(patch)
        .then(() => refreshStatus())
        .catch((error) => {
          message.error(`保存设置失败：${formatErrorMessage(error, "请稍后重试")}`);
        });
    },
    [updatePreferences, message, refreshStatus]
  );

  const handleToggleEnabled = async (checked: boolean): Promise<void> => {
    setTogglingEnabled(true);
    try {
      const result = checked
        ? await window.nextshell.agent.enable()
        : await window.nextshell.agent.disable();
      applyStatus(result);
      if (checked && result.lastError) {
        message.warning(`Agent 接入已开启，但监听未能建立：${result.lastError}`);
      } else {
        message.success(checked ? "Agent 接入已启用" : "Agent 接入已停用");
      }
    } catch (error) {
      message.error(`操作失败：${formatErrorMessage(error, "请稍后重试")}`);
    } finally {
      setTogglingEnabled(false);
    }
  };

  const handleCopyText = async (text: string, label: string): Promise<void> => {
    try {
      await navigator.clipboard.writeText(text);
      message.success(`${label}已复制到剪贴板`);
    } catch (error) {
      message.error(`复制失败：${formatErrorMessage(error, "请稍后重试")}`);
    }
  };

  const handleAddBlacklistEntry = (): void => {
    const entry = blacklistDraft.trim();
    if (!entry) return;
    if (agentPrefs.blacklist.includes(entry)) {
      message.info("该条目已在黑名单中");
      return;
    }
    save({ agent: { blacklist: [...agentPrefs.blacklist, entry] } });
    setBlacklistDraft("");
  };

  const enabled = status?.enabled ?? false;
  const listening = status?.listening ?? false;
  const runningState = formatRunningState(enabled, listening, status?.halted ?? false);
  const port = status?.port ?? agentPrefs.port;
  const url = buildEndpointUrl(port);
  const claudeCommand = buildClaudeAddCommand(port);

  return (
    <>
      <SettingsCard
        title="Agent 接入（MCP）"
        description="让已授权的 AI Agent 通过 MCP 操作你打开的终端标签页。凭据不会经 MCP 暴露。"
      >
        <div className="stg-switch-row">
          <div className="stg-switch-label">
            <span>启用 Agent 接入</span>
            <span className="stg-row-hint">开启后监听本机 127.0.0.1 的 MCP 端口。</span>
          </div>
          <Switch
            size="small"
            checked={enabled}
            loading={togglingEnabled || (statusLoading && !status)}
            onChange={(v) => void handleToggleEnabled(v)}
          />
        </div>

        <div className="flex items-center gap-2">
          <Typography.Text style={{ fontSize: 12 }}>运行状态：</Typography.Text>
          {statusLoading && !status ? (
            <Tag>加载中…</Tag>
          ) : (
            <Badge status={runningState.status} text={runningState.text} />
          )}
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            {formatClientCount(status?.clients.length ?? 0)}
          </Typography.Text>
        </div>
        {status?.lastError && <Alert type="error" showIcon message={status.lastError} />}

        <SettingsRow label="端口" hint="修改后自动重启；默认 41777">
          <InputNumber
            style={{ width: 160 }}
            min={1024}
            max={65535}
            precision={0}
            value={agentPrefs.port}
            disabled={prefsLoading}
            onChange={(v) => {
              if (typeof v === "number" && Number.isInteger(v) && v !== agentPrefs.port) {
                save({ agent: { port: v } });
              }
            }}
          />
        </SettingsRow>

        <SettingsRow label="MCP 地址" hint="Cursor / Windsurf / Codex 等直接使用">
          <Typography.Text code copyable={{ text: url }} style={{ fontSize: 12 }}>
            {url}
          </Typography.Text>
        </SettingsRow>
        <SettingsRow label="Claude Code" hint="终端执行一次">
          <Typography.Text code copyable={{ text: claudeCommand }} style={{ fontSize: 12 }}>
            {claudeCommand}
          </Typography.Text>
        </SettingsRow>
        <SettingsRow label="mcp.json 片段" hint="Claude Desktop 需用 npx mcp-remote 转接">
          <Button
            size="small"
            onClick={() => void handleCopyText(buildMcpJson(port), "mcp.json 片段")}
          >
            复制 JSON
          </Button>
        </SettingsRow>
      </SettingsCard>

      <SettingsCard
        title="命令黑名单"
        description="命中黑名单的命令会被 Agent 直接拒绝。内置已覆盖常见危险命令，此处追加自定义条目。"
      >
        <SettingsRow label="自定义黑名单" hint="子串匹配；合法正则同时按正则匹配">
          <div className="flex flex-col gap-2">
            {agentPrefs.blacklist.length > 0 ? (
              <Space size={[4, 4]} wrap>
                {agentPrefs.blacklist.map((entry) => (
                  <Tag
                    key={entry}
                    closable
                    onClose={() =>
                      save({
                        agent: { blacklist: agentPrefs.blacklist.filter((item) => item !== entry) }
                      })
                    }
                  >
                    {entry}
                  </Tag>
                ))}
              </Space>
            ) : (
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                未追加自定义条目
              </Typography.Text>
            )}
            <Space.Compact style={{ maxWidth: 480 }}>
              <Input
                size="small"
                placeholder="例如：kubectl delete 或 ^rm\s"
                value={blacklistDraft}
                disabled={prefsLoading}
                onChange={(event) => setBlacklistDraft(event.target.value)}
                onPressEnter={handleAddBlacklistEntry}
              />
              <Button size="small" disabled={prefsLoading} onClick={handleAddBlacklistEntry}>
                添加
              </Button>
            </Space.Compact>
          </div>
        </SettingsRow>
        <SettingsRow label="命令超时（秒）" hint="单条命令最长执行时间">
          <InputNumber
            style={{ width: "100%" }}
            min={1}
            max={3600}
            precision={0}
            value={agentPrefs.execTimeoutSec}
            disabled={prefsLoading}
            onChange={(v) => {
              if (typeof v === "number" && Number.isInteger(v)) {
                save({ agent: { execTimeoutSec: v } });
              }
            }}
          />
        </SettingsRow>
      </SettingsCard>
    </>
  );
};
