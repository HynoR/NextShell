import { InputNumber } from "antd";
import { SettingsCard, SettingsRow, SettingsSwitchRow } from "./shared-components";
import type { SaveFn } from "./types";

export const CommandSection = ({
  loading, rememberTemplateParams, batchMaxConcurrency, batchRetryCount, save,
}: {
  loading: boolean;
  rememberTemplateParams: boolean;
  batchMaxConcurrency: number;
  batchRetryCount: number;
  save: SaveFn;
}) => (
  <SettingsCard title="命令中心" description="模板参数与批量执行默认参数">
    <SettingsSwitchRow
      label="记住模板参数"
      checked={rememberTemplateParams}
      disabled={loading}
      onChange={(v) => save({ commandCenter: { rememberTemplateParams: v } })}
    />
    <SettingsRow label="批量并发" hint="命令库批量执行时每轮并发数量（1-50）">
      <InputNumber
        min={1}
        max={50}
        precision={0}
        value={batchMaxConcurrency}
        disabled={loading}
        onChange={(value) => {
          if (value === null) return;
          const next = Math.min(50, Math.max(1, Number(value) || 1));
          save({ commandCenter: { batchMaxConcurrency: next } });
        }}
      />
    </SettingsRow>
    <SettingsRow label="批量重试" hint="命令库批量执行失败时的额外重试次数（0-5）">
      <InputNumber
        min={0}
        max={5}
        precision={0}
        value={batchRetryCount}
        disabled={loading}
        onChange={(value) => {
          if (value === null) return;
          const next = Math.min(5, Math.max(0, Number(value) || 0));
          save({ commandCenter: { batchRetryCount: next } });
        }}
      />
    </SettingsRow>
  </SettingsCard>
);
