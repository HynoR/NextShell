import { Select } from "antd";
import type { WindowAppearance } from "@nextshell/core";
import { SettingsCard, SettingsRow, SettingsSwitchRow } from "./shared-components";
import type { SaveFn } from "./types";

export const WindowSection = ({
  loading,
  appearance,
  minimizeToTray,
  confirmBeforeClose,
  leftSidebarDefaultCollapsed,
  bottomWorkbenchDefaultCollapsed,
  save,
}: {
  loading: boolean;
  appearance: WindowAppearance;
  minimizeToTray: boolean;
  confirmBeforeClose: boolean;
  leftSidebarDefaultCollapsed: boolean;
  bottomWorkbenchDefaultCollapsed: boolean;
  save: SaveFn;
}) => (
  <>
    <SettingsCard title="界面风格" description="选择应用的外观主题">
      <SettingsRow label="外观模式">
        <Select<WindowAppearance>
          style={{ width: "100%" }}
          value={appearance}
          disabled={loading}
          onChange={(v) => save({ window: { appearance: v } })}
          options={[
            { label: "跟随系统（默认）", value: "system" },
            { label: "亮色模式", value: "light" },
            { label: "暗色模式", value: "dark" },
          ]}
        />
      </SettingsRow>
    </SettingsCard>

    <SettingsCard title="关闭行为" description="控制关闭按钮的行为方式">
      <SettingsSwitchRow
        label="关闭后最小化到托盘"
        checked={minimizeToTray}
        disabled={loading}
        onChange={(v) => save({ window: { minimizeToTray: v } })}
      />
      <SettingsSwitchRow
        label="关闭窗口前确认"
        hint={minimizeToTray ? "启用「最小化到托盘」时自动禁用" : undefined}
        checked={confirmBeforeClose}
        disabled={loading || minimizeToTray}
        onChange={(v) => save({ window: { confirmBeforeClose: v } })}
      />
      {minimizeToTray && (
        <div className="stg-note">
          关闭按钮将隐藏窗口到系统托盘；可从托盘菜单中退出应用。
        </div>
      )}
    </SettingsCard>

    <SettingsCard title="工作区布局" description="默认值仅在首次使用或无历史折叠状态时生效">
      <SettingsSwitchRow
        label="左侧栏默认折叠"
        checked={leftSidebarDefaultCollapsed}
        disabled={loading}
        onChange={(value) => save({ window: { leftSidebarDefaultCollapsed: value } })}
      />
      <SettingsSwitchRow
        label="底部工作区默认折叠"
        checked={bottomWorkbenchDefaultCollapsed}
        disabled={loading}
        onChange={(value) => save({ window: { bottomWorkbenchDefaultCollapsed: value } })}
      />
    </SettingsCard>
  </>
);
