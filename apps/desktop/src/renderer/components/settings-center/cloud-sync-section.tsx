import {
  Alert,
  Badge,
  Button,
  Input,
  InputNumber,
  List,
  Skeleton,
  Space,
  Switch,
  Tag,
  Typography
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
      <SettingsCard title="工作区配置" description="通过 HTTPS API 与远端工作区共享服务器、密钥和代理配置">
        <div className="cloud-sync-config">
          {!apiAvailable ? (
            <Alert
              type="warning"
              showIcon
              message="当前构建尚未提供 cloudSync API"
              description="renderer 已按约定接线；主进程和 preload 暴露该命名空间后，这个分区会自动生效。"
            />
          ) : null}

          {status.keytarAvailable === false ? (
            <Alert
              type="error"
              showIcon
              message="系统钥匙串不可用"
              description="workspace 密码不会写入 settings。请先确保 keytar 可用，再启用云同步。"
            />
          ) : null}

          <div className="cloud-sync-config-grid">
            <div className="cloud-sync-field cloud-sync-field--wide">
              <div className="cloud-sync-field-label">API 地址</div>
              <div className="cloud-sync-field-hint">例如 https://sync.example.com</div>
              <Input
                value={apiBaseUrl}
                disabled={controlsDisabled}
                onChange={(event) => setApiBaseUrl(event.target.value)}
                placeholder="https://your-sync-server"
              />
            </div>

            <div className="cloud-sync-field">
              <div className="cloud-sync-field-label">Workspace 名称</div>
              <div className="cloud-sync-field-hint">同一 workspace 会在多台设备间共享同步域</div>
              <Input
                value={workspaceName}
                disabled={controlsDisabled}
                onChange={(event) => setWorkspaceName(event.target.value)}
                placeholder="例如 personal"
              />
            </div>

            <div className="cloud-sync-field">
              <div className="cloud-sync-field-label">拉取周期（秒）</div>
              <div className="cloud-sync-field-hint">后台按固定周期拉取远端快照并覆盖本地同步域</div>
              <InputNumber
                style={{ width: "100%" }}
                min={10}
                precision={0}
                value={pullIntervalSec}
                disabled={controlsDisabled}
                onChange={(value) => {
                  if (typeof value !== "number" || !Number.isFinite(value)) {
                    return;
                  }
                  setPullIntervalSec(Math.max(10, Math.round(value)));
                }}
              />
            </div>

            <div className="cloud-sync-field">
              <div className="cloud-sync-field-label">Workspace 密码</div>
              <div className="cloud-sync-field-hint">只提交给 cloudSync.configure，不会写入 settings.update</div>
              <Input.Password
                value={workspacePassword}
                disabled={controlsDisabled}
                onChange={(event) => setWorkspacePassword(event.target.value)}
                placeholder="输入后仅用于启用或更新云同步"
              />
            </div>

            <div className="cloud-sync-field cloud-sync-field--toggle">
              <div className="cloud-sync-toggle-row">
                <div>
                  <div className="cloud-sync-field-label">忽略 TLS 校验</div>
                  <div className="cloud-sync-field-hint">仅对当前云同步工作区生效，适用于自签名证书</div>
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

      <SettingsCard title="同步状态" description="远端工作区为准，首次启用或定时拉取时会覆盖本地同步域">
        {loading ? (
          <Skeleton active paragraph={{ rows: 4 }} />
        ) : (
          <div className="cloud-sync-status-grid">
            <div className="cloud-sync-status-card">
              <div className="cloud-sync-status-label">启用状态</div>
              <div className="cloud-sync-status-value">
                <Space size={8}>
                  <Badge status={status.enabled ? "success" : "default"} />
                  <Typography.Text>{status.enabled ? "已启用" : "未启用"}</Typography.Text>
                </Space>
              </div>
            </div>

            <div className="cloud-sync-status-card">
              <div className="cloud-sync-status-label">运行状态</div>
              <div className="cloud-sync-status-value">
                <Tag color={runtime.color}>{runtime.label}</Tag>
              </div>
            </div>

            <div className="cloud-sync-status-card">
              <div className="cloud-sync-status-label">钥匙串能力</div>
              <div className="cloud-sync-status-value">
                <Typography.Text>
                  {status.keytarAvailable === null
                    ? "未知"
                    : status.keytarAvailable
                      ? "可用"
                      : "不可用"}
                </Typography.Text>
              </div>
            </div>

            <div className="cloud-sync-status-card">
              <div className="cloud-sync-status-label">当前工作区</div>
              <div className="cloud-sync-status-value">
                <Typography.Text ellipsis={{ tooltip: status.workspaceName || "未配置" }}>
                  {status.workspaceName || "未配置"}
                </Typography.Text>
              </div>
            </div>

            <div className="cloud-sync-status-card">
              <div className="cloud-sync-status-label">上次同步时间</div>
              <div className="cloud-sync-status-value">
                <Typography.Text>{formatCloudSyncTime(status.lastSyncAt)}</Typography.Text>
              </div>
            </div>

            <div className="cloud-sync-status-card">
              <div className="cloud-sync-status-label">待同步队列</div>
              <div className="cloud-sync-status-value cloud-sync-status-value--metric">
                {status.pendingCount}
              </div>
            </div>

            <div className="cloud-sync-status-card">
              <div className="cloud-sync-status-label">冲突数量</div>
              <div className="cloud-sync-status-value cloud-sync-status-value--metric">
                {status.conflictCount}
              </div>
            </div>

            <div className="cloud-sync-status-card">
              <div className="cloud-sync-status-label">TLS 校验</div>
              <div className="cloud-sync-status-value">
                <Typography.Text>{status.ignoreTlsErrors ? "已忽略证书校验" : "严格校验"}</Typography.Text>
              </div>
            </div>
          </div>
        )}

        {status.lastError ? (
          <div className="cloud-sync-status-feedback">
            <Alert
              type="error"
              showIcon
              message="最近错误"
              description={status.lastError}
            />
          </div>
        ) : (
          <div className="cloud-sync-status-feedback stg-note">
            云同步独立于云存档运行，仅覆盖服务器、 SSH 密钥和代理三个同步域。
          </div>
        )}
      </SettingsCard>

      <SettingsCard title="冲突处理" description="检测到同一资源的远端更新时，不会自动覆盖本地待同步修改">
        {conflictsLoading ? (
          <Skeleton active paragraph={{ rows: 3 }} />
        ) : conflicts.length === 0 ? (
          <div className="stg-note">当前没有待处理的云同步冲突。</div>
        ) : (
          <List
            dataSource={conflicts}
            renderItem={(item) => {
              const overwriteBusy = conflictBusyKey === `${item.resourceType}:${item.resourceId}:overwrite_local`;
              const keepBusy = conflictBusyKey === `${item.resourceType}:${item.resourceId}:keep_local`;
              return (
                <List.Item
                  actions={[
                    <Button
                      key="overwrite"
                      size="small"
                      loading={overwriteBusy}
                      onClick={() => onResolveConflict(item.resourceType, item.resourceId, "overwrite_local")}
                    >
                      覆盖本地
                    </Button>,
                    <Button
                      key="keep"
                      type="primary"
                      size="small"
                      loading={keepBusy}
                      onClick={() => onResolveConflict(item.resourceType, item.resourceId, "keep_local")}
                    >
                      保留本地
                    </Button>
                  ]}
                >
                  <List.Item.Meta
                    title={`${item.displayName} · ${item.resourceType}`}
                    description={[
                      item.serverDeleted ? "远端已删除该资源" : `远端更新时间：${formatCloudSyncTime(item.serverUpdatedAt)}`,
                      `本地更新时间：${formatCloudSyncTime(item.localUpdatedAt)}`,
                      item.hasPendingLocalChange ? "本地有待同步修改" : "本地无待同步修改"
                    ].join(" ｜ ")}
                  />
                </List.Item>
              );
            }}
          />
        )}
      </SettingsCard>
    </>
  );
};
