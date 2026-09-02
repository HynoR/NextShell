import { useCallback, useEffect, useState } from "react";
import {
  App as AntdApp,
  Alert,
  Badge,
  Button,
  Input,
  InputNumber,
  Popconfirm,
  Radio,
  Space,
  Switch,
  Tag,
  Typography
} from "antd";
import type { AgentClientKind, AgentEndpointStatus } from "@nextshell/shared";
import { useAgentActivityStore } from "../../store/useAgentActivityStore";
import { usePreferencesStore } from "../../store/usePreferencesStore";
import { formatErrorMessage } from "../../utils/errorMessage";
import { SettingsCard, SettingsRow } from "./shared-components";

const CLIENT_OPTIONS: Array<{ label: string; value: AgentClientKind }> = [
  { label: "Claude Code", value: "claude-code" },
  { label: "Claude Desktop", value: "claude-desktop" },
  { label: "Cursor", value: "cursor" },
  { label: "通用 JSON", value: "generic" }
];

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

export const AgentSection = () => {
  const { message } = AntdApp.useApp();
  const preferences = usePreferencesStore((s) => s.preferences);
  const prefsLoading = usePreferencesStore((s) => s.loading);
  const updatePreferences = usePreferencesStore((s) => s.updatePreferences);
  const agentPrefs = preferences.agent;

  const [status, setStatus] = useState<AgentEndpointStatus | null>(null);
  const [statusLoading, setStatusLoading] = useState(false);
  const [togglingEnabled, setTogglingEnabled] = useState(false);
  const [clientKind, setClientKind] = useState<AgentClientKind>("claude-code");
  const [copyingConfig, setCopyingConfig] = useState(false);
  const [installing, setInstalling] = useState(false);
  const [configResult, setConfigResult] = useState<{ command: string; json: string } | null>(null);
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
      const result = await window.nextshell.agent.status();
      applyStatus(result);
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
      void updatePreferences(patch).catch((error) => {
        message.error(`保存设置失败：${formatErrorMessage(error, "请稍后重试")}`);
      });
    },
    [updatePreferences, message]
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

  const handleCopyClientConfig = async (): Promise<void> => {
    setCopyingConfig(true);
    try {
      const result = await window.nextshell.agent.copyClientConfig({ client: clientKind });
      setConfigResult({ command: result.command, json: result.json });
      message.success("接入配置已复制到剪贴板");
    } catch (error) {
      message.error(`生成接入配置失败：${formatErrorMessage(error, "请稍后重试")}`);
    } finally {
      setCopyingConfig(false);
    }
  };

  const handleInstallCursor = async (): Promise<void> => {
    setInstalling(true);
    try {
      await window.nextshell.agent.installCursor();
      message.success("已打开 Cursor 安装链接，请在 Cursor 中确认添加");
    } catch (error) {
      message.error(`打开 Cursor 安装链接失败：${formatErrorMessage(error, "请稍后重试")}`);
    } finally {
      setInstalling(false);
    }
  };

  const handleInstallClaudeDesktop = async (): Promise<void> => {
    setInstalling(true);
    try {
      const result = await window.nextshell.agent.installClaudeDesktop();
      message.success(`已写入 ${result.configPath}，重启 Claude Desktop 后生效`);
    } catch (error) {
      message.error(`写入 Claude Desktop 配置失败：${formatErrorMessage(error, "请稍后重试")}`);
    } finally {
      setInstalling(false);
    }
  };

  const handleExportMcpb = async (): Promise<void> => {
    setInstalling(true);
    try {
      const result = await window.nextshell.agent.exportMcpb();
      if (result.ok) {
        message.success(`已导出安装包：${result.filePath}，在 Claude Desktop 中打开即可安装`);
      }
    } catch (error) {
      message.error(`导出 .mcpb 失败：${formatErrorMessage(error, "请稍后重试")}`);
    } finally {
      setInstalling(false);
    }
  };

  const enabled = status?.enabled ?? false;
  const listening = status?.listening ?? false;
  const runningState = formatRunningState(enabled, listening, status?.halted ?? false);

  return (
    <>
      <SettingsCard
        title="Agent 接入（MCP）"
        description="允许 Claude Code 等 AI Agent 通过 MCP 协议连接本机，接管你已经打开的服务器标签页"
      >
        <div className="stg-switch-row">
          <div className="stg-switch-label">
            <span>启用 Agent 接入</span>
            <span className="stg-row-hint">
              开启后本机会监听一个仅当前系统用户可访问的 Unix Socket（0600），供 MCP
              客户端连接。默认关闭，关闭时不会监听任何端点。
            </span>
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
        </div>

        {status?.lastError && <Alert type="error" showIcon message={status.lastError} />}
      </SettingsCard>

      <SettingsCard title="运行详情" description="端点当前的监听信息，仅本机可见">
        <SettingsRow label="Unix Socket 路径">
          <Typography.Text
            style={{ fontSize: 12 }}
            type={status?.socketPath ? undefined : "secondary"}
            copyable={status?.socketPath ? { text: status.socketPath } : false}
          >
            {status?.socketPath ?? "未监听"}
          </Typography.Text>
        </SettingsRow>
        <SettingsRow label="已连接客户端">
          <Typography.Text style={{ fontSize: 12 }}>
            {formatClientCount(status?.clients.length ?? 0)}
          </Typography.Text>
        </SettingsRow>
        <SettingsRow label="endpoint.json 路径" hint="MCP 客户端可通过该文件自动发现端点">
          <Typography.Text
            style={{ fontSize: 12, wordBreak: "break-all" }}
            copyable={status?.endpointFilePath ? { text: status.endpointFilePath } : false}
          >
            {status?.endpointFilePath ?? "-"}
          </Typography.Text>
        </SettingsRow>
      </SettingsCard>

      <SettingsCard title="一键接入客户端" description="按客户端类型生成接入配置，点击后直接复制到剪贴板">
        <SettingsRow label="客户端类型">
          <Radio.Group
            value={clientKind}
            onChange={(e) => setClientKind(e.target.value as AgentClientKind)}
            options={CLIENT_OPTIONS}
            optionType="button"
            size="small"
          />
        </SettingsRow>
        <Space wrap>
          <Button type="primary" loading={copyingConfig} onClick={() => void handleCopyClientConfig()}>
            生成并复制接入配置
          </Button>
          {clientKind === "cursor" && (
            <Button loading={installing} onClick={() => void handleInstallCursor()}>
              在 Cursor 中一键安装
            </Button>
          )}
          {clientKind === "claude-desktop" && (
            <>
              <Popconfirm
                title="写入 Claude Desktop 配置？"
                description="将把 NextShell 的接入配置合并进 claude_desktop_config.json，其他配置项保持不变。"
                okText="写入"
                cancelText="取消"
                onConfirm={() => void handleInstallClaudeDesktop()}
              >
                <Button loading={installing}>写入 Claude Desktop 配置</Button>
              </Popconfirm>
              <Button loading={installing} onClick={() => void handleExportMcpb()}>
                导出 .mcpb 安装包
              </Button>
            </>
          )}
        </Space>
        {configResult && (
          <>
            <SettingsRow label="CLI 命令">
              <div className="flex items-center gap-2">
                <Typography.Text code style={{ fontSize: 12, wordBreak: "break-all" }}>
                  {configResult.command}
                </Typography.Text>
                <Button size="small" onClick={() => void handleCopyText(configResult.command, "命令")}>
                  复制
                </Button>
              </div>
            </SettingsRow>
            <SettingsRow label="JSON 片段">
              <div className="flex items-start gap-2">
                <Typography.Text
                  code
                  style={{ fontSize: 12, whiteSpace: "pre-wrap", wordBreak: "break-all" }}
                >
                  {configResult.json}
                </Typography.Text>
                <Button
                  size="small"
                  onClick={() => void handleCopyText(configResult.json, "JSON 片段")}
                >
                  复制
                </Button>
              </div>
            </SettingsRow>
          </>
        )}
        {clientKind === "claude-desktop" && (
          <div className="stg-note">
            .mcpb 安装包适合分发给同机器的其他账户或离线安装：在 Claude Desktop
            的「扩展」页打开该文件即可完成安装。
          </div>
        )}
      </SettingsCard>

      <SettingsCard
        title="命令黑名单"
        description="命中黑名单的命令会被 Agent 工具直接拒绝并附原因，没有“本次放行”。内置清单已覆盖 rm -rf /、mkfs、dd 写设备、shutdown/reboot、fork 炸弹等显而易见的危险命令，这里用于追加你自己的条目"
      >
        <SettingsRow
          label="自定义黑名单"
          hint="每条按子串匹配；若本身是合法正则，则同时按正则匹配"
        >
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
        <SettingsRow label="命令超时（秒）" hint="Agent 发起的单条命令最长执行时间">
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

      <SettingsCard title="安全说明">
        <div className="stg-note">
          Agent 只能接管你已经在 NextShell 里打开的标签页，不能自行连接新服务器；密码、私钥、
          密钥口令等凭据永远不会通过 MCP 暴露给 Agent。
        </div>
        <div className="stg-note">
          当 Agent 正在操作某个标签页时你在其中敲了键盘，它的下一次注入或命令会直接报错并停手；
          命中黑名单的命令会被直接拒绝。关闭总开关后端点立即停监听，活动面板里的断闸可以一键掐断所有调用。
        </div>
      </SettingsCard>
    </>
  );
};
