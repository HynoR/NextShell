import fsp from "node:fs/promises";
import path from "node:path";
import { CONNECTION_ZONES } from "../../../../../packages/shared/src/constants";

const MAX_IMPORT_DIRECTORY_FILES = 2000;
const MAX_IMPORT_FILE_BYTES = 10 * 1024 * 1024;
const WARNING_LIMIT = 50;

export interface ConnectionImportDirectoryFile {
  filePath: string;
  fileName: string;
  relativePath: string;
  groupPath: string;
  size: number;
}

export interface ConnectionImportDirectoryScanResult {
  directoryPath: string;
  files: ConnectionImportDirectoryFile[];
  warnings: string[];
  truncatedWarnings: number;
}

const pushWarning = (warnings: string[], message: string): number => {
  if (warnings.length < WARNING_LIMIT) {
    warnings.push(message);
    return 0;
  }
  return 1;
};

const appendWarning = (
  warnings: string[],
  truncatedCount: { value: number },
  message: string
): void => {
  truncatedCount.value += pushWarning(warnings, message);
};

const normalizeRelativePath = (relativePath: string): string =>
  relativePath.split(path.sep).filter(Boolean).join("/");

export const buildImportGroupPathFromRelativeFile = (relativePath: string): string => {
  const normalized = normalizeRelativePath(relativePath);
  const normalizedDir = path.posix.dirname(normalized);
  if (!normalized || normalizedDir === "." || normalizedDir === "/") {
    return `/${CONNECTION_ZONES.IMPORT}`;
  }

  const segments = normalizedDir
    .split("/")
    .map((segment) => segment.trim())
    .filter((segment) => segment.length > 0 && segment !== "." && segment !== "..");

  return segments.length > 0
    ? `/${CONNECTION_ZONES.IMPORT}/${segments.join("/")}`
    : `/${CONNECTION_ZONES.IMPORT}`;
};

export const scanConnectionImportDirectory = async (
  directoryPath: string
): Promise<ConnectionImportDirectoryScanResult> => {
  const rootPath = path.resolve(directoryPath);
  const warnings: string[] = [];
  const truncatedWarnings = { value: 0 };
  const files: ConnectionImportDirectoryFile[] = [];

  let rootStat;
  try {
    rootStat = await fsp.stat(rootPath);
  } catch {
    throw new Error("导入目录不存在或不可访问");
  }
  if (!rootStat.isDirectory()) {
    throw new Error("导入路径不是目录");
  }

  const walk = async (currentPath: string): Promise<void> => {
    if (files.length >= MAX_IMPORT_DIRECTORY_FILES) {
      return;
    }

    let dirents;
    try {
      dirents = await fsp.readdir(currentPath, { withFileTypes: true });
    } catch (error) {
      const reason = error instanceof Error ? error.message : "未知错误";
      appendWarning(
        warnings,
        truncatedWarnings,
        `${path.relative(rootPath, currentPath) || "."}：读取目录失败：${reason}`
      );
      return;
    }

    dirents.sort((a, b) => a.name.localeCompare(b.name));
    for (const dirent of dirents) {
      if (files.length >= MAX_IMPORT_DIRECTORY_FILES) {
        appendWarning(
          warnings,
          truncatedWarnings,
          `文件数量超过 ${MAX_IMPORT_DIRECTORY_FILES}，已停止继续扫描`
        );
        return;
      }

      const entryPath = path.join(currentPath, dirent.name);
      if (dirent.isSymbolicLink()) {
        appendWarning(
          warnings,
          truncatedWarnings,
          `${normalizeRelativePath(path.relative(rootPath, entryPath))}：已跳过符号链接`
        );
        continue;
      }

      if (dirent.isDirectory()) {
        await walk(entryPath);
        continue;
      }

      if (!dirent.isFile()) {
        continue;
      }

      let stat;
      try {
        stat = await fsp.stat(entryPath);
      } catch (error) {
        const reason = error instanceof Error ? error.message : "未知错误";
        appendWarning(
          warnings,
          truncatedWarnings,
          `${normalizeRelativePath(path.relative(rootPath, entryPath))}：读取文件失败：${reason}`
        );
        continue;
      }

      const relativePath = normalizeRelativePath(path.relative(rootPath, entryPath));
      if (stat.size > MAX_IMPORT_FILE_BYTES) {
        appendWarning(warnings, truncatedWarnings, `${relativePath}：文件超过 10 MB，已跳过`);
        continue;
      }

      files.push({
        filePath: entryPath,
        fileName: path.basename(entryPath),
        relativePath,
        groupPath: buildImportGroupPathFromRelativeFile(relativePath),
        size: stat.size
      });
    }
  };

  await walk(rootPath);

  if (truncatedWarnings.value > 0) {
    warnings.push(`还有 ${truncatedWarnings.value} 条扫描警告未显示`);
  }

  return {
    directoryPath: rootPath,
    files,
    warnings,
    truncatedWarnings: truncatedWarnings.value
  };
};
