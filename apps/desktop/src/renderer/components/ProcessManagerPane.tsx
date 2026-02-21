import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Alert, Descriptions, Drawer, Input, Popconfirm, Select, Spin, Table, Tooltip, message } from "antd";
import type { ColumnsType } from "antd/es/table";
import type {
  MonitorProcess,
  ProcessDetailSnapshot,
  ProcessSnapshot,
  SessionDescriptor
} from "@nextshell/core";
import { useWorkspaceStore } from "../store/useWorkspaceStore";
import { TableSkeleton } from "./LoadingSkeletons";

interface ProcessManagerPaneProps {
  session: SessionDescriptor;
}

type SortKey = "cpu" | "memory" | "pid";

export const ProcessManagerPane = ({ session }: ProcessManagerPaneProps) => {
  const { connectionId } = session;
  const processSnapshot = useWorkspaceStore(
    (state) => state.processSnapshots[connectionId]
  );
  const setProcessSnapshot = useWorkspaceStore((state) => state.setProcessSnapshot);

  const [search, setSearch] = useState("");
  const [sortKey, setSortKey] = useState<SortKey>("cpu");
  const [killing, setKilling] = useState<Set<number>>(new Set());
  const [initialLoading, setInitialLoading] = useState(true);
  const [detailOpen, setDetailOpen] = useState(false);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailPid, setDetailPid] = useState<number | undefined>(undefined);
  const [detailError, setDetailError] = useState<string | undefined>(undefined);
  const [detailSnapshot, setDetailSnapshot] = useState<ProcessDetailSnapshot | undefined>(undefined);
  const unsubRef = useRef<(() => void) | undefined>(undefined);

  // Subscribe to process data events + start monitor on mount
  useEffect(() => {
    let disposed = false;

    const unsub = window.nextshell.monitor.onProcessData((snapshot: ProcessSnapshot) => {
      if (!disposed && snapshot.connectionId === connectionId) {
        setProcessSnapshot(connectionId, snapshot);
        setInitialLoading(false);
      }
    });
    unsubRef.current = unsub;

    void window.nextshell.monitor.startProcess({ connectionId }).catch((err: unknown) => {
      if (disposed) return;
      const reason = err instanceof Error ? err.message : "启动进程监控失败";
      message.error(reason);
    });

    return () => {
      disposed = true;
      unsub();
      void window.nextshell.monitor.stopProcess({ connectionId }).catch(() => {});
    };
  }, [connectionId, setProcessSnapshot]);

  const handleKill = useCallback(
    async (pid: number, signal: "SIGTERM" | "SIGKILL") => {
      setKilling((prev) => new Set(prev).add(pid));
      try {
        await window.nextshell.monitor.killProcess({ connectionId, pid, signal });
        message.success(`已发送 ${signal} 到 PID ${pid}`);
      } catch (err) {
        const reason = err instanceof Error ? err.message : "终止进程失败";
        message.error(reason);
      } finally {
        setKilling((prev) => {
          const next = new Set(prev);
          next.delete(pid);
          return next;
        });
      }
    },
    [connectionId]
  );

  const handleCopyRow = useCallback((record: MonitorProcess) => {
    const text = `PID: ${record.pid}  User: ${record.user ?? "-"}  CPU: ${record.cpuPercent}%  MEM: ${record.memoryMb}MB  CMD: ${record.command}`;
    void navigator.clipboard.writeText(text);
    message.success("已复制到剪贴板");
  }, []);

  const handleViewDetail = useCallback(
    async (pid: number) => {
      setDetailOpen(true);
      setDetailLoading(true);
      setDetailPid(pid);
      setDetailError(undefined);
      setDetailSnapshot(undefined);

      try {
        const snapshot = await window.nextshell.monitor.getProcessDetail({ connectionId, pid });
        setDetailSnapshot(snapshot);
      } catch (error) {
        const reason = error instanceof Error ? error.message : "读取进程详情失败";
        setDetailError(reason);
      } finally {
        setDetailLoading(false);
      }
    },
    [connectionId]
  );

  const processes = useMemo(() => {
    if (!processSnapshot) return [];
    let list = [...processSnapshot.processes];

    if (search.trim()) {
      const lower = search.trim().toLowerCase();
      list = list.filter(
        (p) =>
          String(p.pid).includes(lower) ||
          p.command.toLowerCase().includes(lower) ||
          (p.user ?? "").toLowerCase().includes(lower)
      );
    }

    list.sort((a, b) => {
      if (sortKey === "cpu") return b.cpuPercent - a.cpuPercent;
      if (sortKey === "memory") return b.memoryMb - a.memoryMb;
      return a.pid - b.pid;
    });

    return list;
  }, [processSnapshot, search, sortKey]);

  const columns: ColumnsType<MonitorProcess> = [
    {
      title: "PID",
      dataIndex: "pid",
      width: 80,
      sorter: (a, b) => a.pid - b.pid,
      render: (pid: number) => <span className="font-[var(--mono)] text-[11.5px]">{pid}</span>
    },
    {
      title: "用户",
      dataIndex: "user",
      width: 90,
      render: (user: string | undefined) => user ?? "-"
    },
    {
      title: "内存",
      dataIndex: "memoryMb",
      width: 90,
      sorter: (a, b) => a.memoryMb - b.memoryMb,
      render: (mb: number) => {
        if (mb >= 1024) return `${(mb / 1024).toFixed(1)}G`;
        return `${mb.toFixed(1)}M`;
      }
    },
    {
      title: "CPU %",
      dataIndex: "cpuPercent",
      width: 80,
      defaultSortOrder: "descend",
      sorter: (a, b) => a.cpuPercent - b.cpuPercent,
      render: (cpu: number) => (
        <span className={cpu >= 50 ? "cpu-hot" : cpu >= 20 ? "cpu-warm" : ""}>
          {cpu.toFixed(1)}
        </span>
      )
    },
    {
      title: "名称",
      dataIndex: "command",
      width: 220,
      ellipsis: true
    },
    {
      title: "操作",
      key: "action",
      width: 152,
      render: (_: unknown, record: MonitorProcess) => (
        <span className="inline-flex gap-1">
          <Tooltip title="查看详情">
            <button
              type="button"
              className="pm-action-btn"
              onClick={() => void handleViewDetail(record.pid)}
            >
              <i className="ri-information-line" aria-hidden="true" />
            </button>
          </Tooltip>
          <Popconfirm
            title={`确定终止 PID ${record.pid}？`}
            description="发送 SIGTERM 信号，进程将优雅退出"
            onConfirm={() => void handleKill(record.pid, "SIGTERM")}
            okText="终止"
            cancelText="取消"
          >
            <Tooltip title="优雅终止 (SIGTERM)">
              <button
                type="button"
                className="pm-action-btn"
                disabled={killing.has(record.pid)}
              >
                <i className="ri-stop-circle-line" aria-hidden="true" />
              </button>
            </Tooltip>
          </Popconfirm>
          <Popconfirm
            title={`强制杀死 PID ${record.pid}？`}
            description="发送 SIGKILL 信号，进程将被强制终止"
            onConfirm={() => void handleKill(record.pid, "SIGKILL")}
            okText="强杀"
            cancelText="取消"
            okButtonProps={{ danger: true }}
          >
            <Tooltip title="强制终止 (SIGKILL)">
              <button
                type="button"
                className="pm-action-btn danger"
                disabled={killing.has(record.pid)}
              >
                <i className="ri-skull-line" aria-hidden="true" />
              </button>
            </Tooltip>
          </Popconfirm>
          <Tooltip title="复制基础信息">
            <button
              type="button"
              className="pm-action-btn"
              onClick={() => handleCopyRow(record)}
            >
              <i className="ri-clipboard-line" aria-hidden="true" />
            </button>
          </Tooltip>
        </span>
      )
    }
  ];

  const capturedAt = processSnapshot?.capturedAt
    ? new Date(processSnapshot.capturedAt).toLocaleTimeString()
    : "--:--:--";

  return (
    <div className="flex flex-col h-full py-2 px-3 overflow-hidden bg-[var(--bg-surface)]">
      <div className="flex items-center gap-2 mb-2 shrink-0">
        <Input
          className="pm-search"
          placeholder="搜索进程名 / PID / 用户..."
          prefix={<i className="ri-search-line" aria-hidden="true" />}
          allowClear
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <Select
          className="pm-sort-select"
          value={sortKey}
          onChange={setSortKey}
          options={[
            { label: "CPU ↓", value: "cpu" },
            { label: "内存 ↓", value: "memory" },
            { label: "PID ↑", value: "pid" }
          ]}
          style={{ width: 100 }}
        />
        <span className="ml-auto text-[11px] text-[var(--t3)] whitespace-nowrap">
          {processes.length} 进程 · {capturedAt}
        </span>
      </div>
      <div className="flex-1 overflow-hidden">
        <Table<MonitorProcess>
          rowKey="pid"
          columns={columns}
          dataSource={processes}
          size="small"
          pagination={false}
          scroll={{ y: "calc(100vh - 200px)" }}
          className="pm-table"
          locale={{ emptyText: initialLoading ? <TableSkeleton rows={6} columns={4} /> : "暂无进程数据" }}
        />
      </div>
      <Drawer
        title={detailPid ? `进程详情 · PID ${detailPid}` : "进程详情"}
        placement="right"
        size={460}
        open={detailOpen}
        onClose={() => setDetailOpen(false)}
      >
        {detailLoading ? (
          <div className="py-10 flex items-center justify-center">
            <Spin />
          </div>
        ) : detailError ? (
          <Alert type="error" showIcon message={detailError} />
        ) : detailSnapshot ? (
          <div className="flex flex-col gap-3">
            <Descriptions column={1} size="small" bordered>
              <Descriptions.Item label="PID">{detailSnapshot.pid}</Descriptions.Item>
              <Descriptions.Item label="PPID">{detailSnapshot.ppid}</Descriptions.Item>
              <Descriptions.Item label="用户">{detailSnapshot.user}</Descriptions.Item>
              <Descriptions.Item label="状态">{detailSnapshot.state}</Descriptions.Item>
              <Descriptions.Item label="CPU %">{detailSnapshot.cpuPercent.toFixed(2)}</Descriptions.Item>
              <Descriptions.Item label="内存 %">{detailSnapshot.memoryPercent.toFixed(2)}</Descriptions.Item>
              <Descriptions.Item label="RSS (MB)">{detailSnapshot.rssMb.toFixed(2)}</Descriptions.Item>
              <Descriptions.Item label="运行时长">{detailSnapshot.elapsed}</Descriptions.Item>
              <Descriptions.Item label="命令">{detailSnapshot.command}</Descriptions.Item>
              <Descriptions.Item label="采样时间">
                {new Date(detailSnapshot.capturedAt).toLocaleString()}
              </Descriptions.Item>
            </Descriptions>
            <div>
              <div className="text-xs text-[var(--t3)] mb-1">命令行</div>
              <pre className="text-[11.5px] leading-5 whitespace-pre-wrap break-all p-2 rounded border border-[var(--line)] bg-[var(--bg-soft)] font-[var(--mono)] text-[var(--t2)] m-0">
                {detailSnapshot.commandLine}
              </pre>
            </div>
          </div>
        ) : (
          <div className="text-[12px] text-[var(--t3)]">请选择进程查看详情</div>
        )}
      </Drawer>
    </div>
  );
};
