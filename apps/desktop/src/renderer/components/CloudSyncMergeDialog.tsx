import { useCallback, useMemo, useState } from "react";
import { Alert, Button, Checkbox, Modal, Space, Table, Tag, Typography } from "antd";
import type {
  CloudSyncPreviewResult,
  CloudSyncPreviewConnection,
  CloudSyncPreviewSshKey
} from "@nextshell/shared";

/* ───────── Types ───────── */

type MergeDecision = {
  resourceType: "connection" | "sshKey";
  resourceId: string;
  action: "accept_remote" | "keep_local";
};

interface MergeRow {
  key: string;
  resourceType: "connection" | "sshKey";
  resourceId: string;
  name: string;
  detail: string;
  localExists: boolean;
  remoteExists: boolean;
  action: "accept_remote" | "keep_local";
}

export interface CloudSyncMergeDialogProps {
  open: boolean;
  preview: CloudSyncPreviewResult | null;
  loading?: boolean;
  onConfirm: (decisions: MergeDecision[]) => void;
  onCancel: () => void;
}

/* ───────── Helpers ───────── */

const buildMergeRows = (preview: CloudSyncPreviewResult): MergeRow[] => {
  const rows: MergeRow[] = [];
  const localConnMap = new Map(
    preview.localWorkspaceConnections.map((c) => [c.id, c])
  );
  const localKeyMap = new Map(
    preview.localSshKeys.map((k) => [k.id, k])
  );

  // Process remote connections
  for (const rc of preview.remoteConnections) {
    const local = localConnMap.get(rc.id);
    rows.push({
      key: `connection:${rc.id}`,
      resourceType: "connection",
      resourceId: rc.id,
      name: rc.name,
      detail: `${rc.host}:${rc.port}`,
      localExists: !!local,
      remoteExists: true,
      action: local ? "keep_local" : "accept_remote"
    });
    localConnMap.delete(rc.id);
  }

  // Remaining local-only connections (not in remote)
  for (const lc of localConnMap.values()) {
    rows.push({
      key: `connection:${lc.id}`,
      resourceType: "connection",
      resourceId: lc.id,
      name: lc.name,
      detail: `${lc.host}:${lc.port}`,
      localExists: true,
      remoteExists: false,
      action: "keep_local"
    });
  }

  // Process remote SSH keys
  for (const rk of preview.remoteSshKeys) {
    const local = localKeyMap.get(rk.id);
    rows.push({
      key: `sshKey:${rk.id}`,
      resourceType: "sshKey",
      resourceId: rk.id,
      name: rk.name,
      detail: "SSH 密钥",
      localExists: !!local,
      remoteExists: true,
      action: local ? "keep_local" : "accept_remote"
    });
    localKeyMap.delete(rk.id);
  }

  // Remaining local-only SSH keys
  for (const lk of localKeyMap.values()) {
    rows.push({
      key: `sshKey:${lk.id}`,
      resourceType: "sshKey",
      resourceId: lk.id,
      name: lk.name,
      detail: "SSH 密钥",
      localExists: true,
      remoteExists: false,
      action: "keep_local"
    });
  }

  return rows;
};

/* ───────── Component ───────── */

