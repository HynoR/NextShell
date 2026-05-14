import type { AiExecutionPlan } from "../../../../../../packages/core/src/index";

/**
 * 从 LLM 回复文本中提取 JSON 执行计划。
 * 支持 ```json ... ``` 代码块格式。
 */
export const extractPlanFromResponse = (content: string): AiExecutionPlan | undefined => {
  const jsonBlockRegex = /```json\s*\n([\s\S]*?)\n\s*```/;
  const match = jsonBlockRegex.exec(content);
  if (!match?.[1]) return undefined;

  try {
    const parsed = JSON.parse(match[1]) as {
      plan?: Array<{
        step: number;
        command: string;
        description: string;
        risky?: boolean;
      }>;
      summary?: string;
    };

    if (!Array.isArray(parsed.plan) || parsed.plan.length === 0) return undefined;

    return {
      steps: parsed.plan.map((s, i) => ({
        step: s.step ?? i + 1,
        command: String(s.command ?? ""),
        description: String(s.description ?? ""),
        risky: Boolean(s.risky),
      })),
      summary: String(parsed.summary ?? ""),
    };
  } catch {
    return undefined;
  }
};
