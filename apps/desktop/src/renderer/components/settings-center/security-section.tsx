import {
  Badge,
  Button,
  Checkbox,
  Input,
  Skeleton,
  Space,
  Tag,
  Typography
} from "antd";
import { AuditRetentionDaysInput } from "../AuditRetentionDaysInput";
import { SettingsCard, SettingsRow, SettingsSwitchRow } from "./shared-components";
import type { SaveFn } from "./types";

export const SecuritySection = ({
  pwdStatus, pwdStatusLoading, pwdInput, pwdConfirm, pwdBusy,
  changeOldPwd, changeNewPwd, changeConfirmPwd, changeAckRisk, changeBusy,
  backupRememberPassword, loading,
  auditEnabled, auditRetentionDays, clearingAuditLogs, setAuditEnabled, setAuditRetentionDays,
  setPwdInput, setPwdConfirm,
  setChangeOldPwd, setChangeNewPwd, setChangeConfirmPwd, setChangeAckRisk,
  onSetPassword, onUnlockPassword, onChangePassword, onClearRemembered, onClearAuditLogs,
  save,
}: {
  pwdStatus: { isSet: boolean; isUnlocked: boolean; keytarAvailable: boolean };
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
  auditEnabled: boolean;
  auditRetentionDays: number;
  clearingAuditLogs: boolean;
  setAuditEnabled: (v: boolean) => void;
  setAuditRetentionDays: (v: number) => void;
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
  onClearAuditLogs: () => void;
  save: SaveFn;
}) => (
  <>
  <SettingsCard title="主密码" description="用于云同步备份、导出加密默认填充和连接密码查看授权">
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
      {pwdStatus.keytarAvailable && (
        <Tag color="blue" style={{ marginLeft: 4 }}>钥匙串可用</Tag>
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
          <Button type="primary" loading={pwdBusy} disabled={pwdStatus.isUnlocked} onClick={onUnlockPassword}>
            解锁
          </Button>
        ) : (
          <Button type="primary" loading={pwdBusy} onClick={onSetPassword}>
            设置主密码
          </Button>
        )}
        {pwdStatus.keytarAvailable && pwdStatus.isSet && (
          <Button onClick={onClearRemembered}>清除钥匙串缓存</Button>
        )}
      </Space>
    </SettingsRow>

    <SettingsSwitchRow
      label="使用系统钥匙串记住主密码"
      checked={backupRememberPassword}
      disabled={loading || !pwdStatus.keytarAvailable}
      onChange={(v) => save({ backup: { rememberPassword: v } })}
    />

    {pwdStatus.isSet && (
      <div style={{ marginTop: 12, paddingTop: 12, borderTop: "1px dashed var(--border)" }}>
        <Typography.Text style={{ fontSize: 12, fontWeight: 600 }}>
          修改主密码
        </Typography.Text>
        <div className="stg-note" style={{ marginTop: 4 }}>
          修改后旧云存档可能无法还原，建议尽快重新备份。
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
            我已知晓修改后旧云存档可能无法还原，需要重新备份。
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

  <SettingsCard title="审计日志" description="默认关闭，仅在你明确启用后才记录新的操作日志">
    <SettingsSwitchRow
      label="启用审计日志"
      hint="切换结果在下次启动应用后生效"
      checked={auditEnabled}
      disabled={loading}
      onChange={(value) => {
        setAuditEnabled(value);
        save({ audit: { enabled: value } });
      }}
    />
    <SettingsRow label="日志保留天数" hint="设为 0 表示永不清理">
      <AuditRetentionDaysInput
        value={auditRetentionDays}
        disabled={loading || !auditEnabled}
        onChange={(value) => {
          setAuditRetentionDays(value);
          save({ audit: { retentionDays: value } });
        }}
      />
    </SettingsRow>
    <SettingsRow label="历史日志">
      <Button danger loading={clearingAuditLogs} disabled={loading || clearingAuditLogs} onClick={onClearAuditLogs}>
        清空审计日志
      </Button>
    </SettingsRow>
    <div className="stg-note">
      {auditEnabled
        ? "审计日志已设为启用。新设置会在下次启动后开始生效，超过保留天数的日志会在启动时自动清理。"
        : "审计日志当前未启用，不会新增记录。历史日志仍可查看和清空，保留天数仅用于启动时清理旧记录。"}
    </div>
    <div className="stg-note">
      审计日志不包含在云同步备份中。
    </div>
  </SettingsCard>
  </>
);