export const CloudSyncMergeDialog = ({
  open,
  preview,
  loading,
  onConfirm,
  onCancel
}: CloudSyncMergeDialogProps) => {
  const initialRows = useMemo(
    () => (preview ? buildMergeRows(preview) : []),
    [preview]
  );

  const [rows, setRows] = useState<MergeRow[]>([]);

  // sync when preview changes
  useMemo(() => {
    setRows(initialRows);
  }, [initialRows]);

  const hasConflicts = useMemo(
    () => rows.some((r) => r.localExists && r.remoteExists),
    [rows]
  );

  const setAction = useCallback((key: string, action: "accept_remote" | "keep_local") => {
    setRows((prev) =>
      prev.map((r) => (r.key === key ? { ...r, action } : r))
    );
  }, []);

  const batchSetAction = useCallback((action: "accept_remote" | "keep_local") => {
    setRows((prev) =>
      prev.map((r) => {
        // Only allow toggling items that exist on both sides
        if (r.localExists && r.remoteExists) {
          return { ...r, action };
        }
        return r;
      })
    );
  }, []);

  const handleConfirm = useCallback(() => {
    const decisions: MergeDecision[] = rows
      .filter((r) => r.remoteExists) // only send decisions for resources that exist remotely
      .map((r) => ({
        resourceType: r.resourceType,
        resourceId: r.resourceId,
        action: r.action
      }));
    onConfirm(decisions);
  }, [rows, onConfirm]);

  const columns = useMemo(
    () => [
      {
        title: "类型",
        dataIndex: "resourceType",
        width: 80,
        render: (type: string) => (
          <Tag color={type === "connection" ? "blue" : "green"}>
            {type === "connection" ? "连接" : "密钥"}
          </Tag>
        )
      },
      {
        title: "名称",
        dataIndex: "name",
        ellipsis: true,
        render: (name: string, record: MergeRow) => (
          <div>
            <Typography.Text strong>{name}</Typography.Text>
            <br />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {record.detail}
            </Typography.Text>
          </div>
        )
      },
      {
        title: "状态",
        width: 120,
        render: (_: unknown, record: MergeRow) => {
          if (record.localExists && record.remoteExists) {
            return <Tag color="orange">本地 + 远端均存在</Tag>;
          }
          if (record.remoteExists && !record.localExists) {
            return <Tag color="cyan">仅远端</Tag>;
          }
          return <Tag>仅本地</Tag>;
        }
      },
      {
        title: "操作",
        width: 160,
        render: (_: unknown, record: MergeRow) => {
          // Local-only items always keep local
          if (!record.remoteExists) {
            return (
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                保留（本地独有）
              </Typography.Text>
            );
          }
          // Remote-only items always accept remote
          if (!record.localExists) {
            return (
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                接受远端（本地无此项）
              </Typography.Text>
            );
          }
          // Both exist: user decides
          return (
            <Space size={4}>
              <Checkbox
                checked={record.action === "keep_local"}
                onChange={(e) =>
                  setAction(record.key, e.target.checked ? "keep_local" : "accept_remote")
                }
              >
                保留本地
              </Checkbox>
            </Space>
          );
        }
      }
    ],
    [setAction]
  );

  const conflictCount = useMemo(
    () => rows.filter((r) => r.localExists && r.remoteExists).length,
    [rows]
  );
  const keepLocalCount = useMemo(
    () => rows.filter((r) => r.localExists && r.remoteExists && r.action === "keep_local").length,
    [rows]
  );

  return (
    <Modal
      open={open}
      title="云同步合并确认"
      width={720}
      mask={{ closable: false }}
      closable={!loading}
      keyboard={!loading}
      onCancel={onCancel}
      footer={
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
          <div style={{ display: "flex", gap: 8 }}>
            {hasConflicts && (
              <>
                <Button size="small" onClick={() => batchSetAction("accept_remote")}>
                  全部接受远端
                </Button>
                <Button size="small" onClick={() => batchSetAction("keep_local")}>
                  全部保留本地
                </Button>
              </>
            )}
          </div>
          <Space>
            <Button onClick={onCancel} disabled={loading}>
              取消
            </Button>
            <Button type="primary" onClick={handleConfirm} loading={loading}>
              确认并启用同步
            </Button>
          </Space>
        </div>
      }
    >
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 16 }}
        message="首次启用云同步时，远端工作区已有数据"
        description={
          hasConflicts
            ? `检测到 ${conflictCount} 项本地与远端均存在的资源。默认保留本地版本，您可以逐项或批量调整。仅远端的项将自动同步到本地，仅本地的项将自动推送到远端。`
            : "远端包含的新资源将被同步到本地，本地独有的资源将被推送到远端。"
        }
      />

      {hasConflicts && (
        <div style={{ marginBottom: 12, fontSize: 13, color: "var(--color-text-secondary)" }}>
          冲突项：{conflictCount} 个，当前保留本地 {keepLocalCount} 个、接受远端 {conflictCount - keepLocalCount} 个
        </div>
      )}

      <Table
        dataSource={rows}
        columns={columns}
        pagination={false}
        size="small"
        scroll={{ y: 360 }}
        rowKey="key"
        locale={{ emptyText: "无需合并的资源" }}
      />
    </Modal>
  );
};
