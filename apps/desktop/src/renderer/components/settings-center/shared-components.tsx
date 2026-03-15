import { Switch } from "antd";
export { SettingsCard } from "../SettingsCard";

export const SettingsRow = ({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: React.ReactNode;
}) => (
  <div className="stg-row">
    <div className="stg-row-label">
      <span>{label}</span>
      {hint && <span className="stg-row-hint">{hint}</span>}
    </div>
    <div className="stg-row-control">{children}</div>
  </div>
);

export const SettingsSwitchRow = ({
  label,
  hint,
  checked,
  disabled,
  onChange,
}: {
  label: string;
  hint?: string;
  checked: boolean;
  disabled?: boolean;
  onChange: (value: boolean) => void;
}) => (
  <div className="stg-switch-row">
    <div className="stg-switch-label">
      <span>{label}</span>
      {hint && <span className="stg-row-hint">{hint}</span>}
    </div>
    <Switch size="small" checked={checked} disabled={disabled} onChange={onChange} />
  </div>
);
