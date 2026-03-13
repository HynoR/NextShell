import {
  Alert,
  Badge,
  Button,
  Input,
  Skeleton,
  Space,
  Switch,
  Tag,
  Typography,
  Tooltip,
} from "antd";
import type { CloudSyncConflictItem } from "@nextshell/shared";
import { SettingsCard } from "./shared-components";
import { formatCloudSyncState, formatCloudSyncTime } from "./constants";
import type { CloudSyncStatusView } from "./types";

export const CloudSyncSection = ({
  apiAvailable,
  status,
  loading,
  busyAction,
  conflicts,
  conflictsLoading,
  conflictBusyKey,
  apiBaseUrl,
  workspaceName,
  workspacePassword,
  pullIntervalSec,
  ignoreTlsErrors,
  setApiBaseUrl,
  setWorkspaceName,
  setWorkspacePassword,
  setPullIntervalSec,
  setIgnoreTlsErrors,
  onConfigure,
  onDisable,
  onSyncNow,
  onResolveConflict
}: {
  apiAvailable: boolean;
  status: CloudSyncStatusView;
  loading: boolean;
  busyAction: "configure" | "disable" | "sync" | null;
  conflicts: CloudSyncConflictItem[];
  conflictsLoading: boolean;
  conflictBusyKey: string | null;
  apiBaseUrl: string;
  workspaceName: string;
  workspacePassword: string;
  pullIntervalSec: number;
  ignoreTlsErrors: boolean;
  setApiBaseUrl: (value: string) => void;
  setWorkspaceName: (value: string) => void;
  setWorkspacePassword: (value: string) => void;
  setPullIntervalSec: (value: number) => void;
  setIgnoreTlsErrors: (value: boolean) => void;
  onConfigure: () => void;
  onDisable: () => void;
  onSyncNow: () => void;
  onResolveConflict: (
    resourceType: CloudSyncConflictItem["resourceType"],
    resourceId: string,
    strategy: "overwrite_local" | "keep_local"
  ) => void;
}) => {
  const runtime = formatCloudSyncState(status.state);
  const controlsDisabled = loading || busyAction !== null || !apiAvailable;
  const configureDisabled =
    controlsDisabled ||
    status.keytarAvailable === false ||
    apiBaseUrl.trim().length === 0 ||
    workspaceName.trim().length === 0 ||
    workspacePassword.length === 0;

  return (
    <>
      <SettingsCard title="工作区配置" description="配置云端工作区，在多台设备间同步连接信息">
        <div className="cloud-sync-config">
          {!apiAvailable ? (
            <Alert
              type="warning"
              showIcon
              message="云同步功能暂不可用"
              description="当前版本不支持云同步功能，请更新到支持该功能的版本。"
            />
          ) : null}

          {status.keytarAvailable === false ? (
            <Alert
              type="error"
              showIcon
              message="系统安全存储不可用"
              description="无法访问系统钥匙串，云同步密码无法安全存储。请检查系统权限设置。"
            />
          ) : null}

          <div className="cloud-sync-config-grid cloud-sync-config-grid--simple">
            <div className="cloud-sync-field cloud-sync-field--simple cloud-sync-field--wide">
              <div className="cloud-sync-field-label">服务器地址</div>
              <Input
                value={apiBaseUrl}
                disabled={controlsDisabled}
                onChange={(event) => setApiBaseUrl(event.target.value)}
                placeholder="https://your-sync-server.example.com"
              />
            </div>

            <div className="cloud-sync-field cloud-sync-field--simple">
              <div className="cloud-sync-field-label">工作区名称</div>
              <Input
                value={workspaceName}
                disabled={controlsDisabled}
                onChange={(event) => setWorkspaceName(event.target.value)}
                placeholder="例如：personal"
              />
            </div>

            <div className="cloud-sync-field cloud-sync-field--simple">
              <div className="cloud-sync-field-label">工作区密码</div>
              <Input.Password
                value={workspacePassword}
                disabled={controlsDisabled}
                onChange={(event) => setWorkspacePassword(event.target.value)}
                placeholder="启用或更新同步时需要"
              />
            </div>

            <div className="cloud-sync-field cloud-sync-field--simple">
              <div className="cloud-sync-field-label">自动同步间隔</div>
              <Space.Compact style={{ width: "100%" }}>
                <Input
                  type="number"
                  min={10}
                  value={pullIntervalSec}
                  disabled={controlsDisabled}
                  onChange={(event) => {
                    const value = parseInt(event.target.value, 10);
                    if (Number.isNaN(value)) {
                      return;
                    }
                    setPullIntervalSec(Math.max(10, value));
                  }}
                />
                <Button disabled>秒</Button>
              </Space.Compact>
            </div>

            <div className="cloud-sync-field cloud-sync-field--simple cloud-sync-field--toggle">
              <div className="cloud-sync-toggle-row cloud-sync-toggle-row--simple">
                <div>
                  <div className="cloud-sync-field-label">忽略证书错误</div>
                  <div className="cloud-sync-field-hint">用于自签名证书（不推荐）</div>
                </div>
                <Switch
                  checked={ignoreTlsErrors}
                  disabled={controlsDisabled}
                  onChange={setIgnoreTlsErrors}
                />
              </div>
            </div>
          </div>

          <div className="cloud-sync-actions">
            <Button
              type="primary"
              loading={busyAction === "configure"}
              disabled={configureDisabled}
              onClick={onConfigure}
            >
              保存并启用
            </Button>
            <Button
              danger
              loading={busyAction === "disable"}
              disabled={controlsDisabled || !status.enabled}
              onClick={onDisable}
            >
              停用云同步
            </Button>
            <Button
              loading={busyAction === "sync" || status.state === "syncing"}
              disabled={controlsDisabled || !status.enabled}
              onClick={onSyncNow}
            >
              立即同步
            </Button>
          </div>
        </div>
      </SettingsCard>

      <SettingsCard title="同步状态" description="查看云端同步的最新状态">
        {loading ? (
          <Skeleton active paragraph={{ rows: 3 }} />
        ) : (
          <div className="cloud-sync-status-list">
            {[
              {
                label: "同步状态",
                value: <Tag color={runtime.color}>{runtime.label}</Tag>
              },
              {
                label: "钥匙串能力",
                value: (
                  <Typography.Text>
                    {status.keytarAvailable === null
                      ? "未知"
                      : status.keytarAvailable
                        ? "可用"
                        : "不可用"}
                  </Typography.Text>
                )
              },
              {
                label: "API 地址",
                value: (
                  <Typography.Text ellipsis={{ tooltip: status.apiBaseUrl || "未配置" }}>
                    {status.apiBaseUrl || "未配置"}
                  </Typography.Text>
                )
              },
              {
                label: "当前工作区",
                value: (
                  <Typography.Text ellipsis={{ tooltip: status.workspaceName || "未配置" }}>
                    {status.workspaceName || "未配置"}
                  </Typography.Text>
                )
              },
              {
                label: "上次同步",
                value: <Typography.Text>{formatCloudSyncTime(status.lastSyncAt)}</Typography.Text>
              },
              {
                label: "待同步",
                value: status.pendingCount
              },
              {
                label: "冲突",
                value: (
                  <Space size={4}>
                    <span>{status.conflictCount}</span>
                    {status.conflictCount > 0 && <Badge status="error" />}
                  </Space>
                )
              }
            ].map((item, index) => (
              <div key={index} className="cloud-sync-status-list-item">
                <span className="cloud-sync-status-list-label">{item.label}</span>
                <span className="cloud-sync-status-list-value">{item.value}</span>
              </div>
            ))}
          </div>
        )}

        {status.lastError ? (
          <div className="cloud-sync-status-feedback">
            <Alert
              type="error"
              showIcon
              message="同步出错"
              description={status.lastError}
            />
          </div>
        ) : null}
      </SettingsCard>

      <SettingsCard title="冲突处理" description="当本地和云端同时修改了同一项目时出现冲突">
        {conflictsLoading ? (
          <Skeleton active paragraph={{ rows: 3 }} />
        ) : conflicts.length === 0 ? (
          <div className="cloud-sync-empty-state">
            <span className="cloud-sync-empty-icon">✓</span>
            <span>没有待处理的冲突</span>
          </div>
        ) : (
          <div className="cloud-sync-conflict-list">
            {conflicts.map((item) => {
              const overwriteBusy = conflictBusyKey === `${item.resourceType}:${item.resourceId}:overwrite_local`;
              const keepBusy = conflictBusyKey === `${item.resourceType}:${item.resourceId}:keep_local`;
              const resourceTypeLabel: Record<string, string> = {
                connection: "连接",
                sshKey: "SSH 密钥",
                proxy: "代理"
              };
              
              return (
                <div key={`${item.resourceType}:${item.resourceId}`} className="cloud-sync-conflict-item">
                  <div className="cloud-sync-conflict-content">
                    <div className="cloud-sync-conflict-title">
                      <Space size={8}>
                        <span>{item.displayName}</span>
                        <Tag className="cloud-sync-resource-tag">{resourceTypeLabel[item.resourceType]}</Tag>
                      </Space>
                    </div>
                    <div className="cloud-sync-conflict-meta">
                      <Space size={16}>
                        {item.serverDeleted ? (
                          <span className="cloud-sync-conflict-deleted">云端已删除</span>
                        ) : (
                          <Tooltip title="云端修改时间">
                            <span>云端: {formatCloudSyncTime(item.serverUpdatedAt)}</span>
                          </Tooltip>
                        )}
                        <Tooltip title="本地修改时间">
                          <span>本地: {formatCloudSyncTime(item.localUpdatedAt)}</span>
                        </Tooltip>
                      </Space>
                    </div>
                  </div>
                  <div className="cloud-sync-conflict-actions">
                    <Button
                      type="primary"
                      size="small"
                      loading={keepBusy}
                      onClick={() => onResolveConflict(item.resourceType, item.resourceId, "keep_local")}
                    >
                      保留本地
                    </Button>
                    <Button
                      size="small"
                      danger
                      loading={overwriteBusy}
                      onClick={() => onResolveConflict(item.resourceType, item.resourceId, "overwrite_local")}
                    >
                      使用云端
                    </Button>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </SettingsCard>
    </>
  );
};
