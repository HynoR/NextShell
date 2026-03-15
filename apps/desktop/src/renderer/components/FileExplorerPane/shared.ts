import type { RemoteFileEntry } from "@nextshell/core";

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

export const fileTypeLabel = (type: RemoteFileEntry["type"]): string => {
  switch (type) {
    case "directory":
      return "文件夹";
    case "link":
      return "链接";
    default:
      return "文件";
  }
};

export const isPermissionDenied = (stderr: string): boolean =>
  /permission denied|operation not permitted/i.test(stderr);

export const EDITOR_PRESETS: { label: string; value: string }[] = [
  { label: "VS Code", value: "code" },
  { label: "Cursor", value: "cursor" },
  { label: "Sublime Text", value: "subl" },
  { label: "Vim (Terminal)", value: "vim" },
  { label: "Nano (Terminal)", value: "nano" },
  { label: "Notepad++ (Windows)", value: "notepad++" },
  { label: "TextEdit (macOS)", value: "open -t" },
  { label: "Xcode (macOS)", value: "open -a Xcode" }
];

export const shellEscape = (path: string): string => `'${path.replace(/'/g, "'\\''")}'`;

export const inferName = (value: string): string => {
  const normalized = value.trim().replace(/\/+$/, "");
  if (!normalized) return "file";
  const pieces = normalized.split(/[\\/]/).filter(Boolean);
  return pieces.at(-1) ?? "file";
};
