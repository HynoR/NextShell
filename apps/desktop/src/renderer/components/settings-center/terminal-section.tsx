import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { App as AntdApp, Button, Input, InputNumber, Radio, Select, Slider } from "antd";
import { SUPPORTED_BACKGROUND_IMAGE_EXTENSIONS } from "@nextshell/shared";
import type { ShellIntegrationMode } from "@nextshell/core";
import { buildManualInstallInstructions } from "../../../shared/shell-integration";
import { canonicalizeFontFamily, getTerminalFontOptions } from "../../utils/terminalFonts";
import { SettingsCard, SettingsRow, SettingsSwitchRow } from "./shared-components";
import {
  HEX_COLOR_PATTERN,
  CUSTOM_THEME_PRESET,
  CUSTOM_FONT_PRESET,
  TERMINAL_THEME_PRESETS,
  getLocalShellOptions
} from "./constants";
import type {
  LocalShellMode,
  LocalShellPreset,
  LocalShellPreference,
  SaveFn,
  TerminalWallpaperPreference
} from "./types";

/** App background opacity is persisted as an integer percentage in 30..80. */
const clampAppBackgroundOpacity = (value: number): number =>
  Math.min(80, Math.max(30, Math.round(value)));

export const TerminalSection = ({
  loading,
  terminalBackgroundColor,
  terminalForegroundColor,
  terminalThemePreset,
  terminalFontSize,
  terminalLineHeight,
  terminalFontFamily,
  localShell,
  oscClipboardWrite,
  oscClipboardRead,
  oscNotifications,
  oscTitleUpdates,
  hyperlinkConfirm,
  shellIntegration,
  appBackgroundImagePath,
  appBackgroundOpacity,
  terminalWallpaper,
  setTerminalBackgroundColor,
  setTerminalForegroundColor,
  setTerminalThemePreset,
  setLocalShell,
  setAppBackgroundImagePath,
  save,
  message: msg
}: {
  loading: boolean;
  terminalBackgroundColor: string;
  terminalForegroundColor: string;
  terminalThemePreset: string;
  terminalFontSize: number;
  terminalLineHeight: number;
  terminalFontFamily: string;
  localShell: LocalShellPreference;
  oscClipboardWrite: boolean;
  oscClipboardRead: boolean;
  oscNotifications: boolean;
  oscTitleUpdates: boolean;
  hyperlinkConfirm: boolean;
  shellIntegration: ShellIntegrationMode;
  appBackgroundImagePath: string;
  appBackgroundOpacity: number;
  terminalWallpaper: TerminalWallpaperPreference;
  setTerminalBackgroundColor: (v: string) => void;
  setTerminalForegroundColor: (v: string) => void;
  setTerminalThemePreset: (v: string) => void;
  setLocalShell: (value: LocalShellPreference) => void;
  setAppBackgroundImagePath: (v: string) => void;
  save: SaveFn;
  message: ReturnType<typeof AntdApp.useApp>["message"];
}) => {
  /** 数字框失焦/回车时读当前输入;空串或非数字返回 null(沿用已存值)。 */
  const readInputNumber = (raw: string): number | null => {
    const trimmed = raw.trim();
    if (!trimmed) {
      return null;
    }
    const value = Number(trimmed);
    return Number.isFinite(value) ? value : null;
  };

  const persistAppBackgroundOpacity = useCallback(
    (value: number | null) => {
      const numeric = value !== null && Number.isFinite(value) ? value : appBackgroundOpacity;
      // 输入途中可能越界(比如要输 35 时先敲出 "3"),先钳制再保存,Zod patch 只收 30..80。
      const clamped = clampAppBackgroundOpacity(numeric);
      if (clamped !== appBackgroundOpacity) {
        save({ window: { backgroundOpacity: clamped } });
      }
    },
    [appBackgroundOpacity, save]
  );

  const terminalFontOptions = useMemo(() => getTerminalFontOptions(window.nextshell.platform), []);
  const [terminalFontFamilyInput, setTerminalFontFamilyInput] = useState(terminalFontFamily);
  const lastValidTerminalFontFamilyRef = useRef(terminalFontFamily);
  const [localShellCustomPathInput, setLocalShellCustomPathInput] = useState(localShell.customPath);
  const lastValidLocalShellCustomPathRef = useRef(localShell.customPath);

  useEffect(() => {
    setTerminalFontFamilyInput(terminalFontFamily);
    lastValidTerminalFontFamilyRef.current = terminalFontFamily;
  }, [terminalFontFamily]);

  useEffect(() => {
    setLocalShellCustomPathInput(localShell.customPath);
    lastValidLocalShellCustomPathRef.current = localShell.customPath;
  }, [localShell]);

  const selectedTerminalFontPreset = useMemo(() => {
    const normalizedInput = canonicalizeFontFamily(terminalFontFamilyInput);
    const preset = terminalFontOptions.find(
      (option) => canonicalizeFontFamily(option.value) === normalizedInput
    );
    return preset?.value ?? CUSTOM_FONT_PRESET;
  }, [terminalFontFamilyInput, terminalFontOptions]);

  const applyTerminalFontFamily = useCallback(() => {
    const trimmed = terminalFontFamilyInput.trim();
    if (!trimmed) {
      setTerminalFontFamilyInput(lastValidTerminalFontFamilyRef.current);
      msg.warning("终端字体不能为空，已恢复为上一次有效值。");
      return;
    }
    lastValidTerminalFontFamilyRef.current = trimmed;
    setTerminalFontFamilyInput(trimmed);
    save({ terminal: { fontFamily: trimmed } });
  }, [msg, save, terminalFontFamilyInput]);

  const localShellOptions = useMemo(() => getLocalShellOptions(window.nextshell.platform), []);

  const persistLocalShell = useCallback(
    (next: LocalShellPreference) => {
      setLocalShell(next);
      save({ terminal: { localShell: next } });
    },
    [save, setLocalShell]
  );

  const applyLocalShellCustomPath = useCallback(() => {
    const trimmed = localShellCustomPathInput.trim();
    if (!trimmed) {
      setLocalShellCustomPathInput(lastValidLocalShellCustomPathRef.current);
      msg.warning("本地终端可执行文件路径不能为空，已恢复为上一次有效值。");
      return;
    }

    lastValidLocalShellCustomPathRef.current = trimmed;
    setLocalShellCustomPathInput(trimmed);
    persistLocalShell({
      ...localShell,
      mode: "custom",
      customPath: trimmed
    });
  }, [localShell, localShellCustomPathInput, msg, persistLocalShell]);

  const commitTerminalFontSize = useCallback(
    (raw: string) => {
      const value = readInputNumber(raw);
      if (
        value !== null &&
        Number.isInteger(value) &&
        value >= 10 &&
        value <= 24 &&
        value !== terminalFontSize
      ) {
        save({ terminal: { fontSize: value } });
      }
    },
    [save, terminalFontSize]
  );

  const commitTerminalLineHeight = useCallback(
    (raw: string) => {
      const value = readInputNumber(raw);
      if (value !== null && value >= 1 && value <= 2 && value !== terminalLineHeight) {
        save({ terminal: { lineHeight: value } });
      }
    },
    [save, terminalLineHeight]
  );

  return (
    <>
      <SettingsCard title="APP 背景" description="设置应用背景图片、透明度与终端透出行为">
        <SettingsRow label="背景图片">
          <div className="flex gap-2 items-center">
            <Input
              style={{ flex: 1 }}
              value={appBackgroundImagePath}
              disabled={loading}
              readOnly
              placeholder="未设置（点击右侧按钮选择图片）"
            />
            <Button
              onClick={() =>
                void (async () => {
                  try {
                    const result = await window.nextshell.dialog.openFiles({
                      title: "选择 APP 背景图片",
                      filters: [
                        { name: "图片文件", extensions: SUPPORTED_BACKGROUND_IMAGE_EXTENSIONS }
                      ],
                      multi: false
                    });
                    if (!result.canceled && result.filePaths[0]) {
                      setAppBackgroundImagePath(result.filePaths[0]);
                      save({ window: { backgroundImagePath: result.filePaths[0] } });
                    }
                  } catch {
                    msg.error("打开文件选择器失败");
                  }
                })()
              }
            >
              选择图片
            </Button>
            {appBackgroundImagePath && (
              <Button
                danger
                onClick={() => {
                  setAppBackgroundImagePath("");
                  save({ window: { backgroundImagePath: "" } });
                }}
              >
                清除
              </Button>
            )}
          </div>
          {appBackgroundImagePath && (
            <div
              style={{
                height: 80,
                borderRadius: 6,
                overflow: "hidden",
                marginTop: 8,
                backgroundImage: `url("nextshell-asset://local${appBackgroundImagePath}")`,
                backgroundSize: "cover",
                backgroundPosition: "center",
                border: "1px solid rgba(255,255,255,0.1)"
              }}
            />
          )}
        </SettingsRow>

        <SettingsRow label="整体透明度" hint="可调范围 30%-80%">
          <div className="flex gap-3 items-center">
            <Slider
              min={30}
              max={80}
              step={1}
              disabled={loading || !appBackgroundImagePath}
              style={{ flex: 1, margin: 0 }}
              value={appBackgroundOpacity}
              onChangeComplete={persistAppBackgroundOpacity}
            />
            <div className="flex items-center gap-1">
              <InputNumber
                min={30}
                max={80}
                precision={0}
                disabled={loading || !appBackgroundImagePath}
                value={appBackgroundOpacity}
                onBlur={(e) => persistAppBackgroundOpacity(readInputNumber(e.target.value))}
                onPressEnter={(e) =>
                  persistAppBackgroundOpacity(readInputNumber(e.currentTarget.value))
                }
              />
              <span>%</span>
            </div>
          </div>
        </SettingsRow>

        <SettingsSwitchRow
          label="终端透出背景图"
          hint="终端画布转为透明，文字浮在背景图上；关闭则终端保持纯色。切换会重建终端（会话内容自动恢复）"
          checked={terminalWallpaper.seeThrough}
          disabled={loading || !appBackgroundImagePath}
          onChange={(v) => save({ terminal: { wallpaper: { seeThrough: v } } })}
        />
        <SettingsSwitchRow
          label="透出时启用 GPU 加速"
          hint="实验性：大流量输出可能出现字形残影（上游 xterm #5847）；关闭时使用 DOM 渲染器"
          checked={terminalWallpaper.useWebgl}
          disabled={loading || !appBackgroundImagePath || !terminalWallpaper.seeThrough}
          onChange={(v) => save({ terminal: { wallpaper: { useWebgl: v } } })}
        />
      </SettingsCard>

      <SettingsCard title="终端颜色" description="选择终端配色主题或自定义颜色">
        <SettingsRow label="主题预设">
          <Select
            style={{ width: "100%" }}
            value={terminalThemePreset}
            disabled={loading}
            options={[
              ...TERMINAL_THEME_PRESETS.map((p) => ({ label: p.label, value: p.value })),
              { label: "自定义", value: CUSTOM_THEME_PRESET }
            ]}
            onChange={(value) => {
              setTerminalThemePreset(value);
              const preset = TERMINAL_THEME_PRESETS.find((p) => p.value === value);
              if (preset) {
                setTerminalBackgroundColor(preset.backgroundColor);
                setTerminalForegroundColor(preset.foregroundColor);
                save({
                  terminal: {
                    backgroundColor: preset.backgroundColor,
                    foregroundColor: preset.foregroundColor
                  }
                });
              }
            }}
          />
        </SettingsRow>

        <SettingsRow label="终端背景颜色">
          <div className="flex gap-2 items-center">
            <Input
              style={{ flex: 1 }}
              value={terminalBackgroundColor}
              disabled={loading}
              onChange={(e) => setTerminalBackgroundColor(e.target.value)}
              onBlur={() => {
                if (HEX_COLOR_PATTERN.test(terminalBackgroundColor.trim())) {
                  save({ terminal: { backgroundColor: terminalBackgroundColor.trim() } });
                }
              }}
              placeholder="#000000"
            />
            <input
              className="settings-color-input"
              type="color"
              disabled={loading}
              value={
                HEX_COLOR_PATTERN.test(terminalBackgroundColor)
                  ? terminalBackgroundColor
                  : "#000000"
              }
              onChange={(e) => setTerminalBackgroundColor(e.target.value)}
              onBlur={(e) => save({ terminal: { backgroundColor: e.target.value } })}
            />
          </div>
        </SettingsRow>

        <SettingsRow label="终端文字颜色">
          <div className="flex gap-2 items-center">
            <Input
              style={{ flex: 1 }}
              value={terminalForegroundColor}
              disabled={loading}
              onChange={(e) => setTerminalForegroundColor(e.target.value)}
              onBlur={() => {
                if (HEX_COLOR_PATTERN.test(terminalForegroundColor.trim())) {
                  save({ terminal: { foregroundColor: terminalForegroundColor.trim() } });
                }
              }}
              placeholder="#d8eaff"
            />
            <input
              className="settings-color-input"
              type="color"
              disabled={loading}
              value={
                HEX_COLOR_PATTERN.test(terminalForegroundColor)
                  ? terminalForegroundColor
                  : "#d8eaff"
              }
              onChange={(e) => setTerminalForegroundColor(e.target.value)}
              onBlur={(e) => save({ terminal: { foregroundColor: e.target.value } })}
            />
          </div>
        </SettingsRow>
      </SettingsCard>

      <SettingsCard title="终端排版" description="字体、字号和行距设置">
        <SettingsRow label="常用字体">
          <Select
            style={{ width: "100%" }}
            value={selectedTerminalFontPreset}
            disabled={loading}
            options={[...terminalFontOptions, { label: "自定义", value: CUSTOM_FONT_PRESET }]}
            onChange={(value) => {
              if (value === CUSTOM_FONT_PRESET) {
                return;
              }
              lastValidTerminalFontFamilyRef.current = value;
              setTerminalFontFamilyInput(value);
              save({ terminal: { fontFamily: value } });
            }}
          />
        </SettingsRow>
        <SettingsRow label="自定义字体栈" hint="支持 CSS font-family，失焦后保存">
          <Input
            value={terminalFontFamilyInput}
            disabled={loading}
            onChange={(e) => setTerminalFontFamilyInput(e.target.value)}
            onBlur={applyTerminalFontFamily}
            onPressEnter={() => applyTerminalFontFamily()}
            placeholder="'JetBrains Mono', Menlo, Monaco, monospace"
          />
        </SettingsRow>
        <SettingsRow label="终端字号">
          <InputNumber
            style={{ width: "100%" }}
            min={10}
            max={24}
            precision={0}
            value={terminalFontSize}
            disabled={loading}
            onBlur={(e) => commitTerminalFontSize(e.target.value)}
            onPressEnter={(e) => commitTerminalFontSize(e.currentTarget.value)}
          />
        </SettingsRow>
        <SettingsRow label="终端行距">
          <InputNumber
            style={{ width: "100%" }}
            min={1}
            max={2}
            step={0.05}
            precision={2}
            value={terminalLineHeight}
            disabled={loading}
            onBlur={(e) => commitTerminalLineHeight(e.target.value)}
            onPressEnter={(e) => commitTerminalLineHeight(e.currentTarget.value)}
          />
        </SettingsRow>
      </SettingsCard>

      <SettingsCard title="本地终端" description="选择本地终端默认 shell">
        <SettingsRow label="默认 shell">
          <div className="flex gap-2 items-center">
            <Select<LocalShellMode>
              style={{ width: 132, flexShrink: 0 }}
              value={localShell.mode}
              disabled={loading}
              options={[
                { label: "预设", value: "preset" },
                { label: "自定义", value: "custom" }
              ]}
              onChange={(value) => {
                if (value === "preset") {
                  persistLocalShell({
                    ...localShell,
                    mode: value
                  });
                  return;
                }

                persistLocalShell({
                  ...localShell,
                  mode: value,
                  customPath: localShell.customPath.trim()
                });
              }}
            />
            {localShell.mode === "preset" ? (
              <Select<LocalShellPreset>
                style={{ width: "100%" }}
                value={localShell.preset}
                disabled={loading}
                options={localShellOptions}
                onChange={(value) => {
                  persistLocalShell({
                    ...localShell,
                    preset: value
                  });
                }}
              />
            ) : (
              <Input
                value={localShellCustomPathInput}
                disabled={loading}
                onChange={(event) => setLocalShellCustomPathInput(event.target.value)}
                onBlur={applyLocalShellCustomPath}
                onPressEnter={() => applyLocalShellCustomPath()}
                placeholder={
                  window.nextshell.platform === "win32"
                    ? "例如 C:\\Windows\\System32\\cmd.exe"
                    : "/bin/zsh"
                }
              />
            )}
          </div>
        </SettingsRow>
        {localShell.mode === "custom" ? (
          <SettingsRow label="选择可执行文件" hint="仅支持可执行文件路径，不支持整段命令参数">
            <div className="flex gap-2 items-center">
              <Button
                disabled={loading}
                onClick={() =>
                  void (async () => {
                    try {
                      const result = await window.nextshell.dialog.openFiles({
                        title: "选择本地 shell 可执行文件",
                        multi: false
                      });
                      if (!result.canceled && result.filePaths[0]) {
                        lastValidLocalShellCustomPathRef.current = result.filePaths[0];
                        setLocalShellCustomPathInput(result.filePaths[0]);
                        persistLocalShell({
                          ...localShell,
                          mode: "custom",
                          customPath: result.filePaths[0]
                        });
                      }
                    } catch {
                      msg.error("打开文件选择器失败");
                    }
                  })()
                }
              >
                浏览
              </Button>
              <span className="stg-row-hint">启动目录固定为当前用户 Home 目录</span>
            </div>
          </SettingsRow>
        ) : null}
      </SettingsCard>

      <SettingsCard title="终端集成" description="控制远端程序通过 OSC 转义序列与系统交互的能力">
        <SettingsSwitchRow
          label="允许终端写入系统剪贴板"
          hint="OSC 52；远端程序可将文本放入剪贴板"
          checked={oscClipboardWrite}
          disabled={loading}
          onChange={(v) => save({ terminal: { oscClipboardWrite: v } })}
        />
        <SettingsSwitchRow
          label="允许终端读取系统剪贴板"
          hint="OSC 52 读取；默认关闭，开启需谨慎"
          checked={oscClipboardRead}
          disabled={loading}
          onChange={(v) => save({ terminal: { oscClipboardRead: v } })}
        />
        <SettingsSwitchRow
          label="桌面通知"
          hint="OSC 9 / 777；窗口失焦时才弹出"
          checked={oscNotifications}
          disabled={loading}
          onChange={(v) => save({ terminal: { oscNotifications: v } })}
        />
        <SettingsSwitchRow
          label="标签标题跟随终端"
          hint="OSC 0/2；远端程序可修改会话标题"
          checked={oscTitleUpdates}
          disabled={loading}
          onChange={(v) => save({ terminal: { oscTitleUpdates: v } })}
        />
        <SettingsSwitchRow
          label="打开链接前确认"
          hint="显示完整目标地址，防止钓鱼链接"
          checked={hyperlinkConfirm}
          disabled={loading}
          onChange={(v) => save({ terminal: { hyperlinkConfirm: v } })}
        />
        <SettingsRow
          label="Shell 集成"
          hint="自动：检测到远端未集成时注入提示符与 cwd 上报；手动：仅提供安装命令自行安装；关闭：不注入"
        >
          <div className="flex gap-2 items-center flex-wrap">
            <Radio.Group
              value={shellIntegration}
              disabled={loading}
              size="small"
              onChange={(e) =>
                save({ terminal: { shellIntegration: e.target.value as ShellIntegrationMode } })
              }
            >
              <Radio.Button value="auto">自动</Radio.Button>
              <Radio.Button value="manual">手动</Radio.Button>
              <Radio.Button value="off">关闭</Radio.Button>
            </Radio.Group>
            {shellIntegration === "manual" ? (
              <Button
                size="small"
                disabled={loading}
                onClick={() =>
                  void (async () => {
                    try {
                      await navigator.clipboard.writeText(buildManualInstallInstructions());
                      msg.success("已复制，请粘贴到远端 shell 执行");
                    } catch {
                      msg.error("复制失败，请检查剪贴板权限后重试");
                    }
                  })()
                }
              >
                复制安装命令
              </Button>
            ) : null}
          </div>
        </SettingsRow>
      </SettingsCard>
    </>
  );
};
