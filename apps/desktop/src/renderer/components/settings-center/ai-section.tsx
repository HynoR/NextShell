import { useState } from "react";
import {
  Button,
  Input,
  InputNumber,
  Select,
  Space,
  Tag,
  Typography
} from "antd";
import { DEFAULT_APP_PREFERENCES, type AiProviderConfig, type AiProviderType } from "@nextshell/core";
import { SettingsCard, SettingsRow, SettingsSwitchRow } from "./shared-components";
import type { SaveFn } from "./types";
import { formatAiErrorMessage } from "../../utils/ai-error-message";

const PROVIDER_TYPE_OPTIONS: Array<{ label: string; value: AiProviderType }> = [
  { label: "OpenAI 兼容", value: "openai" },
  { label: "Anthropic Claude", value: "anthropic" },
  { label: "Google Gemini", value: "gemini" },
];

const DEFAULT_BASE_URLS: Record<AiProviderType, string> = {
  openai: "https://api.openai.com/v1",
  anthropic: "https://api.anthropic.com",
  gemini: "https://generativelanguage.googleapis.com",
};

const DEFAULT_MODELS: Record<AiProviderType, string> = {
  openai: "gpt-4o",
  anthropic: "claude-sonnet-4-20250514",
  gemini: "gemini-2.5-flash",
};

const AI_RUNTIME_PRESETS = [
  {
    id: "cloud",
    label: "官方云服务",
    description: "适合直连 OpenAI / Claude / Gemini 官方接口，优先保持响应速度。",
    providerRequestTimeoutSec: 30,
    providerMaxRetries: 1,
  },
  {
    id: "proxy",
    label: "代理 / 中转",
    description: "适合经代理或网关访问海外模型，适当放宽等待时间并保留一次兜底重试。",
    providerRequestTimeoutSec: 45,
    providerMaxRetries: 2,
  },
  {
    id: "local",
    label: "本地模型",
    description: "适合 Ollama 或局域网模型网关，给推理时间，避免高重试拖慢交互。",
    providerRequestTimeoutSec: 75,
    providerMaxRetries: 0,
  },
] as const;

interface AiSectionProps {
  loading: boolean;
  enabled: boolean;
  providers: AiProviderConfig[];
  activeProviderId?: string;
  executionTimeoutSec: number;
  providerRequestTimeoutSec: number;
  providerMaxRetries: number;
  save: SaveFn;
  message: { success: (msg: string) => void; error: (msg: string) => void; warning: (msg: string) => void };
}

