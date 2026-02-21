import { BrowserWindow, nativeTheme } from "electron";
import type { BrowserWindowConstructorOptions } from "electron";
import type { WindowAppearance } from "../../../../packages/core/src/index";
import { WINDOWS_TITLEBAR_SAFE_TOP } from "../shared/window-ui";

type TitleBarOverlayOptions = Exclude<
  BrowserWindowConstructorOptions["titleBarOverlay"],
  boolean | undefined
>;

const WINDOWS_OVERLAY_LIGHT: TitleBarOverlayOptions = {
  color: "#f7f9fc",
  symbolColor: "#122033",
  height: WINDOWS_TITLEBAR_SAFE_TOP
};

const WINDOWS_OVERLAY_DARK: TitleBarOverlayOptions = {
  color: "#161c27",
  symbolColor: "#dde4f0",
  height: WINDOWS_TITLEBAR_SAFE_TOP
};

const WINDOW_BACKGROUND_LIGHT = "#eef2f8";
const WINDOW_BACKGROUND_DARK = "#0d1117";

export const resolveDarkMode = (
  appearance: WindowAppearance,
  systemPrefersDark = nativeTheme.shouldUseDarkColors
): boolean => {
  if (appearance === "light") {
    return false;
  }

  if (appearance === "dark") {
    return true;
  }

  return systemPrefersDark;
};

export const resolveWindowBackgroundColor = (appearance: WindowAppearance): string => {
  return resolveDarkMode(appearance) ? WINDOW_BACKGROUND_DARK : WINDOW_BACKGROUND_LIGHT;
};

export const resolveWindowsTitleBarOverlay = (
  appearance: WindowAppearance
): TitleBarOverlayOptions => {
  return resolveDarkMode(appearance) ? WINDOWS_OVERLAY_DARK : WINDOWS_OVERLAY_LIGHT;
};

export const applyAppearanceToWindow = (
  window: BrowserWindow,
  appearance: WindowAppearance
): void => {
  window.setBackgroundColor(resolveWindowBackgroundColor(appearance));

  if (process.platform === "win32") {
    window.setTitleBarOverlay(resolveWindowsTitleBarOverlay(appearance));
  }
};

export const applyAppearanceToAllWindows = (appearance: WindowAppearance): void => {
  for (const window of BrowserWindow.getAllWindows()) {
    applyAppearanceToWindow(window, appearance);
  }
};
