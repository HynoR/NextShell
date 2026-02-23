import { useCallback, useEffect, useRef, useState } from "react";
import { Button, Typography } from "antd";
import type { ConnectionProfile } from "@nextshell/core";
import type { TracerouteEvent } from "@nextshell/shared";

// ─── ANSI colour renderer ────────────────────────────────────────────────────

// Map of SGR colour codes → CSS colour values.
// Covers standard 8 + bright 8 foreground (30-37, 90-97) and background (40-47, 100-107).
const ANSI_FG: Record<number, string> = {
  30: "#4c4c4c", 31: "#ff5555", 32: "#50fa7b", 33: "#f1fa8c",
  34: "#6272a4", 35: "#ff79c6", 36: "#8be9fd", 37: "#f8f8f2",
  90: "#6272a4", 91: "#ff6e6e", 92: "#69ff94", 93: "#ffffa5",
  94: "#d6acff", 95: "#ff92df", 96: "#a4ffff", 97: "#ffffff",
};
const ANSI_BG: Record<number, string> = {
  40: "#4c4c4c", 41: "#ff5555", 42: "#50fa7b", 43: "#f1fa8c",
  44: "#6272a4", 45: "#ff79c6", 46: "#8be9fd", 47: "#f8f8f2",
  100: "#6272a4", 101: "#ff6e6e", 102: "#69ff94", 103: "#ffffa5",
  104: "#d6acff", 105: "#ff92df", 106: "#a4ffff", 107: "#ffffff",
};

interface Span { text: string; color?: string; bg?: string; bold?: boolean; dim?: boolean; }

/** Parse a single raw line (may contain ANSI escape sequences) into styled spans. */
const parseAnsi = (raw: string): Span[] => {
  const result: Span[] = [];
  // Strip OSC sequences (e.g. hyperlinks: ESC ] ... ESC \ or BEL)
  const stripped = raw.replace(/\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)/g, "");
  // Tokenise: plain text | ESC [ ... m (SGR) | other ESC sequences (discard)
  const re = /([^\x1b]+)|\x1b\[([0-9;]*)m|\x1b[^m]/g;
  let fg: string | undefined;
  let bg: string | undefined;
  let bold = false;
  let dim = false;
  let match: RegExpExecArray | null;

  while ((match = re.exec(stripped)) !== null) {
    const text = match[1];
    const csi = match[2];

    if (text !== undefined && text.length > 0) {
      result.push({ text, color: fg, bg, bold, dim });
      continue;
    }

    if (csi === undefined) continue; // other ESC sequence, skip

    const codes = csi.split(";").map(Number);
    let i = 0;
    while (i < codes.length) {
      const c = codes[i]!;
      if (c === 0) { fg = undefined; bg = undefined; bold = false; dim = false; }
      else if (c === 1) { bold = true; }
      else if (c === 2) { dim = true; }
      else if (c === 22) { bold = false; dim = false; }
      else if (c === 38) {
        // 256-colour or truecolor fg
        if (codes[i + 1] === 5 && codes[i + 2] !== undefined) {
          fg = ansi256(codes[i + 2]!); i += 2;
        } else if (codes[i + 1] === 2 && codes[i + 4] !== undefined) {
          fg = `rgb(${codes[i + 2]},${codes[i + 3]},${codes[i + 4]})`; i += 4;
        }
      } else if (c === 48) {
        // 256-colour or truecolor bg
        if (codes[i + 1] === 5 && codes[i + 2] !== undefined) {
          bg = ansi256(codes[i + 2]!); i += 2;
        } else if (codes[i + 1] === 2 && codes[i + 4] !== undefined) {
          bg = `rgb(${codes[i + 2]},${codes[i + 3]},${codes[i + 4]})`; i += 4;
        }
      } else if (c >= 30 && c <= 37) { fg = ANSI_FG[c]; }
      else if (c >= 40 && c <= 47) { bg = ANSI_BG[c]; }
      else if (c >= 90 && c <= 97) { fg = ANSI_FG[c]; }
      else if (c >= 100 && c <= 107) { bg = ANSI_BG[c]; }
      else if (c === 39) { fg = undefined; }
      else if (c === 49) { bg = undefined; }
      i++;
    }
  }
  return result;
};

