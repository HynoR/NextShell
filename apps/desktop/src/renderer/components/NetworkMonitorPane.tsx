import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Input, Spin, Table, Tooltip, message } from "antd";
import type { ColumnsType } from "antd/es/table";
import type {
  NetworkConnection,
  NetworkListener,
  NetworkSnapshot,
  SessionDescriptor
} from "@nextshell/core";
import { useWorkspaceStore } from "../store/useWorkspaceStore";
import { TableSkeleton } from "./LoadingSkeletons";

interface NetworkMonitorPaneProps {
  session: SessionDescriptor;
}

const DETAIL_POLL_INTERVAL_MS = 5000;

const getListenerKey = (listener: NetworkListener): string => {
  return `${listener.pid}-${listener.port}-${listener.listenIp}`;
};

export const NetworkMonitorPane = ({ session }: NetworkMonitorPaneProps) => {
  const { connectionId } = session;
  const networkSnapshot = useWorkspaceStore((state) => state.networkSnapshots[connectionId]);
  const setNetworkSnapshot = useWorkspaceStore((state) => state.setNetworkSnapshot);

  const [search, setSearch] = useState("");
  const [selectedListenerKey, setSelectedListenerKey] = useState<string>();
  const [portConnections, setPortConnections] = useState<NetworkConnection[]>([]);
  const [detailCapturedAt, setDetailCapturedAt] = useState<string>();
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState<string>();
  const [initialLoading, setInitialLoading] = useState(true);
  const detailRequestIdRef = useRef(0);
  const allListeners = networkSnapshot?.listeners ?? [];

  useEffect(() => {
    const unsub = window.nextshell.monitor.onNetworkData((snapshot: NetworkSnapshot) => {
      if (snapshot.connectionId === connectionId) {
        setNetworkSnapshot(connectionId, snapshot);
        setInitialLoading(false);
      }
    });

    void window.nextshell.monitor.startNetwork({ connectionId }).catch((err: unknown) => {
      const reason = err instanceof Error ? err.message : "启动网络监控失败";
      message.error(reason);
    });

    return () => {
      unsub();
      void window.nextshell.monitor.stopNetwork({ connectionId }).catch(() => {});
    };
  }, [connectionId, setNetworkSnapshot]);

  const listeners = useMemo(() => {
    if (allListeners.length === 0) {
      return [];
    }

    const lower = search.trim().toLowerCase();
    const filtered = lower
      ? allListeners.filter((listener) => {
          return (
            String(listener.port).includes(lower) ||
            String(listener.pid).includes(lower) ||
            listener.name.toLowerCase().includes(lower) ||
            listener.listenIp.toLowerCase().includes(lower)
          );
        })
      : allListeners;

    return [...filtered].sort((a, b) => {
      if (a.port !== b.port) {
        return a.port - b.port;
      }
      return a.pid - b.pid;
    });
  }, [allListeners, search]);

  const selectedListener = useMemo(() => {
    if (!selectedListenerKey) {
      return undefined;
    }
    return allListeners.find((listener) => getListenerKey(listener) === selectedListenerKey);
  }, [allListeners, selectedListenerKey]);

  useEffect(() => {
    if (selectedListenerKey && !selectedListener) {
      setSelectedListenerKey(undefined);
      setPortConnections([]);
      setDetailCapturedAt(undefined);
      setDetailError(undefined);
      setDetailLoading(false);
    }
  }, [selectedListener, selectedListenerKey]);

  const fetchPortConnections = useCallback(
    async (listener: NetworkListener, silent: boolean) => {
      const requestId = detailRequestIdRef.current + 1;
      detailRequestIdRef.current = requestId;

      if (!silent) {
        setDetailLoading(true);
      }
      setDetailError(undefined);

      try {
        const rows = await window.nextshell.monitor.getNetworkConnections({
          connectionId,
          port: listener.port
        });

        if (detailRequestIdRef.current !== requestId) {
          return;
        }

        setPortConnections(rows);
        setDetailCapturedAt(new Date().toISOString());
      } catch (error) {
        if (detailRequestIdRef.current !== requestId) {
          return;
        }
        const reason = error instanceof Error ? error.message : "读取端口连接失败";
        setDetailError(reason);
      } finally {
        if (detailRequestIdRef.current === requestId) {
          setDetailLoading(false);
        }
      }
    },
    [connectionId]
  );

  const handleOpenListenerDetail = useCallback(
    (listener: NetworkListener) => {
      setSelectedListenerKey(getListenerKey(listener));
      setPortConnections([]);
      setDetailCapturedAt(undefined);
      void fetchPortConnections(listener, false);
    },
    [fetchPortConnections]
  );

  useEffect(() => {
    if (!selectedListener) {
      return;
    }

    const timer = window.setInterval(() => {
      void fetchPortConnections(selectedListener, true);
    }, DETAIL_POLL_INTERVAL_MS);

    return () => {
      window.clearInterval(timer);
    };
  }, [fetchPortConnections, selectedListener]);

  const listenerColumns: ColumnsType<NetworkListener> = [
    {
      title: "PID",
      dataIndex: "pid",
      width: 78,
      render: (pid: number) => <span className="mono-cell">{pid}</span>
    },
    {
      title: "进程",
      dataIndex: "name",
      width: 140,
      ellipsis: true
    },
    {
      title: "监听 IP",
      dataIndex: "listenIp",
      width: 150,
      ellipsis: true,
      render: (ip: string) => <span className="mono-cell">{ip}</span>
    },
    {
      title: "端口",
      dataIndex: "port",
      width: 86,
      sorter: (a, b) => a.port - b.port,
      render: (port: number) => <span className="mono-cell port-cell">{port}</span>
    },
    {
      title: "详情",
      key: "detail",
      width: 70,
      render: (_: unknown, record: NetworkListener) => (
        <Tooltip title="查看该端口连接详情">
          <button
            type="button"
            className="pm-action-btn"
            aria-label="查看该端口连接详情"
            onClick={() => handleOpenListenerDetail(record)}
          >
            <i className="ri-information-line" aria-hidden="true" />
          </button>
        </Tooltip>
      )
    }
  ];

  const connectionColumns: ColumnsType<NetworkConnection> = [
    {
      title: "远程 IP",
      dataIndex: "remoteIp",
      width: 170,
      ellipsis: true,
      render: (ip: string) => <span className="mono-cell">{ip}</span>
    },
    {
      title: "远程端口",
      dataIndex: "remotePort",
      width: 90,
      render: (port: number) => <span className="mono-cell">{port}</span>
    },
    {
      title: "状态",
      dataIndex: "state",
      width: 120,
      render: (state: string) => (
        <span className={`conn-state conn-state-${state.toLowerCase()}`}>
          {state}
        </span>
      )
    },
    {
      title: "PID",
      dataIndex: "pid",
      width: 76,
      render: (pid: number) => <span className="mono-cell">{pid}</span>
    },
    {
      title: "进程",
      dataIndex: "processName",
      ellipsis: true
    }
  ];

  const listenerCapturedAt = networkSnapshot?.capturedAt
    ? new Date(networkSnapshot.capturedAt).toLocaleTimeString()
    : "--:--:--";
  const detailTimeText = detailCapturedAt
    ? new Date(detailCapturedAt).toLocaleTimeString()
    : "--:--:--";

  return (
    <div className="flex flex-col h-full py-2 px-3 overflow-hidden bg-[var(--bg-surface)] gap-2">
      <div className="flex flex-col overflow-hidden min-h-0 flex-[1.1]">
        <div className="flex items-center gap-2 mb-2 shrink-0">
          <Input
            className="nm-search"
            placeholder="搜索 PID / 端口 / 进程 / 监听 IP..."
            prefix={<i className="ri-search-line" aria-hidden="true" />}
            allowClear
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
          <span className="ml-auto text-[11px] text-[var(--t3)] whitespace-nowrap">
            {listeners.length} 监听项 · {listenerCapturedAt}
          </span>
        </div>
        <div className="flex-1 overflow-hidden">
          <Table<NetworkListener>
            rowKey={getListenerKey}
            columns={listenerColumns}
            dataSource={listeners}
            size="small"
            pagination={false}
            scroll={{ y: "calc(50vh - 156px)" }}
            className="nm-table"
            locale={{ emptyText: initialLoading ? <TableSkeleton rows={5} columns={4} /> : "暂无监听数据" }}
            onRow={(record) => ({
              className: getListenerKey(record) === selectedListenerKey ? "nm-row-selected" : "",
              onClick: () => handleOpenListenerDetail(record)
            })}
          />
        </div>
      </div>

      <div className="flex flex-col overflow-hidden min-h-0 flex-1 border-t border-[var(--border)] pt-2">
        {!selectedListener ? (
          <div className="nm-detail-placeholder">
            <i className="ri-cursor-line text-[20px] opacity-60" aria-hidden="true" />
            <span>点击上方监听项查看该端口的连接详情。</span>
          </div>
        ) : (
          <>
            <div className="nm-detail-header">
              <div className="nm-detail-meta">
                <span className="nm-detail-label">当前端口</span>
                <span className="nm-detail-value mono-cell">:{selectedListener.port}</span>
                <span className="nm-detail-sub">
                  PID {selectedListener.pid} · {selectedListener.name}
                </span>
              </div>
              <span className="text-[11px] text-[var(--t3)] whitespace-nowrap">
                明细轮询 {Math.floor(DETAIL_POLL_INTERVAL_MS / 1000)}s · {detailTimeText}
              </span>
            </div>

            {detailError ? (
              <div className="nm-detail-error">{detailError}</div>
            ) : null}

            <div className="flex-1 overflow-hidden">
              <Table<NetworkConnection>
                rowKey={(row) => `${row.remoteIp}-${row.remotePort}-${row.localPort}-${row.pid}-${row.state}`}
                columns={connectionColumns}
                dataSource={portConnections}
                size="small"
                pagination={false}
                scroll={{ y: "calc(50vh - 210px)" }}
                className="nm-table"
                locale={{
                  emptyText: detailLoading ? (
                    <span className="inline-flex items-center gap-2 text-[var(--t3)]">
                      <Spin size="small" />
                      读取端口连接中...
                    </span>
                  ) : "该端口暂无活跃连接"
                }}
              />
            </div>
          </>
        )}
      </div>
    </div>
  );
};
