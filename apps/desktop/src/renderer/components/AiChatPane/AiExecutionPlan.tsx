import { Button, Space, Tag } from "antd";
import type { AiExecutionPlan } from "@nextshell/core";

interface AiExecutionPlanProps {
  plan: AiExecutionPlan;
  onApprove: () => void;
  onReject: () => void;
}

export const AiExecutionPlanCard = ({
  plan,
  onApprove,
  onReject,
}: AiExecutionPlanProps) => {
  return (
    <div className="ai-plan-card">
      <div className="ai-plan-header">
        <i className="ri-file-list-3-line" />
        <span>执行计划</span>
      </div>
      {plan.summary && (
        <p className="ai-plan-summary">{plan.summary}</p>
      )}
      <div className="ai-plan-steps">
        {plan.steps.map((step) => (
          <div key={step.step} className={`ai-plan-step ${step.risky ? "risky" : ""}`}>
            <div className="ai-plan-step-header">
              <Tag color={step.risky ? "red" : "blue"}>
                {step.risky ? "危险" : `#${step.step}`}
              </Tag>
              <span className="ai-plan-step-desc">{step.description}</span>
            </div>
            <code className="ai-plan-step-cmd">{step.command}</code>
          </div>
        ))}
      </div>
      <div className="ai-plan-actions">
        <Space>
          <Button type="primary" onClick={onApprove}>
            <i className="ri-check-line" /> 批准执行
          </Button>
          <Button onClick={onReject}>
            修改计划
          </Button>
        </Space>
      </div>
    </div>
  );
};
