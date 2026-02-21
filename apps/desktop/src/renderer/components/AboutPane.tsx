import { useCallback, useEffect, useRef, useState } from "react";
import { Button, Tag, Tooltip, message } from "antd";
import type { DebugLogEntry, UpdateCheckResult } from "@nextshell/shared";

const appVersion = __APP_VERSION__;
const githubRepo = __GITHUB_REPO__;
const normalizedRepo = githubRepo.trim();
const hasRepo = normalizedRepo.length > 0;

const displayRepo = hasRepo ? normalizedRepo : "owner/repo";
const displayRepoUrl = `https://github.com/${displayRepo}`;
const licenseUrl = `https://github.com/${displayRepo}/blob/main/LICENSE`;

const DEBUG_MAX_ENTRIES = 300;

const formatTimestamp = (ts: number): string => {
  const d = new Date(ts);
  const hh = String(d.getHours()).padStart(2, "0");
  const mm = String(d.getMinutes()).padStart(2, "0");
  const ss = String(d.getSeconds()).padStart(2, "0");
  const ms = String(d.getMilliseconds()).padStart(3, "0");
  return `${hh}:${mm}:${ss}.${ms}`;
};

const truncateCommand = (cmd: string, maxLen = 80): string => {
  return cmd.length > maxLen ? `${cmd.slice(0, maxLen)}…` : cmd;
};

