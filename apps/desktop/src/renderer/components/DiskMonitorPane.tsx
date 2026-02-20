import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Table, Typography, message } from "antd";
import type { ColumnsType } from "antd/es/table";
import type { CommandExecutionResult, ConnectionProfile } from "@nextshell/core";
import { TableSkeleton } from "./LoadingSkeletons";
import { formatDiskSize, parseDfOutput, type DiskUsageEntry } from "../utils/diskUsage";

interface DiskMonitorPaneProps {
  connection?: ConnectionProfile;
  connected: boolean;
  onOpenSettings?: () => void;
}

const DF_COMMAND = "export LANG=C LC_ALL=C; (df -kP || df -k || df) 2>/dev/null";
const REFRESH_INTERVAL_MS = 10000;

const formatRefreshTime = (iso?: string): string => {
  if (!iso) {
    return "--:--:--";
  }
  return new Date(iso).toLocaleTimeString();
};

const formatPercent = (value: number): string => `${Math.round(value)}%`;

const toFailureReason = (result: CommandExecutionResult): string => {
  if (result.stderr.trim()) {
    return result.stderr.trim();
  }
  return `命令执行失败，退出码 ${result.exitCode}`;
};

export const DiskMonitorPane = ({ connection, connected, onOpenSettings }: DiskMonitorPaneProps) => {
  const connectionId = connection?.id;
  const monitorEnabled = Boolean(connection?.monitorSession);

  const [rows, setRows] = useState<DiskUsageEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [lastUpdatedAt, setLastUpdatedAt] = useState<string>();
  const [errorText, setErrorText] = useState<string>();
  const requestIdRef = useRef(0);

  const fetchDiskData = useCallback(
    async (silent: boolean) => {
      if (!connectionId) {
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
        const result = await window.nextshell.command.exec({
          connectionId,
          command: DF_COMMAND
        });

        if (requestIdRef.current !== requestId) {
          return;
        }

        if (result.exitCode !== 0) {
          throw new Error(toFailureReason(result));
        }

        const parsed = parseDfOutput(result.stdout);
        setRows(parsed);
        setLastUpdatedAt(new Date().toISOString());
        setErrorText(undefined);
      } catch (error) {
        if (requestIdRef.current !== requestId) {
          return;
        }

        const reason = error instanceof Error ? error.message : "读取磁盘信息失败";
        setErrorText(reason);
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
    [connectionId]
  );

  useEffect(() => {
    if (!connectionId || !monitorEnabled || !connected) {
      requestIdRef.current += 1;
      setRows([]);
      setLoading(false);
      setRefreshing(false);
      setErrorText(undefined);
      setLastUpdatedAt(undefined);
      return;
    }

    void fetchDiskData(false);
    const timer = window.setInterval(() => {
      void fetchDiskData(true);
    }, REFRESH_INTERVAL_MS);

    return () => {
      requestIdRef.current += 1;
      window.clearInterval(timer);
    };
  }, [connected, connectionId, fetchDiskData, monitorEnabled]);

  const columns = useMemo<ColumnsType<DiskUsageEntry>>(
    () => [
      {
        title: "路径",
        dataIndex: "mountPoint",
        width: 280,
        ellipsis: true,
        render: (mountPoint: string) => <span className="disk-cell-path">{mountPoint}</span>
      },
      {
        title: "可用/大小",
        key: "available",
        width: 156,
        render: (_: unknown, row: DiskUsageEntry) => (
          <span className="disk-cell-size">
            <span>{formatDiskSize(row.availableKb)}</span>
            <span className="disk-cell-sep">/</span>
            <span>{formatDiskSize(row.totalKb)}</span>
          </span>
        )
      },
      {
        title: "使用率",
        dataIndex: "usedPercent",
        width: 90,
        render: (usedPercent: number) => (
          <span
            className={
              usedPercent >= 90 ? "disk-pct-hot" : usedPercent >= 75 ? "disk-pct-warm" : undefined
            }
          >
            {formatPercent(usedPercent)}
          </span>
        )
      },
      {
        title: "文件系统",
        dataIndex: "filesystem",
        ellipsis: true,
        render: (filesystem: string) => <span className="disk-cell-path">{filesystem}</span>
      }
    ],
    []
  );

  if (!connection) {
    return <Typography.Text className="text-[var(--t3)]">先选择一个连接再查看磁盘。</Typography.Text>;
  }

  if (!monitorEnabled) {
    return (
      <div className="disk-gate">
        <Typography.Text className="text-[var(--t2)]">
          当前连接未启用 Monitor Session，请在连接设置中开启后再查看磁盘。
        </Typography.Text>
        <button type="button" className="disk-gate-btn" onClick={onOpenSettings}>
          <i className="ri-settings-3-line" aria-hidden="true" />
          前往设置
        </button>
      </div>
    );
  }

  if (!connected) {
    return (
      <Typography.Text className="text-[var(--t3)]">
        当前连接未建立会话，请双击左侧服务器建立 SSH 连接后查看磁盘。
      </Typography.Text>
    );
  }

  return (
    <div className="disk-pane">
      <div className="disk-toolbar">
        <button
          type="button"
          className="disk-refresh-btn"
          onClick={() => void fetchDiskData(false)}
          disabled={loading || refreshing}
          title="刷新磁盘信息"
        >
          <i className={refreshing ? "ri-loader-4-line disk-refresh-spin" : "ri-refresh-line"} aria-hidden="true" />
        </button>
        <span className="disk-summary">{rows.length} 个挂载点</span>
        <span className="disk-summary">更新时间 {formatRefreshTime(lastUpdatedAt)}</span>
      </div>
      {errorText ? <div className="disk-error">{errorText}</div> : null}
      <div className="disk-table-wrap">
        <Table<DiskUsageEntry>
          rowKey={(row) => `${row.mountPoint}-${row.filesystem}`}
          columns={columns}
          dataSource={rows}
          size="small"
          pagination={false}
          scroll={{ y: "calc(100vh - 286px)" }}
          className="disk-table"
          locale={{ emptyText: loading ? <TableSkeleton rows={6} columns={3} /> : "暂无磁盘数据" }}
        />
      </div>
    </div>
  );
};
