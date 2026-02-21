import { useEffect, useState } from "react";

const PING_INTERVAL_MS = 5000;
const PING_CHART_HEIGHT = 84;
const PING_HISTORY_CAP = 50;

interface PingCardProps {
  /** 当前选中连接的 host，无则不显示卡片 */
  host?: string;
}

export const PingCard = ({ host }: PingCardProps) => {
  const [result, setResult] = useState<
    { ok: true; avgMs: number } | { ok: false; error: string } | null
  >(null);
  const [loading, setLoading] = useState(false);
  const [pingHistory, setPingHistory] = useState<number[]>([]);

  useEffect(() => {
    if (!host || !window.nextshell?.ping?.probe) {
      setResult(null);
      setPingHistory([]);
      return;
    }

    let cancelled = false;
    const run = async () => {
      if (cancelled) return;
      setLoading(true);
      try {
        const res = await window.nextshell.ping.probe({ host });
        if (cancelled) return;
        if (res.ok === true) {
          setResult(res);
          setPingHistory((prev) => [...prev.slice(-(PING_HISTORY_CAP - 1)), res.avgMs]);
        } else {
          console.warn("[PingCard]", host, res.error);
          setResult({ ok: true, avgMs: 0 });
          setPingHistory((prev) => [...prev.slice(-(PING_HISTORY_CAP - 1)), 0]);
        }
      } catch (err) {
        if (!cancelled) {
          console.warn("[PingCard]", host, err);
          setResult({ ok: true, avgMs: 0 });
          setPingHistory((prev) => [...prev.slice(-(PING_HISTORY_CAP - 1)), 0]);
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    };

    void run();
    const timer = setInterval(run, PING_INTERVAL_MS);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [host]);

  if (!host) {
    return null;
  }

  const chartMax = Math.max(50, ...pingHistory);
  const chartPoints = pingHistory;

  return (
    <section className="monitor-panel">
      <div className="monitor-panel-header cursor-default">
        <i className="ri-wifi-line text-[var(--t3)]" aria-hidden="true" />
        <span className="text-[10px] font-semibold tracking-[0.08em] uppercase text-[var(--t3)]">
          Ping 延迟
        </span>
      </div>
      <div className="monitor-panel-body">
        <div className="flex items-center gap-2 text-[11px]">
          {loading && !result ? (
            <span className="text-[var(--t3)]">检测中…</span>
          ) : result ? (
            <>
              <span className="font-mono font-semibold text-[var(--t1)]">
                {result.avgMs === 0 ? "0" : result.avgMs < 1 ? "<1" : Math.round(result.avgMs)} ms
              </span>
              <span className="text-[10px] text-[var(--t3)]">每 5 秒刷新</span>
            </>
          ) : null}
        </div>

        <div className="mt-2 h-[90px] rounded-lg border border-[var(--border-dim)] bg-black/[0.03] dark:bg-white/[0.02] p-2 relative overflow-hidden group shadow-inner">
          {chartPoints.length === 0 ? (
            <div className="w-full h-full flex items-center justify-center text-[11px] text-[var(--t3)] animate-pulse">
              等待 Ping 采样数据...
            </div>
          ) : (
            <svg
              viewBox={`0 0 ${chartPoints.length * 10 + 8} ${PING_CHART_HEIGHT}`}
              preserveAspectRatio="none"
              className="w-full h-full drop-shadow-sm"
            >
              {[0.25, 0.5, 0.75].map((line) => (
                <line
                  key={line}
                  x1="0"
                  y1={(1 - line) * PING_CHART_HEIGHT}
                  x2={chartPoints.length * 10 + 8}
                  y2={(1 - line) * PING_CHART_HEIGHT}
                  className="stroke-[var(--border-dim)] stroke-1"
                  strokeDasharray="3 3"
                />
              ))}
              {chartPoints.map((ms, index) => {
                if (ms === 0) return null;
                const x = index * 10 + 4;
                const barHeight = (ms / chartMax) * (PING_CHART_HEIGHT - 4);
                return (
                  <rect
                    key={`${index}-${ms}`}
                    x={x}
                    y={PING_CHART_HEIGHT - barHeight}
                    width="8"
                    height={Math.max(2, barHeight)}
                    rx="2"
                    className="fill-blue-500/70 dark:fill-blue-400/80 transition-all duration-300 hover:opacity-80"
                  />
                );
              })}
            </svg>
          )}
        </div>
      </div>
    </section>
  );
};