export const AiSection = ({
  loading,
  enabled,
  providers,
  activeProviderId,
  executionTimeoutSec,
  providerRequestTimeoutSec,
  providerMaxRetries,
  save,
  message,
}: AiSectionProps) => {
  const [editingProvider, setEditingProvider] = useState<AiProviderConfig | null>(null);
  const [apiKeyInput, setApiKeyInput] = useState("");
  const [testing, setTesting] = useState(false);
  const activeRuntimePreset = AI_RUNTIME_PRESETS.find((preset) =>
    preset.providerRequestTimeoutSec === providerRequestTimeoutSec
      && preset.providerMaxRetries === providerMaxRetries
  );
  const matchesDefaultRuntime =
    executionTimeoutSec === DEFAULT_APP_PREFERENCES.ai.executionTimeoutSec
    && providerRequestTimeoutSec === DEFAULT_APP_PREFERENCES.ai.providerRequestTimeoutSec
    && providerMaxRetries === DEFAULT_APP_PREFERENCES.ai.providerMaxRetries;
  const runtimeSummaryLabel = activeRuntimePreset?.label ?? "自定义配置";
  const runtimeSummaryDescription = activeRuntimePreset?.description
    ?? "当前值已手动调整，可按实际网络环境继续微调。";

  const startAddProvider = (): void => {
    const defaultType: AiProviderType = "openai";
    setEditingProvider({
      id: crypto.randomUUID(),
      type: defaultType,
      name: "",
      baseUrl: DEFAULT_BASE_URLS[defaultType],
      model: DEFAULT_MODELS[defaultType],
      enabled: true,
    });
    setApiKeyInput("");
  };

  const startEditProvider = (provider: AiProviderConfig): void => {
    setEditingProvider({ ...provider });
    setApiKeyInput("");
  };

  const handleProviderTypeChange = (type: AiProviderType): void => {
    if (!editingProvider) return;
    setEditingProvider({
      ...editingProvider,
      type,
      baseUrl: DEFAULT_BASE_URLS[type],
      model: DEFAULT_MODELS[type],
    });
  };

  const handleSaveProvider = async (): Promise<void> => {
    if (!editingProvider) return;
    if (!editingProvider.name.trim()) {
      message.warning("请输入提供商名称");
      return;
    }
    if (!editingProvider.baseUrl.trim()) {
      message.warning("请输入 API 地址");
      return;
    }
    if (!editingProvider.model.trim()) {
      message.warning("请输入模型名称");
      return;
    }

    if (apiKeyInput.trim()) {
      try {
        await window.nextshell.ai.setApiKey({
          providerId: editingProvider.id,
          apiKey: apiKeyInput.trim(),
        });
        editingProvider.apiKeyRef = `secret://ai-provider-${editingProvider.id}`;
      } catch (err) {
        message.error(`保存 API Key 失败：${err instanceof Error ? err.message : "未知错误"}`);
        return;
      }
    }

    const existingIdx = providers.findIndex((p) => p.id === editingProvider.id);
    const updated = [...providers];
    if (existingIdx >= 0) {
      updated[existingIdx] = editingProvider;
    } else {
      updated.push(editingProvider);
    }

    const isFirstProvider = providers.length === 0;
    save({
      ai: {
        providers: updated,
        ...(isFirstProvider ? { activeProviderId: editingProvider.id, enabled: true } : {}),
      },
    });

    setEditingProvider(null);
    setApiKeyInput("");
    message.success("提供商配置已保存");
  };

  const handleRemoveProvider = (id: string): void => {
    const updated = providers.filter((p) => p.id !== id);
    const patch: Record<string, unknown> = { providers: updated };
    if (activeProviderId === id) {
      patch.activeProviderId = updated[0]?.id;
    }
    save({ ai: patch });
  };

  const handleTestProvider = async (): Promise<void> => {
    if (!editingProvider) return;
    const key = apiKeyInput.trim();
    if (!key) {
      message.warning("请先输入 API Key 以进行测试");
      return;
    }
    setTesting(true);
    try {
      const result = await window.nextshell.ai.testProvider({
        type: editingProvider.type,
        baseUrl: editingProvider.baseUrl,
        model: editingProvider.model,
        apiKey: key,
      });
      if (result.ok) {
        message.success("连接测试成功");
      } else {
        message.error(`连接测试失败：${formatAiErrorMessage(result.error, "未知错误")}`);
      }
    } catch (err) {
      message.error(`测试失败：${formatAiErrorMessage(err, "未知错误")}`);
    } finally {
      setTesting(false);
    }
  };

  const restoreDefaultRuntime = (): void => {
    save({
      ai: {
        executionTimeoutSec: DEFAULT_APP_PREFERENCES.ai.executionTimeoutSec,
        providerRequestTimeoutSec: DEFAULT_APP_PREFERENCES.ai.providerRequestTimeoutSec,
        providerMaxRetries: DEFAULT_APP_PREFERENCES.ai.providerMaxRetries,
      },
    });
    message.success("已恢复默认推荐值");
  };

  return (
    <>
      <SettingsCard title="AI 助手" description="配置大模型接口，使用 AI 辅助执行运维操作">
        <SettingsSwitchRow
          label="启用 AI 助手"
          checked={enabled}
          disabled={loading}
          onChange={(v) => save({ ai: { enabled: v } })}
        />

        {enabled && (
          <>
            <SettingsRow label="命令执行超时（秒）" hint="AI 执行单条命令的最大等待时间">
              <InputNumber
                min={5}
                max={300}
                value={executionTimeoutSec}
                disabled={loading}
                onChange={(v) => {
                  if (v !== null) save({ ai: { executionTimeoutSec: v } });
                }}
                style={{ width: 120 }}
              />
            </SettingsRow>

            <SettingsRow label="模型请求超时（秒）" hint="单次调用 OpenAI / Claude / Gemini 的最大等待时间">
              <InputNumber
                min={5}
                max={120}
                value={providerRequestTimeoutSec}
                disabled={loading}
                onChange={(v) => {
                  if (v !== null) save({ ai: { providerRequestTimeoutSec: v } });
                }}
                style={{ width: 120 }}
              />
            </SettingsRow>

            <SettingsRow label="模型请求重试次数" hint="遇到 429 或 5xx 等可重试错误时的最大自动重试次数">
              <InputNumber
                min={0}
                max={3}
                value={providerMaxRetries}
                disabled={loading}
                onChange={(v) => {
                  if (v !== null) save({ ai: { providerMaxRetries: v } });
                }}
                style={{ width: 120 }}
              />
            </SettingsRow>

            <SettingsRow label="当前生效策略" hint="摘要会根据当前超时与重试配置自动识别匹配策略">
              <div
                style={{
                  width: "100%",
                  border: "1px solid var(--border)",
                  borderRadius: 8,
                  padding: 10,
                  display: "grid",
                  gap: 8,
                }}
              >
                <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
                  <Typography.Text strong>{runtimeSummaryLabel}</Typography.Text>
                  {activeRuntimePreset ? (
                    <Tag color="green" style={{ marginInlineEnd: 0 }}>推荐匹配</Tag>
                  ) : (
                    <Tag color="gold" style={{ marginInlineEnd: 0 }}>手动调整</Tag>
                  )}
                  <Button
                    size="small"
                    disabled={loading || matchesDefaultRuntime}
                    onClick={restoreDefaultRuntime}
                  >
                    {matchesDefaultRuntime ? "已是默认值" : "恢复默认推荐值"}
                  </Button>
                </div>
                <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
                  <Tag color="blue" style={{ marginInlineEnd: 0 }}>
                    请求超时 {providerRequestTimeoutSec}s
                  </Tag>
                  <Tag color="purple" style={{ marginInlineEnd: 0 }}>
                    自动重试 {providerMaxRetries} 次
                  </Tag>
                  <Tag color="cyan" style={{ marginInlineEnd: 0 }}>
                    命令超时 {executionTimeoutSec}s
                  </Tag>
                </div>
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  {runtimeSummaryDescription}
                </Typography.Text>
              </div>
            </SettingsRow>
          </>
        )}
      </SettingsCard>

      {enabled && (
        <SettingsCard
          title="模型提供商"
          description="管理大模型 API 接口配置，支持 OpenAI 兼容（含 DeepSeek / Ollama）、Anthropic、Gemini"
        >
          {providers.length > 0 && (
            <div style={{ marginBottom: 12 }}>
              <SettingsRow label="当前使用">
                <Select
                  value={activeProviderId}
                  disabled={loading}
                  style={{ width: 200 }}
                  onChange={(v) => save({ ai: { activeProviderId: v } })}
                  options={providers
                    .filter((p) => p.enabled)
                    .map((p) => ({
                      label: p.name,
                      value: p.id,
                    }))}
                />
              </SettingsRow>
            </div>
          )}

          {/* Provider list */}
          <div className="flex flex-col gap-2">
            {providers.map((provider) => (
              <div
                key={provider.id}
                className="flex items-center justify-between p-2 rounded"
                style={{ border: "1px solid var(--border)" }}
              >
                <div className="flex items-center gap-2 min-w-0">
                  <Tag color={provider.enabled ? "blue" : "default"}>
                    {PROVIDER_TYPE_OPTIONS.find((o) => o.value === provider.type)?.label ?? provider.type}
                  </Tag>
                  <Typography.Text ellipsis style={{ maxWidth: 160 }}>
                    {provider.name}
                  </Typography.Text>
                  <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                    {provider.model}
                  </Typography.Text>
                  {provider.id === activeProviderId && (
                    <Tag color="green" style={{ marginLeft: 4 }}>当前</Tag>
                  )}
                </div>
                <Space size={4}>
                  <Button size="small" onClick={() => startEditProvider(provider)}>
                    编辑
                  </Button>
                  <Button size="small" danger onClick={() => handleRemoveProvider(provider.id)}>
                    删除
                  </Button>
                </Space>
              </div>
            ))}
          </div>

          {!editingProvider && (
            <Button
              type="dashed"
              block
              style={{ marginTop: 8 }}
              onClick={startAddProvider}
            >
              <i className="ri-add-line" /> 添加提供商
            </Button>
          )}

          {/* Edit / Add form */}
          {editingProvider && (
            <div
              style={{
                marginTop: 12,
                padding: 12,
                borderRadius: 6,
                border: "1px solid var(--border)",
              }}
            >
              <Typography.Text strong style={{ fontSize: 13 }}>
                {providers.some((p) => p.id === editingProvider.id) ? "编辑提供商" : "添加提供商"}
              </Typography.Text>

              <SettingsRow label="类型">
                <Select
                  value={editingProvider.type}
                  options={PROVIDER_TYPE_OPTIONS}
                  style={{ width: 200 }}
                  onChange={handleProviderTypeChange}
                />
              </SettingsRow>

              <SettingsRow label="名称">
                <Input
                  value={editingProvider.name}
                  placeholder="如：DeepSeek / 本地 Ollama"
                  onChange={(e) =>
                    setEditingProvider({ ...editingProvider, name: e.target.value })
                  }
                />
              </SettingsRow>

              <SettingsRow label="API 地址">
                <Input
                  value={editingProvider.baseUrl}
                  placeholder={DEFAULT_BASE_URLS[editingProvider.type]}
                  onChange={(e) =>
                    setEditingProvider({ ...editingProvider, baseUrl: e.target.value })
                  }
                />
              </SettingsRow>

              <SettingsRow label="模型">
                <Input
                  value={editingProvider.model}
                  placeholder={DEFAULT_MODELS[editingProvider.type]}
                  onChange={(e) =>
                    setEditingProvider({ ...editingProvider, model: e.target.value })
                  }
                />
              </SettingsRow>

              <SettingsRow label="API Key" hint={editingProvider.apiKeyRef ? "已存储，留空表示不更改" : ""}>
                <Input.Password
                  value={apiKeyInput}
                  placeholder={editingProvider.apiKeyRef ? "留空保留原密钥" : "输入 API Key"}
                  onChange={(e) => setApiKeyInput(e.target.value)}
                />
              </SettingsRow>

              <SettingsSwitchRow
                label="启用此提供商"
                checked={editingProvider.enabled}
                onChange={(v) =>
                  setEditingProvider({ ...editingProvider, enabled: v })
                }
              />

              <Space style={{ marginTop: 8 }}>
                <Button
                  type="primary"
                  onClick={() => void handleSaveProvider()}
                >
                  保存
                </Button>
                <Button
                  loading={testing}
                  onClick={() => void handleTestProvider()}
                >
                  测试连接
                </Button>
                <Button onClick={() => setEditingProvider(null)}>取消</Button>
              </Space>
            </div>
          )}
        </SettingsCard>
      )}
    </>
  );
};
