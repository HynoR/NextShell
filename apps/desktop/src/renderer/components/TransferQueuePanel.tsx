import { Button, Tag } from "antd";
import type { TransferTask } from "../store/useTransferQueueStore";

interface TransferQueuePanelProps {
  tasks: TransferTask[];
  collapsed: boolean;
  onToggle: () => void;
  onRetry: (taskId: string) => void;
  onClearFinished: () => void;
  onOpenLocalFile?: (task: TransferTask) => void;
}

const taskLabel = (task: TransferTask): string => {
  const remoteName = task.remotePath.split("/").pop() || task.remotePath;
  return `${task.direction === "upload" ? "上传" : "下载"} · ${remoteName}`;
};

const statusTag = (task: TransferTask): { color: string; text: string } => {
  switch (task.status) {
    case "running":
      return { color: "processing", text: "进行中" };
    case "success":
      return { color: "success", text: "完成" };
    case "failed":
      return { color: "error", text: "失败" };
    default:
      return { color: "default", text: "排队中" };
  }
};

export const TransferQueuePanel = ({
  tasks,
  collapsed,
  onToggle,
  onRetry,
  onClearFinished,
  onOpenLocalFile
}: TransferQueuePanelProps) => {
  const runningCount = tasks.filter((task) => task.status === "running").length;
  const failedCount = tasks.filter((task) => task.status === "failed").length;
  const finishedCount = tasks.filter((task) => task.status === "success").length;

  return (
    <section className="transfer-panel">
      <div className="transfer-panel-header" onClick={onToggle}>
        <i className={collapsed ? "ri-arrow-right-s-line" : "ri-arrow-down-s-line"} aria-hidden="true" />
        <span className="text-[10px] font-semibold tracking-[0.08em] uppercase text-[var(--t3)]">传输队列</span>
        <div className="transfer-header-right" onClick={(e) => e.stopPropagation()}>
          {collapsed && (runningCount > 0 || failedCount > 0 || finishedCount > 0) ? (
            <span className="transfer-summary">
              {runningCount > 0 ? `运行 ${runningCount} / ` : ""}失败 {failedCount} / 完成 {finishedCount}
            </span>
          ) : !collapsed ? (
            <Button
              type="text"
              size="small"
              className="transfer-clear-btn"
              onClick={onClearFinished}
              disabled={finishedCount === 0}
            >
              清理记录
            </Button>
          ) : null}
        </div>
      </div>

      {!collapsed ? (
        <div className="transfer-task-list">
          {tasks.length === 0 ? (
            <div className="transfer-empty">暂无传输任务</div>
          ) : (
            tasks.map((task) => {
              const tag = statusTag(task);
              const canOpenLocal =
                task.direction === "download" &&
                task.status === "success" &&
                Boolean(onOpenLocalFile);
              return (
                <div
                  key={task.id}
                  className={`transfer-task-item${canOpenLocal ? " openable" : ""}`}
                  title={canOpenLocal ? "双击打开本地文件" : undefined}
                  onDoubleClick={() => {
                    if (canOpenLocal) {
                      onOpenLocalFile?.(task);
                    }
                  }}
                >
                  <div className="transfer-task-main">
                    <span className="transfer-task-name" title={taskLabel(task)}>{taskLabel(task)}</span>
                    <Tag color={tag.color} bordered={false} className="transfer-task-tag">{tag.text}</Tag>
                  </div>
                  <div className="transfer-progress-row">
                    <div className="transfer-progress-track">
                      <div className="transfer-progress-fill" style={{ width: `${task.progress}%` }} />
                    </div>
                    <span>{Math.round(task.progress)}%</span>
                  </div>
                  {task.error ? <div className="transfer-task-error">{task.error}</div> : null}
                  <div className="transfer-task-paths">
                    <div className="path-row">
                      <span className="path-label">本地</span>
                      <span className="path-value" title={task.localPath}>{task.localPath}</span>
                    </div>
                    <div className="path-row">
                      <span className="path-label">远端</span>
                      <span className="path-value" title={task.remotePath}>{task.remotePath}</span>
                    </div>
                  </div>
                  {task.status === "failed" ? (
                    <div className="transfer-task-actions">
                      <Button size="small" type="primary" onClick={() => onRetry(task.id)}>
                        重试
                      </Button>
                    </div>
                  ) : null}
                </div>
              );
            })
          )}
        </div>
      ) : null}
    </section>
  );
};
