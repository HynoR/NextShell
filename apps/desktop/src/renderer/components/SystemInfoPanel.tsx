import { useEffect, useMemo, useState } from "react";
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

const barClass = (pct: number) => {
  if (pct >= 90) return "err";
  if (pct >= 70) return "warn";
  return "";
};

const summaryLine = (snapshot: MonitorSnapshot): string => {
  return `CPU ${snapshot.cpuPercent.toFixed(0)}% / MEM ${snapshot.memoryPercent.toFixed(0)}%`;
};

const formatUptime = (uptimeHours: number): string => {
  const totalHours = Math.max(0, Math.floor(uptimeHours));
  const days = Math.floor(totalHours / 24);
  const hours = totalHours % 24;

  if (days > 0) {
    return `运行 ${days} 天 ${hours} 小时`;
  }

  return `运行 ${hours} 小时`;
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
  monitorActionsDisabled
}: SystemInfoPanelProps) => {
  const [collapsed, setCollapsed] = useState(false);
  const networkRateHistory = useWorkspaceStore((s) => s.networkRateHistory);

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

  const chartPoints =
    snapshot?.connectionId && snapshot?.networkInterface
      ? (networkRateHistory[`${snapshot.connectionId}:${snapshot.networkInterface}`] ?? [])
      : [];
  const chartMax = Math.max(1, ...chartPoints.map((point) => Math.max(point.inMbps, point.outMbps)));

  if (!monitorSessionEnabled) {
    return null;
  }

  const showManagerActions = Boolean(onOpenProcessManager || onOpenNetworkMonitor);

  return (
    <section className="monitor-panel">
      <button
        type="button"
        className="monitor-panel-header"
        onClick={() => setCollapsed((prev) => !prev)}
      >
        <i
          className={collapsed ? "ri-arrow-right-s-line" : "ri-arrow-down-s-line"}
          aria-hidden="true"
        />
        <span className="text-[10px] font-semibold tracking-[0.08em] uppercase text-[var(--t3)]">系统监控</span>
        {collapsed && snapshot ? (
          <span className="monitor-summary">{summaryLine(snapshot)}</span>
        ) : null}
      </button>

      {!collapsed ? (
        <div className="monitor-panel-body">
          {!hasVisibleTerminal ? (
            <div className="monitor-placeholder">请先连接 SSH 终端以启动 Monitor Session</div>
          ) : snapshot ? (
            <div className="flex flex-col gap-3 py-1">
              <div className="flex flex-col gap-2 px-1 mb-1">
                <div className="flex items-center gap-2 text-[11px] text-[var(--t2)] font-medium bg-[var(--bg-elevated)] px-2.5 py-1.5 rounded-md border border-[var(--border-dim)] shadow-sm">
                  <i className="ri-timer-line text-[var(--t3)]" />
                  {formatUptime(snapshot.uptimeHours)}
                </div>
                <div className="flex items-center gap-2 text-[11px] text-[var(--t2)] font-mono bg-[var(--bg-elevated)] px-2.5 py-1.5 rounded-md border border-[var(--border-dim)] shadow-sm">
                  <i className="ri-dashboard-3-line text-[var(--t3)]" />
                  {formatLoad(snapshot)}
                </div>
              </div>

              <div className="flex flex-col gap-2.5 px-1">
                <div className="grid grid-cols-[40px_1fr_85px] items-center gap-2.5">
                  <div className="text-[11px] font-semibold text-[var(--t2)] tracking-wide uppercase">CPU</div>
                  <div className="h-6 relative bg-black/5 dark:bg-white/5 border border-[var(--border-dim)] rounded-md overflow-hidden shadow-inner">
                    <div
                      className={`absolute left-0 top-0 bottom-0 transition-all duration-700 ${snapshot.cpuPercent > 90 ? 'bg-red-500/40' : snapshot.cpuPercent > 70 ? 'bg-amber-500/40' : 'bg-blue-500/30'}`}
                      style={{ width: `${Math.min(snapshot.cpuPercent, 100)}%` }}
                    />
                    <div className="absolute inset-y-0 left-2.5 flex items-center text-[11.5px] font-mono font-bold text-[var(--t1)] drop-shadow-sm">
                      {snapshot.cpuPercent.toFixed(0)}%
                    </div>
                  </div>
                  <div className="text-right text-[10.5px] font-mono text-[var(--t3)]" />
                </div>

                <div className="grid grid-cols-[40px_1fr_85px] items-center gap-2.5">
                  <div className="text-[11px] font-semibold text-[var(--t2)] tracking-wide uppercase">内存</div>
                  <div className="h-6 relative bg-black/5 dark:bg-white/5 border border-[var(--border-dim)] rounded-md overflow-hidden shadow-inner">
                    <div
                      className={`absolute left-0 top-0 bottom-0 transition-all duration-700 ${snapshot.memoryPercent > 90 ? 'bg-red-500/40' : snapshot.memoryPercent > 70 ? 'bg-amber-500/40' : 'bg-emerald-500/30'}`}
                      style={{ width: `${Math.min(snapshot.memoryPercent, 100)}%` }}
                    />
                    <div className="absolute inset-y-0 left-2.5 flex items-center text-[11.5px] font-mono font-bold text-[var(--t1)] drop-shadow-sm">
                      {snapshot.memoryPercent.toFixed(0)}%
                    </div>
                  </div>
                  <div className="text-right text-[10.5px] font-mono text-[var(--t2)]">
                    <span className="text-[var(--t1)]">{formatMemoryShort(snapshot.memoryUsedMb)}</span>
                    <span className="text-[var(--t3)] opacity-60 mx-0.5">/</span>
                    <span className="text-[var(--t3)]">{formatMemoryShort(snapshot.memoryTotalMb)}</span>
                  </div>
                </div>

                <div className="grid grid-cols-[40px_1fr_85px] items-center gap-2.5">
                  <div className="text-[11px] font-semibold text-[var(--t2)] tracking-wide uppercase">交换</div>
                  <div className="h-6 relative bg-black/5 dark:bg-white/5 border border-[var(--border-dim)] rounded-md overflow-hidden shadow-inner">
                    <div
                      className={`absolute left-0 top-0 bottom-0 transition-all duration-700 ${snapshot.swapPercent > 90 ? 'bg-red-500/40' : snapshot.swapPercent > 70 ? 'bg-amber-500/40' : 'bg-indigo-500/30'}`}
                      style={{ width: `${Math.min(snapshot.swapPercent, 100)}%` }}
                    />
                    <div className="absolute inset-y-0 left-2.5 flex items-center text-[11.5px] font-mono font-bold text-[var(--t1)] drop-shadow-sm">
                      {snapshot.swapPercent.toFixed(0)}%
                    </div>
                  </div>
                  <div className="text-right text-[10.5px] font-mono text-[var(--t2)]">
                    <span className="text-[var(--t1)]">{formatMemoryShort(snapshot.swapUsedMb)}</span>
                    <span className="text-[var(--t3)] opacity-60 mx-0.5">/</span>
                    <span className="text-[var(--t3)]">{formatMemoryShort(snapshot.swapTotalMb)}</span>
                  </div>
                </div>

                <div className="grid grid-cols-[40px_1fr_85px] items-center gap-2.5">
                  <div className="text-[11px] font-semibold text-[var(--t2)] tracking-wide uppercase">磁盘</div>
                  <div className="h-6 relative bg-black/5 dark:bg-white/5 border border-[var(--border-dim)] rounded-md overflow-hidden shadow-inner">
                    <div
                      className={`absolute left-0 top-0 bottom-0 transition-all duration-700 ${snapshot.diskPercent > 90 ? 'bg-red-500/40' : snapshot.diskPercent > 70 ? 'bg-amber-500/40' : 'bg-teal-500/30'}`}
                      style={{ width: `${Math.min(snapshot.diskPercent, 100)}%` }}
                    />
                    <div className="absolute inset-y-0 left-2.5 flex items-center text-[11.5px] font-mono font-bold text-[var(--t1)] drop-shadow-sm">
                      {snapshot.diskPercent.toFixed(0)}%
                    </div>
                  </div>
                  <div className="text-right text-[10.5px] font-mono text-[var(--t2)]">
                    <span className="text-[var(--t1)]">{snapshot.diskUsedGb.toFixed(1)}G</span>
                    <span className="text-[var(--t3)] opacity-60 mx-0.5">/</span>
                    <span className="text-[var(--t3)]">{snapshot.diskTotalGb.toFixed(1)}G</span>
                  </div>
                </div>
              </div>

              <div className="mt-1 pt-3 border-t border-[var(--border-dim)] px-1">
                <div className="flex items-center justify-between mb-2.5">
                  <div className="flex items-center gap-3.5 text-[11.5px] font-mono">
                    <div className="flex items-center gap-1.5 px-2 py-0.5 rounded bg-orange-500/10 text-orange-500 border border-orange-500/20">
                      <i className="ri-arrow-up-line" />
                      <span className="font-semibold">{formatRate(snapshot.networkOutMbps)}</span>
                    </div>
                    <div className="flex items-center gap-1.5 px-2 py-0.5 rounded bg-emerald-500/10 text-emerald-500 border border-emerald-500/20">
                      <i className="ri-arrow-down-line" />
                      <span className="font-semibold">{formatRate(snapshot.networkInMbps)}</span>
                    </div>
                  </div>
                  <select
                    aria-label="选择网络接口"
                    className="h-[22px] px-2 rounded bg-[var(--bg-elevated)] border border-[var(--border)] text-[var(--t1)] text-[10.5px] font-mono focus:outline-none focus:border-[var(--accent)] cursor-pointer shadow-sm transition-colors hover:bg-[var(--bg-hover)]"
                    value={snapshot.networkInterface}
                    onChange={(event) => onSelectNetworkInterface?.(event.target.value)}
                  >
                    {interfaceOptions.map((item) => (
                      <option key={item} value={item}>{item}</option>
                    ))}
                  </select>
                </div>

                <div className="h-[90px] rounded-lg border border-[var(--border-dim)] bg-black/[0.03] dark:bg-white/[0.02] p-2 relative overflow-hidden group shadow-inner">
                  {chartPoints.length === 0 ? (
                    <div className="w-full h-full flex items-center justify-center text-[11px] text-[var(--t3)] animate-pulse">
                      等待网络采样数据...
                    </div>
                  ) : (
                    <svg
                      viewBox={`0 0 ${chartPoints.length * 10 + 8} ${NETWORK_CHART_HEIGHT}`}
                      preserveAspectRatio="none"
                      className="w-full h-full drop-shadow-sm"
                    >
                      {[0.25, 0.5, 0.75].map((line) => (
                        <line
                          key={line}
                          x1="0"
                          y1={(1 - line) * NETWORK_CHART_HEIGHT}
                          x2={chartPoints.length * 10 + 8}
                          y2={(1 - line) * NETWORK_CHART_HEIGHT}
                          className="stroke-[var(--border-dim)] stroke-1"
                          strokeDasharray="3 3"
                        />
                      ))}
                      {chartPoints.map((point, index) => {
                        const x = index * 10 + 4;
                        const inHeight = (point.inMbps / chartMax) * (NETWORK_CHART_HEIGHT - 4);
                        const outHeight = (point.outMbps / chartMax) * (NETWORK_CHART_HEIGHT - 4);

                        return (
                          <g key={`${point.capturedAt}-${index}`} className="transition-all duration-300 hover:opacity-80 cursor-default">
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
                  )}
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
