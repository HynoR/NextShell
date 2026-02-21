import React, { useEffect, useMemo, useState } from "react";
import ReactDOM from "react-dom/client";
import { App as AntdApp, ConfigProvider, theme } from "antd";
import "antd/dist/reset.css";
import "@xterm/xterm/css/xterm.css";
import "remixicon/fonts/remixicon.css";
import type { WindowAppearance } from "@nextshell/core";
import { App } from "./App";
import { usePreferencesStore } from "./store/usePreferencesStore";
import "./styles.css";

// Expose current OS platform as a CSS data attribute so layout can adapt
// without JS conditionals (e.g. macOS traffic-lights vs Windows overlay)
document.documentElement.dataset.platform = window.nextshell.platform;
if (window.nextshell.platform === "win32") {
  document.documentElement.style.setProperty(
    "--window-titlebar-safe-top",
    `${window.nextshell.ui.titlebarSafeTop}px`
  );
}
const SYSTEM_DARK_QUERY = "(prefers-color-scheme: dark)";

const DARK_THEME_CONFIG = {
  algorithm: theme.darkAlgorithm,
  token: {
    colorPrimary: "#4d9fff",
    colorBgContainer: "#1d2435",
    colorBgElevated: "#232d40",
    colorBorder: "rgba(255,255,255,0.09)",
    colorText: "#dde4f0",
    colorTextSecondary: "#8893a6",
    colorTextDisabled: "#4a5568",
    colorBgLayout: "#0d1117",
    borderRadius: 4,
    fontFamily: '"Inter", "PingFang SC", system-ui, sans-serif',
    fontSize: 13
  },
  components: {
    Tree: {
      colorBgContainer: "transparent",
      nodeSelectedBg: "rgba(77,159,255,0.14)",
      nodeHoverBg: "rgba(255,255,255,0.06)",
      colorText: "#dde4f0"
    },
    Modal: {
      contentBg: "#161c27",
      headerBg: "#161c27"
    },
    Input: {
      colorBgContainer: "#1d2435",
      colorBorder: "rgba(255,255,255,0.09)"
    },
    Select: {
      colorBgContainer: "#1d2435",
      colorBorder: "rgba(255,255,255,0.09)"
    },
    Table: {
      colorBgContainer: "transparent",
      headerBg: "#1d2435",
      rowHoverBg: "#232d40",
      borderColor: "rgba(255,255,255,0.07)"
    },
    Tabs: {
      itemColor: "#8893a6",
      itemActiveColor: "#4d9fff",
      itemHoverColor: "#dde4f0",
      inkBarColor: "#4d9fff",
      cardBg: "#1d2435"
    },
    Progress: {
      colorInfo: "#4d9fff"
    }
  }
};

const LIGHT_THEME_CONFIG = {
  algorithm: theme.defaultAlgorithm,
  token: {
    colorPrimary: "#1768d6",
    colorBgContainer: "#ffffff",
    colorBgElevated: "#f7f9fc",
    colorBorder: "rgba(14, 31, 53, 0.14)",
    colorText: "#122033",
    colorTextSecondary: "#4d5b73",
    colorTextDisabled: "#8f9bb0",
    colorBgLayout: "#eef2f8",
    borderRadius: 4,
    fontFamily: '"Inter", "PingFang SC", system-ui, sans-serif',
    fontSize: 13
  },
  components: {
    Tree: {
      colorBgContainer: "transparent",
      nodeSelectedBg: "rgba(23, 104, 214, 0.14)",
      nodeHoverBg: "rgba(14, 31, 53, 0.06)",
      colorText: "#122033"
    },
    Modal: {
      contentBg: "#ffffff",
      headerBg: "#ffffff"
    },
    Input: {
      colorBgContainer: "#ffffff",
      colorBorder: "rgba(14, 31, 53, 0.14)"
    },
    Select: {
      colorBgContainer: "#ffffff",
      colorBorder: "rgba(14, 31, 53, 0.14)"
    },
    Table: {
      colorBgContainer: "transparent",
      headerBg: "#eef2f8",
      rowHoverBg: "#f7f9fc",
      borderColor: "rgba(14, 31, 53, 0.08)"
    },
    Tabs: {
      itemColor: "#4d5b73",
      itemActiveColor: "#1768d6",
      itemHoverColor: "#122033",
      inkBarColor: "#1768d6",
      cardBg: "#f7f9fc"
    },
    Progress: {
      colorInfo: "#1768d6"
    }
  }
};

const resolveDarkMode = (appearance: WindowAppearance, systemPrefersDark: boolean): boolean => {
  if (appearance === "light") {
    return false;
  }

  if (appearance === "dark") {
    return true;
  }

  return systemPrefersDark;
};

const Root = () => {
  const initializePreferences = usePreferencesStore((state) => state.initialize);
  const appearance = usePreferencesStore((state) => state.preferences.window.appearance);
  const [systemPrefersDark, setSystemPrefersDark] = useState<boolean>(
    () => window.matchMedia(SYSTEM_DARK_QUERY).matches
  );

  useEffect(() => {
    void initializePreferences();
  }, [initializePreferences]);

  useEffect(() => {
    const mediaQuery = window.matchMedia(SYSTEM_DARK_QUERY);
    const handleChange = (event: MediaQueryListEvent): void => {
      setSystemPrefersDark(event.matches);
    };

    setSystemPrefersDark(mediaQuery.matches);
    mediaQuery.addEventListener("change", handleChange);
    return () => {
      mediaQuery.removeEventListener("change", handleChange);
    };
  }, []);

  const isDarkMode = resolveDarkMode(appearance, systemPrefersDark);

  useEffect(() => {
    document.documentElement.dataset.theme = isDarkMode ? "dark" : "light";
  }, [isDarkMode]);

  const appTheme = useMemo(
    () => (isDarkMode ? DARK_THEME_CONFIG : LIGHT_THEME_CONFIG),
    [isDarkMode]
  );

  return (
    <ConfigProvider theme={appTheme}>
      <AntdApp>
        <App />
      </AntdApp>
    </ConfigProvider>
  );
};

ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
  <React.StrictMode>
    <Root />
  </React.StrictMode>
);
