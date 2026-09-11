import {
  Alert,
  Button,
  Input,
  InputNumber,
  List,
  Modal,
  Popconfirm,
  Space,
  Switch,
  Tag,
  Typography,
  message
} from "antd";
import { useCallback, useEffect, useState } from "react";
import type { CloudSyncWorkspaceProfile, WorkspaceRepoStatus } from "@nextshell/core";
import { CLOUD_SYNC_WORKSPACE_PASSWORD_MIN_LENGTH } from "@nextshell/shared";
import { SettingsCard } from "./SettingsCard";

const api = () =>
  (window as unknown as { nextshell: import("@nextshell/shared").NextShellApi }).nextshell;

const PASSWORD_MIN = CLOUD_SYNC_WORKSPACE_PASSWORD_MIN_LENGTH;
const PASSWORD_TOO_SHORT = `工作区密码至少 ${PASSWORD_MIN} 位（与服务端要求一致，过短会被服务端拒绝）`;
const isPasswordTooShort = (password: string) =>
  password.length > 0 && password.length < PASSWORD_MIN;

interface WorkspaceFormState {
  id?: string;
  apiBaseUrl: string;
  workspaceName: string;
  displayName: string;
  workspacePassword: string;
  pullIntervalSec: number;
  ignoreTlsErrors: boolean;
  enabled: boolean;
}

const emptyForm: WorkspaceFormState = {
  apiBaseUrl: "",
  workspaceName: "",
  displayName: "",
  workspacePassword: "",
  pullIntervalSec: 300,
  ignoreTlsErrors: false,
  enabled: true
};

type WorkspaceStatus = WorkspaceRepoStatus;

const STATE_COLORS: Record<string, string> = {
  idle: "green",
  synced: "green",
  syncing: "blue",
  error: "red",
  disabled: "default",
  diverged: "orange"
};

const STATE_LABELS: Record<string, string> = {
  idle: "空闲",
  synced: "已同步",
  syncing: "同步中",
  error: "出错",
  disabled: "已停用",
  diverged: "待选择"
};

