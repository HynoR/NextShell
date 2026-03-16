import { useState, useCallback } from "react";
import { Button, Space, Tag, Input, Tooltip } from "antd";
import type { AiExecutionPlan, AiExecutionStep } from "@nextshell/core";

interface AiExecutionPlanProps {
  plan: AiExecutionPlan;
  userRequest?: string;
  onApprove: (plan: AiExecutionPlan) => void;
  onReject: () => void;
  onAbort: () => void;
}

const createEmptyStep = (stepNumber: number): AiExecutionStep => ({
  step: stepNumber,
  command: "",
  description: "",
  risky: false,
});

const renumberSteps = (steps: AiExecutionStep[]): AiExecutionStep[] =>
  steps.map((s, i) => ({ ...s, step: i + 1 }));

export const AiExecutionPlanCard = ({
  plan,
  userRequest,
  onApprove,
  onReject,
  onAbort,
}: AiExecutionPlanProps) => {
  const [editing, setEditing] = useState(false);
  const [editSteps, setEditSteps] = useState<AiExecutionStep[]>([]);
  const [editSummary, setEditSummary] = useState("");

  const enterEdit = useCallback(() => {
    setEditSteps(plan.steps.map((s) => ({ ...s })));
    setEditSummary(plan.summary);
    setEditing(true);
  }, [plan]);

  const cancelEdit = useCallback(() => {
    setEditing(false);
  }, []);

  const updateStepField = useCallback(
    (index: number, field: keyof AiExecutionStep, value: string | boolean) => {
      setEditSteps((prev) =>
        prev.map((s, i) => (i === index ? { ...s, [field]: value } : s))
      );
    },
    []
  );

  const removeStep = useCallback((index: number) => {
    setEditSteps((prev) => renumberSteps(prev.filter((_, i) => i !== index)));
  }, []);

  const addStep = useCallback((afterIndex: number) => {
    setEditSteps((prev) => {
      const next = [...prev];
      next.splice(afterIndex + 1, 0, createEmptyStep(0));
      return renumberSteps(next);
    });
  }, []);

  const moveStep = useCallback((index: number, direction: "up" | "down") => {
    setEditSteps((prev) => {
      const target = direction === "up" ? index - 1 : index + 1;
      if (target < 0 || target >= prev.length) return prev;
      const next = [...prev];
      [next[index], next[target]] = [next[target]!, next[index]!];
      return renumberSteps(next);
    });
  }, []);

  const handleApproveEdited = useCallback(() => {
    const validSteps = editSteps.filter((s) => s.command.trim().length > 0);
    if (validSteps.length === 0) return;
    const editedPlan: AiExecutionPlan = {
      steps: renumberSteps(validSteps),
      summary: editSummary,
    };
    setEditing(false);
    onApprove(editedPlan);
  }, [editSteps, editSummary, onApprove]);

  if (editing) {
    return (
      <div className="ai-plan-card ai-plan-editing">
        {userRequest && (
          <div className="ai-plan-user-request">
            <i className="ri-user-3-line" />
            <span>{userRequest}</span>
          </div>
        )}
        <div className="ai-plan-header">
          <i className="ri-edit-2-line" />
          <span>编辑计划</span>
          <Tag color="orange" style={{ marginLeft: "auto" }}>编辑中</Tag>
        </div>

        <div className="ai-plan-edit-summary">
          <Input.TextArea
            value={editSummary}
            onChange={(e) => setEditSummary(e.target.value)}
            placeholder="计划摘要"
            autoSize={{ minRows: 1, maxRows: 3 }}
            size="small"
          />
        </div>

        <div className="ai-plan-steps">
          {editSteps.map((step, idx) => (
            <div key={`edit-${idx}`} className={`ai-plan-step-edit ${step.risky ? "risky" : ""}`}>
              <div className="ai-plan-step-edit-top">
                <Tag color={step.risky ? "red" : "blue"}>#{step.step}</Tag>
                <div className="ai-plan-step-edit-toolbar">
                  <Tooltip title="上移">
                    <button
                      type="button"
                      className="ai-plan-edit-btn"
                      disabled={idx === 0}
                      onClick={() => moveStep(idx, "up")}
                    >
                      <i className="ri-arrow-up-s-line" />
                    </button>
                  </Tooltip>
                  <Tooltip title="下移">
                    <button
                      type="button"
                      className="ai-plan-edit-btn"
                      disabled={idx === editSteps.length - 1}
                      onClick={() => moveStep(idx, "down")}
                    >
                      <i className="ri-arrow-down-s-line" />
                    </button>
                  </Tooltip>
                  <Tooltip title={step.risky ? "取消危险标记" : "标记为危险"}>
                    <button
                      type="button"
                      className={`ai-plan-edit-btn ${step.risky ? "active-danger" : ""}`}
                      onClick={() => updateStepField(idx, "risky", !step.risky)}
                    >
                      <i className="ri-alarm-warning-line" />
                    </button>
                  </Tooltip>
                  <Tooltip title="在下方插入步骤">
                    <button
                      type="button"
                      className="ai-plan-edit-btn"
                      onClick={() => addStep(idx)}
                    >
                      <i className="ri-add-line" />
                    </button>
                  </Tooltip>
                  <Tooltip title="删除此步骤">
                    <button
                      type="button"
                      className="ai-plan-edit-btn danger"
                      onClick={() => removeStep(idx)}
                      disabled={editSteps.length <= 1}
                    >
                      <i className="ri-delete-bin-line" />
                    </button>
                  </Tooltip>
                </div>
              </div>
              <Input
                value={step.description}
                onChange={(e) => updateStepField(idx, "description", e.target.value)}
                placeholder="步骤说明"
                size="small"
                className="ai-plan-edit-desc-input"
              />
              <Input.TextArea
                value={step.command}
                onChange={(e) => updateStepField(idx, "command", e.target.value)}
                placeholder="命令（必填）"
                autoSize={{ minRows: 1, maxRows: 4 }}
                size="small"
                className="ai-plan-edit-cmd-input"
                status={step.command.trim().length === 0 ? "error" : undefined}
              />
            </div>
          ))}
          {editSteps.length === 0 && (
            <div className="ai-plan-empty-hint">
              <Button size="small" onClick={() => addStep(-1)}>
                <i className="ri-add-line" /> 添加步骤
              </Button>
            </div>
          )}
        </div>

        <div className="ai-plan-actions">
          <Space>
            <Button
              type="primary"
              onClick={handleApproveEdited}
              disabled={editSteps.every((s) => s.command.trim().length === 0)}
            >
              <i className="ri-check-line" /> 确认并执行
            </Button>
            <Button onClick={cancelEdit}>
              返回预览
            </Button>
            <Button danger onClick={onAbort}>
              <i className="ri-stop-circle-line" /> 中止
            </Button>
          </Space>
        </div>
      </div>
    );
  }

  return (
    <div className="ai-plan-card">
      {userRequest && (
        <div className="ai-plan-user-request">
          <i className="ri-user-3-line" />
          <span>{userRequest}</span>
        </div>
      )}
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
          <Button type="primary" onClick={() => onApprove(plan)}>
            <i className="ri-check-line" /> 批准执行
          </Button>
          <Button onClick={enterEdit}>
            <i className="ri-edit-2-line" /> 修改计划
          </Button>
          <Button danger onClick={onAbort}>
            <i className="ri-stop-circle-line" /> 中止
          </Button>
        </Space>
      </div>
    </div>
  );
};
