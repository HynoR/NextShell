import { Button, Space, Tag } from "antd";
import type { AiExecutionProgress as ProgressType } from "@nextshell/core";
import type { ExecutionPhase } from "../../store/useAiChatStore";

interface AiExecutionProgressProps {
  progress: ProgressType;
  phase?: ExecutionPhase;
  retrySourceStep?: number;
  canResume?: boolean;
  onRetry?: () => void;
  onEditRetryPlan?: () => void;
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

const PHASE_CONFIG: Record<ExecutionPhase, { icon: string; text: string }> = {
  executing: { icon: "ri-terminal-box-line ai-spin", text: "正在执行命令" },
  collecting: { icon: "ri-file-download-line ai-spin", text: "正在搜集结果" },
  analyzing: { icon: "ri-brain-line ai-spin", text: "正在提交 AI 分析" },
  receiving: { icon: "ri-robot-2-line ai-spin", text: "正在接收 AI 的结论" },
};

export const AiExecutionProgressCard = ({
  progress,
  phase,
  retrySourceStep,
  canResume = false,
  onRetry,
  onEditRetryPlan,
}: AiExecutionProgressProps) => {
  const totalSteps = progress.steps.length;
  const phaseInfo = phase ? PHASE_CONFIG[phase] : undefined;
  const hasFailedStep = progress.steps.some((step) => step.status === "failed");

  const headerIcon = progress.completed
    ? hasFailedStep
      ? "ri-error-warning-line"
      : "ri-check-double-line"
    : phaseInfo?.icon ?? "ri-loader-4-line ai-spin";

  let headerText: string;
  if (progress.completed) {
    headerText = hasFailedStep ? "执行失败" : "执行完成";
  } else if (phaseInfo) {
    const suffix = (phase === "executing" || phase === "collecting") && progress.currentStep > 0
      ? ` (${progress.currentStep}/${totalSteps})`
      : "";
    headerText = `${phaseInfo.text}${suffix}`;
  } else {
    headerText = "正在执行...";
  }

  return (
    <div className="ai-progress-card">
      <div className="ai-progress-header">
        <i className={headerIcon} />
        <span>{headerText}</span>
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
      {canResume && (onRetry || onEditRetryPlan) && (
        <div className="ai-plan-actions">
          <Space>
            {onRetry && (
              <Button type="primary" onClick={onRetry}>
                <i className="ri-refresh-line" /> {retrySourceStep
                  ? `从步骤 ${retrySourceStep} 继续`
                  : "重试失败步骤及后续"}
              </Button>
            )}
            {onEditRetryPlan && (
              <Button onClick={onEditRetryPlan}>
                <i className="ri-edit-2-line" /> 编辑后继续
              </Button>
            )}
          </Space>
        </div>
      )}
    </div>
  );
};
