import { Button, List, Popconfirm, Space, Tag, Typography, Empty, message } from "antd";
import { useState, useEffect, useCallback } from "react";
import type { RecycleBinEntry } from "@nextshell/core";
import { SettingsCard } from "./shared-components";
import { formatRelativeTime, formatDateTime } from "../../utils/formatTime";

const api = () =>
  (window as unknown as { nextshell: import("@nextshell/shared").NextShellApi }).nextshell;

const REASON_LABELS: Record<string, string> = {
  delete: "用户删除",
  conflict_accept_remote: "冲突接受远端",
  conflict_keep_local: "冲突保留本地",
  danger_move: "危险移动",
};

const TYPE_LABELS: Record<string, string> = {
  server: "连接",
  sshKey: "密钥",
};

const formatScopeKey = (scopeKey: string): string => {
  if (scopeKey === "local-default") return "本地";
  if (scopeKey.startsWith("cloud:")) return `云工作区 ${scopeKey.slice(6)}`;
  return scopeKey;
};

export const RecycleBinSection = () => {
  const [entries, setEntries] = useState<RecycleBinEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [busyId, setBusyId] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const list = await api().recycleBin.list();
      setEntries(list);
    } catch (err) {
      message.error(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const handleRestore = async (entryId: string) => {
    setBusyId(entryId);
    try {
      await api().recycleBin.restore({ recycleBinEntryId: entryId, targetOriginKind: "local" });
      await refresh();
    } catch (err) {
      message.error(err instanceof Error ? err.message : String(err));
    } finally {
      setBusyId(null);
    }
  };

  const handlePurge = async (entryId: string) => {
    setBusyId(entryId);
    try {
      await api().recycleBin.purge({ id: entryId });
      await refresh();
    } catch (err) {
      message.error(err instanceof Error ? err.message : String(err));
    } finally {
      setBusyId(null);
    }
  };

  const handleClear = async () => {
    setBusyId("__clearing__");
    try {
      await api().recycleBin.clear();
      await refresh();
    } catch (err) {
      message.error(err instanceof Error ? err.message : String(err));
    } finally {
      setBusyId(null);
    }
  };

  return (
    <SettingsCard title="回收站" description="被删除或因冲突替换的资源会保留在此，可恢复或永久删除。">
      {entries.length > 0 && (
        <div style={{ marginBottom: 12, textAlign: "right" }}>
          <Popconfirm
            title="清空回收站？"
            description="所有条目将被永久删除，此操作不可恢复。"
            onConfirm={handleClear}
            okText="清空"
            cancelText="取消"
            okButtonProps={{ danger: true }}
          >
            <Button size="small" danger loading={busyId === "__clearing__"}>
              清空回收站
            </Button>
          </Popconfirm>
        </div>
      )}
      <List
        loading={loading}
        dataSource={entries}
        locale={{
          emptyText: (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="回收站为空" />
          ),
        }}
        renderItem={(entry) => (
          <List.Item
            actions={[
              <Button
                key="restore"
                size="small"
                loading={busyId === entry.id}
                onClick={() => handleRestore(entry.id)}
              >
                恢复
              </Button>,
              <Popconfirm
                key="purge"
                title="永久删除？"
                description="此操作不可恢复。"
                onConfirm={() => handlePurge(entry.id)}
                okText="删除"
                cancelText="取消"
                okButtonProps={{ danger: true }}
              >
                <Button size="small" danger loading={busyId === entry.id}>
                  删除
                </Button>
              </Popconfirm>,
            ]}
          >
            <List.Item.Meta
              title={
                <Space>
                  <span>{entry.displayName || entry.originalResourceId}</span>
                  <Tag>{TYPE_LABELS[entry.resourceType] ?? entry.resourceType}</Tag>
                  <Tag color="orange">{REASON_LABELS[entry.reason] ?? entry.reason}</Tag>
                </Space>
              }
              description={
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  删除时间: <span title={formatDateTime(entry.createdAt)}>{formatRelativeTime(entry.createdAt)}</span>
                  {entry.originalScopeKey && ` · 来自: ${formatScopeKey(entry.originalScopeKey)}`}
                </Typography.Text>
              }
            />
          </List.Item>
        )}
      />
    </SettingsCard>
  );
};