export const CloudSyncManagerPanel = () => {
  const [workspaces, setWorkspaces] = useState<CloudSyncWorkspaceProfile[]>([]);
  const [loading, setLoading] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const [editForm, setEditForm] = useState<WorkspaceFormState>(emptyForm);
  const [isEditing, setIsEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [syncingId, setSyncingId] = useState<string | null>(null);
  const [copyingTokenId, setCopyingTokenId] = useState<string | null>(null);
  const [pastingToken, setPastingToken] = useState(false);

  const [statusMap, setStatusMap] = useState<Map<string, WorkspaceStatus>>(new Map());
  const [testing, setTesting] = useState(false);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const [list, statusResult] = await Promise.all([
        api().cloudSync.workspaceList(),
        api().cloudSync.status()
      ]);
      setWorkspaces(list);
      const map = new Map<string, WorkspaceStatus>();
      for (const s of statusResult.workspaces) {
        map.set(s.workspaceId, s);
      }
      setStatusMap(map);
    } catch (err) {
      message.error(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
    const unsubStatus = api().cloudSync.onStatus(() => {
      void refresh();
    });
    const unsubApplied = api().cloudSync.onApplied(() => {
      void refresh();
    });
    return () => {
      unsubStatus();
      unsubApplied();
    };
  }, [refresh]);

  const openAddModal = (prefill?: Partial<WorkspaceFormState>) => {
    setEditForm({ ...emptyForm, ...prefill });
    setIsEditing(false);
    setModalOpen(true);
  };

  const openEditModal = (ws: CloudSyncWorkspaceProfile) => {
    setEditForm({
      id: ws.id,
      apiBaseUrl: ws.apiBaseUrl,
      workspaceName: ws.workspaceName,
      displayName: ws.displayName,
      workspacePassword: "",
      pullIntervalSec: ws.pullIntervalSec,
      ignoreTlsErrors: ws.ignoreTlsErrors,
      enabled: ws.enabled
    });
    setIsEditing(true);
    setModalOpen(true);
  };

  const handleSave = async () => {
    if (!editForm.apiBaseUrl.trim()) {
      message.warning("请填写 API 地址");
      return;
    }
    if (!editForm.workspaceName.trim()) {
      message.warning("请填写工作区名称");
      return;
    }
    if (!isEditing && !editForm.workspacePassword) {
      message.warning("新建工作区时需设置密码");
      return;
    }
    if (isPasswordTooShort(editForm.workspacePassword)) {
      message.warning(PASSWORD_TOO_SHORT);
      return;
    }
    setSaving(true);
    try {
      if (isEditing && editForm.id) {
        await api().cloudSync.workspaceUpdate({
          id: editForm.id,
          apiBaseUrl: editForm.apiBaseUrl,
          workspaceName: editForm.workspaceName,
          displayName: editForm.displayName || undefined,
          workspacePassword: editForm.workspacePassword || undefined,
          pullIntervalSec: editForm.pullIntervalSec,
          ignoreTlsErrors: editForm.ignoreTlsErrors,
          enabled: editForm.enabled
        });
      } else {
        await api().cloudSync.workspaceAdd({
          apiBaseUrl: editForm.apiBaseUrl,
          workspaceName: editForm.workspaceName,
          displayName: editForm.displayName || undefined,
          workspacePassword: editForm.workspacePassword,
          pullIntervalSec: editForm.pullIntervalSec,
          ignoreTlsErrors: editForm.ignoreTlsErrors,
          enabled: editForm.enabled
        });
      }
      setModalOpen(false);
      await refresh();
    } catch (err) {
      message.error(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const handleRemove = async (id: string) => {
    try {
      await api().cloudSync.workspaceRemove({ id });
      await refresh();
    } catch (err) {
      message.error(err instanceof Error ? err.message : String(err));
    }
  };

  const handleCopyToken = async (id: string) => {
    setCopyingTokenId(id);
    try {
      const { token } = await api().cloudSync.workspaceExportToken({ id });
      await navigator.clipboard.writeText(token);
      message.success("已复制工作区 Token，内容包含敏感信息");
    } catch (err) {
      message.error(err instanceof Error ? err.message : String(err));
    } finally {
      setCopyingTokenId(null);
    }
  };

  const handlePasteToken = async () => {
    setPastingToken(true);
    try {
      const token = await navigator.clipboard.readText();
      const draft = await api().cloudSync.workspaceParseToken({ token });
      openAddModal({
        apiBaseUrl: draft.apiBaseUrl,
        workspaceName: draft.workspaceName,
        displayName: draft.displayName,
        workspacePassword: draft.workspacePassword,
        pullIntervalSec: draft.pullIntervalSec,
        ignoreTlsErrors: draft.ignoreTlsErrors,
        enabled: draft.enabled
      });
      message.success("已从工作区 Token 自动填充表单");
    } catch (err) {
      message.error(err instanceof Error ? err.message : String(err));
    } finally {
      setPastingToken(false);
    }
  };

  const handleSync = async (workspaceId: string, mode?: "cloud-wins" | "local-wins") => {
    setSyncingId(workspaceId);
    try {
      await api().cloudSync.syncNow({ workspaceId, mode });
      await refresh();
    } catch (err) {
      message.error(err instanceof Error ? err.message : String(err));
    } finally {
      setSyncingId(null);
    }
  };

  const handleTestConnection = async () => {
    if (!editForm.apiBaseUrl.trim() || !editForm.workspaceName.trim()) {
      message.warning("请先填写 API 地址和工作区名称");
      return;
    }
    if (!editForm.workspacePassword) {
      message.warning("请输入工作区密码后再测试");
      return;
    }
    if (isPasswordTooShort(editForm.workspacePassword)) {
      message.warning(PASSWORD_TOO_SHORT);
      return;
    }
    setTesting(true);
    try {
      const result = await api().cloudSync.testConnection({
        apiBaseUrl: editForm.apiBaseUrl,
        workspaceName: editForm.workspaceName,
        workspacePassword: editForm.workspacePassword,
        ignoreTlsErrors: editForm.ignoreTlsErrors
      });
      message.success(`连接成功${result.displayName ? `：${result.displayName}` : ""}`);
    } catch (err) {
      message.error(err instanceof Error ? err.message : String(err));
    } finally {
      setTesting(false);
    }
  };

  return (
    <>
      <SettingsCard title="云工作区" description="管理云端同步的服务器资产仓库和共享命令集。">
        <div style={{ marginBottom: 12 }}>
          <Space size={8} wrap>
            <Button type="primary" size="small" onClick={() => openAddModal()}>
              添加工作区
            </Button>
            <Button size="small" onClick={() => void handlePasteToken()} loading={pastingToken}>
              粘贴 Token
            </Button>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              可粘贴从其他 NextShell 复制的 Token 自动填充
            </Typography.Text>
          </Space>
        </div>

        <List
          loading={loading}
          dataSource={workspaces}
          locale={{ emptyText: "尚未添加任何工作区" }}
          renderItem={(ws) => {
            const st = statusMap.get(ws.id);
            return (
              <List.Item
                actions={[
                  <Button
                    key="sync"
                    size="small"
                    loading={syncingId === ws.id}
                    disabled={!ws.enabled}
                    onClick={() => handleSync(ws.id)}
                  >
                    同步
                  </Button>,
                  <Button key="edit" size="small" onClick={() => openEditModal(ws)}>
                    编辑
                  </Button>,
                  <Button
                    key="copy-token"
                    size="small"
                    loading={copyingTokenId === ws.id}
                    onClick={() => void handleCopyToken(ws.id)}
                  >
                    复制 Token
                  </Button>,
                  <Popconfirm
                    key="del"
                    title="确认删除此工作区？"
                    description="关联的同步状态和云端资产都会被清除。"
                    onConfirm={() => handleRemove(ws.id)}
                    okText="删除"
                    cancelText="取消"
                    okButtonProps={{ danger: true }}
                  >
                    <Button size="small" danger>
                      删除
                    </Button>
                  </Popconfirm>
                ]}
              >
                <List.Item.Meta
                  title={
                    <Space>
                      <span>{ws.displayName || ws.workspaceName}</span>
                      {st ? (
                        <Tag color={STATE_COLORS[st.state] ?? "default"}>
                          {STATE_LABELS[st.state] ?? st.state}
                        </Tag>
                      ) : ws.enabled ? (
                        <Tag color="green">已启用</Tag>
                      ) : (
                        <Tag>已停用</Tag>
                      )}
                      {(ws.lastError || st?.lastError) && <Tag color="red">错误</Tag>}
                      {ws.ignoreTlsErrors && (
                        <Tag color="orange" title="该工作区已关闭 TLS 证书校验，仅限调试环境使用">
                          忽略 TLS
                        </Tag>
                      )}
                    </Space>
                  }
                  description={
                    <div>
                      <Typography.Text type="secondary" style={{ fontSize: 12, display: "block" }}>
                        {ws.apiBaseUrl} / {ws.workspaceName}
                        {(st?.lastSyncAt || ws.lastSyncAt) &&
                          ` · 上次同步: ${st?.lastSyncAt ?? ws.lastSyncAt}`}
                      </Typography.Text>
                      {(st?.lastError || ws.lastError) && (
                        <Typography.Text type="danger" style={{ fontSize: 12, display: "block" }}>
                          {st?.lastError ?? ws.lastError}
                        </Typography.Text>
                      )}
                    </div>
                  }
                />
              </List.Item>
            );
          }}
        />
        {workspaces
          .filter((workspace) => statusMap.get(workspace.id)?.state === "diverged")
          .map((workspace) => (
            <Alert
              key={workspace.id}
              type="warning"
              showIcon
              style={{ marginTop: 12 }}
              message="云端与本地都有改动"
              description={`工作区「${workspace.displayName || workspace.workspaceName}」需要选择覆盖方向。`}
              action={
                <Space>
                  <Button
                    type="primary"
                    size="small"
                    loading={syncingId === workspace.id}
                    onClick={() => void handleSync(workspace.id, "cloud-wins")}
                  >
                    以云为主
                  </Button>
                  <Button
                    size="small"
                    loading={syncingId === workspace.id}
                    onClick={() => void handleSync(workspace.id, "local-wins")}
                  >
                    以本地为主
                  </Button>
                </Space>
              }
            />
          ))}
      </SettingsCard>

      <Modal
        title={isEditing ? "编辑工作区" : "添加工作区"}
        open={modalOpen}
        onOk={() => void handleSave()}
        onCancel={() => setModalOpen(false)}
        confirmLoading={saving}
        okText={isEditing ? "保存" : "添加"}
        cancelText="取消"
        footer={[
          <Button
            key="test"
            loading={testing}
            onClick={() => void handleTestConnection()}
            style={{ float: "left" }}
          >
            测试连接
          </Button>,
          <Button key="cancel" onClick={() => setModalOpen(false)}>
            取消
          </Button>,
          <Button key="ok" type="primary" loading={saving} onClick={() => void handleSave()}>
            {isEditing ? "保存" : "添加"}
          </Button>
        ]}
      >
        <Space direction="vertical" style={{ width: "100%" }}>
          {!isEditing && (
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              可粘贴从其他 NextShell 复制的工作区 Token 自动填充以下字段。
            </Typography.Text>
          )}
          <div>
            <Typography.Text>API 地址</Typography.Text>
            <Input
              value={editForm.apiBaseUrl}
              onChange={(e) => setEditForm((f) => ({ ...f, apiBaseUrl: e.target.value }))}
              placeholder="https://sync.example.com"
            />
          </div>
          <div>
            <Typography.Text>工作区名称</Typography.Text>
            <Input
              value={editForm.workspaceName}
              onChange={(e) => setEditForm((f) => ({ ...f, workspaceName: e.target.value }))}
              placeholder="my-workspace"
            />
          </div>
          <div>
            <Typography.Text>显示名称（可选）</Typography.Text>
            <Input
              value={editForm.displayName}
              onChange={(e) => setEditForm((f) => ({ ...f, displayName: e.target.value }))}
              placeholder="我的工作区"
            />
          </div>
          <div>
            <Typography.Text>{isEditing ? "更新密码" : "工作区密码"}</Typography.Text>
            <Input.Password
              value={editForm.workspacePassword}
              onChange={(e) => setEditForm((f) => ({ ...f, workspacePassword: e.target.value }))}
              placeholder={isEditing ? "留空则不更新" : `输入密码（至少 ${PASSWORD_MIN} 位）`}
              status={isPasswordTooShort(editForm.workspacePassword) ? "error" : undefined}
            />
            <Typography.Text
              type={isPasswordTooShort(editForm.workspacePassword) ? "danger" : "secondary"}
              style={{ fontSize: 12 }}
            >
              {isPasswordTooShort(editForm.workspacePassword)
                ? PASSWORD_TOO_SHORT
                : `至少 ${PASSWORD_MIN} 位；首次创建工作区时该密码即为团队共享凭据`}
            </Typography.Text>
          </div>
          <div>
            <Typography.Text>拉取间隔（秒）</Typography.Text>
            <InputNumber
              min={10}
              max={86400}
              value={editForm.pullIntervalSec}
              onChange={(v) => setEditForm((f) => ({ ...f, pullIntervalSec: v ?? 300 }))}
              style={{ width: "100%" }}
            />
          </div>
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
            <Typography.Text>忽略 TLS 错误</Typography.Text>
            <Switch
              checked={editForm.ignoreTlsErrors}
              onChange={(v) => setEditForm((f) => ({ ...f, ignoreTlsErrors: v }))}
            />
          </div>
          {editForm.ignoreTlsErrors && (
            <Alert
              type="warning"
              showIcon
              message="已关闭 TLS 证书校验，连接可被中间人窃听或篡改"
              description="仅用于本机 / 内网自签名证书调试（例如 https://127.0.0.1:8443 --dev）。生产环境请使用受信任证书并关闭此开关。"
            />
          )}
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
            <Typography.Text>启用</Typography.Text>
            <Switch
              checked={editForm.enabled}
              onChange={(v) => setEditForm((f) => ({ ...f, enabled: v }))}
            />
          </div>
        </Space>
      </Modal>
    </>
  );
};
