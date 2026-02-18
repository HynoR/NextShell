import { useEffect, useMemo, useState } from "react";
import type { MonitorSnapshot } from "@nextshell/core";

interface SystemInfoPanelProps {
  monitorSessionEnabled?: boolean;
  hasVisibleTerminal: boolean;
  snapshot?: MonitorSnapshot;
  onSelectNetworkInterface?: (networkInterface: string) => void;
}

interface NetworkPoint {
  inMbps: number;
  outMbps: number;
  capturedAt: string;
}

const MAX_NETWORK_POINTS = 28;
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
  onSelectNetworkInterface
}: SystemInfoPanelProps) => {
  const [collapsed, setCollapsed] = useState(false);
  const [networkHistory, setNetworkHistory] = useState<Record<string, NetworkPoint[]>>({});

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

  useEffect(() => {
    if (!snapshot || !snapshot.networkInterface) {
      return;
    }

    setNetworkHistory((previous) => {
      const next = { ...previous };
      const key = snapshot.networkInterface;
      const existing = next[key] ?? [];
      const point: NetworkPoint = {
        inMbps: snapshot.networkInMbps,
        outMbps: snapshot.networkOutMbps,
        capturedAt: snapshot.capturedAt
      };

      const latest = existing[existing.length - 1];
      let merged = existing;
      if (latest?.capturedAt === point.capturedAt) {
        merged = [...existing.slice(0, -1), point];
      } else {
        merged = [...existing, point];
      }
      next[key] = merged.slice(-MAX_NETWORK_POINTS);
      return next;
    });
  }, [snapshot]);

  const chartPoints = snapshot?.networkInterface
    ? (networkHistory[snapshot.networkInterface] ?? [])
    : [];
  const chartMax = Math.max(1, ...chartPoints.map((point) => Math.max(point.inMbps, point.outMbps)));

  if (!monitorSessionEnabled) {
    return null;
  }

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
            <div className="monitor-v2">
              <div className="monitor-v2-meta">
                <div>{formatUptime(snapshot.uptimeHours)}</div>
                <div>{formatLoad(snapshot)}</div>
              </div>

              <div className="monitor-v2-list">
                <div className="monitor-v2-row">
                  <div className="monitor-v2-label">CPU</div>
                  <div className="monitor-v2-track">
                    <div
                      className={`monitor-v2-fill ${barClass(snapshot.cpuPercent)}`}
                      style={{ width: `${Math.min(snapshot.cpuPercent, 100)}%` }}
                    />
                    <span className="monitor-v2-pct">{snapshot.cpuPercent.toFixed(0)}%</span>
                  </div>
                  <div className="monitor-v2-extra" />
                </div>

                <div className="monitor-v2-row">
                  <div className="monitor-v2-label">内存</div>
                  <div className="monitor-v2-track">
                    <div
                      className={`monitor-v2-fill mem ${barClass(snapshot.memoryPercent)}`}
                      style={{ width: `${Math.min(snapshot.memoryPercent, 100)}%` }}
                    />
                    <span className="monitor-v2-pct">{snapshot.memoryPercent.toFixed(0)}%</span>
                  </div>
                  <div className="monitor-v2-extra">
                    {formatMemoryShort(snapshot.memoryUsedMb)}/{formatMemoryShort(snapshot.memoryTotalMb)}
                  </div>
                </div>

                <div className="monitor-v2-row">
                  <div className="monitor-v2-label">交换</div>
                  <div className="monitor-v2-track">
                    <div
                      className={`monitor-v2-fill swap ${barClass(snapshot.swapPercent)}`}
                      style={{ width: `${Math.min(snapshot.swapPercent, 100)}%` }}
                    />
                    <span className="monitor-v2-pct">{snapshot.swapPercent.toFixed(0)}%</span>
                  </div>
                  <div className="monitor-v2-extra">
                    {formatMemoryShort(snapshot.swapUsedMb)}/{formatMemoryShort(snapshot.swapTotalMb)}
                  </div>
                </div>

                <div className="monitor-v2-row">
                  <div className="monitor-v2-label">磁盘</div>
                  <div className="monitor-v2-track">
                    <div
                      className={`monitor-v2-fill disk ${barClass(snapshot.diskPercent)}`}
                      style={{ width: `${Math.min(snapshot.diskPercent, 100)}%` }}
                    />
                    <span className="monitor-v2-pct">{snapshot.diskPercent.toFixed(0)}%</span>
                  </div>
                  <div className="monitor-v2-extra">
                    {snapshot.diskUsedGb.toFixed(1)}G/{snapshot.diskTotalGb.toFixed(1)}G
                  </div>
                </div>
              </div>

              <div className="monitor-net-v2">
                <div className="monitor-net-v2-head">
                  <div className="monitor-net-v2-flow">
                    <span className="up">↑ {formatRate(snapshot.networkOutMbps)}</span>
                    <span className="down">↓ {formatRate(snapshot.networkInMbps)}</span>
                  </div>
                  <select
                    className="monitor-net-v2-select"
                    value={snapshot.networkInterface}
                    onChange={(event) => onSelectNetworkInterface?.(event.target.value)}
                  >
                    {interfaceOptions.map((item) => (
                      <option key={item} value={item}>{item}</option>
                    ))}
                  </select>
                </div>

                <div className="monitor-net-v2-chart">
                  {chartPoints.length === 0 ? (
                    <div className="monitor-net-v2-empty">等待网络采样数据...</div>
                  ) : (
                    <svg
                      viewBox={`0 0 ${chartPoints.length * 10 + 8} ${NETWORK_CHART_HEIGHT}`}
                      preserveAspectRatio="none"
                    >
                      {[0.25, 0.5, 0.75].map((line) => (
                        <line
                          key={line}
                          x1="0"
                          y1={(1 - line) * NETWORK_CHART_HEIGHT}
                          x2={chartPoints.length * 10 + 8}
                          y2={(1 - line) * NETWORK_CHART_HEIGHT}
                          className="monitor-net-v2-grid"
                        />
                      ))}
                      {chartPoints.map((point, index) => {
                        const x = index * 10 + 4;
                        const inHeight = (point.inMbps / chartMax) * (NETWORK_CHART_HEIGHT - 4);
                        const outHeight = (point.outMbps / chartMax) * (NETWORK_CHART_HEIGHT - 4);

                        return (
                          <g key={`${point.capturedAt}-${index}`}>
                            <rect
                              x={x}
                              y={NETWORK_CHART_HEIGHT - inHeight}
                              width="3"
                              height={Math.max(1, inHeight)}
                              className="monitor-net-v2-bar-in"
                            />
                            <rect
                              x={x + 4}
                              y={NETWORK_CHART_HEIGHT - outHeight}
                              width="3"
                              height={Math.max(1, outHeight)}
                              className="monitor-net-v2-bar-out"
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
    </section>
  );
};
