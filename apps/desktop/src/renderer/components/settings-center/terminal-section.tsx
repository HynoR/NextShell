import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { App as AntdApp, Button, Input, InputNumber, Select, Slider } from "antd";
import { SUPPORTED_BACKGROUND_IMAGE_EXTENSIONS } from "@nextshell/shared";
import {
  canonicalizeFontFamily,
  getTerminalFontOptions
} from "../../utils/terminalFonts";
import { SettingsCard, SettingsRow } from "./shared-components";
import {
  HEX_COLOR_PATTERN,
  CUSTOM_THEME_PRESET,
  CUSTOM_FONT_PRESET,
  TERMINAL_THEME_PRESETS,
  TERMINAL_DEBOUNCE_MS,
  getLocalShellOptions,
} from "./constants";
import type { LocalShellMode, LocalShellPreset, LocalShellPreference, SaveFn } from "./types";

export const TerminalSection = ({
  loading, terminalBackgroundColor, terminalForegroundColor,
  terminalThemePreset, terminalFontSize, terminalLineHeight, terminalFontFamily, localShell,
  appBackgroundImagePath, appBackgroundOpacity,
  setTerminalBackgroundColor, setTerminalForegroundColor,
  setTerminalThemePreset, setLocalShell, setAppBackgroundImagePath,
  save, message: msg,
}: {
  loading: boolean;
  terminalBackgroundColor: string;
  terminalForegroundColor: string;
  terminalThemePreset: string;
  terminalFontSize: number;
  terminalLineHeight: number;
  terminalFontFamily: string;
  localShell: LocalShellPreference;
  appBackgroundImagePath: string;
  appBackgroundOpacity: number;
  setTerminalBackgroundColor: (v: string) => void;
  setTerminalForegroundColor: (v: string) => void;
  setTerminalThemePreset: (v: string) => void;
  setLocalShell: (value: LocalShellPreference) => void;
  setAppBackgroundImagePath: (v: string) => void;
  save: SaveFn;
  message: ReturnType<typeof AntdApp.useApp>["message"];
}) => {
  const pendingRef = useRef<Record<string, Record<string, unknown>>>({});
  const timerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  const flushPending = useCallback(() => {
    clearTimeout(timerRef.current);
    timerRef.current = undefined;
    const sections = pendingRef.current;
    if (Object.keys(sections).length > 0) {
      pendingRef.current = {};
      const merged: Record<string, unknown> = {};
      for (const [section, patch] of Object.entries(sections)) {
        merged[section] = patch;
      }
      save(merged);
    }
  }, [save]);

  useEffect(() => {
    return () => {
      clearTimeout(timerRef.current);
      const sections = pendingRef.current;
      if (Object.keys(sections).length > 0) {
        const merged: Record<string, unknown> = {};
        for (const [section, patch] of Object.entries(sections)) {
          merged[section] = patch;
        }
        save(merged);
      }
    };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const debouncedSave = useCallback((section: string, patch: Record<string, unknown>) => {
    const prev = pendingRef.current[section] ?? {};
    pendingRef.current[section] = { ...prev, ...patch };
    clearTimeout(timerRef.current);
    timerRef.current = setTimeout(flushPending, TERMINAL_DEBOUNCE_MS);
  }, [flushPending]);

  const debouncedSaveTerminal = useCallback((patch: Record<string, unknown>) => {
    debouncedSave("terminal", patch);
  }, [debouncedSave]);

  const terminalFontOptions = useMemo(
    () => getTerminalFontOptions(window.nextshell.platform),
    []
  );
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
    debouncedSaveTerminal({ fontFamily: trimmed });
  }, [debouncedSaveTerminal, msg, terminalFontFamilyInput]);

  const localShellOptions = useMemo(
    () => getLocalShellOptions(window.nextshell.platform),
    []
  );

  const persistLocalShell = useCallback((next: LocalShellPreference) => {
    setLocalShell(next);
    debouncedSaveTerminal({ localShell: next });
  }, [debouncedSaveTerminal, setLocalShell]);

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

  return (
  <>
    <SettingsCard title="APP 背景" description="设置应用背景图片和透明度（透明度修改后 3 秒生效）">
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
                    filters: [{ name: "图片文件", extensions: SUPPORTED_BACKGROUND_IMAGE_EXTENSIONS }],
                    multi: false
                  });
                  if (!result.canceled && result.filePaths[0]) {
                    setAppBackgroundImagePath(result.filePaths[0]);
                    save({ window: { backgroundImagePath: result.filePaths[0] } });
                  }
                } catch { msg.error("打开文件选择器失败"); }
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
              height: 80, borderRadius: 6, overflow: "hidden", marginTop: 8,
              backgroundImage: `url("nextshell-asset://local${appBackgroundImagePath}")`,
              backgroundSize: "cover", backgroundPosition: "center",
              border: "1px solid rgba(255,255,255,0.1)",
            }}
          />
        )}
      </SettingsRow>

      <SettingsRow label="整体透明度" hint="可调范围 30%-80%">
        <div className="flex gap-3 items-center">
          <Slider
            min={30} max={80} step={1}
            disabled={loading || !appBackgroundImagePath}
            style={{ flex: 1, margin: 0 }}
            value={appBackgroundOpacity}
            onChange={(v) => debouncedSave("window", { backgroundOpacity: typeof v === "number" ? v : 60 })}
          />
          <div className="flex items-center gap-1">
            <InputNumber
              min={30} max={80} precision={0}
              disabled={loading || !appBackgroundImagePath}
              value={appBackgroundOpacity}
              onChange={(v) => debouncedSave("window", { backgroundOpacity: typeof v === "number" ? v : 60 })}
            />
            <span>%</span>
          </div>
        </div>
      </SettingsRow>
    </SettingsCard>

    <SettingsCard title="终端颜色" description="选择终端配色主题或自定义颜色（修改后 3 秒生效）">
      <SettingsRow label="主题预设">
        <Select
          style={{ width: "100%" }}
          value={terminalThemePreset}
          disabled={loading}
          options={[
            ...TERMINAL_THEME_PRESETS.map((p) => ({ label: p.label, value: p.value })),
            { label: "自定义", value: CUSTOM_THEME_PRESET },
          ]}
          onChange={(value) => {
            setTerminalThemePreset(value);
            const preset = TERMINAL_THEME_PRESETS.find((p) => p.value === value);
            if (preset) {
              setTerminalBackgroundColor(preset.backgroundColor);
              setTerminalForegroundColor(preset.foregroundColor);
              debouncedSaveTerminal({
                backgroundColor: preset.backgroundColor,
                foregroundColor: preset.foregroundColor,
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
                debouncedSaveTerminal({ backgroundColor: terminalBackgroundColor.trim() });
              }
            }}
            placeholder="#000000"
          />
          <input
            className="settings-color-input"
            type="color"
            disabled={loading}
            value={HEX_COLOR_PATTERN.test(terminalBackgroundColor) ? terminalBackgroundColor : "#000000"}
            onChange={(e) => {
              setTerminalBackgroundColor(e.target.value);
              debouncedSaveTerminal({ backgroundColor: e.target.value });
            }}
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
                debouncedSaveTerminal({ foregroundColor: terminalForegroundColor.trim() });
              }
            }}
            placeholder="#d8eaff"
          />
          <input
            className="settings-color-input"
            type="color"
            disabled={loading}
            value={HEX_COLOR_PATTERN.test(terminalForegroundColor) ? terminalForegroundColor : "#d8eaff"}
            onChange={(e) => {
              setTerminalForegroundColor(e.target.value);
              debouncedSaveTerminal({ foregroundColor: e.target.value });
            }}
          />
        </div>
      </SettingsRow>
    </SettingsCard>

    <SettingsCard title="终端排版" description="字体、字号和行距设置（修改后 3 秒生效）">
      <SettingsRow label="常用字体">
        <Select
          style={{ width: "100%" }}
          value={selectedTerminalFontPreset}
          disabled={loading}
          options={[
            ...terminalFontOptions,
            { label: "自定义", value: CUSTOM_FONT_PRESET },
          ]}
          onChange={(value) => {
            if (value === CUSTOM_FONT_PRESET) {
              return;
            }
            lastValidTerminalFontFamilyRef.current = value;
            setTerminalFontFamilyInput(value);
            debouncedSaveTerminal({ fontFamily: value });
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
          min={10} max={24} precision={0}
          value={terminalFontSize}
          disabled={loading}
          onChange={(v) => {
            if (typeof v === "number" && Number.isInteger(v) && v >= 10 && v <= 24) {
              debouncedSaveTerminal({ fontSize: v });
            }
          }}
        />
      </SettingsRow>
      <SettingsRow label="终端行距">
        <InputNumber
          style={{ width: "100%" }}
          min={1} max={2} step={0.05} precision={2}
          value={terminalLineHeight}
          disabled={loading}
          onChange={(v) => {
            if (typeof v === "number" && v >= 1 && v <= 2) {
              debouncedSaveTerminal({ lineHeight: v });
            }
          }}
        />
      </SettingsRow>
    </SettingsCard>

    <SettingsCard title="本地终端" description="选择本地终端默认 shell（修改后 3 秒生效）">
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
              placeholder={window.nextshell.platform === "win32" ? "例如 C:\\Windows\\System32\\cmd.exe" : "/bin/zsh"}
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
  </>
  );
};
