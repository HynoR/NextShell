import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { MonitorSnapshot } from "@nextshell/core";
import { useWorkspaceStore } from "../store/useWorkspaceStore";

interface SystemInfoPanelProps {
  monitorSessionEnabled?: boolean;
  hasVisibleTerminal: boolean;
  snapshot?: MonitorSnapshot;
  onSelectNetworkInterface?: (networkInterface: string) => void;
  onOpenProcessManager?: () => void;
  onOpenNetworkMonitor?: () => void;
  monitorActionsDisabled?: boolean;
}

const NETWORK_CHART_HEIGHT = 84;
const NETWORK_HISTORY_CAP = 50;
const NETWORK_CHART_WIDTH = NETWORK_HISTORY_CAP * 10 + 8;

const barClass = (pct: number) => {
  if (pct >= 90) return "err";
  if (pct >= 70) return "warn";
  return "";
};

const summaryLine = (snapshot: MonitorSnapshot): string => {
  return `CPU ${snapshot.cpuPercent.toFixed(0)}% / MEM ${snapshot.memoryPercent.toFixed(0)}%`;
};

const formatLoad = (snapshot: MonitorSnapshot): string => {
  return `负载 ${snapshot.loadAverage.map((value) => value.toFixed(2)).join(", ")}`;
};

const formatMemoryShort = (mb: number): string => {
  if (mb >= 1024) {
    return `${(mb / 1024).toFixed(1)}G`;
  }
  return `${Math.max(0, mb).toFixed(0)}M`;
};

const formatRate = (mbps: number): string => {
  const kbps = Math.max(0, mbps * 1024);
  if (kbps >= 1024) {
    return `${(kbps / 1024).toFixed(1)}M`;
  }
  return `${kbps.toFixed(0)}K`;
};

