export const normalizeRemotePath = (rawPath: string): string => {
  const value = rawPath.trim();
  if (!value) return "/";
  const withSlash = value.startsWith("/") ? value : `/${value}`;
  return withSlash.replace(/\/+/g, "/").replace(/\/$/, "") || "/";
};

export const joinRemotePath = (base: string, next: string): string => {
  const root = normalizeRemotePath(base);
  const clean = next.trim().replace(/^\/+/, "").replace(/\/+$/, "");
  if (!clean) return root;
  return root === "/" ? `/${clean}` : `${root}/${clean}`;
};

export const joinLocalPath = (base: string, next: string): string => {
  if (base.endsWith("/") || base.endsWith("\\")) return `${base}${next}`;
  return `${base}/${next}`;
};

export const ensureTarGzName = (name: string): string => {
  const trimmed = name.trim();
  if (!trimmed) return "archive.tar.gz";
  if (trimmed.toLowerCase().endsWith(".tar.gz")) return trimmed;
  return `${trimmed}.tar.gz`;
};

export const formatFileSize = (size: number, isDir: boolean): string => {
  if (isDir) return "";
  if (size === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(size) / Math.log(1024));
  const val = size / Math.pow(1024, i);
  return `${val.toFixed(i > 0 ? 1 : 0)} ${units[i]}`;
};

export const formatModifiedTime = (iso: string): string => {
  if (!iso) return "";
  try {
    const d = new Date(iso);
    const y = d.getFullYear();
    const m = String(d.getMonth() + 1).padStart(2, "0");
    const day = String(d.getDate()).padStart(2, "0");
    const h = String(d.getHours()).padStart(2, "0");
    const min = String(d.getMinutes()).padStart(2, "0");
    return `${y}/${m}/${day} ${h}:${min}`;
  } catch {
    return iso;
  }
};

/** 入参可能是 formatErrorMessage 已译成「权限不足：EACCES」的串,中英文与 errno 都认。 */
export const isPermissionDenied = (text: string): boolean =>
  /权限不足|permission denied|operation not permitted|\bEACCES\b|\bEPERM\b/i.test(text);

export const shellEscape = (path: string): string => `'${path.replace(/'/g, "'\\''")}'`;

export const inferName = (value: string): string => {
  const normalized = value.trim().replace(/\/+$/, "");
  if (!normalized) return "file";
  const pieces = normalized.split(/[\\/]/).filter(Boolean);
  return pieces.at(-1) ?? "file";
};
