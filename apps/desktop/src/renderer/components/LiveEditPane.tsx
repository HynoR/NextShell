import { useCallback, useEffect, useMemo } from "react";
import { Button, Tag, Tooltip, message } from "antd";
import type { ConnectionProfile } from "@nextshell/core";
import { useScheduledPoll } from "../hooks/useScheduledPoll";
import { useEditSessionStore } from "../store/useEditSessionStore";
import { PanelSkeleton } from "./LoadingSkeletons";

interface LiveEditPaneProps {
  connections: ConnectionProfile[];
  /** When true, panel is expanded and polling is enabled */
  active: boolean;
  collapsed: boolean;
  onToggle: () => void;
}

const formatTimeAgo = (timestamp: number): string => {
  const diff = Date.now() - timestamp;
  if (diff < 60_000) return "刚刚";
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)} 分钟前`;
  return `${Math.floor(diff / 3_600_000)} 小时前`;
};

const statusTag = (status: string): { color: string; text: string } => {
  switch (status) {
    case "uploading":
      return { color: "processing", text: "同步中" };
    case "editing":
      return { color: "cyan", text: "监听中" };
    default:
      return { color: "default", text: status };
  }
};

export const LiveEditPane = ({ connections, active, collapsed, onToggle }: LiveEditPaneProps) => {
  const { sessions, loading, fetchSessions, applyEvent, stopSession, stopAllSessions } =
    useEditSessionStore();

  // Subscribe to edit status events from main process
  useEffect(() => {
    void fetchSessions();
    const unsub = window.nextshell.sftp.onEditStatus((event) => {
      applyEvent(event);
    });
    return unsub;
  }, [applyEvent, fetchSessions]);

  useScheduledPoll({
    enabled: active,
    intervalMs: 30_000,
    task: async () => {
      await fetchSessions();
    }
  });

  const connMap = useMemo(() => {
    const map = new Map<string, ConnectionProfile>();
    for (const conn of connections) {
      map.set(conn.id, conn);
    }
    return map;
  }, [connections]);

  const handleStop = useCallback(
    (editId: string) => {
      void stopSession(editId);
      message.info("已停止监听");
    },
    [stopSession]
  );

  const handleStopAll = useCallback(() => {
    if (sessions.length === 0) return;
    void stopAllSessions();
    message.info("已停止所有监听");
  }, [sessions.length, stopAllSessions]);

  return (
    <section className="live-edit-panel">
      <div className="live-edit-panel-header" onClick={onToggle}>
        <i className={collapsed ? "ri-arrow-right-s-line" : "ri-arrow-down-s-line"} aria-hidden="true" />
        <span className="live-edit-panel-title">实时编辑</span>
        <div className="live-edit-header-right" onClick={(e) => e.stopPropagation()}>
          {collapsed && sessions.length > 0 ? (
            <span className="live-edit-summary">{sessions.length} 个监听</span>
          ) : !collapsed && sessions.length > 0 ? (
            <Button
              type="text"
              size="small"
              className="live-edit-stop-all-btn"
              onClick={handleStopAll}
            >
              全部断开
            </Button>
          ) : null}
        </div>
      </div>

      {!collapsed ? (
        <div className="live-edit-list">
          {loading && sessions.length === 0 ? (
            <PanelSkeleton rows={3} compact className="live-edit-empty" />
          ) : sessions.length === 0 ? (
            <div className="live-edit-empty">
              <i className="ri-file-edit-line live-edit-empty-icon" aria-hidden="true" />
              <span className="live-edit-empty-text">暂无正在编辑的远程文件</span>
              <span className="live-edit-empty-hint">
                在 SFTP 文件列表中双击文件或右键「远程编辑」即可开始
              </span>
            </div>
          ) : (
            sessions.map((session) => {
              const conn = connMap.get(session.connectionId);
              const fileName = session.remotePath.split("/").pop() || session.remotePath;
              const dirPath = session.remotePath.slice(0, session.remotePath.lastIndexOf("/")) || "/";
              const tag = statusTag(session.status);

              return (
                <div key={session.editId} className="live-edit-item">
                  <div className="live-edit-item-header">
                    <div className="live-edit-item-name">
                      <i className="ri-file-code-line live-edit-file-icon" aria-hidden="true" />
                      <Tooltip title={session.remotePath}>
                        <span className="live-edit-file-name">{fileName}</span>
                      </Tooltip>
                      <Tag color={tag.color} bordered={false} className="live-edit-task-tag">
                        {tag.text}
                      </Tag>
                    </div>
                    <button
                      type="button"
                      className="live-edit-close-btn"
                      onClick={() => handleStop(session.editId)}
                      title="停止监听并清理本地临时文件"
                    >
                      <i className="ri-close-circle-line" aria-hidden="true" />
                    </button>
                  </div>
                  <div className="live-edit-item-meta">
                    <span className="live-edit-meta-row">
                      <i className="ri-server-line" aria-hidden="true" />
                      {conn ? conn.name : session.connectionId.slice(0, 8)}
                    </span>
                    <span className="live-edit-meta-row">
                      <i className="ri-folder-3-line" aria-hidden="true" />
                      <Tooltip title={dirPath}>
                        <span className="live-edit-meta-path">{dirPath}</span>
                      </Tooltip>
                    </span>
                    <span className="live-edit-meta-row live-edit-meta-time">
                      <i className="ri-time-line" aria-hidden="true" />
                      {formatTimeAgo(session.lastActivityAt)}
                    </span>
                  </div>
                </div>
              );
            })
          )}
        </div>
      ) : null}
    </section>
  );
};