/** Convert 256-colour index → CSS hex string. */
const ansi256 = (n: number): string => {
  if (n < 16) {
    // Standard palette (reuse ANSI_FG + add index 8-15 aliases)
    const base = [
      "#4c4c4c","#ff5555","#50fa7b","#f1fa8c","#6272a4","#ff79c6","#8be9fd","#f8f8f2",
      "#6272a4","#ff6e6e","#69ff94","#ffffa5","#d6acff","#ff92df","#a4ffff","#ffffff",
    ];
    return base[n] ?? "#f8f8f2";
  }
  if (n < 232) {
    const idx = n - 16;
    const r = Math.floor(idx / 36) * 51;
    const g = Math.floor((idx % 36) / 6) * 51;
    const b = (idx % 6) * 51;
    return `rgb(${r},${g},${b})`;
  }
  // Greyscale ramp 232-255
  const v = 8 + (n - 232) * 10;
  return `rgb(${v},${v},${v})`;
};

interface AnsiLineProps { raw: string; }

const AnsiLine = ({ raw }: AnsiLineProps) => {
  const spans = parseAnsi(raw);
  if (spans.length === 0) return <br />;
  return (
    <span>
      {spans.map((s, i) => {
        const style: React.CSSProperties = {};
        if (s.color) style.color = s.color;
        if (s.bg) style.backgroundColor = s.bg;
        if (s.bold) style.fontWeight = "bold";
        if (s.dim) style.opacity = 0.6;
        return (
          <span key={i} style={Object.keys(style).length > 0 ? style : undefined}>
            {s.text}
          </span>
        );
      })}
    </span>
  );
};

// ─── TraceroutePane ──────────────────────────────────────────────────────────

interface TraceroutePaneProps {
  connection?: ConnectionProfile;
  connected: boolean;
}

export const TraceroutePane = ({ connection, connected }: TraceroutePaneProps) => {
  const [lines, setLines] = useState<string[]>([]);
  const [running, setRunning] = useState(false);
  const [error, setError] = useState<string>();
  const outputRef = useRef<HTMLPreElement>(null);
  const prevHostRef = useRef<string | undefined>(undefined);

  // Reset output when connection changes
  useEffect(() => {
    if (connection?.host !== prevHostRef.current) {
      prevHostRef.current = connection?.host;
      setLines([]);
      setError(undefined);
    }
  }, [connection?.host]);

  // Auto-scroll to bottom
  useEffect(() => {
    const el = outputRef.current;
    if (el) {
      el.scrollTop = el.scrollHeight;
    }
  }, [lines]);

  // Subscribe to traceroute events
  useEffect(() => {
    const unsubscribe = window.nextshell.traceroute.onData((event: TracerouteEvent) => {
      switch (event.type) {
        case "data":
          setLines((prev) => [...prev, event.line]);
          break;
        case "done":
          setRunning(false);
          break;
        case "error":
          setError(event.message);
          setRunning(false);
          break;
      }
    });
    return () => {
      unsubscribe();
    };
  }, []);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      void window.nextshell.traceroute.stop();
    };
  }, []);

  const handleStart = useCallback(async () => {
    if (!connection?.host) return;
    setLines([]);
    setError(undefined);
    setRunning(true);
    try {
      await window.nextshell.traceroute.run({ host: connection.host });
    } catch (err) {
      setError(err instanceof Error ? err.message : "启动路由追踪失败");
      setRunning(false);
    }
  }, [connection?.host]);

  const handleStop = useCallback(async () => {
    try {
      await window.nextshell.traceroute.stop();
    } catch {
      // ignore
    }
    setRunning(false);
  }, []);

  if (!connection) {
    return (
      <Typography.Text className="text-[var(--t3)]">
        先选择一个连接再使用路由追踪。
      </Typography.Text>
    );
  }

  if (!connected) {
    return (
      <Typography.Text className="text-[var(--t3)]">
        当前连接未建立会话，请双击左侧服务器建立 SSH 连接。
      </Typography.Text>
    );
  }

  return (
    <div className="flex flex-col h-full overflow-hidden gap-2 p-2">
      <div className="flex items-center gap-2 shrink-0">
        <Typography.Text strong style={{ fontSize: 13 }}>
          目标: {connection.host}
        </Typography.Text>
        {running ? (
          <Button size="small" danger onClick={() => void handleStop()}>
            停止
          </Button>
        ) : (
          <Button size="small" type="primary" onClick={() => void handleStart()}>
            开始追踪
          </Button>
        )}
        {running && (
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            追踪中...
          </Typography.Text>
        )}
      </div>
      {error && (
        <Typography.Text type="danger" style={{ fontSize: 12 }}>
          {error}
        </Typography.Text>
      )}
      <pre ref={outputRef} className="traceroute-output">
        {lines.length === 0 ? (
          running ? null : <span className="traceroute-placeholder">点击「开始追踪」运行 nexttrace</span>
        ) : (
          lines.map((line, i) => (
            <span key={i} className="traceroute-line">
              <AnsiLine raw={line} />
              {"\n"}
            </span>
          ))
        )}
      </pre>
    </div>
  );
};
