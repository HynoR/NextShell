import { useEffect, useRef, useState } from "react";

const PING_INTERVAL_MS = 5000;
const PING_CHART_HEIGHT = 84;
const PING_HISTORY_CAP = 50;

interface PingCardProps {
  /** 当前选中连接的 host，无则不显示卡片 */
  host?: string;
}

/** 折叠时展示的摘要文案 */
function summaryLine(
  result: { ok: true; avgMs: number } | { ok: false; error: string } | null,
  loading: boolean
): string {
  if (loading && !result) return "检测中…";
  if (result && result.ok === true) {
    const ms = result.avgMs;
    return ms === 0 ? "0 ms" : ms < 1 ? "<1 ms" : `${Math.round(ms)} ms`;
  }
  if (result && !result.ok) return result.error;
  return "展开以启用 Ping";
}

export const PingCard = ({ host }: PingCardProps) => {
  const [collapsed, setCollapsed] = useState(true);
  const [result, setResult] = useState<
    { ok: true; avgMs: number } | { ok: false; error: string } | null
  >(null);
  const [loading, setLoading] = useState(false);
  const [pingHistory, setPingHistory] = useState<number[]>([]);
  /** 当前 host 下是否已因「展开」而启用过 ping；换 host 时重置，折叠不重置 */
  const pingEnabledForHostRef = useRef(false);
  const prevHostRef = useRef<string | undefined>(undefined);

  useEffect(() => {
    if (!host || !window.nextshell?.ping?.probe) {
      setResult(null);
      setPingHistory([]);
      return;
    }
    if (prevHostRef.current !== host) {
      prevHostRef.current = host;
      pingEnabledForHostRef.current = false;
    }
    const shouldRun =
      !collapsed || pingEnabledForHostRef.current;
    if (!shouldRun) {
      setResult(null);
      setPingHistory([]);
      return;
    }
    if (!collapsed) {
      pingEnabledForHostRef.current = true;
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
  }, [host, collapsed]);

  if (!host) {
    return null;
  }

  const chartMax = Math.max(50, ...pingHistory);

  // Fixed 50-slot queue: always render 50 columns, right-aligned (newest on right)
  const slots: number[] = Array.from<number>({ length: PING_HISTORY_CAP }).fill(0);
  const offset = PING_HISTORY_CAP - pingHistory.length;
  for (let i = 0; i < pingHistory.length; i++) {
    slots[offset + i] = pingHistory[i]!;
  }

  const CHART_WIDTH = PING_HISTORY_CAP * 10 + 8;

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
        <span className="text-[10px] font-semibold tracking-[0.08em] uppercase text-[var(--t3)]">
          Ping 延迟
        </span>
        {collapsed ? (
          <span className="monitor-summary">{summaryLine(result, loading)}</span>
        ) : null}
      </button>

      {!collapsed ? (
        <div className="monitor-panel-body">
          <div className="flex items-center gap-2 text-[11px]">
            {loading && !result ? (
              <span className="text-[var(--t3)]">检测中…</span>
            ) : result?.ok === true ? (
              <>
                <span className="font-mono font-semibold text-[var(--t1)]">
                  {result.avgMs === 0 ? "0" : result.avgMs < 1 ? "<1" : Math.round(result.avgMs)} ms
                </span>
                <span className="text-[10px] text-[var(--t3)]">每 5 秒刷新</span>
              </>
            ) : (
              <span className="text-[var(--t3)]">展开后启用 Ping，折叠不停止</span>
            )}
          </div>

          <div className="mt-2 h-[90px] rounded-lg border border-[var(--border-dim)] bg-black/[0.03] dark:bg-white/[0.02] pl-[34px] pr-2 py-2 relative overflow-hidden group shadow-inner">
            {pingHistory.length === 0 ? (
              <div className="w-full h-full flex items-center justify-center text-[11px] text-[var(--t3)] animate-pulse">
                等待 Ping 采样数据...
              </div>
            ) : (
              <>
                <span className="absolute left-1 text-[9px] font-mono text-[var(--t3)] leading-none" style={{ top: "8px" }}>{Math.round(chartMax)} ms</span>
                <span className="absolute left-1 text-[9px] font-mono text-[var(--t3)] leading-none" style={{ top: "50%", transform: "translateY(-50%)" }}>{Math.round(chartMax / 2)}</span>
                <span className="absolute left-1 text-[9px] font-mono text-[var(--t3)] leading-none" style={{ bottom: "8px" }}>0</span>
                <svg
                  viewBox={`0 0 ${CHART_WIDTH} ${PING_CHART_HEIGHT}`}
                  preserveAspectRatio="none"
                  className="w-full h-full drop-shadow-sm"
                >
                  {[0.25, 0.5, 0.75].map((line) => (
                    <line
                      key={line}
                      x1="0"
                      y1={(1 - line) * PING_CHART_HEIGHT}
                      x2={CHART_WIDTH}
                      y2={(1 - line) * PING_CHART_HEIGHT}
                      className="stroke-[var(--border-dim)] stroke-1"
                      strokeDasharray="3 3"
                    />
                  ))}
                  {slots.map((ms, index) => {
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
              </>
            )}
          </div>
        </div>
      ) : null}
    </section>
  );
};
