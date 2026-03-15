import {
  Alert,
  Button,
  Input,
  InputNumber,
  List,
  Modal,
  Popconfirm,
  Skeleton,
  Space,
  Switch,
  Tag,
  Typography,
  message,
} from "antd";
import { useCallback, useEffect, useState } from "react";
import type { CloudSyncWorkspaceProfile } from "@nextshell/core";
import { SettingsCard } from "./SettingsCard";

const api = () => (window as unknown as { nextshell: import("@nextshell/shared").NextShellApi }).nextshell;

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
  enabled: true,
};

interface WorkspaceStatus {
  workspaceId: string;
  state: string;
  lastSyncAt: string | null;
  lastError: string | null;
  pendingCount: number;
  conflictCount: number;
  currentVersion: number | null;
}

interface ConflictItem {
  workspaceId: string;
  workspaceName: string;
  resourceType: string;
  resourceId: string;
  displayName: string;
  serverRevision: number;
  conflictRemoteRevision: number;
  conflictRemoteDeleted: boolean;
  conflictDetectedAt: string;
}

const STATE_COLORS: Record<string, string> = {
  idle: "green",
  syncing: "blue",
  error: "red",
  disabled: "default",
};

const STATE_LABELS: Record<string, string> = {
  idle: "空闲",
  syncing: "同步中",
  error: "出错",
  disabled: "已停用",
};