export const AboutPane = () => {
  const [checking, setChecking] = useState(false);
  const [result, setResult] = useState<UpdateCheckResult | null>(null);
  const releaseUrl = result?.hasUpdate ? result.releaseUrl : null;

  const [debugEnabled, setDebugEnabled] = useState(false);
  const [debugLogs, setDebugLogs] = useState<DebugLogEntry[]>([]);
  const [autoScroll, setAutoScroll] = useState(true);
  const logBoxRef = useRef<HTMLDivElement>(null);
  const autoScrollRef = useRef(autoScroll);
  autoScrollRef.current = autoScroll;

  const handleOpenLink = useCallback(async (url: string) => {
    const openResult = await window.nextshell.dialog.openPath({ path: url, revealInFolder: false });
    if (!openResult.ok) {
      void message.error(openResult.error ? `打开链接失败: ${openResult.error}` : "打开链接失败");
    }
  }, []);

  const handleCheckUpdate = useCallback(async () => {
    setChecking(true);
    try {
      const res = await window.nextshell.about.checkUpdate();
      setResult(res);
      if (res.error) {
        void message.warning(`检查更新失败: ${res.error}`);
      } else if (res.hasUpdate) {
        void message.success(`发现新版本 ${res.latestVersion}`);
      } else {
        void message.info("当前已是最新版本");
      }
    } catch {
      void message.error("检查更新失败");
    } finally {
      setChecking(false);
    }
  }, []);

  const handleToggleDebug = useCallback(async () => {
    if (debugEnabled) {
      await window.nextshell.debug.disableLog();
      setDebugEnabled(false);
    } else {
      setDebugLogs([]);
      await window.nextshell.debug.enableLog();
      setDebugEnabled(true);
    }
  }, [debugEnabled]);

  const handleClearLogs = useCallback(() => {
    setDebugLogs([]);
  }, []);

  useEffect(() => {
    if (!debugEnabled) return;

    const unsub = window.nextshell.debug.onLogEvent((entry) => {
      setDebugLogs((prev) => {
        const next = prev.length >= DEBUG_MAX_ENTRIES
          ? [...prev.slice(-(DEBUG_MAX_ENTRIES - 1)), entry]
          : [...prev, entry];
        return next;
      });
    });

    return () => {
      unsub();
    };
  }, [debugEnabled]);

  useEffect(() => {
    if (autoScrollRef.current && logBoxRef.current) {
      logBoxRef.current.scrollTop = logBoxRef.current.scrollHeight;
    }
  }, [debugLogs]);

  const handleLogScroll = useCallback(() => {
    const el = logBoxRef.current;
    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
    setAutoScroll(atBottom);
  }, []);

  return (
    <div className="about-pane">
      <div className="about-header">
        <div className="about-logo">
          <i className="ri-terminal-box-fill" aria-hidden="true" />
          <span className="about-app-name">NextShell</span>
          <Tag color="blue">v{appVersion}</Tag>
        </div>
        <p className="about-desc">现代化的 SSH 终端管理工具</p>
      </div>

      <div className="about-section">
        <div className="about-row">
          <span className="about-label">
            <i className="ri-github-fill" aria-hidden="true" /> GitHub 仓库
          </span>
          {hasRepo ? (
            <a
              className="about-link"
              href={displayRepoUrl}
              target="_blank"
              rel="noopener noreferrer"
              onClick={(e) => {
                e.preventDefault();
                void handleOpenLink(displayRepoUrl);
              }}
            >
              {displayRepo}
              <i className="ri-external-link-line" aria-hidden="true" />
            </a>
          ) : (
            <span className="about-value about-placeholder">{displayRepo}</span>
          )}
        </div>

        <div className="about-row">
          <span className="about-label">
            <i className="ri-information-line" aria-hidden="true" /> 当前版本
          </span>
          <span className="about-value">v{appVersion}</span>
        </div>

        <div className="about-row">
          <span className="about-label">
            <i className="ri-book-2-line" aria-hidden="true" /> License
          </span>
          {hasRepo ? (
            <a
              className="about-link"
              href={licenseUrl}
              target="_blank"
              rel="noopener noreferrer"
              onClick={(e) => {
                e.preventDefault();
                void handleOpenLink(licenseUrl);
              }}
            >
              GNU General Public License v3.0
              <i className="ri-external-link-line" aria-hidden="true" />
            </a>
          ) : (
            <span className="about-value about-placeholder">GNU General Public License v3.0</span>
          )}
        </div>

        <div className="about-row">
          <span className="about-label">
            <i className="ri-download-cloud-line" aria-hidden="true" /> 最新版本
          </span>
          {result?.latestVersion ? (
            <span className="about-value">
              {result.latestVersion}
              {result.hasUpdate ? (
                <Tag color="green" style={{ marginLeft: 8 }}>有更新</Tag>
              ) : (
                <Tag color="default" style={{ marginLeft: 8 }}>已是最新</Tag>
              )}
            </span>
          ) : (
            <span className="about-value about-placeholder">—</span>
          )}
        </div>

        {releaseUrl ? (
          <div className="about-row">
            <span className="about-label">
              <i className="ri-links-line" aria-hidden="true" /> 下载地址
            </span>
            <a
              className="about-link"
              href={releaseUrl}
              target="_blank"
              rel="noopener noreferrer"
              onClick={(e) => {
                e.preventDefault();
                void handleOpenLink(releaseUrl);
              }}
            >
              前往 Release 页面
              <i className="ri-external-link-line" aria-hidden="true" />
            </a>
          </div>
        ) : null}
      </div>

      <div className="about-actions">
        <Tooltip title={!hasRepo ? "未配置 GitHub 仓库，无法检查更新" : undefined}>
          <Button
            type="primary"
            icon={<i className="ri-refresh-line" aria-hidden="true" />}
            loading={checking}
            disabled={!hasRepo}
            onClick={() => void handleCheckUpdate()}
          >
            检查更新
          </Button>
        </Tooltip>
        <Tooltip title={debugEnabled ? "关闭后台 Shell 日志监听" : "开启后台 Shell 日志监听，实时查看数据采集命令执行情况"}>
          <Button
            type={debugEnabled ? "default" : "dashed"}
            danger={debugEnabled}
            icon={<i className={debugEnabled ? "ri-stop-circle-line" : "ri-bug-line"} aria-hidden="true" />}
            onClick={() => void handleToggleDebug()}
          >
            {debugEnabled ? "停止 Debug" : "Debug 日志"}
          </Button>
        </Tooltip>
      </div>

      {debugEnabled && (
        <div className="about-debug-section">
          <div className="about-debug-header">
            <span className="about-debug-title">
              <i className="ri-terminal-line" aria-hidden="true" />
              Hidden Shell 执行日志
              <Tag color="processing" style={{ marginLeft: 8 }}>实时</Tag>
              <span className="about-debug-count">{debugLogs.length} 条</span>
            </span>
            <div className="about-debug-actions">
              <Tooltip title={autoScroll ? "已开启自动滚动" : "点击启用自动滚动"}>
                <button
                  className={`about-debug-icon-btn ${autoScroll ? "active" : ""}`}
                  onClick={() => {
                    setAutoScroll(true);
                    if (logBoxRef.current) {
                      logBoxRef.current.scrollTop = logBoxRef.current.scrollHeight;
                    }
                  }}
                  aria-label="自动滚动"
                >
                  <i className="ri-arrow-down-double-line" aria-hidden="true" />
                </button>
              </Tooltip>
              <Tooltip title="清空日志">
                <button
                  className="about-debug-icon-btn"
                  onClick={handleClearLogs}
                  aria-label="清空日志"
                >
                  <i className="ri-delete-bin-line" aria-hidden="true" />
                </button>
              </Tooltip>
            </div>
          </div>
          <div
            className="about-debug-log-box"
            ref={logBoxRef}
            onScroll={handleLogScroll}
          >
            {debugLogs.length === 0 ? (
              <div className="about-debug-empty">
                等待 Hidden Shell 命令执行…
              </div>
            ) : (
              debugLogs.map((entry) => (
                <div key={entry.id} className={`about-debug-entry ${entry.ok ? "ok" : "fail"}`}>
                  <div className="about-debug-entry-header">
                    <span className="about-debug-time">{formatTimestamp(entry.timestamp)}</span>
                    <span className={`about-debug-badge ${entry.ok ? "ok" : "fail"}`}>
                      {entry.ok ? "OK" : `ERR ${entry.exitCode}`}
                    </span>
                    <span className="about-debug-duration">{entry.durationMs}ms</span>
                    <span className="about-debug-conn" title={entry.connectionId}>
                      {entry.connectionId.slice(0, 8)}
                    </span>
                  </div>
                  <div className="about-debug-cmd" title={entry.command}>
                    <i className="ri-terminal-line" aria-hidden="true" />
                    {truncateCommand(entry.command)}
                  </div>
                  {entry.error ? (
                    <div className="about-debug-error">{entry.error}</div>
                  ) : entry.stdout ? (
                    <pre className="about-debug-stdout">{entry.stdout}</pre>
                  ) : null}
                </div>
              ))
            )}
          </div>
        </div>
      )}
    </div>
  );
};
