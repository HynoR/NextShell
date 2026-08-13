import { Tooltip } from "antd";
import type { ConnectionProfile, SshKeyProfile } from "@nextshell/core";
import { formatDateTime, formatRelativeTime } from "../../../utils/formatTime";

interface DetailCardProps {
  connection: ConnectionProfile;
  sshKeys: SshKeyProfile[];
  folderLabel: string;
  onEdit: () => void;
  onConnect: () => void;
}

const Row = ({ label, children }: { label: string; children: React.ReactNode }) => (
  <div className="cm2-detail-row">
    <span className="cm2-detail-label">{label}</span>
    <span className="cm2-detail-value">{children}</span>
  </div>
);

/**
 * 单击行进到的只读态。刻意不是表单:浏览时不可能误改,切行也就没有"未保存改动"要拦截。
 * 这里还能放下表单里塞不进的东西——密钥指纹、连接历史。
 */
export const DetailCard = ({
  connection,
  sshKeys,
  folderLabel,
  onEdit,
  onConnect
}: DetailCardProps) => {
  const boundKey =
    connection.authType === "privateKey" && connection.sshKeyId
      ? sshKeys.find((key) => key.id === connection.sshKeyId)
      : undefined;

  return (
    <div className="cm2-detail">
      <header className="cm2-detail-head">
        <h3 className="cm2-detail-title">{connection.name}</h3>
        <p className="cm2-detail-sub">
          {connection.username ? `${connection.username}@` : ""}
          {connection.host}:{connection.port}
        </p>
      </header>

      <div className="cm2-detail-body">
        <Row label="目录">{folderLabel}</Row>
        <Row label="认证">
          {connection.authType === "privateKey"
            ? boundKey
              ? `私钥 · ${boundKey.name}`
              : "私钥 · 未绑定"
            : connection.authType === "password"
              ? "密码"
              : connection.authType === "interactive"
                ? "交互式"
                : "SSH Agent"}
        </Row>
        {boundKey ? (
          <Row label="指纹">
            <span className="cm2-detail-mono" title={boundKey.fingerprint ?? undefined}>
              {boundKey.fingerprint ?? "未解析（早于保存即解析的旧密钥）"}
            </span>
          </Row>
        ) : null}
        {connection.proxyId ? <Row label="代理">已配置</Row> : null}
        <Row label="标签">
          {connection.tags.length > 0 ? connection.tags.join("、") : <span className="cm2-muted">—</span>}
        </Row>
        <Row label="最后连接">
          {connection.lastConnectedAt ? (
            <Tooltip title={formatDateTime(connection.lastConnectedAt)}>
              {formatRelativeTime(connection.lastConnectedAt)}
            </Tooltip>
          ) : (
            <span className="cm2-muted">从未连接</span>
          )}
        </Row>
        <Row label="修改于">
          <Tooltip title={formatDateTime(connection.updatedAt)}>
            {formatRelativeTime(connection.updatedAt)}
          </Tooltip>
        </Row>
        {connection.notes ? <Row label="备注">{connection.notes}</Row> : null}
      </div>

      <footer className="cm2-detail-foot">
        <button type="button" className="cm2-btn cm2-btn--primary" onClick={onConnect}>
          <i className="ri-terminal-box-line" aria-hidden="true" />
          连接
        </button>
        <button type="button" className="cm2-btn" onClick={onEdit}>
          <i className="ri-edit-line" aria-hidden="true" />
          编辑
        </button>
      </footer>
    </div>
  );
};
