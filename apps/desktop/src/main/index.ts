import path from "node:path";
import { fileURLToPath } from "node:url";
import {
  app,
  BrowserWindow,
  Tray,
  Menu,
  dialog,
  nativeImage,
  protocol,
  net,
  nativeTheme,
  powerMonitor
} from "electron";
import { logger } from "./logger";
import { registerIpcHandlers } from "./ipc/register";
import { createServiceContainer, type ServiceContainer } from "./services/container";
import {
  applyAppearanceToAllWindows,
  resolveWindowBackgroundColor,
  resolveWindowsTitleBarOverlay
} from "./window-theme";

// Must be called before app is ready — register local asset protocol for app background images
protocol.registerSchemesAsPrivileged([
  { scheme: "nextshell-asset", privileges: { secure: true, standard: true, supportFetchAPI: true, bypassCSP: true } }
]);

const __dirname = path.dirname(fileURLToPath(import.meta.url));
let services: ServiceContainer | undefined;
let tray: Tray | undefined;
let isQuitting = false;

// ─── Renderer auto-reload & monitor pause/resume ────────────────────────────
const RENDERER_RELOAD_WINDOW_MS = 5 * 60_000;
const RENDERER_MAX_AUTO_RELOADS = 3;
const MONITOR_RESUME_DELAY_MS = 5_000;

let rendererReloadTimestamps: number[] = [];
let monitorsPausedByHide = false;
let monitorsPausedBySuspend = false;
let monitorResumeTimer: ReturnType<typeof setTimeout> | undefined;

const tryAutoReloadRenderer = (webContents: Electron.WebContents, reason: string): void => {
  const now = Date.now();
  rendererReloadTimestamps = rendererReloadTimestamps.filter((t) => now - t < RENDERER_RELOAD_WINDOW_MS);
  if (rendererReloadTimestamps.length >= RENDERER_MAX_AUTO_RELOADS) {
    logger.error("[Window] renderer auto-reload limit reached, not reloading", { reason });
    return;
  }
  rendererReloadTimestamps.push(now);
  logger.warn("[Window] reloading renderer", { reason });
  webContents.reload();
};

const pauseMonitors = (reason: string): void => {
  logger.info("[Monitor] pausing all monitor polling", { reason });
  services?.pauseMonitors();
};

const resumeMonitorsIfClear = (reason: string): void => {
  if (monitorsPausedByHide || monitorsPausedBySuspend) {
    return;
  }
  logger.info("[Monitor] resuming all monitor polling", { reason });
  services?.resumeMonitors();
};

const getWindowPrefs = () => {
  const prefs = services?.getAppPreferences();
  return {
    minimizeToTray: prefs?.window?.minimizeToTray ?? false,
    confirmBeforeClose: prefs?.window?.confirmBeforeClose ?? true
  };
};

const destroyTray = (): void => {
  if (tray) {
    tray.destroy();
    tray = undefined;
  }
};

const createTray = (mainWindow: BrowserWindow): Tray => {
  const t = new Tray(nativeImage.createEmpty());
  void app.getFileIcon(process.execPath, { size: "small" })
    .then((icon) => {
      if (!icon.isEmpty()) {
        t.setImage(icon);
      } else {
        logger.warn("[Tray] default executable icon is empty");
      }
    })
    .catch((error) => {
      logger.warn("[Tray] failed to load default executable icon", error);
    });
  if (process.platform === "darwin") {
    t.setTitle(">_");
  }
  t.setToolTip("NextShell");

  const buildMenu = () => Menu.buildFromTemplate([
    {
      label: "显示 NextShell",
      click: () => {
        mainWindow.show();
        mainWindow.focus();
      }
    },
    { type: "separator" },
    {
      label: "退出",
      click: () => {
        isQuitting = true;
        app.quit();
      }
    }
  ]);

  t.setContextMenu(buildMenu());
  t.on("click", () => {
    mainWindow.show();
    mainWindow.focus();
  });
  return t;
};

