import { App as AntdApp, Alert, Button, Input, List, Modal, Select, Space, Spin } from "antd";
import type { BackupArchiveMeta } from "@nextshell/core";
import { resolveBackupSectionAccess } from "../settingsCenterBackupAccess";
import { SettingsCard, SettingsRow } from "./shared-components";
import type { SaveFn } from "./types";

export const BackupSection = ({
  loading, backupRemotePath, rclonePath,
  backupConflictPolicy, restoreConflictPolicy, pwdStatus, pwdStatusKnown,
  backupRunning, archiveList, archiveListVisible, archiveListLoading,
  restoring, lastBackupAt,
  setBackupRemotePath, setRclonePath,
  setBackupConflictPolicy, setRestoreConflictPolicy,
  setArchiveListVisible, onOpenSecurity,
  onRunBackup, onListArchives, onRestore,
  save, message: msg,
}: {
  loading: boolean;
  backupRemotePath: string;
  rclonePath: string;
  backupConflictPolicy: "skip" | "force";
  restoreConflictPolicy: "skip_older" | "force";
  pwdStatus: { isSet: boolean; isUnlocked: boolean; keytarAvailable: boolean };
  pwdStatusKnown: boolean;
  backupRunning: boolean;
  archiveList: BackupArchiveMeta[];
  archiveListVisible: boolean;
  archiveListLoading: boolean;
  restoring: string | null;
  lastBackupAt?: string;
  setBackupRemotePath: (v: string) => void;
  setRclonePath: (v: string) => void;
  setBackupConflictPolicy: (v: "skip" | "force") => void;
  setRestoreConflictPolicy: (v: "skip_older" | "force") => void;
  setArchiveListVisible: (v: boolean) => void;
  onOpenSecurity: () => void;
  onRunBackup: () => void;
  onListArchives: () => void;
  onRestore: (id: string) => void;
  save: SaveFn;
  message: ReturnType<typeof AntdApp.useApp>["message"];
}) => {
  const access = resolveBackupSectionAccess({ pwdStatusKnown, pwdStatus });

  return (
    <>
      {access.showSetPasswordAlert && (
        <Alert
          type="warning"
          showIcon
          message={
            <span style={{ fontSize: 12 }}>
              请先设置主密码，
              <Button type="link" size="small" style={{ paddingInline: 0, height: "auto" }} onClick={onOpenSecurity}>
                点击前往「安全与审计」
              </Button>
              。
            </span>
          }
        />
      )}
      {access.showUnlockPasswordAlert && (
        <Alert
          type="warning"
          showIcon
          message={
            <span style={{ fontSize: 12 }}>
              请先解锁主密码，
              <Button type="link" size="small" style={{ paddingInline: 0, height: "auto" }} onClick={onOpenSecurity}>
                点击前往「安全与审计」
              </Button>
              。
            </span>
          }
        />
      )}

      {access.showProtectedContent && (
        <>
          <SettingsCard title="远端配置" description="配置 rclone 路径和冲突策略">
            <SettingsRow label="远端路径">
              <Input
                value={backupRemotePath}
                disabled={loading}
                onChange={(e) => setBackupRemotePath(e.target.value)}
                onBlur={() => save({ backup: { remotePath: backupRemotePath.trim() } })}
                placeholder="例如 myremote:nextshell-backups"
              />
              <div className="stg-note">对应 rclone 已配置的 remote:path 格式。</div>
            </SettingsRow>

            <SettingsRow label="rclone 可执行文件路径">
              <div className="flex gap-2">
                <Input
                  style={{ flex: 1 }}
                  value={rclonePath}
                  disabled={loading}
                  onChange={(e) => setRclonePath(e.target.value)}
                  onBlur={() => save({ backup: { rclonePath: rclonePath.trim() } })}
                  placeholder="留空则自动从 PATH 查找"
                />
                <Button
                  onClick={() =>
                    void (async () => {
                      try {
                        const result = await window.nextshell.dialog.openFiles({ title: "选择 rclone 可执行文件", multi: false });
                        if (!result.canceled && result.filePaths[0]) {
                          setRclonePath(result.filePaths[0]);
                          save({ backup: { rclonePath: result.filePaths[0] } });
                        }
                      } catch { msg.error("打开文件选择器失败"); }
                    })()
                  }
                >
                  浏览
                </Button>
              </div>
            </SettingsRow>

            <div className="flex gap-4 mt-2">
              <SettingsRow label="备份冲突策略">
                <Select
                  style={{ width: "100%" }}
                  value={backupConflictPolicy}
                  disabled={loading}
                  onChange={(v) => {
                    setBackupConflictPolicy(v);
                    save({ backup: { defaultBackupConflictPolicy: v } });
                  }}
                  options={[
                    { label: "跳过已存在", value: "skip" },
                    { label: "强制覆盖", value: "force" },
                  ]}
                />
              </SettingsRow>
              <SettingsRow label="还原冲突策略">
                <Select
                  style={{ width: "100%" }}
                  value={restoreConflictPolicy}
                  disabled={loading}
                  onChange={(v) => {
                    setRestoreConflictPolicy(v);
                    save({ backup: { defaultRestoreConflictPolicy: v } });
                  }}
                  options={[
                    { label: "跳过较旧存档", value: "skip_older" },
                    { label: "强制覆盖", value: "force" },
                  ]}
                />
              </SettingsRow>
            </div>
          </SettingsCard>

          <SettingsCard title="备份操作">
            <Space>
              <Button
                type="primary"
                loading={backupRunning}
                disabled={!pwdStatus.isUnlocked || !backupRemotePath.trim()}
                onClick={onRunBackup}
              >
                立即备份
              </Button>
              <Button
                disabled={!pwdStatus.isUnlocked || !backupRemotePath.trim()}
                onClick={onListArchives}
              >
                查看存档列表
              </Button>
            </Space>
            {lastBackupAt && (
              <div className="stg-note" style={{ marginTop: 4 }}>
                上次备份: {new Date(lastBackupAt).toLocaleString()}
              </div>
            )}

            <Modal
              title="远端存档列表"
              open={archiveListVisible}
              onCancel={() => setArchiveListVisible(false)}
              footer={null}
              width={600}
            >
              {archiveListLoading ? (
                <div style={{ textAlign: "center", padding: 24 }}><Spin /></div>
              ) : (
                <List
                  dataSource={archiveList}
                  locale={{ emptyText: "暂无远端存档" }}
                  renderItem={(item) => (
                    <List.Item
                      actions={[
                        <Button
                          key="restore"
                          size="small"
                          danger
                          loading={restoring === item.id}
                          onClick={() => onRestore(item.id)}
                        >
                          还原
                        </Button>,
                      ]}
                    >
                      <List.Item.Meta
                        title={item.fileName}
                        description={
                          <>
                            <span>{new Date(item.timestamp).toLocaleString()}</span>
                            {" · "}
                            <span>{(item.sizeBytes / 1024).toFixed(1)} KB</span>
                            {" · "}
                            <span>设备: {item.deviceId}</span>
                          </>
                        }
                      />
                    </List.Item>
                  )}
                />
              )}
            </Modal>
          </SettingsCard>
        </>
      )}
    </>
  );
};
