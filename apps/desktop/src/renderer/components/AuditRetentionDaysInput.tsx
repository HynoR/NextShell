import { Input, InputNumber, Space } from "antd";

interface AuditRetentionDaysInputProps {
  value: number;
  disabled?: boolean;
  onChange: (value: number) => void;
}

export const AuditRetentionDaysInput = ({
  value,
  disabled,
  onChange
}: AuditRetentionDaysInputProps) => (
  <Space.Compact>
    <InputNumber
      min={0}
      max={365}
      precision={0}
      value={value}
      disabled={disabled}
      onChange={(nextValue) => {
        if (typeof nextValue === "number" && nextValue >= 0 && nextValue <= 365) {
          onChange(nextValue);
        }
      }}
    />
    <Input
      value="天"
      readOnly
      tabIndex={-1}
      aria-label="日志保留天数单位"
      style={{ width: 48, textAlign: "center", pointerEvents: "none" }}
    />
  </Space.Compact>
);
