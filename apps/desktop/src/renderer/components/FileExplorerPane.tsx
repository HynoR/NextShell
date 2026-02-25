import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { App as AntdApp, Modal, Table, Tooltip, Tree, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import type { DataNode } from "antd/es/tree";
import type { ConnectionProfile, RemoteFileEntry } from "@nextshell/core";
import { usePreferencesStore } from "../store/usePreferencesStore";
import { useTransferQueueStore } from "../store/useTransferQueueStore";
import { pMap } from "../utils/concurrentLimit";
import { formatErrorMessage } from "../utils/errorMessage";
import { promptModal } from "../utils/promptModal";

interface FileExplorerPaneProps {
  connection?: ConnectionProfile;
  connected: boolean;
  onOpenSettings?: () => void;
  onOpenEditorTab?: (connectionId: string, remotePath: string) => Promise<void>;
}

interface DirTreeNode extends DataNode {
  key: string;
  title: string;
  isLeaf: false;
  children?: DirTreeNode[];
}

type ClipboardMode = "copy" | "cut";
interface Clipboard {
  mode: ClipboardMode;
  entries: RemoteFileEntry[];
  sourceConnectionId: string;
}

interface ContextMenuState {
  x: number;
  y: number;
  entries: RemoteFileEntry[];
}

// ── Path helpers ──────────────────────────────────────────
const normalizeRemotePath = (rawPath: string): string => {
  const value = rawPath.trim();
  if (!value) return "/";
  const withSlash = value.startsWith("/") ? value : `/${value}`;
  return withSlash.replace(/\/+/g, "/").replace(/\/$/, "") || "/";
};

const joinRemotePath = (base: string, next: string): string => {
  const root = normalizeRemotePath(base);
  const clean = next.trim().replace(/^\/+/, "").replace(/\/+$/, "");
  if (!clean) return root;
  return root === "/" ? `/${clean}` : `${root}/${clean}`;
};

const joinLocalPath = (base: string, next: string): string => {
  if (base.endsWith("/") || base.endsWith("\\")) return `${base}${next}`;
  return `${base}/${next}`;
};

const ensureTarGzName = (name: string): string => {
  const trimmed = name.trim();
  if (!trimmed) return "archive.tar.gz";
  if (trimmed.toLowerCase().endsWith(".tar.gz")) return trimmed;
  return `${trimmed}.tar.gz`;
};

// ── Format helpers ────────────────────────────────────────
const formatFileSize = (size: number, isDir: boolean): string => {
  if (isDir) return "";
  if (size === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(size) / Math.log(1024));
  const val = size / Math.pow(1024, i);
  return `${val.toFixed(i > 0 ? 1 : 0)} ${units[i]}`;
};

const formatModifiedTime = (iso: string): string => {
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

const fileTypeLabel = (type: RemoteFileEntry["type"]): string => {
  switch (type) {
    case "directory": return "文件夹";
    case "link": return "链接";
    default: return "文件";
  }
};

// ── Permission error detection ────────────────────────────
const isPermissionDenied = (stderr: string): boolean =>
  /permission denied|operation not permitted/i.test(stderr);

const EDITOR_PRESETS: { label: string; value: string }[] = [
  { label: "VS Code", value: "code" },
  { label: "Cursor", value: "cursor" },
  { label: "Sublime Text", value: "subl" },
  { label: "Vim (Terminal)", value: "vim" },
  { label: "Nano (Terminal)", value: "nano" },
  { label: "Notepad++ (Windows)", value: "notepad++" },
  { label: "TextEdit (macOS)", value: "open -t" },
  { label: "Xcode (macOS)", value: "open -a Xcode" },
];

// ── Shell-escape a single path segment ───────────────────
const shellEscape = (p: string): string => `'${p.replace(/'/g, "'\\''")}'`;

// ── Context Menu Component ────────────────────────────────
interface ContextMenuProps {
  state: ContextMenuState;
  clipboard: Clipboard | null;
  currentPath: string;
  connectionId: string;
  onClose: () => void;
  onRefresh: () => void;
  onDownload: (entries: RemoteFileEntry[]) => void;
  onPackedDownload: (entries: RemoteFileEntry[]) => void;
  onUpload: () => void;
  onPackedUpload: () => void;
  onCopyPath: (entries: RemoteFileEntry[]) => void;
  onCopy: (entries: RemoteFileEntry[]) => void;
  onCut: (entries: RemoteFileEntry[]) => void;
  onPaste: () => void;
  onNewFolder: () => void;
  onNewFile: () => void;
  onRename: (entry: RemoteFileEntry) => void;
  onDelete: (entries: RemoteFileEntry[]) => void;
  onQuickDelete: (entries: RemoteFileEntry[]) => void;
  onRemoteEdit: (entry: RemoteFileEntry) => void;
}

const ContextMenu = ({
  state,
  clipboard,
  connectionId,
  onClose,
  onRefresh,
  onDownload,
  onPackedDownload,
  onUpload,
  onPackedUpload,
  onCopyPath,
  onCopy,
  onCut,
  onPaste,
  onNewFolder,
  onNewFile,
  onRename,
  onDelete,
  onQuickDelete,
  onRemoteEdit
}: ContextMenuProps) => {
  const { x, y, entries } = state;
  const hasEntries = entries.length > 0;
  const single = entries.length === 1 ? entries[0] : undefined;
  const hasPaste = Boolean(clipboard);
  const [downloadOpen, setDownloadOpen] = useState(false);
  const [uploadOpen, setUploadOpen] = useState(false);
  const [newOpen, setNewOpen] = useState(false);
  const [pos, setPos] = useState<{ left: number; top: number }>({ left: x, top: y });

  const menuRef = useRef<HTMLDivElement>(null);

  const [visible, setVisible] = useState(false);

  // Adjust position after render so the menu stays within the viewport.
  // By default the cursor sits at the bottom of the menu (menu opens upward);
  // fall back to opening downward only when there isn't enough room above.
  useLayoutEffect(() => {
    const el = menuRef.current;
    if (!el) return;

    const { offsetWidth: w, offsetHeight: h } = el;
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    const GAP = 4;

    // Vertical: prefer opening upward (cursor = menu bottom)
    let top = y - h - GAP;
    if (top < GAP) {
      // Not enough room above → open downward
      top = y + GAP;
    }
    // Clamp so menu never overflows bottom either
    if (top + h > vh - GAP) {
      top = vh - h - GAP;
    }

    // Horizontal: open to the right by default, flip left when needed
    let left = x;
    if (left + w > vw - GAP) {
      left = x - w;
    }
    if (left < GAP) left = GAP;

    setPos({ left, top });
    setVisible(true);
  }, [x, y]);

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        onClose();
      }
    };
    window.addEventListener("mousedown", handler);
    return () => window.removeEventListener("mousedown", handler);
  }, [onClose]);

  const run = (fn: () => void) => {
    fn();
    onClose();
  };

  return (
    <div
      ref={menuRef}
      className="fe-ctx-menu"
      style={{ left: pos.left, top: pos.top, visibility: visible ? "visible" : "hidden" }}
      onContextMenu={(e) => e.preventDefault()}
    >
      <button className="fe-ctx-item" onClick={() => run(onRefresh)}>
        <span className="fe-ctx-icon"><i className="ri-refresh-line" aria-hidden="true" /></span> 刷新
      </button>

      <div className="fe-ctx-divider" />

      <div
        className="fe-ctx-item fe-ctx-submenu-trigger"
        onMouseEnter={() => {
          setDownloadOpen(true);
          setUploadOpen(false);
        }}
        onMouseLeave={() => setDownloadOpen(false)}
      >
        <span className="fe-ctx-icon"><i className="ri-download-2-line" aria-hidden="true" /></span> 下载
        <span className="fe-ctx-arrow">›</span>
        {downloadOpen && (
          <div className="fe-ctx-submenu">
            <button
              className="fe-ctx-item"
              disabled={!hasEntries}
              onClick={() => run(() => onDownload(entries))}
            >
              <span className="fe-ctx-icon"><i className="ri-download-line" aria-hidden="true" /></span> 逐个下载
            </button>
            <button
              className="fe-ctx-item"
              disabled={!hasEntries}
              onClick={() => run(() => onPackedDownload(entries))}
            >
              <span className="fe-ctx-icon"><i className="ri-file-zip-line" aria-hidden="true" /></span> 打包下载
            </button>
          </div>
        )}
      </div>

      <div
        className="fe-ctx-item fe-ctx-submenu-trigger"
        onMouseEnter={() => {
          setUploadOpen(true);
          setDownloadOpen(false);
        }}
        onMouseLeave={() => setUploadOpen(false)}
      >
        <span className="fe-ctx-icon"><i className="ri-upload-2-line" aria-hidden="true" /></span> 上传
        <span className="fe-ctx-arrow">›</span>
        {uploadOpen && (
          <div className="fe-ctx-submenu">
            <button className="fe-ctx-item" onClick={() => run(onUpload)}>
              <span className="fe-ctx-icon"><i className="ri-upload-line" aria-hidden="true" /></span> 逐个上传
            </button>
            <button className="fe-ctx-item" onClick={() => run(onPackedUpload)}>
              <span className="fe-ctx-icon"><i className="ri-inbox-archive-line" aria-hidden="true" /></span> 打包上传
            </button>
          </div>
        )}
      </div>

      <div className="fe-ctx-divider" />

      <button
        className="fe-ctx-item"
        disabled={!hasEntries}
        onClick={() => run(() => onCopyPath(entries))}
      >
        <span className="fe-ctx-icon"><i className="ri-link-m" aria-hidden="true" /></span> 复制路径
      </button>

      <div className="fe-ctx-divider" />

      <button
        className="fe-ctx-item"
        disabled={!hasEntries}
        onClick={() => run(() => onCopy(entries))}
      >
        <span className="fe-ctx-icon"><i className="ri-file-copy-line" aria-hidden="true" /></span> 复制
      </button>
      <button
        className="fe-ctx-item"
        disabled={!hasEntries}
        onClick={() => run(() => onCut(entries))}
      >
        <span className="fe-ctx-icon"><i className="ri-scissors-cut-line" aria-hidden="true" /></span> 剪切
      </button>
      <button
        className="fe-ctx-item"
        disabled={!hasPaste}
        onClick={() => run(onPaste)}
      >
        <span className="fe-ctx-icon"><i className="ri-clipboard-line" aria-hidden="true" /></span>
        粘贴
        {clipboard ? (
          <span className="fe-ctx-badge">{clipboard.mode === "copy" ? "复制" : "剪切"}</span>
        ) : null}
      </button>

      <div className="fe-ctx-divider" />

      {/* New submenu */}
      <div
        className="fe-ctx-item fe-ctx-submenu-trigger"
        onMouseEnter={() => setNewOpen(true)}
        onMouseLeave={() => setNewOpen(false)}
      >
        <span className="fe-ctx-icon"><i className="ri-add-line" aria-hidden="true" /></span> 新建
        <span className="fe-ctx-arrow">›</span>
        {newOpen && (
          <div className="fe-ctx-submenu">
            <button className="fe-ctx-item" onClick={() => run(onNewFolder)}>
              <span className="fe-ctx-icon"><i className="ri-folder-3-line" aria-hidden="true" /></span> 文件夹
            </button>
            <button className="fe-ctx-item" onClick={() => run(onNewFile)}>
              <span className="fe-ctx-icon"><i className="ri-file-line" aria-hidden="true" /></span> 文件
            </button>
          </div>
        )}
      </div>

      <div className="fe-ctx-divider" />

      <button
        className="fe-ctx-item"
        disabled={!single}
        onClick={() => single && run(() => onRename(single))}
      >
        <span className="fe-ctx-icon"><i className="ri-edit-line" aria-hidden="true" /></span> 重命名
      </button>

      <button
        className="fe-ctx-item fe-ctx-danger"
        disabled={!hasEntries}
        onClick={() => run(() => onDelete(entries))}
      >
        <span className="fe-ctx-icon"><i className="ri-delete-bin-6-line" aria-hidden="true" /></span> 删除
      </button>

      <button
        className="fe-ctx-item fe-ctx-danger"
        disabled={!hasEntries}
        onClick={() => run(() => onQuickDelete(entries))}
      >
        <span className="fe-ctx-icon"><i className="ri-flashlight-line" aria-hidden="true" /></span> 快速删除 (rm)
      </button>

      <div className="fe-ctx-divider" />

      <button
        className="fe-ctx-item"
        disabled={!single || single.type === "directory"}
        onClick={() => single && run(() => onRemoteEdit(single))}
      >
        <span className="fe-ctx-icon"><i className="ri-edit-box-line" aria-hidden="true" /></span> 远端编辑
      </button>

      {connectionId && (
        <div className="fe-ctx-connection-hint">
          连接：{connectionId.slice(0, 8)}…
        </div>
      )}
    </div>
  );
};