const RESOURCE_TYPE_LABELS: Record<string, string> = {
  server: "连接",
  sshKey: "SSH 密钥",
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
  const [conflicts, setConflicts] = useState<ConflictItem[]>([]);
  const [conflictsLoading, setConflictsLoading] = useState(false);
  const [conflictBusyKey, setConflictBusyKey] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const [list, statusResult] = await Promise.all([
        api().cloudSync.workspaceList(),
        api().cloudSync.status(),
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

  const refreshConflicts = useCallback(async () => {
    setConflictsLoading(true);
    try {
      const list = await api().cloudSync.listConflicts();
      setConflicts(list);
    } catch (err) {
      message.error(err instanceof Error ? err.message : String(err));
    } finally {
      setConflictsLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
    void refreshConflicts();
    const unsubStatus = api().cloudSync.onStatus(() => {
      void refresh();
      void refreshConflicts();
    });
    const unsubApplied = api().cloudSync.onApplied(() => {
      void refresh();
      void refreshConflicts();
    });
    return () => {
      unsubStatus();
      unsubApplied();
    };
  }, [refresh, refreshConflicts]);

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
      enabled: ws.enabled,
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
          enabled: editForm.enabled,
        });
      } else {
        await api().cloudSync.workspaceAdd({
          apiBaseUrl: editForm.apiBaseUrl,
          workspaceName: editForm.workspaceName,
          displayName: editForm.displayName || undefined,
          workspacePassword: editForm.workspacePassword,
          pullIntervalSec: editForm.pullIntervalSec,
          ignoreTlsErrors: editForm.ignoreTlsErrors,
          enabled: editForm.enabled,
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
        enabled: draft.enabled,
      });
      message.success("已从工作区 Token 自动填充表单");
    } catch (err) {
      message.error(err instanceof Error ? err.message : String(err));
    } finally {
      setPastingToken(false);
    }
  };

  const handleSync = async (workspaceId: string) => {
    setSyncingId(workspaceId);
    try {
      await api().cloudSync.syncNow({ workspaceId });
      await refresh();
      await refreshConflicts();
    } catch (err) {
      message.error(err instanceof Error ? err.message : String(err));
    } finally {
      setSyncingId(null);
    }
  };

  const handleResolveConflict = async (
    workspaceId: string,
    resourceType: string,
    resourceId: string,
    strategy: "keep_local" | "accept_remote",
  ) => {
    const key = `${workspaceId}:${resourceType}:${resourceId}:${strategy}`;
    setConflictBusyKey(key);
    try {
      await api().cloudSync.resolveConflict({
        workspaceId,
        resourceType: resourceType as "server" | "sshKey",
        resourceId,
        strategy,
      });
      await refreshConflicts();
      await refresh();
    } catch (err) {
      message.error(err instanceof Error ? err.message : String(err));
    } finally {
      setConflictBusyKey(null);
    }
  };

  const conflictsByWorkspace = new Map<string, { workspaceName: string; items: ConflictItem[] }>();
  for (const c of conflicts) {
    const existing = conflictsByWorkspace.get(c.workspaceId);
    if (existing) {
      existing.items.push(c);
    } else {
      conflictsByWorkspace.set(c.workspaceId, { workspaceName: c.workspaceName, items: [c] });
    }
  }

  return (
    <>
      <SettingsCard title="云同步 v2 — 多工作区" description="管理多个云同步工作区，每个工作区独立同步。">
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
                    description="关联的同步状态和待推送操作都会被清除。"
                    onConfirm={() => handleRemove(ws.id)}
                    okText="删除"
                    cancelText="取消"
                    okButtonProps={{ danger: true }}
                  >
                    <Button size="small" danger>
                      删除
                    </Button>
                  </Popconfirm>,
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
                      {st && st.conflictCount > 0 && (
                        <Tag color="orange">{st.conflictCount} 冲突</Tag>
                      )}
                    </Space>
                  }
                  description={
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {ws.apiBaseUrl} / {ws.workspaceName}
                      {(st?.lastSyncAt || ws.lastSyncAt) &&
                        ` · 上次同步: ${st?.lastSyncAt ?? ws.lastSyncAt}`}
                      {st && st.pendingCount > 0 && ` · 待推送: ${st.pendingCount}`}
                    </Typography.Text>
                  }
                />
              </List.Item>
            );
          }}
        />
      </SettingsCard>

      <SettingsCard
        title="冲突处理"
        description="当本地和云端同时修改了同一资源时出现冲突。无论选择哪种解决方式，旧版本都会进入回收站。"
      >
        {conflictsLoading ? (
          <Skeleton active paragraph={{ rows: 3 }} />
        ) : conflicts.length === 0 ? (
          <div style={{ textAlign: "center", padding: "16px 0", color: "var(--text-secondary)" }}>
            <span style={{ marginRight: 4 }}>✓</span>
            <span>没有待处理的冲突</span>
          </div>
        ) : (
          Array.from(conflictsByWorkspace.entries()).map(([wsId, group]) => (
            <div key={wsId} style={{ marginBottom: 16 }}>
              <Typography.Text strong style={{ fontSize: 13, display: "block", marginBottom: 8 }}>
                工作区: {group.workspaceName}
              </Typography.Text>
              {group.items.map((item) => {
                const keepBusy = conflictBusyKey === `${wsId}:${item.resourceType}:${item.resourceId}:keep_local`;
                const acceptBusy = conflictBusyKey === `${wsId}:${item.resourceType}:${item.resourceId}:accept_remote`;
                return (
                  <div
                    key={`${item.resourceType}:${item.resourceId}`}
                    style={{
                      display: "flex",
                      justifyContent: "space-between",
                      alignItems: "center",
                      padding: "8px 0",
                      borderBottom: "1px solid var(--border-color)",
                    }}
                  >
                    <div>
                      <Space size={8}>
                        <span>{item.displayName}</span>
                        <Tag>{RESOURCE_TYPE_LABELS[item.resourceType] ?? item.resourceType}</Tag>
                        {item.conflictRemoteDeleted && <Tag color="red">云端已删除</Tag>}
                      </Space>
                      <div>
                        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                          检测时间: {item.conflictDetectedAt}
                        </Typography.Text>
                      </div>
                    </div>
                    <Space>
                      <Button
                        type="primary"
                        size="small"
                        loading={keepBusy}
                        onClick={() =>
                          handleResolveConflict(wsId, item.resourceType, item.resourceId, "keep_local")
                        }
                      >
                        保留本地
                      </Button>
                      <Button
                        size="small"
                        danger
                        loading={acceptBusy}
                        onClick={() =>
                          handleResolveConflict(wsId, item.resourceType, item.resourceId, "accept_remote")
                        }
                      >
                        接受云端
                      </Button>
                    </Space>
                  </div>
                );
              })}
            </div>
          ))
        )}
        {conflicts.length > 0 && (
          <Alert
            type="info"
            showIcon
            message="无论选择「保留本地」还是「接受云端」，被替换的版本都会自动保存到回收站。"
            style={{ marginTop: 8 }}
          />
        )}
      </SettingsCard>

      <Modal
        title={isEditing ? "编辑工作区" : "添加工作区"}
        open={modalOpen}
        onOk={() => void handleSave()}
        onCancel={() => setModalOpen(false)}
        confirmLoading={saving}
        okText={isEditing ? "保存" : "添加"}
        cancelText="取消"
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
              placeholder="https://api.example.com"
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
              placeholder={isEditing ? "留空则不更新" : "输入密码"}
            />
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