const createMainWindow = async (): Promise<void> => {
  const isMac = process.platform === "darwin";
  const isWin = process.platform === "win32";
  const appearance = services?.getAppPreferences().window.appearance ?? "system";

  const mainWindow = new BrowserWindow({
    width: 1560,
    height: 940,
    minWidth: 1280,
    minHeight: 780,
    backgroundColor: resolveWindowBackgroundColor(appearance),
    title: "NextShell",
    titleBarStyle: "hidden",
    ...(isMac && { windowButtonPosition: { x: 14, y: 14 } }),
    ...(isWin && { titleBarOverlay: resolveWindowsTitleBarOverlay(appearance) }),
    webPreferences: {
      preload: path.join(__dirname, "../preload/index.mjs"),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true
    }
  });

  mainWindow.on("close", (event) => {
    if (isQuitting) {
      destroyTray();
      return;
    }

    const { minimizeToTray, confirmBeforeClose } = getWindowPrefs();

    if (minimizeToTray) {
      event.preventDefault();
      mainWindow.hide();
      if (!tray) {
        tray = createTray(mainWindow);
      }
      return;
    }

    destroyTray();

    if (confirmBeforeClose) {
      event.preventDefault();
      const result = dialog.showMessageBoxSync(mainWindow, {
        type: "question",
        buttons: ["取消", "退出"],
        defaultId: 0,
        cancelId: 0,
        title: "退出确认",
        message: "确定要退出 NextShell 吗？"
      });
      if (result === 1) {
        isQuitting = true;
        app.quit();
      }
    }
  });

  mainWindow.webContents.on("render-process-gone", (_event, details) => {
    logger.error("[Window] render process gone", { reason: details.reason, exitCode: details.exitCode });
    tryAutoReloadRenderer(mainWindow.webContents, `render-process-gone:${details.reason}`);
  });

  mainWindow.webContents.on("unresponsive", () => {
    logger.warn("[Window] renderer unresponsive");
    tryAutoReloadRenderer(mainWindow.webContents, "unresponsive");
  });

  mainWindow.webContents.on("responsive", () => {
    logger.info("[Window] renderer responsive again");
  });

  // Minimize-to-tray uses mainWindow.hide(): stop hidden monitor polling while
  // nobody can see the data, resume when the window is shown again.
  mainWindow.on("hide", () => {
    monitorsPausedByHide = true;
    pauseMonitors("window hidden");
  });

  mainWindow.on("show", () => {
    monitorsPausedByHide = false;
    resumeMonitorsIfClear("window shown");
  });

  if (process.env.VITE_DEV_SERVER_URL) {
    await mainWindow.loadURL(process.env.VITE_DEV_SERVER_URL);
    mainWindow.webContents.openDevTools({ mode: "detach" });
    return;
  }

  await mainWindow.loadFile(path.join(app.getAppPath(), "dist/index.html"));
};

app.whenReady().then(async () => {
  process.on("uncaughtException", (error) => {
    logger.error("[App] uncaughtException", error);
  });

  process.on("unhandledRejection", (reason) => {
    logger.error("[App] unhandledRejection", reason);
  });

  services = await createServiceContainer({
    dataDir: path.join(app.getPath("userData"), "storage"),
    keytarServiceName: "NextShell"
  });

  // Serve local image files under nextshell-asset:// for app background images
  protocol.handle("nextshell-asset", (request) => {
    const url = new URL(request.url);
    const filePath = decodeURIComponent(url.pathname);
    return net.fetch(`file://${filePath}`);
  });

  registerIpcHandlers(services);

  if (process.platform === "win32") {
    nativeTheme.on("updated", () => {
      const appearance = services?.getAppPreferences().window.appearance ?? "system";
      if (appearance === "system") {
        applyAppearanceToAllWindows(appearance);
      }
    });
  }

  powerMonitor.on("suspend", () => {
    logger.info("[Power] system suspend");
    monitorsPausedBySuspend = true;
    if (monitorResumeTimer) {
      clearTimeout(monitorResumeTimer);
      monitorResumeTimer = undefined;
    }
    pauseMonitors("system suspend");
  });

  powerMonitor.on("resume", () => {
    logger.info("[Power] system resume, resuming monitors shortly");
    if (monitorResumeTimer) {
      clearTimeout(monitorResumeTimer);
    }
    monitorResumeTimer = setTimeout(() => {
      monitorResumeTimer = undefined;
      monitorsPausedBySuspend = false;
      resumeMonitorsIfClear("system resume");
    }, MONITOR_RESUME_DELAY_MS);
  });

  await createMainWindow();
  logger.info("[App] main window ready");

  app.on("activate", () => {
    const existingWindow = BrowserWindow.getAllWindows()[0];
    if (existingWindow) {
      existingWindow.show();
      existingWindow.focus();
    } else {
      void createMainWindow();
    }
  });
});

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") {
    app.quit();
  }
});

app.on("before-quit", () => {
  isQuitting = true;
  destroyTray();
  if (services) {
    void services.dispose();
  }
  logger.info("[App] before quit");
});
