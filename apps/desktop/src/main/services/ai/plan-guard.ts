import type { AiExecutionPlan, AiExecutionStep } from "../../../../../../packages/core/src/index";

const RISKY_COMMAND_PATTERNS: RegExp[] = [
  /\brm\b/i,
  /(^|[\s"'`])\/etc\//i,
  /\b(?:systemctl|service)\s+restart\b/i,
  /\breboot\b/i,
  /\b(?:chmod|chown)\b/i,
  /\b(?:apt|apt-get|yum|dnf|apk|pacman|brew)\s+(?:install|remove|upgrade|update)\b/i,
  /\b(?:fdisk|mkfs|mount|umount|parted)\b/i,
  /\b(?:iptables|firewall-cmd|ufw)\b/i,
];

const FORBIDDEN_COMMAND_PATTERNS: Array<{ pattern: RegExp; message: string }> = [
  {
    pattern: /(^|[;&|]\s*)(?:sudo\s+)?rm\s+-[^\n]*r[^\n]*f[^\n]*\s+(?:--no-preserve-root\s+)?\/(?=\s|$|[;&|])/i,
    message: "禁止执行会删除根目录的 rm -rf / 命令",
  },
  {
    pattern: /\b(?:systemctl|service)\s+(?:stop|disable|mask)\s+ssh(?:d)?\b/i,
    message: "禁止执行可能导致远程 SSH 断联的命令",
  },
];

const normalizeStepDescription = (description: string, index: number): string => {
  const trimmed = description.trim();
  if (trimmed) {
    return trimmed;
  }
  return `执行步骤 ${index + 1}`;
};

export const isRiskyAiCommand = (command: string): boolean => {
  return RISKY_COMMAND_PATTERNS.some((pattern) => pattern.test(command));
};

export const assertAiCommandAllowed = (command: string): void => {
  for (const rule of FORBIDDEN_COMMAND_PATTERNS) {
    if (rule.pattern.test(command)) {
      throw new Error(rule.message);
    }
  }
};

export const normalizeApprovedPlan = (plan: AiExecutionPlan): AiExecutionPlan => {
  const normalizedSteps: AiExecutionStep[] = plan.steps.map((step, index) => {
    const command = step.command.trim();
    if (!command) {
      throw new Error(`步骤 ${index + 1} 的命令不能为空`);
    }

    assertAiCommandAllowed(command);

    return {
      step: index + 1,
      command,
      description: normalizeStepDescription(step.description, index),
      risky: isRiskyAiCommand(command),
    };
  });

  if (normalizedSteps.length === 0) {
    throw new Error("执行计划至少需要一个有效步骤");
  }

  return {
    summary: plan.summary.trim(),
    steps: normalizedSteps,
  };
};
