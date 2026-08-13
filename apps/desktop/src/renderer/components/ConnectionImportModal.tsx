import { useEffect, useMemo, useState, type ReactNode } from "react";
import { App as AntdApp, Modal, Radio, Select, Table, Tag } from "antd";
import type {
  ConnectionImportEntry,
  ConnectionProfile,
  ImportConflictPolicy,
  SshKeyProfile
} from "@nextshell/core";
import { LOCAL_DEFAULT_SCOPE_KEY } from "@nextshell/core";
import { filterResourcesByOriginScope } from "@nextshell/shared";
import { formatErrorMessage } from "../utils/errorMessage";

interface ConnectionImportModalProps {
  open: boolean;
  entries: ConnectionImportEntry[];
  existingConnections: ConnectionProfile[];
  sshKeys: SshKeyProfile[];
  sourceName?: string;
  sourceProgress?: string;
  onClose: () => void;
  onImported: () => Promise<void>;
}

export const ConnectionImportModal = ({
  open,
  entries,
  existingConnections,
  sshKeys,
  sourceName,
  sourceProgress,
  onClose,
  onImported
}: ConnectionImportModalProps) => {
  const { message } = AntdApp.useApp();
  const [conflictPolicy, setConflictPolicy] = useState<ImportConflictPolicy>("skip");
  const [importing, setImporting] = useState(false);
  const [keyBindings, setKeyBindings] = useState<Record<number, string>>({});

  useEffect(() => {
    setKeyBindings({});
  }, [entries]);

  const existingSet = useMemo(() => {
    const set = new Set<string>();
    for (const c of existingConnections) {
      set.add(`${c.host}:${c.port}:${c.username}`);
    }
    return set;
  }, [existingConnections]);

  const localSshKeys = useMemo(
    () => filterResourcesByOriginScope(sshKeys, LOCAL_DEFAULT_SCOPE_KEY),
    [sshKeys]
  );
  const sshKeyOptions = useMemo(
    () => localSshKeys.map((key) => ({ label: key.name, value: key.id })),
    [localSshKeys]
  );

  const columns = [
    {
      title: "名称",
      dataIndex: "name",
      key: "name",
      width: 140,
      ellipsis: true
    },
    {
      title: "主机:端口",
      key: "hostPort",
      width: 160,
      render: (_: unknown, record: ConnectionImportEntry) => `${record.host}:${record.port}`
    },
    {
      title: "用户名",
      dataIndex: "username",
      key: "username",
      width: 100,
      ellipsis: true
    },
    {
      title: "分组",
      dataIndex: "groupPath",
      key: "groupPath",
      width: 150,
      ellipsis: true
    },
    {
      title: "来源文件",
      key: "sourceFile",
      width: 140,
      ellipsis: true,
      render: (_: unknown, record: ConnectionImportEntry) =>
        record.sourceRelativePath ?? record.sourceFileName ?? "-"
    },
    {
      title: "认证",
      key: "authType",
      width: 220,
      render: (_: unknown, record: ConnectionImportEntry, index: number) => {
        const boundKeyId = keyBindings[index] ?? record.sshKeyId;
        const authLabel =
          record.originalAuth === "privateKey" || record.authType === "privateKey"
            ? "私钥"
            : record.authType === "interactive"
              ? "交互式"
              : record.authType === "agent"
                ? "Agent"
                : "密码";
        return (
          <div style={{ display: "grid", gap: 6 }}>
            <span>{authLabel}</span>
            {record.needsKeyRebind ? (
              <Select
                size="small"
                allowClear
                placeholder="重新绑定密钥"
                options={sshKeyOptions}
                value={boundKeyId}
                onChange={(value) => {
                  setKeyBindings((prev) => {
                    const next = { ...prev };
                    if (value) {
                      next[index] = value;
                    } else {
                      delete next[index];
                    }
                    return next;
                  });
                }}
              />
            ) : null}
          </div>
        );
      }
    },
    {
      title: "状态",
      key: "status",
      width: 180,
      render: (_: unknown, record: ConnectionImportEntry, index: number) => {
        const tags: ReactNode[] = [];
        const key = `${record.host}:${record.port}:${record.username}`;
        const boundKeyId = keyBindings[index] ?? record.sshKeyId;
        if (existingSet.has(key)) {
          tags.push(
            <Tag key="conflict" color="orange">
              已存在
            </Tag>
          );
        }
        if (record.passwordUnavailable) {
          tags.push(
            <Tag key="pw" color="red">
              密码缺失
            </Tag>
          );
        }
        if (record.needsKeyRebind && !boundKeyId) {
          tags.push(
            <Tag key="key" color="red">
              {record.sourceFormat === "finalshell" ? "原为密钥认证，需重新绑定" : "需重新绑定密钥"}
            </Tag>
          );
        }
        if (record.sourceFormat === "finalshell") {
          tags.push(
            <Tag key="src" color="blue">
              FinalShell
            </Tag>
          );
        }
        if (tags.length === 0) {
          tags.push(
            <Tag key="new" color="green">
              新建
            </Tag>
          );
        }
        return <>{tags}</>;
      }
    }
  ];

  const handleConfirm = async () => {
    setImporting(true);
    try {
      const result = await window.nextshell.connection.importExecute({
        entries: entries.map((e, index) => {
          const sshKeyId = keyBindings[index] ?? e.sshKeyId;
          return {
            name: e.name,
            host: e.host,
            port: e.port,
            username: e.username,
            authType: sshKeyId
              ? "privateKey"
              : e.authType === "privateKey"
                ? "interactive"
                : e.authType,
            password: e.password,
            sshKeyId,
            sshKeyRef: e.sshKeyRef,
            keepAliveEnabled: e.keepAliveEnabled,
            keepAliveIntervalSec: e.keepAliveIntervalSec,
            groupPath: e.groupPath,
            tags: e.tags,
            notes: e.notes,
            favorite: e.favorite,
            terminalEncoding: e.terminalEncoding,
            backspaceMode: e.backspaceMode,
            deleteMode: e.deleteMode,
            monitorSession: e.monitorSession
          };
        }),
        conflictPolicy
      });

      const parts: string[] = [];
      if (result.created > 0) parts.push(`创建 ${result.created}`);
      if (result.overwritten > 0) parts.push(`覆盖 ${result.overwritten}`);
      if (result.skipped > 0) parts.push(`跳过 ${result.skipped}`);
      if (result.failed > 0) parts.push(`失败 ${result.failed}`);
      if (result.passwordsUnavailable > 0)
        parts.push(`${result.passwordsUnavailable} 个密码需手动填写`);

      message.success(`导入完成：${parts.join("，")}`);

      if (result.errors.length > 0) {
        for (const err of result.errors) {
          message.warning(formatErrorMessage(err, "部分连接导入失败"));
        }
      }

      await onImported();
    } catch (error) {
      message.error(`导入失败：${formatErrorMessage(error, "请检查导入文件")}`);
    } finally {
      setImporting(false);
    }
  };

  return (
    <Modal
      open={open}
      onCancel={onClose}
      title={sourceProgress ? `导入连接 (${sourceProgress})` : "导入连接"}
      width={1080}
      okText="确认导入"
      cancelText="取消"
      onOk={handleConfirm}
      confirmLoading={importing}
      destroyOnHidden
    >
      {sourceName ? (
        <div
          style={{ marginBottom: 8, color: "var(--t3)", fontSize: 12, fontFamily: "var(--mono)" }}
        >
          来源：{sourceName}
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
        rowKey={(record, index) =>
          `${record.sourceRelativePath ?? ""}:${record.host}:${record.port}:${record.username}:${index}`
        }
        size="small"
        pagination={false}
        scroll={{ y: 360 }}
        className="app-table"
      />
    </Modal>
  );
};
