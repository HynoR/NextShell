import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Table, Typography, message } from "antd";
import type { ColumnsType } from "antd/es/table";
import type {
  ConnectionProfile,
  SystemFilesystemEntry,
  SystemInfoSnapshot,
  SystemNetworkInterfaceTotal
} from "@nextshell/core";
import { TableSkeleton } from "./LoadingSkeletons";
import { formatDiskSize } from "../utils/diskUsage";
import { formatErrorMessage } from "../utils/errorMessage";
import { buildSystemDiskCacheKey } from "../utils/systemDiskSharedCache";

interface SystemStaticInfoPaneProps {
  connection?: ConnectionProfile;
  connected: boolean;
  active: boolean;
  connectedTerminalSessionId?: string;
  onOpenSettings?: () => void;
}

interface SystemInfoCacheSnapshot {
  snapshot?: SystemInfoSnapshot;
  lastUpdatedAt?: string;
  errorText?: string;
}

type SystemInfoGroup = "basic" | "network" | "disk";

const AUTO_REFRESH_INTERVAL_MS = 5 * 60 * 1000;

const formatUptimeSeconds = (seconds: number): string => {
  const total = Math.max(0, Math.floor(seconds));
  const days = Math.floor(total / 86400);
  const hours = Math.floor((total % 86400) / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  if (days > 0) return `${days} 天 ${hours} 小时`;
  if (hours > 0) return `${hours} 小时 ${minutes} 分`;
  return `${minutes} 分钟`;
};

const formatRefreshTime = (iso?: string): string => {
  if (!iso) {
    return "--:--:--";
  }
  return new Date(iso).toLocaleTimeString();
};

const formatBytes = (bytes: number): string => {
  const units = ["B", "KB", "MB", "GB", "TB", "PB"];
  let value = Math.max(0, bytes);
  let unitIndex = 0;

  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024;
    unitIndex += 1;
  }

  const precision = value >= 100 || unitIndex === 0 ? 0 : value >= 10 ? 1 : 2;
  return `${value.toFixed(precision)} ${units[unitIndex]}`;
};

const formatCpuFrequency = (frequencyMhz?: number): string => {
  if (frequencyMhz === undefined || frequencyMhz <= 0) {
    return "-";
  }
  return `${frequencyMhz.toFixed(3)} MHz`;
};

const formatBogoMips = (bogoMips?: number): string => {
  if (bogoMips === undefined || bogoMips <= 0) {
    return "-";
  }
  return bogoMips.toFixed(2);
};

