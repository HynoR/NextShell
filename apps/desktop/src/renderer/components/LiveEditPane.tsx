import { useCallback, useEffect, useMemo } from "react";
import { Spin, Tag, Tooltip, message } from "antd";
import type { ConnectionProfile } from "@nextshell/core";
import { useEditSessionStore } from "../store/useEditSessionStore";
import { PanelSkeleton } from "./LoadingSkeletons";

interface LiveEditPaneProps {
  connections: ConnectionProfile[];
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

export const LiveEditPane = ({ connections }: LiveEditPaneProps) => {
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

  // Periodically refresh to keep "time ago" fresh + catch any missed events
  useEffect(() => {
    const timer = setInterval(() => {
      void fetchSessions();
    }, 30_000);
    return () => clearInterval(timer);
  }, [fetchSessions]);

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
    <div className="le-pane">
      {/* header – compact control bar */}
      <div className="le-header">
        <div className="le-header-left">
          <i className="ri-eye-line le-header-icon" aria-hidden="true" />
          <span className="le-header-title">实时编辑</span>
          {sessions.length > 0 && (
            <span className="le-badge">{sessions.length}</span>
          )}
        </div>
        {sessions.length > 0 && (
          <button
            className="le-stop-all-btn"
            onClick={handleStopAll}
            title="停止所有监听"
          >
            <i className="ri-stop-circle-line" aria-hidden="true" />
            <span>全部断开</span>
          </button>
        )}
      </div>

      {/* content */}
      {loading && sessions.length === 0 ? (
        <PanelSkeleton rows={3} compact className="le-empty" />
      ) : sessions.length === 0 ? (
        <div className="le-empty">
          <i className="ri-file-edit-line le-empty-icon" aria-hidden="true" />
          <span className="le-empty-text">暂无正在编辑的远程文件</span>
          <span className="le-empty-hint">
            在 SFTP 文件列表中双击文件或右键「远程编辑」即可开始
          </span>
        </div>
      ) : (
        <div className="le-list">
          {sessions.map((session) => {
            const conn = connMap.get(session.connectionId);
            const fileName = session.remotePath.split("/").pop() || session.remotePath;
            const dirPath = session.remotePath.slice(0, session.remotePath.lastIndexOf("/")) || "/";
            const tag = statusTag(session.status);

            return (
              <div key={session.editId} className="le-item">
                <div className="le-item-header">
                  <div className="le-item-name">
                    <i className="ri-file-code-line le-file-icon" aria-hidden="true" />
                    <Tooltip title={session.remotePath}>
                      <span className="le-file-name">{fileName}</span>
                    </Tooltip>
                    <Tag color={tag.color} className="le-status-tag">
                      {tag.text}
                    </Tag>
                  </div>
                  <button
                    className="le-close-btn"
                    onClick={() => handleStop(session.editId)}
                    title="停止监听并清理本地临时文件"
                  >
                    <i className="ri-close-circle-line" aria-hidden="true" />
                  </button>
                </div>
                <div className="le-item-meta">
                  <span className="le-meta-row">
                    <i className="ri-server-line" aria-hidden="true" />
                    {conn ? conn.name : session.connectionId.slice(0, 8)}
                  </span>
                  <span className="le-meta-row">
                    <i className="ri-folder-3-line" aria-hidden="true" />
                    <Tooltip title={dirPath}>
                      <span className="le-meta-path">{dirPath}</span>
                    </Tooltip>
                  </span>
                  <span className="le-meta-row le-meta-time">
                    <i className="ri-time-line" aria-hidden="true" />
                    {formatTimeAgo(session.lastActivityAt)}
                  </span>
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
};