// ── Main component ────────────────────────────────────────
export const FileExplorerPane = ({ connection, connected, onOpenSettings, onOpenEditorTab }: FileExplorerPaneProps) => {
  const { message, modal } = AntdApp.useApp();
  const preferences = usePreferencesStore((state) => state.preferences);
  const updatePreferences = usePreferencesStore((state) => state.updatePreferences);
  const enqueueTask = useTransferQueueStore((state) => state.enqueueTask);
  const markFailed = useTransferQueueStore((state) => state.markFailed);
  const markSuccess = useTransferQueueStore((state) => state.markSuccess);

  const [pathName, setPathName] = useState("/");
  const [pathInput, setPathInput] = useState("/");
  const [files, setFiles] = useState<RemoteFileEntry[]>([]);
  const [selectedPaths, setSelectedPaths] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);

  const [treeData, setTreeData] = useState<DirTreeNode[]>([]);
  const [expandedKeys, setExpandedKeys] = useState<string[]>([]);
  const [pathHistory, setPathHistory] = useState<string[]>([]);
  const [historyIndex, setHistoryIndex] = useState(-1);
  const skipHistoryRef = useRef(false);
  const pathNameRef = useRef(pathName);

  const [clipboard, setClipboard] = useState<Clipboard | null>(null);
  const [contextMenu, setContextMenu] = useState<ContextMenuState | null>(null);

  const [editorModalOpen, setEditorModalOpen] = useState(false);
  const [editorModalValue, setEditorModalValue] = useState(
    preferences.remoteEdit.defaultEditorCommand
  );
  const pendingEditRef = useRef<RemoteFileEntry | null>(null);

  const [followCwd, setFollowCwd] = useState(false);
  const followCwdLastRef = useRef<string | null>(null);
  const followCwdDebounceRef = useRef<ReturnType<typeof setTimeout> | undefined>(
    undefined
  );
  const navigateRef = useRef<(p: string) => void>(() => {});

  const selectedEntries = useMemo(() => {
    const selected = new Set(selectedPaths);
    return files.filter((item) => selected.has(item.path));
  }, [files, selectedPaths]);

  const singleSelected = selectedEntries.length === 1 ? selectedEntries[0] : undefined;

  const columns: ColumnsType<RemoteFileEntry> = useMemo(
    () => [
      {
        title: "文件名",
        dataIndex: "name",
        key: "name",
        sorter: (a, b) => a.name.localeCompare(b.name),
        defaultSortOrder: "ascend",
        render: (_v: string, row: RemoteFileEntry) => (
          <span className="inline-flex items-center gap-1.5">
            <i
              className={row.type === "directory" ? "ri-folder-3-fill text-sm shrink-0 leading-none" : "ri-file-text-line text-sm shrink-0 leading-none"}
              aria-hidden="true"
            />
            {row.name}
          </span>
        )
      },
      {
        title: "大小",
        dataIndex: "size",
        key: "size",
        width: 90,
        sorter: (a, b) => a.size - b.size,
        render: (v: number, row) => formatFileSize(v, row.type === "directory")
      },
      {
        title: "类型",
        dataIndex: "type",
        key: "type",
        width: 72,
        render: (v: RemoteFileEntry["type"]) => fileTypeLabel(v)
      },
      {
        title: "修改时间",
        dataIndex: "modifiedAt",
        key: "modifiedAt",
        width: 140,
        sorter: (a, b) => a.modifiedAt.localeCompare(b.modifiedAt),
        render: (v: string) => formatModifiedTime(v)
      },
      {
        title: "权限",
        dataIndex: "permissions",
        key: "permissions",
        width: 110
      },
      {
        title: "用户/用户组",
        key: "ownerGroup",
        width: 120,
        render: (_v, row) => `${row.owner}/${row.group}`
      }
    ],
    []
  );

  // ── Navigation ──────────────────────────────────────────
  const pushHistory = useCallback((p: string) => {
    setPathHistory((prev) => {
      const next = prev.slice(0, historyIndex + 1);
      next.push(p);
      return next;
    });
    setHistoryIndex((prev) => prev + 1);
  }, [historyIndex]);

  const navigate = useCallback(
    (p: string) => {
      if (skipHistoryRef.current) {
        skipHistoryRef.current = false;
      } else {
        pushHistory(p);
      }
      setPathName(p);
    },
    [pushHistory]
  );

  useEffect(() => {
    navigateRef.current = navigate;
  }, [navigate]);

  const goBack = useCallback(() => {
    if (historyIndex <= 0) return;
    const prev = pathHistory[historyIndex - 1];
    if (!prev) return;
    skipHistoryRef.current = true;
    setHistoryIndex((i) => i - 1);
    setPathName(prev);
  }, [historyIndex, pathHistory]);

  const goForward = useCallback(() => {
    if (historyIndex >= pathHistory.length - 1) return;
    const next = pathHistory[historyIndex + 1];
    if (!next) return;
    skipHistoryRef.current = true;
    setHistoryIndex((i) => i + 1);
    setPathName(next);
  }, [historyIndex, pathHistory]);

  // ── File loading ────────────────────────────────────────
  const loadFiles = useCallback(async (): Promise<void> => {
    if (!connection || !connected) {
      setFiles([]);
      setSelectedPaths([]);
      return;
    }

    const normalizedPath = normalizeRemotePath(pathName);
    setBusy(true);
    try {
      const list = await window.nextshell.sftp.list({
        connectionId: connection.id,
        path: normalizedPath
      });
      // Don't clear files before setting — keep old list visible until new data arrives
      setFiles(list);
      setSelectedPaths([]);
      setPathName(normalizedPath);
    } catch (error) {
      message.error(`读取目录失败：${formatErrorMessage(error, "请检查连接状态")}`);
      // Only clear on error if this is a new path
      setFiles([]);
    } finally {
      setBusy(false);
    }
  }, [connection, connected, pathName]);

  // ── Tree helpers ────────────────────────────────────────
  const loadTreeChildren = useCallback(
    async (parentPath: string): Promise<DirTreeNode[]> => {
      if (!connection || !connected) return [];
      try {
        const list = await window.nextshell.sftp.list({
          connectionId: connection.id,
          path: parentPath
        });
        return list
          .filter((f) => f.type === "directory")
          .sort((a, b) => a.name.localeCompare(b.name))
          .map((f) => ({
            key: f.path,
            title: f.name,
            isLeaf: false as const
          }));
      } catch {
        return [];
      }
    },
    [connection, connected]
  );

  const updateTreeNode = useCallback(
    (nodes: DirTreeNode[], key: string, children: DirTreeNode[]): DirTreeNode[] =>
      nodes.map((node) => {
        if (node.key === key) return { ...node, children };
        if (node.children) return { ...node, children: updateTreeNode(node.children, key, children) };
        return node;
      }),
    []
  );

  const initTree = useCallback(async () => {
    if (!connection || !connected) {
      setTreeData([]);
      setExpandedKeys([]);
      return;
    }
    const children = await loadTreeChildren("/");
    setTreeData([{ key: "/", title: "/", isLeaf: false, children }]);
    setExpandedKeys(["/"]);
  }, [connection, connected, loadTreeChildren]);

  useEffect(() => {
    setPathName("/");
    setSelectedPaths([]);
    setPathHistory(["/"]);
    setHistoryIndex(0);
    setClipboard(null);
    void initTree();
  }, [connection?.id, connected, initTree]);

  useEffect(() => {
    pathNameRef.current = pathName;
  }, [pathName]);

  useEffect(() => {
    setPathInput(pathName);
  }, [pathName]);

  useEffect(() => {
    if (!connection || !connected) setFollowCwd(false);
  }, [connection?.id, connected]);

  useEffect(() => {
    if (!followCwd || !connection || !connected) {
      if (followCwdDebounceRef.current) {
        clearTimeout(followCwdDebounceRef.current);
        followCwdDebounceRef.current = undefined;
      }
      followCwdLastRef.current = null;
      return;
    }
    let cancelled = false;
    const connId = connection.id;
    const tick = async () => {
      if (cancelled) return;
      try {
        const result = await window.nextshell.session.getCwd({ connectionId: connId });
        if (cancelled) return;
        if (!result?.cwd) return;
        const normalized = normalizeRemotePath(result.cwd);
        if (normalized === followCwdLastRef.current) return;
        followCwdLastRef.current = normalized;
        if (followCwdDebounceRef.current) clearTimeout(followCwdDebounceRef.current);
        followCwdDebounceRef.current = setTimeout(() => {
          followCwdDebounceRef.current = undefined;
          if (!cancelled && pathNameRef.current !== normalized) {
            navigateRef.current(normalized);
          }
        }, 3000);
      } catch {
        // ignore
      }
    };
    void tick();
    const interval = setInterval(() => void tick(), 2000);
    return () => {
      cancelled = true;
      clearInterval(interval);
      if (followCwdDebounceRef.current) {
        clearTimeout(followCwdDebounceRef.current);
        followCwdDebounceRef.current = undefined;
      }
    };
  }, [followCwd, connection?.id, connected]);

  useEffect(() => {
    setEditorModalValue(preferences.remoteEdit.defaultEditorCommand);
  }, [preferences.remoteEdit.defaultEditorCommand]);

  useEffect(() => {
    if (!connection || !connected) {
      setFiles([]);
      setSelectedPaths([]);
      return;
    }
    void loadFiles();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connection?.id, connected, pathName]);

  useEffect(() => {
    const unsub = window.nextshell.sftp.onEditStatus((event) => {
      switch (event.status) {
        case "synced":
          message.success({ content: `已同步: ${event.remotePath.split("/").pop()}`, duration: 2 });
          break;
        case "error":
          message.error({ content: event.message ?? "同步失败", duration: 4 });
          break;
        case "closed":
          message.info({ content: `编辑已关闭: ${event.remotePath.split("/").pop()}`, duration: 2 });
          break;
      }
    });
    return unsub;
  }, []);

  const handleTreeExpand = useCallback(
    async (keys: string[], info: { node: DirTreeNode; expanded: boolean }) => {
      setExpandedKeys(keys);
      if (!info.expanded) return;
      const node = info.node;
      if (node.children && node.children.length > 0) return;
      const children = await loadTreeChildren(node.key);
      setTreeData((prev) => updateTreeNode(prev, node.key, children));
    },
    [loadTreeChildren, updateTreeNode]
  );

  const toParentPath = (): void => {
    const normalized = normalizeRemotePath(pathName);
    if (normalized === "/") return;
    const next = normalized.slice(0, normalized.lastIndexOf("/")) || "/";
    navigate(next);
  };

  const inferName = (value: string): string => {
    const normalized = value.trim().replace(/\/+$/, "");
    if (!normalized) return "file";
    const pieces = normalized.split(/[\\/]/).filter(Boolean);
    return pieces.at(-1) ?? "file";
  };

  // ── SSH exec with permission check ──────────────────────
  const execSSH = useCallback(
    async (command: string): Promise<{ ok: boolean; stderr: string }> => {
      if (!connection) return { ok: false, stderr: "no connection" };
      try {
        const result = await window.nextshell.command.exec({
          connectionId: connection.id,
          command
        });
        if (result.exitCode !== 0) {
          if (isPermissionDenied(result.stderr)) {
            message.error(`权限不足，无法执行操作：${result.stderr.trim()}`);
          } else {
            message.error(`命令执行失败（exit ${result.exitCode}）：${result.stderr.trim() || result.stdout.trim()}`);
          }
          return { ok: false, stderr: result.stderr };
        }
        return { ok: true, stderr: "" };
      } catch (error) {
        const reason = formatErrorMessage(error, "远端命令执行失败");
        message.error(reason);
        return { ok: false, stderr: reason };
      }
    },
    [connection]
  );

  // ── SFTP upload / download ──────────────────────────────
  const handleUpload = async (): Promise<void> => {
    if (!connection) return;

    try {
      const picked = await window.nextshell.dialog.openFiles({
        title: "选择要上传的本地文件",
        defaultPath: preferences.transfer.uploadDefaultDir,
        multi: true
      });

      if (picked.canceled || picked.filePaths.length === 0) {
        return;
      }

      const firstFile = picked.filePaths[0]!;
      const firstDir = firstFile.replace(/[\\/][^\\/]+$/, "");
      if (firstDir && firstDir !== preferences.transfer.uploadDefaultDir) {
        void updatePreferences({
          transfer: {
            uploadDefaultDir: firstDir
          }
        });
      }

      let successCount = 0;
      setBusy(true);

      // Concurrent upload with limit of 5
      await pMap(picked.filePaths, async (localPath) => {
        const remotePath = normalizeRemotePath(joinRemotePath(pathName, inferName(localPath)));
        const task = enqueueTask({
          direction: "upload",
          connectionId: connection.id,
          localPath,
          remotePath
        });

        try {
          await window.nextshell.sftp.upload({
            connectionId: connection.id,
            localPath,
            remotePath,
            taskId: task.id
          });
          markSuccess(task.id);
          successCount += 1;
        } catch (error) {
          const reason = formatErrorMessage(error, "上传失败");
          markFailed(task.id, reason);
          message.error(`上传失败：${inferName(localPath)}（${reason}）`);
        }
      }, 5);

      if (successCount > 0) {
        message.success(`上传完成 (${successCount}/${picked.filePaths.length})`);
      }
      await loadFiles();
    } catch (error) {
      message.error(`上传失败：${formatErrorMessage(error, "请稍后重试")}`);
    } finally {
      setBusy(false);
    }
  };

  const handlePackedUpload = async (): Promise<void> => {
    if (!connection) return;

    try {
      const picked = await window.nextshell.dialog.openFiles({
        title: "选择要打包上传的本地文件",
        defaultPath: preferences.transfer.uploadDefaultDir,
        multi: true
      });
      if (picked.canceled || picked.filePaths.length === 0) {
        return;
      }

      const firstFile = picked.filePaths[0]!;
      const firstDir = firstFile.replace(/[\\/][^\\/]+$/, "");
      if (firstDir && firstDir !== preferences.transfer.uploadDefaultDir) {
        void updatePreferences({
          transfer: {
            uploadDefaultDir: firstDir
          }
        });
      }

      const archiveBase = picked.filePaths.length === 1
        ? inferName(firstFile)
        : `upload-bundle-${Date.now()}`;
      const archiveName = ensureTarGzName(archiveBase);
      const remotePath = normalizeRemotePath(joinRemotePath(pathName, archiveName));
      const localDisplayPath = picked.filePaths.length === 1
        ? firstFile
        : `${firstFile} (+${picked.filePaths.length - 1} files)`;

      setBusy(true);
      const task = enqueueTask({
        direction: "upload",
        connectionId: connection.id,
        localPath: localDisplayPath,
        remotePath,
        retryable: false
      });

      try {
        await window.nextshell.sftp.uploadPacked({
          connectionId: connection.id,
          localPaths: picked.filePaths,
          remoteDir: normalizeRemotePath(pathName),
          archiveName,
          taskId: task.id
        });
        markSuccess(task.id);
        message.success("打包上传完成");
        await loadFiles();
      } catch (error) {
        const reason = formatErrorMessage(error, "打包上传失败");
        markFailed(task.id, reason);
        message.error(`打包上传失败：${reason}`);
      }
    } catch (error) {
      message.error(`打包上传失败：${formatErrorMessage(error, "请稍后重试")}`);
    } finally {
      setBusy(false);
    }
  };

  const handleDownload = useCallback(
    async (
      entries: RemoteFileEntry[],
      targetBaseDir?: string,
      persistDefaultDir = false
    ): Promise<void> => {
      if (!connection || entries.length === 0) return;
      const localBasePath = (targetBaseDir || preferences.transfer.downloadDefaultDir).trim();
      if (!localBasePath) return;

      if (persistDefaultDir && localBasePath !== preferences.transfer.downloadDefaultDir) {
        void updatePreferences({
          transfer: {
            downloadDefaultDir: localBasePath
          }
        });
      }

      try {
        let successCount = 0;
        setBusy(true);

        // Concurrent download with limit of 5
        await pMap(entries, async (entry) => {
          const targetPath = joinLocalPath(localBasePath, entry.name);
          const task = enqueueTask({
            direction: "download",
            connectionId: connection.id,
            localPath: targetPath,
            remotePath: entry.path
          });
          try {
            await window.nextshell.sftp.download({
              connectionId: connection.id,
              remotePath: entry.path,
              localPath: targetPath,
              taskId: task.id
            });
            markSuccess(task.id);
            successCount += 1;
          } catch (error) {
            const reason = formatErrorMessage(error, "下载失败");
            markFailed(task.id, reason);
            message.error(`下载失败：${entry.name}（${reason}）`);
          }
        }, 5);

        if (successCount > 0) {
          message.success(`下载完成 (${successCount}/${entries.length}) → ${localBasePath}`);
        }
      } catch (error) {
        message.error(`下载失败：${formatErrorMessage(error, "请稍后重试")}`);
      } finally {
        setBusy(false);
      }
    },
    [
      connection,
      enqueueTask,
      markFailed,
      markSuccess,
      preferences.transfer.downloadDefaultDir,
      updatePreferences
    ]
  );

  const handlePackedDownload = useCallback(
    async (
      entries: RemoteFileEntry[],
      targetBaseDir?: string,
      persistDefaultDir = false
    ): Promise<void> => {
      if (!connection || entries.length === 0) return;
      const localBasePath = (targetBaseDir || preferences.transfer.downloadDefaultDir).trim();
      if (!localBasePath) return;

      if (persistDefaultDir && localBasePath !== preferences.transfer.downloadDefaultDir) {
        void updatePreferences({
          transfer: {
            downloadDefaultDir: localBasePath
          }
        });
      }

      const normalizedCurrentPath = normalizeRemotePath(pathName);
      const pathSegment = normalizedCurrentPath === "/"
        ? "root"
        : normalizedCurrentPath.split("/").filter(Boolean).at(-1) ?? "bundle";
      const archiveBase = entries.length === 1
        ? entries[0]!.name
        : `${pathSegment}-bundle-${Date.now()}`;
      const archiveName = ensureTarGzName(archiveBase);
      const localArchivePath = joinLocalPath(localBasePath, archiveName);
      const remoteArchivePath = normalizeRemotePath(joinRemotePath(pathName, archiveName));

      setBusy(true);
      const task = enqueueTask({
        direction: "download",
        connectionId: connection.id,
        localPath: localArchivePath,
        remotePath: remoteArchivePath,
        retryable: false
      });

      try {
        await window.nextshell.sftp.downloadPacked({
          connectionId: connection.id,
          remoteDir: normalizedCurrentPath,
          entryNames: entries.map((entry) => entry.name),
          localDir: localBasePath,
          archiveName,
          taskId: task.id
        });
        markSuccess(task.id);
        message.success(`打包下载完成 → ${localArchivePath}`);
      } catch (error) {
        const reason = formatErrorMessage(error, "打包下载失败");
        markFailed(task.id, reason);
        message.error(`打包下载失败：${reason}`);
      } finally {
        setBusy(false);
      }
    },
    [
      connection,
      enqueueTask,
      markFailed,
      markSuccess,
      pathName,
      preferences.transfer.downloadDefaultDir,
      updatePreferences
    ]
  );

  // ── Create directory / file ─────────────────────────────
  const handleCreateDirectory = async (): Promise<void> => {
    if (!connection) return;
    const folderName = await promptModal(modal, "新建目录名称");
    if (!folderName) return;
    const targetPath = joinRemotePath(pathName, folderName);
    try {
      setBusy(true);
      await window.nextshell.sftp.mkdir({ connectionId: connection.id, path: targetPath });
      message.success("目录已创建");
      await loadFiles();
    } catch (error) {
      message.error(`创建目录失败：${formatErrorMessage(error, "请检查目录名称")}`);
    } finally {
      setBusy(false);
    }
  };

  const handleCreateFile = async (): Promise<void> => {
    if (!connection) return;
    const fileName = await promptModal(modal, "新建文件名称");
    if (!fileName) return;
    const targetPath = joinRemotePath(pathName, fileName);
    setBusy(true);
    const { ok } = await execSSH(`touch ${shellEscape(targetPath)}`);
    setBusy(false);
    if (ok) {
      message.success("文件已创建");
      await loadFiles();
    }
  };

  // ── Rename ──────────────────────────────────────────────
  const handleRename = async (entry?: RemoteFileEntry): Promise<void> => {
    const target = entry ?? singleSelected;
    if (!connection || !target) return;
    const toPath = await promptModal(modal, "重命名为", undefined, target.path);
    if (!toPath || toPath === target.path) return;
    const normalized = normalizeRemotePath(toPath);
    setBusy(true);
    try {
      await window.nextshell.sftp.rename({
        connectionId: connection.id,
        fromPath: target.path,
        toPath: normalized
      });
      message.success("重命名成功");
      await loadFiles();
    } catch (error) {
      message.error(`重命名失败：${formatErrorMessage(error, "请稍后重试")}`);
    } finally {
      setBusy(false);
    }
  };

  // ── Delete via SFTP ─────────────────────────────────────
  const handleDelete = (targets: RemoteFileEntry[] = selectedEntries): void => {
    if (!connection || targets.length === 0) return;
    Modal.confirm({
      title: "删除远端文件",
      content:
        targets.length === 1
          ? `确认删除 ${targets[0]?.path} ?`
          : `确认删除选中的 ${targets.length} 项?`,
      okButtonProps: { danger: true },
      onOk: async () => {
        // Optimistic: remove from UI immediately
        const prevFiles = [...files];
        const targetPaths = new Set(targets.map((t) => t.path));
        setFiles((prev) => prev.filter((f) => !targetPaths.has(f.path)));

        try {
          setBusy(true);
          // Concurrent delete with limit of 5
          await pMap(targets, async (entry) => {
            await window.nextshell.sftp.remove({
              connectionId: connection.id,
              path: entry.path,
              type: entry.type
            });
          }, 5);
          message.success("删除成功");
          await loadFiles();
        } catch (error) {
          message.error(`删除失败：${formatErrorMessage(error, "请稍后重试")}`);
          // Rollback on failure
          setFiles(prevFiles);
        } finally {
          setBusy(false);
        }
      }
    });
  };

  // ── Quick delete via SSH rm -rf ─────────────────────────
  const handleQuickDelete = (targets: RemoteFileEntry[]): void => {
    if (!connection || targets.length === 0) return;
    Modal.confirm({
      title: "快速删除（rm 命令）",
      content: (
        <div>
          <p>将在远端执行 <code>rm -rf</code> 命令，<strong>不可撤销</strong>！</p>
          <p>
            {targets.length === 1
              ? targets[0]?.path
              : `${targets.length} 个文件/目录`}
          </p>
        </div>
      ),
      okButtonProps: { danger: true },
      okText: "强制删除",
      onOk: async () => {
        const paths = targets.map((e) => shellEscape(e.path)).join(" ");
        setBusy(true);
        const { ok } = await execSSH(`rm -rf ${paths}`);
        setBusy(false);
        if (ok) {
          message.success("已删除");
          await loadFiles();
        }
      }
    });
  };

  // ── Copy / Cut / Paste ──────────────────────────────────
  const handleCopy = (entries: RemoteFileEntry[]) => {
    if (!connection) return;
    setClipboard({ mode: "copy", entries, sourceConnectionId: connection.id });
    message.success(`已复制 ${entries.length} 项到剪切板`);
  };

  const handleCut = (entries: RemoteFileEntry[]) => {
    if (!connection) return;
    setClipboard({ mode: "cut", entries, sourceConnectionId: connection.id });
    message.success(`已剪切 ${entries.length} 项到剪切板`);
  };

  const handlePaste = useCallback(async (): Promise<void> => {
    if (!connection || !clipboard) return;
    if (clipboard.sourceConnectionId !== connection.id) {
      message.warning("仅支持在同一连接内粘贴");
      return;
    }

    const destDir = normalizeRemotePath(pathName);
    setBusy(true);

    const ops = clipboard.entries.map((entry) => {
      const destPath = joinRemotePath(destDir, entry.name);
      if (clipboard.mode === "copy") {
        const flag = entry.type === "directory" ? "-r" : "";
        return execSSH(`cp ${flag} ${shellEscape(entry.path)} ${shellEscape(destPath)}`);
      } else {
        return execSSH(`mv ${shellEscape(entry.path)} ${shellEscape(destPath)}`);
      }
    });

    const results = await Promise.all(ops);
    setBusy(false);

    const failed = results.filter((r) => !r.ok).length;
    if (failed === 0) {
      message.success(
        `${clipboard.mode === "copy" ? "复制" : "移动"}完成（${clipboard.entries.length} 项）`
      );
      if (clipboard.mode === "cut") setClipboard(null);
    } else {
      message.warning(`操作完成，${failed} 项失败`);
    }

    await loadFiles();
  }, [clipboard, connection, execSSH, loadFiles, pathName]);

  // ── Copy path to clipboard ──────────────────────────────
  const handleCopyPath = (entries: RemoteFileEntry[]) => {
    const paths = entries.map((e) => e.path).join("\n");
    void navigator.clipboard.writeText(paths);
    message.success("路径已复制到系统剪切板");
  };

  // ── Remote edit ────────────────────────────────────────
  const doRemoteEdit = async (entry: RemoteFileEntry, editorCmd: string) => {
    if (!connection) return;
    setBusy(true);
    try {
      await window.nextshell.sftp.editOpen({
        connectionId: connection.id,
        remotePath: entry.path,
        editorCommand: editorCmd
      });
      message.success(`已打开远端编辑: ${entry.name}`);
    } catch (err) {
      message.error(`远端编辑失败：${formatErrorMessage(err, "请检查编辑器配置或环境变量")}`);
    } finally {
      setBusy(false);
    }
  };

  const handleRemoteEdit = (entry: RemoteFileEntry) => {
    const editorMode = preferences.remoteEdit.editorMode ?? "builtin";
    if (editorMode === "builtin" && onOpenEditorTab && connection) {
      void onOpenEditorTab(connection.id, entry.path);
      return;
    }
    const editor = preferences.remoteEdit.defaultEditorCommand?.trim() ?? "";
    void doRemoteEdit(entry, editor);
  };

  const handleEditorModalOk = () => {
    const cmd = editorModalValue.trim();
    if (!cmd) return;
    void updatePreferences({
      remoteEdit: {
        defaultEditorCommand: cmd
      }
    });
    setEditorModalOpen(false);
    const pending = pendingEditRef.current;
    pendingEditRef.current = null;
    if (pending) {
      void doRemoteEdit(pending, cmd);
    }
  };

  // ── Context menu trigger ────────────────────────────────
  const handleContextMenu = useCallback(
    (e: React.MouseEvent, row?: RemoteFileEntry) => {
      e.preventDefault();
      e.stopPropagation();

      let targetEntries: RemoteFileEntry[];
      if (row) {
        // If right-clicked row is already selected, use all selected; otherwise use only this row
        targetEntries =
          selectedPaths.includes(row.path) && selectedEntries.length > 0
            ? selectedEntries
            : [row];
      } else {
        targetEntries = [];
      }

      setContextMenu({ x: e.clientX, y: e.clientY, entries: targetEntries });
    },
    [selectedEntries, selectedPaths]
  );

  const handleDownloadTo = async (entries: RemoteFileEntry[]): Promise<void> => {
    if (!entries.length) return;
    try {
      const result = await window.nextshell.dialog.openDirectory({
        title: "选择下载目录",
        defaultPath: preferences.transfer.downloadDefaultDir
      });
      if (result.canceled || !result.filePath) {
        return;
      }
      await handleDownload(entries, result.filePath, false);
    } catch (error) {
      message.error(`打开目录选择器失败：${formatErrorMessage(error, "请稍后重试")}`);
    }
  };

  if (!connection) {
    return <Typography.Text className="text-[var(--t3)]">先选择一个连接再浏览文件。</Typography.Text>;
  }

  if (!connected) {
    return <Typography.Text className="text-[var(--t3)]">当前连接未建立会话，请双击左侧服务器建立 SSH 连接。</Typography.Text>;
  }

  return (
    <div
      className="flex h-full overflow-hidden"
      onContextMenu={(e) => handleContextMenu(e)}
    >
      {/* ── Left: directory tree ── */}
      <aside className="fe-tree-panel">
        <Tree<DirTreeNode>
          treeData={treeData}
          expandedKeys={expandedKeys}
          selectedKeys={[pathName]}
          onExpand={(keys, info) =>
            void handleTreeExpand(
              keys.map(String),
              info as unknown as { node: DirTreeNode; expanded: boolean }
            )
          }
          onSelect={(keys) => {
            const selected = keys[0];
            if (selected) navigate(String(selected));
          }}
          blockNode
          showLine={false}
        />
      </aside>

      {/* ── Right: file list ── */}
      <section className="flex-1 min-w-0 flex flex-col overflow-hidden">
        {/* toolbar */}
        <div className="fe-toolbar">
          <div className="fe-path-area">
            <input
              className="fe-path-input"
              value={pathInput}
              onChange={(e) => setPathInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") navigate(normalizeRemotePath(pathInput));
              }}
              placeholder="输入路径后回车跳转"
              title={pathName}
            />
          </div>
          <div className="fe-actions">
            <Tooltip title="打开设置中心">
              <button className="fe-icon-btn" onClick={onOpenSettings} aria-label="打开设置中心">
                <i className="ri-settings-3-line" aria-hidden="true" />
              </button>
            </Tooltip>
            <span className="w-px h-4 bg-[var(--border)] mx-[3px] shrink-0" />
            <Tooltip title="跟随终端目录（3 秒防抖）">
              <span className="inline-flex">
                <button
                  className={`fe-icon-btn${followCwd ? " active" : ""}`}
                  aria-label="跟随终端目录"
                  onClick={() => {
                    setFollowCwd((v) => {
                      const next = !v;
                      if (next) {
                        followCwdLastRef.current = null;
                        message.info({ content: "已启用跟随终端目录", duration: 2 });
                      } else {
                        message.info({ content: "已关闭跟随终端目录", duration: 2 });
                      }
                      return next;
                    });
                  }}
                  disabled={!connection || !connected}
                >
                  <i className="ri-terminal-line" aria-hidden="true" />
                </button>
              </span>
            </Tooltip>
            <Tooltip title="刷新">
              <button className="fe-icon-btn" onClick={() => void loadFiles()} disabled={busy} aria-label="刷新"><i className="ri-refresh-line" aria-hidden="true" /></button>
            </Tooltip>
            <Tooltip title="后退">
              <button className="fe-icon-btn" onClick={goBack} disabled={historyIndex <= 0} aria-label="后退"><i className="ri-arrow-left-s-line" aria-hidden="true" /></button>
            </Tooltip>
            <Tooltip title="前进">
              <button className="fe-icon-btn" onClick={goForward} disabled={historyIndex >= pathHistory.length - 1} aria-label="前进"><i className="ri-arrow-right-s-line" aria-hidden="true" /></button>
            </Tooltip>
            <Tooltip title="上级目录">
              <button className="fe-icon-btn" onClick={toParentPath} disabled={pathName === "/" || busy} aria-label="上级目录"><i className="ri-arrow-up-s-line" aria-hidden="true" /></button>
            </Tooltip>
            <span className="w-px h-4 bg-[var(--border)] mx-[3px] shrink-0" />
            <Tooltip title="上传">
              <button className="fe-icon-btn" onClick={() => void handleUpload()} disabled={busy} aria-label="上传"><i className="ri-upload-2-line" aria-hidden="true" /></button>
            </Tooltip>
            <Tooltip title="打包上传（自动解包）">
              <button className="fe-icon-btn" onClick={() => void handlePackedUpload()} disabled={busy} aria-label="打包上传"><i className="ri-inbox-archive-line" aria-hidden="true" /></button>
            </Tooltip>
            <Tooltip title={`下载到默认目录（${preferences.transfer.downloadDefaultDir}）`}>
              <span className="inline-flex">
                <button
                  className="fe-icon-btn"
                  aria-label="下载到默认目录"
                  onClick={() => void handleDownload(selectedEntries)}
                  disabled={selectedEntries.length === 0 || busy}
                >
                  <i className="ri-download-2-line" aria-hidden="true" />
                </button>
              </span>
            </Tooltip>
            <Tooltip title={`打包下载到默认目录（${preferences.transfer.downloadDefaultDir}）`}>
              <span className="inline-flex">
                <button
                  className="fe-icon-btn"
                  aria-label="打包下载到默认目录"
                  onClick={() => void handlePackedDownload(selectedEntries)}
                  disabled={selectedEntries.length === 0 || busy}
                >
                  <i className="ri-file-zip-line" aria-hidden="true" />
                </button>
              </span>
            </Tooltip>
            <Tooltip title="下载到...">
              <span className="inline-flex">
                <button
                  className="fe-icon-btn"
                  aria-label="下载到..."
                  onClick={() => void handleDownloadTo(selectedEntries)}
                  disabled={selectedEntries.length === 0 || busy}
                >
                  <i className="ri-folder-open-line" aria-hidden="true" />
                </button>
              </span>
            </Tooltip>
            <span className="w-px h-4 bg-[var(--border)] mx-[3px] shrink-0" />
            <Tooltip title="新建目录">
              <button className="fe-icon-btn" onClick={() => void handleCreateDirectory()} disabled={busy} aria-label="新建目录"><i className="ri-folder-add-line" aria-hidden="true" /></button>
            </Tooltip>
            <Tooltip title="重命名">
              <span className="inline-flex">
                <button className="fe-icon-btn" onClick={() => void handleRename()} disabled={!singleSelected || busy} aria-label="重命名"><i className="ri-edit-line" aria-hidden="true" /></button>
              </span>
            </Tooltip>
            <Tooltip title="删除">
              <span className="inline-flex">
                <button className="fe-icon-btn danger" onClick={() => handleDelete()} disabled={selectedEntries.length === 0 || busy} aria-label="删除"><i className="ri-delete-bin-6-line" aria-hidden="true" /></button>
              </span>
            </Tooltip>
            {clipboard && (
              <>
                <span className="w-px h-4 bg-[var(--border)] mx-[3px] shrink-0" />
                <Tooltip title={`粘贴（${clipboard.mode === "copy" ? "复制" : "移动"} ${clipboard.entries.length} 项）`}>
                  <span className="inline-flex">
                    <button
                      className="fe-icon-btn"
                      aria-label="粘贴"
                      onClick={() => void handlePaste()}
                      disabled={busy}
                    >
                      <i className="ri-clipboard-line" aria-hidden="true" />
                    </button>
                  </span>
                </Tooltip>
                <Tooltip title="清空剪切板">
                  <button className="fe-icon-btn" onClick={() => setClipboard(null)} aria-label="清空剪切板">
                    <i className="ri-close-line" aria-hidden="true" />
                  </button>
                </Tooltip>
              </>
            )}
          </div>
        </div>

        {/* clipboard status bar */}
        {clipboard && (
          <div className="fe-clipboard-bar">
            <span className="fe-clipboard-icon">
              <i
                className={clipboard.mode === "copy" ? "ri-file-copy-line" : "ri-scissors-cut-line"}
                aria-hidden="true"
              />
            </span>
            <span>
              已{clipboard.mode === "copy" ? "复制" : "剪切"} {clipboard.entries.length} 项——在目标目录右键粘贴或点击工具栏粘贴
            </span>
            <button className="fe-clipboard-clear" onClick={() => setClipboard(null)}>清空</button>
          </div>
        )}

        {/* file table */}
        <div
          className="fe-table-wrap flex-1 min-h-0 overflow-auto"
          onContextMenu={(e) => e.stopPropagation()}
        >
          <Table
            size="small"
            pagination={false}
            rowKey="path"
            columns={columns}
            dataSource={files}
            loading={busy}
            scroll={{ y: "100%" }}
            rowSelection={{
              selectedRowKeys: selectedPaths,
              onChange: (keys) => {
                setSelectedPaths(keys.map((key) => String(key)));
              }
            }}
            onRow={(row) => ({
              onDoubleClick: () => {
                if (row.type === "directory") navigate(row.path);
                else handleRemoteEdit(row);
              },
              onContextMenu: (e) => handleContextMenu(e, row)
            })}
          />
        </div>
      </section>

      {/* ── Context menu ── */}
      {contextMenu && connection && (
        <ContextMenu
          state={contextMenu}
          clipboard={clipboard}
          currentPath={pathName}
          connectionId={connection.id}
          onClose={() => setContextMenu(null)}
          onRefresh={() => void loadFiles()}
          onDownload={(entries) => void handleDownload(entries)}
          onPackedDownload={(entries) => void handlePackedDownload(entries)}
          onUpload={() => void handleUpload()}
          onPackedUpload={() => void handlePackedUpload()}
          onCopyPath={handleCopyPath}
          onCopy={handleCopy}
          onCut={handleCut}
          onPaste={() => void handlePaste()}
          onNewFolder={() => void handleCreateDirectory()}
          onNewFile={() => void handleCreateFile()}
          onRename={(entry) => void handleRename(entry)}
          onDelete={handleDelete}
          onQuickDelete={handleQuickDelete}
          onRemoteEdit={handleRemoteEdit}
        />
      )}

      {/* ── Editor selection modal ── */}
      <Modal
        title="选择编辑器"
        open={editorModalOpen}
        onOk={handleEditorModalOk}
        onCancel={() => {
          setEditorModalOpen(false);
          pendingEditRef.current = null;
        }}
        okText="确认"
        cancelText="取消"
        width={420}
      >
        <div className="fe-editor-modal-body">
          <Typography.Text type="secondary">
            选择用于编辑远程文件的本地编辑器，文件保存后将自动同步回服务器。
          </Typography.Text>
          <div className="fe-editor-preset-list">
            {EDITOR_PRESETS.map((p) => (
              <button
                key={p.value}
                className={`fe-editor-preset${editorModalValue === p.value ? " active" : ""}`}
                onClick={() => setEditorModalValue(p.value)}
              >
                {p.label}
              </button>
            ))}
          </div>
          <input
            className="fe-path-input"
            value={editorModalValue}
            onChange={(e) => setEditorModalValue(e.target.value)}
            placeholder="输入编辑器命令，如 code、cursor、vim"
            onKeyDown={(e) => {
              if (e.key === "Enter") handleEditorModalOk();
            }}
          />
        </div>
      </Modal>
    </div>
  );
};
