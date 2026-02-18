import path from "node:path";
import { fileURLToPath } from "node:url";
import { app, BrowserWindow } from "electron";
import { logger } from "./logger";
import { registerIpcHandlers } from "./ipc/register";
import { createServiceContainer, type ServiceContainer } from "./services/container";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
let services: ServiceContainer | undefined;

const createMainWindow = async (): Promise<void> => {
  const mainWindow = new BrowserWindow({
    width: 1560,
    height: 940,
    minWidth: 1280,
    minHeight: 780,
    backgroundColor: "#0a1d30",
    title: "NextShell",
    webPreferences: {
      preload: path.join(__dirname, "../preload/index.mjs"),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true
    }
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

  services = createServiceContainer({
    dataDir: path.join(app.getPath("userData"), "storage"),
    keytarServiceName: "NextShell"
  });

  registerIpcHandlers(services);
  await createMainWindow();
  logger.info("[App] main window ready");

  app.on("activate", () => {
    if (BrowserWindow.getAllWindows().length === 0) {
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
  if (services) {
    void services.dispose();
  }
  logger.info("[App] before quit");
});