export const SystemStaticInfoPane = ({
  connection,
  connected,
  active,
  connectedTerminalSessionId,
  onOpenSettings
}: SystemStaticInfoPaneProps) => {
  const connectionId = connection?.id;
  const monitorEnabled = Boolean(connection?.monitorSession);
  const cacheKey = buildSystemDiskCacheKey(connectionId, connectedTerminalSessionId);

  const [snapshot, setSnapshot] = useState<SystemInfoSnapshot>();
  const [activeGroup, setActiveGroup] = useState<SystemInfoGroup>("basic");
  const [loading, setLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [lastUpdatedAt, setLastUpdatedAt] = useState<string>();
  const [errorText, setErrorText] = useState<string>();
  const requestIdRef = useRef(0);
  const cacheRef = useRef<Record<string, SystemInfoCacheSnapshot>>({});

  useEffect(() => {
    if (!cacheKey) {
      setSnapshot(undefined);
      setLastUpdatedAt(undefined);
      setErrorText(undefined);
      return;
    }

    const cached = cacheRef.current[cacheKey];
    setSnapshot(cached?.snapshot);
    setLastUpdatedAt(cached?.lastUpdatedAt);
    setErrorText(cached?.errorText);
  }, [cacheKey]);

  useEffect(() => {
    setActiveGroup("basic");
  }, [cacheKey]);

  const fetchSystemInfo = useCallback(
    async (silent: boolean, force: boolean) => {
      if (!connectionId || !cacheKey) {
        return;
      }

      const cached = cacheRef.current[cacheKey];
      if (!force && cached?.snapshot) {
        setSnapshot(cached.snapshot);
        setLastUpdatedAt(cached.lastUpdatedAt);
        setErrorText(cached.errorText);
        return;
      }

      const requestId = requestIdRef.current + 1;
      requestIdRef.current = requestId;
      if (silent) {
        setRefreshing(true);
      } else {
        setLoading(true);
      }

      try {
        const nextSnapshot = await window.nextshell.monitor.getSystemInfoSnapshot({ connectionId });
        if (requestIdRef.current !== requestId) {
          return;
        }

        const updatedAt = nextSnapshot.capturedAt || new Date().toISOString();
        setSnapshot(nextSnapshot);
        setLastUpdatedAt(updatedAt);
        setErrorText(undefined);
        cacheRef.current[cacheKey] = {
          snapshot: nextSnapshot,
          lastUpdatedAt: updatedAt,
          errorText: undefined
        };
      } catch (error) {
        if (requestIdRef.current !== requestId) {
          return;
        }

        const reason = formatErrorMessage(error, "读取系统信息失败");
        setErrorText(reason);
        const previous = cacheRef.current[cacheKey];
        cacheRef.current[cacheKey] = {
          snapshot: previous?.snapshot,
          lastUpdatedAt: previous?.lastUpdatedAt,
          errorText: reason
        };
        if (!silent) {
          message.error(reason);
        }
      } finally {
        if (requestIdRef.current === requestId) {
          setLoading(false);
          setRefreshing(false);
        }
      }
    },
    [cacheKey, connectionId]
  );

  useEffect(() => {
    if (!active || !connectionId || !monitorEnabled || !connected || !connectedTerminalSessionId || !cacheKey) {
      requestIdRef.current += 1;
      setLoading(false);
      setRefreshing(false);
      return;
    }

    const lastUpdatedTime = lastUpdatedAt ? Date.parse(lastUpdatedAt) : Number.NaN;
    const elapsedMs = Number.isFinite(lastUpdatedTime) ? Date.now() - lastUpdatedTime : Number.POSITIVE_INFINITY;
    if (!cacheRef.current[cacheKey] || elapsedMs >= AUTO_REFRESH_INTERVAL_MS) {
      void fetchSystemInfo(false, true);
    }

    const delayMs = elapsedMs >= AUTO_REFRESH_INTERVAL_MS
      ? AUTO_REFRESH_INTERVAL_MS
      : Math.max(1, AUTO_REFRESH_INTERVAL_MS - elapsedMs);
    let intervalId: number | undefined;
    const timeoutId = window.setTimeout(() => {
      void fetchSystemInfo(true, true);
      intervalId = window.setInterval(() => {
        void fetchSystemInfo(true, true);
      }, AUTO_REFRESH_INTERVAL_MS);
    }, delayMs);

    return () => {
      window.clearTimeout(timeoutId);
      if (intervalId !== undefined) {
        window.clearInterval(intervalId);
      }
      requestIdRef.current += 1;
      setRefreshing(false);
    }
  }, [
    active,
    cacheKey,
    connected,
    connectedTerminalSessionId,
    connectionId,
    fetchSystemInfo,
    lastUpdatedAt,
    monitorEnabled
  ]);

  const filesystemColumns = useMemo<ColumnsType<SystemFilesystemEntry>>(
    () => [
      {
        title: "挂载点",
        dataIndex: "mountPoint",
        width: 220,
        ellipsis: true,
        render: (mountPoint: string) => <span className="disk-cell-path">{mountPoint}</span>
      },
      {
        title: "文件系统",
        dataIndex: "filesystem",
        width: 220,
        ellipsis: true,
        render: (filesystem: string) => <span className="disk-cell-path">{filesystem}</span>
      },
      {
        title: "大小",
        dataIndex: "totalKb",
        width: 110,
        render: (totalKb: number) => formatDiskSize(totalKb)
      },
      {
        title: "已用",
        dataIndex: "usedKb",
        width: 110,
        render: (usedKb: number) => formatDiskSize(usedKb)
      },
      {
        title: "可用",
        dataIndex: "availableKb",
        width: 110,
        render: (availableKb: number) => formatDiskSize(availableKb)
      }
    ],
    []
  );

  const networkColumns = useMemo<ColumnsType<SystemNetworkInterfaceTotal>>(
    () => [
      {
        title: "网卡",
        dataIndex: "name",
        width: 160,
        render: (name: string) => <span className="disk-cell-path">{name}</span>
      },
      {
        title: "累计发送",
        dataIndex: "txBytes",
        width: 160,
        render: (txBytes: number) => formatBytes(txBytes)
      },
      {
        title: "累计接收",
        dataIndex: "rxBytes",
        width: 160,
        render: (rxBytes: number) => formatBytes(rxBytes)
      }
    ],
    []
  );

  if (!connection) {
    return <Typography.Text className="text-[var(--t3)]">先选择一个连接再查看系统信息。</Typography.Text>;
  }

  if (!monitorEnabled) {
    return (
      <div className="disk-gate">
        <Typography.Text className="text-[var(--t2)]">
          当前连接未启用监控会话，请在连接设置中开启后再查看系统信息。
        </Typography.Text>
        <button type="button" className="disk-gate-btn" onClick={onOpenSettings}>
          <i className="ri-settings-3-line" aria-hidden="true" />
          前往设置
        </button>
      </div>
    );
  }

  if (!connected || !connectedTerminalSessionId) {
    return (
      <Typography.Text className="text-[var(--t3)]">
        当前连接未建立会话，请双击左侧服务器建立 SSH 连接后查看系统信息。
      </Typography.Text>
    );
  }

  return (
    <div className="disk-pane">
      <div className="disk-toolbar">
        <button
          type="button"
          className="disk-refresh-btn"
          onClick={() => void fetchSystemInfo(false, true)}
          disabled={loading || refreshing}
          title="刷新系统信息"
        >
          <i className={refreshing ? "ri-loader-4-line disk-refresh-spin" : "ri-refresh-line"} aria-hidden="true" />
        </button>
        <span className="disk-summary">{snapshot?.networkInterfaces.length ?? 0} 个网卡</span>
        <span className="disk-summary">{snapshot?.filesystems.length ?? 0} 个挂载点</span>
        <span className="disk-summary">快照时间 {formatRefreshTime(lastUpdatedAt)}</span>
      </div>

      {errorText ? <div className="disk-error">{errorText}</div> : null}

      <div className="system-static-card">
        {snapshot ? (
          <>
            <div className="system-static-group-switch">
              <button
                type="button"
                className={`system-static-group-btn ${activeGroup === "basic" ? "active" : ""}`}
                onClick={() => setActiveGroup("basic")}
              >
                基本信息
              </button>
              <button
                type="button"
                className={`system-static-group-btn ${activeGroup === "network" ? "active" : ""}`}
                onClick={() => setActiveGroup("network")}
              >
                网卡信息
              </button>
              <button
                type="button"
                className={`system-static-group-btn ${activeGroup === "disk" ? "active" : ""}`}
                onClick={() => setActiveGroup("disk")}
              >
                磁盘信息
              </button>
            </div>

            <div className="system-static-group-content">
              {activeGroup === "basic" ? (
                <div className="system-static-basic-grid">
                  <div className="rounded-md border border-[var(--border-dim)] bg-[var(--bg-elevated)] px-3 py-2">
                    <div className="text-[var(--t3)]">运行时长</div>
                    <div className="font-medium text-[var(--t1)]">{formatUptimeSeconds(snapshot.uptimeSeconds)}</div>
                  </div>
                  <div className="rounded-md border border-[var(--border-dim)] bg-[var(--bg-elevated)] px-3 py-2">
                    <div className="text-[var(--t3)]">主机名</div>
                    <div className="font-medium text-[var(--t1)]">{snapshot.hostname}</div>
                  </div>
                  <div className="rounded-md border border-[var(--border-dim)] bg-[var(--bg-elevated)] px-3 py-2">
                    <div className="text-[var(--t3)]">操作系统</div>
                    <div className="font-medium text-[var(--t1)]">{snapshot.osName}</div>
                  </div>
                  <div className="rounded-md border border-[var(--border-dim)] bg-[var(--bg-elevated)] px-3 py-2">
                    <div className="text-[var(--t3)]">内核</div>
                    <div className="font-medium text-[var(--t1)]">
                      {snapshot.kernelName} {snapshot.kernelVersion}
                    </div>
                  </div>
                  <div className="rounded-md border border-[var(--border-dim)] bg-[var(--bg-elevated)] px-3 py-2">
                    <div className="text-[var(--t3)]">架构</div>
                    <div className="font-medium text-[var(--t1)]">{snapshot.architecture}</div>
                  </div>
                  <div className="rounded-md border border-[var(--border-dim)] bg-[var(--bg-elevated)] px-3 py-2">
                    <div className="text-[var(--t3)]">CPU 型号</div>
                    <div className="font-medium text-[var(--t1)]">{snapshot.cpu.modelName}</div>
                  </div>
                  <div className="rounded-md border border-[var(--border-dim)] bg-[var(--bg-elevated)] px-3 py-2">
                    <div className="text-[var(--t3)]">核心数 / 频率</div>
                    <div className="font-medium text-[var(--t1)]">
                      {snapshot.cpu.coreCount} / {formatCpuFrequency(snapshot.cpu.frequencyMhz)}
                    </div>
                  </div>
                  <div className="rounded-md border border-[var(--border-dim)] bg-[var(--bg-elevated)] px-3 py-2">
                    <div className="text-[var(--t3)]">缓存 / BogoMIPS</div>
                    <div className="font-medium text-[var(--t1)]">
                      {snapshot.cpu.cacheSize ?? "-"} / {formatBogoMips(snapshot.cpu.bogoMips)}
                    </div>
                  </div>
                  <div className="rounded-md border border-[var(--border-dim)] bg-[var(--bg-elevated)] px-3 py-2">
                    <div className="text-[var(--t3)]">内存总量</div>
                    <div className="font-medium text-[var(--t1)]">{formatDiskSize(snapshot.memoryTotalKb)}</div>
                  </div>
                  <div className="rounded-md border border-[var(--border-dim)] bg-[var(--bg-elevated)] px-3 py-2">
                    <div className="text-[var(--t3)]">交换总量</div>
                    <div className="font-medium text-[var(--t1)]">{formatDiskSize(snapshot.swapTotalKb)}</div>
                  </div>
                </div>
              ) : null}

              {activeGroup === "network" ? (
                <div className="system-static-section">
                  <div className="text-[12px] text-[var(--t2)] mb-1">网络接口累计流量</div>
                  <Table<SystemNetworkInterfaceTotal>
                    rowKey={(row) => row.name}
                    columns={networkColumns}
                    dataSource={snapshot.networkInterfaces}
                    size="small"
                    pagination={false}
                    className="disk-table"
                    scroll={{ y: "calc(100vh - 420px)" }}
                    locale={{ emptyText: "暂无网卡数据" }}
                  />
                </div>
              ) : null}

              {activeGroup === "disk" ? (
                <div className="system-static-section">
                  <div className="text-[12px] text-[var(--t2)] mb-1">文件系统挂载</div>
                  <Table<SystemFilesystemEntry>
                    rowKey={(row) => `${row.mountPoint}-${row.filesystem}`}
                    columns={filesystemColumns}
                    dataSource={snapshot.filesystems}
                    size="small"
                    pagination={false}
                    className="disk-table"
                    scroll={{ y: "calc(100vh - 420px)" }}
                    locale={{ emptyText: "暂无文件系统数据" }}
                  />
                </div>
              ) : null}
            </div>
          </>
        ) : (
          <div className="system-static-empty">
            <TableSkeleton rows={6} columns={3} />
          </div>
        )}
      </div>
    </div>
  );
};
