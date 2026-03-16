import { Tag } from "antd";
import type { AiExecutionProgress as ProgressType } from "@nextshell/core";

interface AiExecutionProgressProps {
  progress: ProgressType;
}

const STATUS_ICON: Record<string, string> = {
  pending: "ri-time-line",
  running: "ri-loader-4-line",
  success: "ri-check-line",
  failed: "ri-close-line",
  skipped: "ri-skip-forward-line",
};

const STATUS_COLOR: Record<string, string> = {
  pending: "default",
  running: "processing",
  success: "success",
  failed: "error",
  skipped: "warning",
};

export const AiExecutionProgressCard = ({
  progress,
}: AiExecutionProgressProps) => {
  return (
    <div className="ai-progress-card">
      <div className="ai-progress-header">
        <i className={progress.completed ? "ri-check-double-line" : "ri-loader-4-line ai-spin"} />
        <span>{progress.completed ? "执行完成" : "正在执行..."}</span>
      </div>
      <div className="ai-progress-steps">
        {progress.steps.map((step) => (
          <div key={step.step} className={`ai-progress-step status-${step.status}`}>
            <i className={STATUS_ICON[step.status] ?? "ri-question-line"} />
            <Tag color={STATUS_COLOR[step.status] ?? "default"}>
              步骤 {step.step}
            </Tag>
            {step.output && (
              <pre className="ai-progress-output">
                {step.output.slice(0, 500)}
              </pre>
            )}
            {step.error && (
              <div className="ai-progress-error">{step.error}</div>
            )}
          </div>
        ))}
      </div>
    </div>
  );
};
