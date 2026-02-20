import React from "react";
import ReactDOM from "react-dom/client";
import { App as AntdApp, ConfigProvider, theme } from "antd";
import "antd/dist/reset.css";
import "@xterm/xterm/css/xterm.css";
import "remixicon/fonts/remixicon.css";
import { App } from "./App";
import "./styles.css";

// Expose current OS platform as a CSS data attribute so layout can adapt
// without JS conditionals (e.g. macOS traffic-lights vs Windows overlay)
document.documentElement.dataset.platform = window.nextshell.platform;

ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
  <React.StrictMode>
    <ConfigProvider
      theme={{
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
          fontSize: 13,
        },
        components: {
          Tree: {
            colorBgContainer: "transparent",
            nodeSelectedBg: "rgba(77,159,255,0.14)",
            nodeHoverBg: "rgba(255,255,255,0.06)",
            colorText: "#dde4f0",
          },
          Modal: {
            contentBg: "#161c27",
            headerBg: "#161c27",
          },
          Input: {
            colorBgContainer: "#1d2435",
            colorBorder: "rgba(255,255,255,0.09)",
          },
          Select: {
            colorBgContainer: "#1d2435",
            colorBorder: "rgba(255,255,255,0.09)",
          },
          Table: {
            colorBgContainer: "transparent",
            headerBg: "#1d2435",
            rowHoverBg: "#232d40",
            borderColor: "rgba(255,255,255,0.07)",
          },
          Tabs: {
            itemColor: "#8893a6",
            itemActiveColor: "#4d9fff",
            itemHoverColor: "#dde4f0",
            inkBarColor: "#4d9fff",
            cardBg: "#1d2435",
          },
          Progress: {
            colorInfo: "#4d9fff",
          },
        },
      }}
    >
      <AntdApp>
        <App />
      </AntdApp>
    </ConfigProvider>
  </React.StrictMode>
);
