import { Badge, Button, Checkbox, Input, Skeleton, Space, Tag, Typography } from "antd";
import { SettingsCard, SettingsRow, SettingsSwitchRow } from "./shared-components";
import type { SaveFn } from "./types";

export const SecuritySection = ({
  pwdStatus,
  pwdStatusLoading,
  pwdInput,
  pwdConfirm,
  pwdBusy,
  changeOldPwd,
  changeNewPwd,
  changeConfirmPwd,
  changeAckRisk,
  changeBusy,
  backupRememberPassword,
  loading,
  setPwdInput,
  setPwdConfirm,
  setChangeOldPwd,
  setChangeNewPwd,
  setChangeConfirmPwd,
  setChangeAckRisk,
  onSetPassword,
  onUnlockPassword,
  onChangePassword,
  onClearRemembered,
  onReauthorizeCredentialStore,
  save
}: {
  pwdStatus: { isSet: boolean; isUnlocked: boolean; canRememberPassword: boolean };
  pwdStatusLoading: boolean;
  pwdInput: string;
  pwdConfirm: string;
  pwdBusy: boolean;
  changeOldPwd: string;
  changeNewPwd: string;
  changeConfirmPwd: string;
  changeAckRisk: boolean;
  changeBusy: boolean;
  backupRememberPassword: boolean;
  loading: boolean;
  setPwdInput: (v: string) => void;
  setPwdConfirm: (v: string) => void;
  setChangeOldPwd: (v: string) => void;
  setChangeNewPwd: (v: string) => void;
  setChangeConfirmPwd: (v: string) => void;
  setChangeAckRisk: (v: boolean) => void;
  onSetPassword: () => void;
  onUnlockPassword: () => void;
  onChangePassword: () => void;
  onClearRemembered: () => void;
  onReauthorizeCredentialStore: () => void;
  save: SaveFn;
}) => (
  <>
    <SettingsCard title="主密码" description="用于数据备份、导出加密默认填充和连接密码查看授权">
      <div className="flex items-center gap-2 mb-2">
        <Typography.Text style={{ fontSize: 12 }}>状态: </Typography.Text>
        {pwdStatusLoading ? (
          <Skeleton.Input active size="small" style={{ width: 120 }} />
        ) : pwdStatus.isSet ? (
          pwdStatus.isUnlocked ? (
            <Badge status="success" text="已设置 · 本次已解锁" />
          ) : (
            <Badge status="processing" text="已设置" />
          )
        ) : (
          <Badge status="default" text="未设置" />
        )}
        {!pwdStatus.canRememberPassword && (
          <>
            <Tag color="orange" style={{ marginLeft: 4 }}>
              钥匙串授权被拒绝
            </Tag>
            <Button size="small" onClick={onReauthorizeCredentialStore}>
              重新授权
            </Button>
          </>
        )}
      </div>

      <SettingsRow label={pwdStatus.isSet ? "输入主密码" : "设置主密码"}>
        <Input.Password
          value={pwdInput}
          onChange={(e) => setPwdInput(e.target.value)}
          placeholder={pwdStatus.isSet ? "输入主密码以解锁" : "新主密码（至少 6 个字符）"}
          disabled={pwdBusy}
        />
        {!pwdStatus.isSet && (
          <>
            <div style={{ marginTop: 8 }}>
              <Typography.Text style={{ fontSize: 12 }}>确认密码</Typography.Text>
            </div>
            <Input.Password
              value={pwdConfirm}
              onChange={(e) => setPwdConfirm(e.target.value)}
              placeholder="再次输入密码"
              disabled={pwdBusy}
              style={{ marginTop: 4 }}
            />
          </>
        )}
        <Space style={{ marginTop: 8 }}>
          {pwdStatus.isSet ? (
            <Button
              type="primary"
              loading={pwdBusy}
              disabled={pwdStatus.isUnlocked}
              onClick={onUnlockPassword}
            >
              解锁
            </Button>
          ) : (
            <Button type="primary" loading={pwdBusy} onClick={onSetPassword}>
              设置主密码
            </Button>
          )}
          {pwdStatus.isSet && <Button onClick={onClearRemembered}>清除已记住的主密码</Button>}
        </Space>
      </SettingsRow>

      <SettingsSwitchRow
        label="记住主密码（本机加密存储）"
        checked={backupRememberPassword}
        disabled={loading || !pwdStatus.canRememberPassword}
        onChange={(v) => save({ backup: { rememberPassword: v } })}
      />

      {pwdStatus.isSet && (
        <div style={{ marginTop: 12, paddingTop: 12, borderTop: "1px dashed var(--border)" }}>
          <Typography.Text style={{ fontSize: 12, fontWeight: 600 }}>修改主密码</Typography.Text>
          <div className="stg-note" style={{ marginTop: 4 }}>
            修改后旧备份可能无法还原，建议尽快重新备份。
          </div>

          <SettingsRow label="原密码">
            <Input.Password
              value={changeOldPwd}
              onChange={(e) => setChangeOldPwd(e.target.value)}
              placeholder="请输入当前主密码"
              disabled={changeBusy}
            />
          </SettingsRow>

          <SettingsRow label="新密码">
            <Input.Password
              value={changeNewPwd}
              onChange={(e) => setChangeNewPwd(e.target.value)}
              placeholder="请输入新主密码（至少 6 个字符）"
              disabled={changeBusy}
            />
          </SettingsRow>

          <SettingsRow label="确认新密码">
            <Input.Password
              value={changeConfirmPwd}
              onChange={(e) => setChangeConfirmPwd(e.target.value)}
              placeholder="请再次输入新主密码"
              disabled={changeBusy}
            />
          </SettingsRow>

          <div style={{ marginTop: 8 }}>
            <Checkbox
              checked={changeAckRisk}
              disabled={changeBusy}
              onChange={(e) => setChangeAckRisk(e.target.checked)}
            >
              我已知晓修改后旧备份可能无法还原，需要重新备份。
            </Checkbox>
          </div>

          <Space style={{ marginTop: 8 }}>
            <Button type="primary" loading={changeBusy} onClick={onChangePassword}>
              修改主密码
            </Button>
          </Space>
        </div>
      )}
    </SettingsCard>

  </>
);
