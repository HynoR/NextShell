import { useState, useMemo } from "react";
import { Modal, Radio, Table, Tag, message } from "antd";
import type { ConnectionImportEntry, ConnectionProfile, ImportConflictPolicy } from "@nextshell/core";

interface ConnectionImportModalProps {
  open: boolean;
  entries: ConnectionImportEntry[];
  existingConnections: ConnectionProfile[];
  sourceName?: string;
  sourceProgress?: string;
  onClose: () => void;
  onImported: () => Promise<void>;
}

export const ConnectionImportModal = ({
  open,
  entries,
  existingConnections,
  sourceName,
  sourceProgress,
  onClose,
  onImported
}: ConnectionImportModalProps) => {
  const [conflictPolicy, setConflictPolicy] = useState<ImportConflictPolicy>("skip");
  const [importing, setImporting] = useState(false);

  const existingSet = useMemo(() => {
    const set = new Set<string>();
    for (const c of existingConnections) {
      set.add(`${c.host}:${c.port}:${c.username}`);
    }
    return set;
  }, [existingConnections]);

  const columns = [
    {
      title: "名称",
      dataIndex: "name",
      key: "name",
      width: 160,
      ellipsis: true
    },
    {
      title: "主机:端口",
      key: "hostPort",
      width: 180,
      render: (_: unknown, record: ConnectionImportEntry) => `${record.host}:${record.port}`
    },
    {
      title: "用户名",
      dataIndex: "username",
      key: "username",
      width: 120,
      ellipsis: true
    },
    {
      title: "认证",
      dataIndex: "authType",
      key: "authType",
      width: 80
    },
    {
      title: "状态",
      key: "status",
      width: 140,
      render: (_: unknown, record: ConnectionImportEntry) => {
        const tags: React.ReactNode[] = [];
        const key = `${record.host}:${record.port}:${record.username}`;
        if (existingSet.has(key)) {
          tags.push(<Tag key="conflict" color="orange">已存在</Tag>);
        }
        if (record.passwordUnavailable) {
          tags.push(<Tag key="pw" color="red">密码缺失</Tag>);
        }
        if (record.sourceFormat === "competitor") {
          tags.push(<Tag key="src" color="blue">第三方</Tag>);
        }
        if (tags.length === 0) {
          tags.push(<Tag key="new" color="green">新建</Tag>);
        }
        return <>{tags}</>;
      }
    }
  ];

  const handleConfirm = async () => {
    setImporting(true);
    try {
      const result = await window.nextshell.connection.importExecute({
        entries: entries.map((e) => ({
          name: e.name,
          host: e.host,
          port: e.port,
          username: e.username,
          authType: e.authType,
          password: e.password,
          groupPath: e.groupPath,
          tags: e.tags,
          notes: e.notes,
          favorite: e.favorite,
          terminalEncoding: e.terminalEncoding,
          backspaceMode: e.backspaceMode,
          deleteMode: e.deleteMode,
          monitorSession: e.monitorSession
        })),
        conflictPolicy
      });

      const parts: string[] = [];
      if (result.created > 0) parts.push(`创建 ${result.created}`);
      if (result.overwritten > 0) parts.push(`覆盖 ${result.overwritten}`);
      if (result.skipped > 0) parts.push(`跳过 ${result.skipped}`);
      if (result.failed > 0) parts.push(`失败 ${result.failed}`);
      if (result.passwordsUnavailable > 0) parts.push(`${result.passwordsUnavailable} 个密码需手动填写`);

      message.success(`导入完成：${parts.join("，")}`);

      if (result.errors.length > 0) {
        for (const err of result.errors) {
          message.warning(err);
        }
      }

      await onImported();
    } catch (error) {
      const reason = error instanceof Error ? error.message : "未知错误";
      message.error(`导入失败：${reason}`);
    } finally {
      setImporting(false);
    }
  };

  return (
    <Modal
      open={open}
      onCancel={onClose}
      title={sourceProgress ? `导入连接 (${sourceProgress})` : "导入连接"}
      width={720}
      okText="确认导入"
      cancelText="取消"
      onOk={handleConfirm}
      confirmLoading={importing}
      destroyOnHidden
    >
      {sourceName ? (
        <div style={{ marginBottom: 8, color: "var(--t3)", fontSize: 12, fontFamily: "var(--mono)" }}>
          文件：{sourceName}
        </div>
      ) : null}
      <div style={{ marginBottom: 12 }}>
        <span style={{ marginRight: 12 }}>冲突处理：</span>
        <Radio.Group
          value={conflictPolicy}
          onChange={(e) => setConflictPolicy(e.target.value)}
          size="small"
        >
          <Radio.Button value="skip">跳过已有</Radio.Button>
          <Radio.Button value="overwrite">覆盖已有</Radio.Button>
          <Radio.Button value="duplicate">创建副本</Radio.Button>
        </Radio.Group>
      </div>

      <Table
        dataSource={entries}
        columns={columns}
        rowKey={(record, index) => `${record.host}:${record.port}:${record.username}:${index}`}
        size="small"
        pagination={false}
        scroll={{ y: 360 }}
      />
    </Modal>
  );
};
