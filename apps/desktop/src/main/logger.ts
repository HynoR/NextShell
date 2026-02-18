import log from "electron-log/main.js";

log.initialize();

log.transports.file.level = "info";
log.transports.file.maxSize = 5 * 1024 * 1024;
log.transports.console.level = process.env.NODE_ENV === "development" ? "debug" : false;

export const logger = log;
