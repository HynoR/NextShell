import { execFile } from "node:child_process";
import fs from "node:fs/promises";
import path from "node:path";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);
const TAR_GZ_SUFFIX = ".tar.gz";
const INVALID_ARCHIVE_CHARS = /[\\/:*?"<>|]+/g;

const normalizeTarError = (error: unknown, phase: string): Error => {
  if (error && typeof error === "object" && "code" in error) {
    const code = (error as NodeJS.ErrnoException).code;
    if (code === "ENOENT") {
      return new Error(`${phase}失败：未找到 tar 命令`);
    }
  }

  if (error instanceof Error) {
    return new Error(`${phase}失败：${error.message}`);
  }

  return new Error(`${phase}失败`);
};

const ensureTarGzSuffix = (name: string): string => {
  const normalized = name.toLowerCase();
  if (normalized.endsWith(TAR_GZ_SUFFIX)) {
    return name;
  }
  return `${name}${TAR_GZ_SUFFIX}`;
};

const sanitizeArchiveBaseName = (rawName: string): string => {
  const stripped = rawName
    .replace(/\.tar\.gz$/i, "")
    .replace(/\.tgz$/i, "")
    .replace(/\.tar$/i, "")
    .replace(/\.gz$/i, "");
  const sanitized = stripped
    .replace(INVALID_ARCHIVE_CHARS, "-")
    .replace(/\s+/g, " ")
    .trim()
    .replace(/^\.+/, "")
    .replace(/\.+$/, "");

  return sanitized;
};

const pathInside = (parentPath: string, childPath: string): boolean => {
  const relative = path.relative(parentPath, childPath);
  return relative === "" || (!relative.startsWith("..") && !path.isAbsolute(relative));
};

const findCommonDirectory = (paths: string[]): string => {
  let commonPath = path.dirname(paths[0]!);

  for (const currentPath of paths) {
    const directory = path.dirname(currentPath);
    while (!pathInside(commonPath, directory)) {
      const next = path.dirname(commonPath);
      if (next === commonPath) {
        break;
      }
      commonPath = next;
    }
  }

  return commonPath;
};

export const shellEscape = (value: string): string => {
  return `'${value.replace(/'/g, "'\\''")}'`;
};

export const normalizeArchiveName = (
  archiveName: string | undefined,
  fallbackBaseName: string
): string => {
  const preferred = archiveName?.trim() || fallbackBaseName.trim() || "nextshell-archive";
  const baseName = sanitizeArchiveBaseName(preferred) || "nextshell-archive";
  return ensureTarGzSuffix(baseName);
};

export const normalizeRemoteEntryNames = (entryNames: string[]): string[] => {
  const normalized = entryNames
    .map((name) => name.trim())
    .filter((name) => name.length > 0);

  if (normalized.length === 0) {
    throw new Error("打包下载失败：未选择远端文件");
  }

  for (const name of normalized) {
    if (name === "." || name === ".." || name.includes("/")) {
      throw new Error(`打包下载失败：非法文件名 ${name}`);
    }
  }

  return normalized;
};

export const buildRemoteTarCheckCommand = (): string => {
  return "command -v tar >/dev/null 2>&1";
};

export const buildRemoteTarCreateCommand = (
  remoteDir: string,
  remoteArchivePath: string,
  entryNames: string[]
): string => {
  const escapedEntries = entryNames.map((entry) => shellEscape(entry)).join(" ");
  return `tar -C ${shellEscape(remoteDir)} -czf ${shellEscape(remoteArchivePath)} -- ${escapedEntries}`;
};

export const buildRemoteTarExtractCommand = (
  remoteArchivePath: string,
  remoteDir: string
): string => {
  return `mkdir -p ${shellEscape(remoteDir)} && tar -xzf ${shellEscape(remoteArchivePath)} -C ${shellEscape(remoteDir)}`;
};

export const buildRemoteRemoveFileCommand = (remotePath: string): string => {
  return `rm -f ${shellEscape(remotePath)}`;
};

export const assertLocalTarAvailable = async (): Promise<void> => {
  try {
    await execFileAsync("tar", ["--version"]);
  } catch (error) {
    throw normalizeTarError(error, "本地 tar 检查");
  }
};

export const createLocalTarGzArchive = async (
  localPaths: string[],
  archivePath: string
): Promise<void> => {
  const resolvedLocalPaths = localPaths.map((rawPath) => path.resolve(rawPath));
  if (resolvedLocalPaths.length === 0) {
    throw new Error("打包上传失败：未选择本地文件");
  }

  for (const localPath of resolvedLocalPaths) {
    let stat;
    try {
      stat = await fs.stat(localPath);
    } catch {
      throw new Error(`打包上传失败：本地路径不存在 ${localPath}`);
    }
    if (!stat.isFile()) {
      throw new Error(`打包上传失败：仅支持文件 ${localPath}`);
    }
  }

  const commonDirectory = findCommonDirectory(resolvedLocalPaths);
  const relativePaths = resolvedLocalPaths.map((localPath) => path.relative(commonDirectory, localPath));

  await fs.mkdir(path.dirname(archivePath), { recursive: true });
  try {
    await execFileAsync("tar", [
      "-C",
      commonDirectory,
      "-czf",
      archivePath,
      "--",
      ...relativePaths
    ]);
  } catch (error) {
    throw normalizeTarError(error, "本地打包");
  }
};