export const SystemInfoPanel = ({
  monitorSessionEnabled,
  hasVisibleTerminal,
  snapshot,
  onSelectNetworkInterface,
  onOpenProcessManager,
  onOpenNetworkMonitor,
  monitorActionsDisabled,
}: SystemInfoPanelProps) => {
  const [collapsed, setCollapsed] = useState(false);
  const networkRateHistory = useWorkspaceStore((s) => s.networkRateHistory);
  const [ctxMenu, setCtxMenu] = useState<{ x: number; y: number } | null>(null);
  const ctxMenuRef = useRef<HTMLDivElement>(null);

  // Close context menu on outside click or escape
  useEffect(() => {
    if (!ctxMenu) return;
    const onDown = (e: MouseEvent) => {
      if (ctxMenuRef.current && !ctxMenuRef.current.contains(e.target as Node)) {
        setCtxMenu(null);
      }
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setCtxMenu(null);
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [ctxMenu]);

  useEffect(() => {
    if (!hasVisibleTerminal && collapsed) {
      setCollapsed(false);
    }
  }, [collapsed, hasVisibleTerminal]);

  const interfaceOptions = useMemo(() => {
    if (!snapshot) {
      return [];
    }

    if (snapshot.networkInterfaceOptions.length > 0) {
      return snapshot.networkInterfaceOptions;
    }
    return [snapshot.networkInterface];
  }, [snapshot]);

  const handleChartContextMenu = useCallback(
    (e: React.MouseEvent) => {
      if (interfaceOptions.length <= 1) return;
      e.preventDefault();
      setCtxMenu({ x: e.clientX, y: e.clientY });
    },
    [interfaceOptions],
  );

  const chartPoints =
    snapshot?.connectionId && snapshot?.networkInterface
      ? (networkRateHistory[
          `${snapshot.connectionId}:${snapshot.networkInterface}`
        ] ?? [])
      : [];
  const chartMax = Math.max(
    1,
    ...chartPoints.map((point) => Math.max(point.inMbps, point.outMbps)),
  );

  // Fixed 50-slot queue: always render 50 columns, right-aligned (newest on right)
  type NetworkPoint = (typeof chartPoints)[number];
  const emptyPoint: NetworkPoint = { inMbps: 0, outMbps: 0, capturedAt: "" };
  const networkSlots: NetworkPoint[] = Array.from<NetworkPoint>({
    length: NETWORK_HISTORY_CAP,
  }).fill(emptyPoint);
  const networkOffset = NETWORK_HISTORY_CAP - chartPoints.length;
  for (let i = 0; i < chartPoints.length; i++) {
    networkSlots[networkOffset + i] = chartPoints[i]!;
  }

  if (!monitorSessionEnabled) {
    return null;
  }

  const showManagerActions = Boolean(
    onOpenProcessManager || onOpenNetworkMonitor,
  );

  return (
    <section className="monitor-panel">
      <button
        type="button"
        className="monitor-panel-header"
        onClick={() => setCollapsed((prev) => !prev)}
      >
        <i
          className={
            collapsed ? "ri-arrow-right-s-line" : "ri-arrow-down-s-line"
          }
          aria-hidden="true"
        />
        <span className="text-[10px] font-semibold tracking-[0.08em] uppercase text-[var(--t3)]">
          系统监控
        </span>
        {collapsed && snapshot ? (
          <span className="monitor-summary">{summaryLine(snapshot)}</span>
        ) : null}
      </button>

      {!collapsed ? (
        <div className="monitor-panel-body">
          {!hasVisibleTerminal ? (
            <div className="monitor-placeholder">
              请先连接 SSH 终端以启动 Monitor Session
            </div>
          ) : snapshot ? (
            <div className="flex flex-col gap-3 py-1">
              <div className="flex flex-col gap-2 px-1 mb-1">
                <div className="flex items-center gap-2 text-[11px] text-[var(--t2)] font-mono bg-[var(--bg-elevated)] px-2.5 py-1.5 rounded-md border border-[var(--border-dim)] shadow-sm">
                  <i className="ri-dashboard-3-line text-[var(--t3)]" />
                  {formatLoad(snapshot)}
                </div>
              </div>

              <div className="flex flex-col gap-2.5 px-1">
                <div className="grid grid-cols-[40px_1fr_85px] items-center gap-2.5">
                  <div className="text-[11px] font-semibold text-[var(--t2)] tracking-wide uppercase">
                    CPU
                  </div>
                  <div className="h-6 relative bg-black/5 dark:bg-white/5 border border-[var(--border-dim)] rounded-md overflow-hidden shadow-inner">
                    <div
                      className={`absolute left-0 top-0 bottom-0 transition-all duration-700 ${snapshot.cpuPercent > 90 ? "bg-red-500/40" : snapshot.cpuPercent > 70 ? "bg-amber-500/40" : "bg-blue-500/30"}`}
                      style={{
                        width: `${Math.min(snapshot.cpuPercent, 100)}%`,
                      }}
                    />
                    <div className="absolute inset-y-0 left-2.5 flex items-center text-[11.5px] font-mono font-bold text-[var(--t1)] drop-shadow-sm">
                      {snapshot.cpuPercent.toFixed(0)}%
                    </div>
                  </div>
                  <div className="text-right text-[10.5px] font-mono text-[var(--t3)]" />
                </div>

                <div className="grid grid-cols-[40px_1fr_85px] items-center gap-2.5">
                  <div className="text-[11px] font-semibold text-[var(--t2)] tracking-wide uppercase">
                    内存
                  </div>
                  <div className="h-6 relative bg-black/5 dark:bg-white/5 border border-[var(--border-dim)] rounded-md overflow-hidden shadow-inner">
                    <div
                      className={`absolute left-0 top-0 bottom-0 transition-all duration-700 ${snapshot.memoryPercent > 90 ? "bg-red-500/40" : snapshot.memoryPercent > 70 ? "bg-amber-500/40" : "bg-emerald-500/30"}`}
                      style={{
                        width: `${Math.min(snapshot.memoryPercent, 100)}%`,
                      }}
                    />
                    <div className="absolute inset-y-0 left-2.5 flex items-center text-[11.5px] font-mono font-bold text-[var(--t1)] drop-shadow-sm">
                      {snapshot.memoryPercent.toFixed(0)}%
                    </div>
                  </div>
                  <div className="text-right text-[10.5px] font-mono text-[var(--t2)]">
                    <span className="text-[var(--t1)]">
                      {formatMemoryShort(snapshot.memoryUsedMb)}
                    </span>
                    <span className="text-[var(--t3)] opacity-60 mx-0.5">
                      /
                    </span>
                    <span className="text-[var(--t3)]">
                      {formatMemoryShort(snapshot.memoryTotalMb)}
                    </span>
                  </div>
                </div>

                <div className="grid grid-cols-[40px_1fr_85px] items-center gap-2.5">
                  <div className="text-[11px] font-semibold text-[var(--t2)] tracking-wide uppercase">
                    交换
                  </div>
                  <div className="h-6 relative bg-black/5 dark:bg-white/5 border border-[var(--border-dim)] rounded-md overflow-hidden shadow-inner">
                    <div
                      className={`absolute left-0 top-0 bottom-0 transition-all duration-700 ${snapshot.swapPercent > 90 ? "bg-red-500/40" : snapshot.swapPercent > 70 ? "bg-amber-500/40" : "bg-indigo-500/30"}`}
                      style={{
                        width: `${Math.min(snapshot.swapPercent, 100)}%`,
                      }}
                    />
                    <div className="absolute inset-y-0 left-2.5 flex items-center text-[11.5px] font-mono font-bold text-[var(--t1)] drop-shadow-sm">
                      {snapshot.swapPercent.toFixed(0)}%
                    </div>
                  </div>
                  <div className="text-right text-[10.5px] font-mono text-[var(--t2)]">
                    <span className="text-[var(--t1)]">
                      {formatMemoryShort(snapshot.swapUsedMb)}
                    </span>
                    <span className="text-[var(--t3)] opacity-60 mx-0.5">
                      /
                    </span>
                    <span className="text-[var(--t3)]">
                      {formatMemoryShort(snapshot.swapTotalMb)}
                    </span>
                  </div>
                </div>

                <div className="grid grid-cols-[40px_1fr_85px] items-center gap-2.5">
                  <div className="text-[11px] font-semibold text-[var(--t2)] tracking-wide uppercase">
                    磁盘
                  </div>
                  <div className="h-6 relative bg-black/5 dark:bg-white/5 border border-[var(--border-dim)] rounded-md overflow-hidden shadow-inner">
                    <div
                      className={`absolute left-0 top-0 bottom-0 transition-all duration-700 ${snapshot.diskPercent > 90 ? "bg-red-500/40" : snapshot.diskPercent > 70 ? "bg-amber-500/40" : "bg-teal-500/30"}`}
                      style={{
                        width: `${Math.min(snapshot.diskPercent, 100)}%`,
                      }}
                    />
                    <div className="absolute inset-y-0 left-2.5 flex items-center text-[11.5px] font-mono font-bold text-[var(--t1)] drop-shadow-sm">
                      {snapshot.diskPercent.toFixed(0)}%
                    </div>
                  </div>
                  <div className="text-right text-[10.5px] font-mono text-[var(--t2)]">
                    <span className="text-[var(--t1)]">
                      {snapshot.diskUsedGb.toFixed(1)}G
                    </span>
                    <span className="text-[var(--t3)] opacity-60 mx-0.5">
                      /
                    </span>
                    <span className="text-[var(--t3)]">
                      {snapshot.diskTotalGb.toFixed(1)}G
                    </span>
                  </div>
                </div>
              </div>
              <div className="flex items-center gap-1.5 px-1">
                <div className="text-[10px] font-semibold text-[var(--t3)] tracking-wide uppercase">
                  网络 · {snapshot.networkInterface}
                  {interfaceOptions.length > 1 ? (
                    <span className="font-normal opacity-60 ml-1">(右击流量图切换网卡)</span>
                  ) : null}
                </div>
              </div>

              <div
                className="h-[90px] rounded-lg border border-[var(--border-dim)] bg-black/[0.03] dark:bg-white/[0.02] pl-[34px] pr-2 py-2 relative overflow-hidden group shadow-inner"
                onContextMenu={handleChartContextMenu}
              >
                {chartPoints.length === 0 ? (
                  <div className="w-full h-full flex items-center justify-center text-[11px] text-[var(--t3)] animate-pulse">
                    等待网络采样数据...
                  </div>
                ) : (
                  <>
                    <span
                      className="absolute left-1 text-[9px] font-mono text-[var(--t3)] leading-none"
                      style={{ top: "8px" }}
                    >
                      {formatRate(chartMax)}
                    </span>
                    <span
                      className="absolute left-1 text-[9px] font-mono text-[var(--t3)] leading-none"
                      style={{ top: "50%", transform: "translateY(-50%)" }}
                    >
                      {formatRate(chartMax / 2)}
                    </span>
                    <span
                      className="absolute left-1 text-[9px] font-mono text-[var(--t3)] leading-none"
                      style={{ bottom: "8px" }}
                    >
                      0
                    </span>
                    <svg
                      viewBox={`0 0 ${NETWORK_CHART_WIDTH} ${NETWORK_CHART_HEIGHT}`}
                      preserveAspectRatio="none"
                      className="w-full h-full drop-shadow-sm"
                    >
                      {[0.25, 0.5, 0.75].map((line) => (
                        <line
                          key={line}
                          x1="0"
                          y1={(1 - line) * NETWORK_CHART_HEIGHT}
                          x2={NETWORK_CHART_WIDTH}
                          y2={(1 - line) * NETWORK_CHART_HEIGHT}
                          className="stroke-[var(--border-dim)] stroke-1"
                          strokeDasharray="3 3"
                        />
                      ))}
                      {networkSlots.map((point, index) => {
                        if (point.inMbps === 0 && point.outMbps === 0)
                          return null;
                        const x = index * 10 + 4;
                        const inHeight =
                          (point.inMbps / chartMax) *
                          (NETWORK_CHART_HEIGHT - 4);
                        const outHeight =
                          (point.outMbps / chartMax) *
                          (NETWORK_CHART_HEIGHT - 4);

                        return (
                          <g
                            key={`${index}-${point.capturedAt}`}
                            className="transition-all duration-300 hover:opacity-80 cursor-default"
                          >
                            <rect
                              x={x}
                              y={NETWORK_CHART_HEIGHT - inHeight}
                              width="4"
                              height={Math.max(2, inHeight)}
                              rx="2"
                              className="fill-emerald-500/70 dark:fill-emerald-400/80"
                            />
                            <rect
                              x={x + 4}
                              y={NETWORK_CHART_HEIGHT - outHeight}
                              width="4"
                              height={Math.max(2, outHeight)}
                              rx="2"
                              className="fill-orange-500/70 dark:fill-orange-400/80"
                            />
                          </g>
                        );
                      })}
                    </svg>
                  </>
                )}
              </div>

              {ctxMenu ? (
                <div
                  ref={ctxMenuRef}
                  className="fixed z-50 min-w-[120px] rounded-md border border-[var(--border)] bg-[var(--bg-elevated)] shadow-lg py-1"
                  style={{ left: ctxMenu.x, top: ctxMenu.y }}
                >
                  {interfaceOptions.map((iface) => (
                    <button
                      key={iface}
                      type="button"
                      className={`w-full text-left px-3 py-1.5 text-[11px] font-mono hover:bg-[var(--bg-hover)] transition-colors ${iface === snapshot.networkInterface ? "text-[var(--accent)] font-semibold" : "text-[var(--t2)]"}`}
                      onClick={() => {
                        onSelectNetworkInterface?.(iface);
                        setCtxMenu(null);
                      }}
                    >
                      {iface}
                    </button>
                  ))}
                </div>
              ) : null}

              <div className="flex items-center justify-center mb-1">
                <div className="flex items-center gap-3.5 text-[11.5px] font-mono">
                  <div className="flex items-center gap-1.5 px-2 py-0.5 rounded bg-orange-500/10 text-orange-500 border border-orange-500/20">
                    <i className="ri-arrow-up-line" />
                    <span className="font-semibold">
                      {formatRate(snapshot.networkOutMbps)}
                    </span>
                  </div>
                  <div className="flex items-center gap-1.5 px-2 py-0.5 rounded bg-emerald-500/10 text-emerald-500 border border-emerald-500/20">
                    <i className="ri-arrow-down-line" />
                    <span className="font-semibold">
                      {formatRate(snapshot.networkInMbps)}
                    </span>
                  </div>
                </div>
              </div>
            </div>
          ) : (
            <div className="monitor-placeholder">等待监控数据...</div>
          )}
        </div>
      ) : null}
      {showManagerActions ? (
        <div className="monitor-manager-actions">
          <button
            type="button"
            className="monitor-manager-btn"
            onClick={() => onOpenProcessManager?.()}
            disabled={monitorActionsDisabled}
            title="打开进程管理器"
          >
            <i className="ri-cpu-line" aria-hidden="true" />
            <span>进程管理器</span>
          </button>
          <button
            type="button"
            className="monitor-manager-btn"
            onClick={() => onOpenNetworkMonitor?.()}
            disabled={monitorActionsDisabled}
            title="打开网络管理器"
          >
            <i className="ri-global-line" aria-hidden="true" />
            <span>网络管理器</span>
          </button>
        </div>
      ) : null}
    </section>
  );
};
