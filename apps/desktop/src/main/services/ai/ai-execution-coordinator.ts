import crypto from "node:crypto";
import type { AiExecutionPlan, CommandExecutionResult } from "@nextshell/core";
import type { AiProgressEvent } from "@nextshell/shared";
import { sanitizeAiOutput, truncateOutput } from "./output-capture";

export interface AiExecutionCoordinatorDeps {
  execCommand: (
    connectionId: string,
    cmd: string,
    options?: { signal?: AbortSignal; timeoutMs?: number; skipAudit?: boolean }
  ) => Promise<CommandExecutionResult>;
  execInSession?: (
    sessionId: string,
    cmd: string,
    options?: { signal?: AbortSignal; timeoutMs?: number }
  ) => Promise<CommandExecutionResult>;
  appendAuditLog: (payload: {
    action: string;
    level: "info" | "warn" | "error";
    connectionId?: string;
    message: string;
    metadata?: Record<string, unknown>;
  }) => void;
  isAbortError: (error: unknown) => boolean;
}

export interface AiExecutionStepResult {
  step: AiExecutionPlan["steps"][number];
  exitCode: number | null;
  output: string;
  sanitizedOutput: string;
  truncated: ReturnType<typeof truncateOutput>;
}

export interface ExecuteAiPlanParams {
  conversationId: string;
  connectionId: string;
  sessionId?: string;
  plan: AiExecutionPlan;
  timeoutMs: number;
  signal: AbortSignal;
  ensureNotAborted: () => void;
  onProgress: (event: AiProgressEvent) => void;
  onStepCompleted: (result: AiExecutionStepResult) => void;
}

export interface ExecuteAiPlanResult {
  runId: string;
  status: "completed" | "failed" | "aborted";
  error?: string;
}

interface AiExecutionAuditContext {
  runId: string;
  conversationId: string;
  connectionId: string;
  planSummary: string;
}

/** 将字符串安全包裹为 shell 单引号参数 */
const escapeShellArg = (arg: string): string => {
  return "'" + arg.replace(/'/g, "'\\''") + "'";
};

export class AiExecutionCoordinator {
  constructor(private readonly deps: AiExecutionCoordinatorDeps) {}

  async executePlan(params: ExecuteAiPlanParams): Promise<ExecuteAiPlanResult> {
    const auditContext: AiExecutionAuditContext = {
      runId: crypto.randomUUID(),
      conversationId: params.conversationId,
      connectionId: params.connectionId,
      planSummary: params.plan.summary,
    };

    this.appendRunAudit(auditContext, "started", "info", {
      stepCount: params.plan.steps.length,
      executionTimeoutMs: params.timeoutMs,
    });

    try {
      for (const step of params.plan.steps) {
        const stepStartedAt = Date.now();

        params.ensureNotAborted();
        this.appendStepAudit(auditContext, step, "started", "info");
        params.onProgress({
          conversationId: params.conversationId,
          type: "step_start",
          step: step.step,
          command: step.command,
          status: "running",
        });

        try {
          const result = await this.executeStepCommand(step.command, params);

          params.ensureNotAborted();
          const executionResult = this.normalizeStepResult(step, result);
          params.onProgress({
            conversationId: params.conversationId,
            type: "step_output",
            step: step.step,
            output: executionResult.output,
            status: "success",
          });
          params.onProgress({
            conversationId: params.conversationId,
            type: "step_done",
            step: step.step,
            status: executionResult.exitCode === 0 || executionResult.exitCode === null ? "success" : "failed",
            output: executionResult.output,
          });
          params.onStepCompleted(executionResult);

          this.appendStepAudit(
            auditContext,
            step,
            "completed",
            executionResult.exitCode === 0 || executionResult.exitCode === null ? "info" : "warn",
            {
              exitCode: executionResult.exitCode,
              durationMs: Date.now() - stepStartedAt,
              outputChars: executionResult.output.length,
              outputWasTruncated: executionResult.truncated.wasTruncated,
            }
          );
        } catch (error) {
          if (this.deps.isAbortError(error) || params.signal.aborted) {
            this.appendStepAudit(auditContext, step, "aborted", "warn", {
              durationMs: Date.now() - stepStartedAt,
            });
            this.appendRunAudit(auditContext, "aborted", "warn", {
              stepCount: params.plan.steps.length,
            });
            return { runId: auditContext.runId, status: "aborted" };
          }

          const errorMsg = error instanceof Error ? error.message : String(error);
          this.appendStepAudit(auditContext, step, "failed", "error", {
            durationMs: Date.now() - stepStartedAt,
            error: errorMsg,
          });
          this.appendRunAudit(auditContext, "failed", "error", {
            stepCount: params.plan.steps.length,
            error: errorMsg,
          });
          params.onProgress({
            conversationId: params.conversationId,
            type: "error",
            step: step.step,
            error: errorMsg,
            status: "failed",
          });
          return { runId: auditContext.runId, status: "failed", error: errorMsg };
        }
      }

      this.appendRunAudit(auditContext, "completed", "info", {
        stepCount: params.plan.steps.length,
      });
      return { runId: auditContext.runId, status: "completed" };
    } catch (error) {
      if (this.deps.isAbortError(error) || params.signal.aborted) {
        this.appendRunAudit(auditContext, "aborted", "warn", {
          stepCount: params.plan.steps.length,
        });
        return { runId: auditContext.runId, status: "aborted" };
      }

      const errorMsg = error instanceof Error ? error.message : String(error);
      this.appendRunAudit(auditContext, "failed", "error", {
        stepCount: params.plan.steps.length,
        error: errorMsg,
      });
      return { runId: auditContext.runId, status: "failed", error: errorMsg };
    }
  }

  private normalizeStepResult(
    step: AiExecutionPlan["steps"][number],
    result: CommandExecutionResult
  ): AiExecutionStepResult {
    const stdoutLines = result.stdout.split("\n");
    const EXIT_MARKER = "__NEXTSHELL_EXIT__";
    let markerIdx = -1;
    for (let index = stdoutLines.length - 1; index >= 0; index--) {
      if (stdoutLines[index]!.startsWith(EXIT_MARKER)) {
        markerIdx = index;
        break;
      }
    }

    let exitCode: number | null = null;
    if (markerIdx >= 0) {
      const code = parseInt(stdoutLines[markerIdx]!.slice(EXIT_MARKER.length), 10);
      exitCode = Number.isFinite(code) ? code : null;
      stdoutLines.splice(markerIdx, 1);
    }

    const cleanStdout = stdoutLines.join("\n");
    const cleanStderr = (result.stderr ?? "")
      .split("\n")
      .filter((line) => !line.includes("cannot set terminal process group") && !line.includes("no job control"))
      .join("\n")
      .trim();
    const mergedOutput = cleanStdout + (cleanStderr ? `\n${cleanStderr}` : "");
    const truncated = truncateOutput(mergedOutput);
    const displayOutput = mergedOutput.length > 2000
      ? mergedOutput.slice(0, 1000)
        + `\n... [省略 ${mergedOutput.length - 2000} 字符] ...\n`
        + mergedOutput.slice(-1000)
      : mergedOutput;

    return {
      step,
      exitCode,
      output: displayOutput,
      sanitizedOutput: sanitizeAiOutput(truncated.text),
      truncated,
    };
  }

  private executeHiddenCommand(
    connectionId: string,
    command: string,
    params: Pick<ExecuteAiPlanParams, "signal" | "timeoutMs">
  ): Promise<CommandExecutionResult> {
    const EXIT_MARKER = "__NEXTSHELL_EXIT__";
    const innerCmd = `${command}; echo "${EXIT_MARKER}$?"`;
    const wrappedCmd = `bash -lic ${escapeShellArg(innerCmd)}`;
    return this.deps.execCommand(connectionId, wrappedCmd, {
      signal: params.signal,
      timeoutMs: params.timeoutMs,
      skipAudit: true,
    });
  }

  private async executeStepCommand(
    command: string,
    params: Pick<ExecuteAiPlanParams, "connectionId" | "sessionId" | "signal" | "timeoutMs">
  ): Promise<CommandExecutionResult> {
    if (!params.sessionId || !this.deps.execInSession) {
      return this.executeHiddenCommand(params.connectionId, command, params);
    }

    try {
      return await this.deps.execInSession(params.sessionId, command, {
        signal: params.signal,
        timeoutMs: params.timeoutMs,
      });
    } catch (error) {
      if (!this.shouldFallbackToHiddenExecution(error)) {
        throw error;
      }
      return this.executeHiddenCommand(params.connectionId, command, params);
    }
  }

  private shouldFallbackToHiddenExecution(error: unknown): boolean {
    const message = error instanceof Error ? error.message : String(error);
    return message === "Session not found" || message === "AI 仅支持在远端终端会话中执行命令";
  }

  private appendRunAudit(
    context: AiExecutionAuditContext,
    phase: "started" | "completed" | "failed" | "aborted",
    level: "info" | "warn" | "error",
    metadata?: Record<string, unknown>
  ): void {
    this.deps.appendAuditLog({
      action: "ai.execution.run",
      level,
      connectionId: context.connectionId,
      message: `AI execution ${phase}`,
      metadata: {
        runId: context.runId,
        conversationId: context.conversationId,
        planSummary: context.planSummary,
        ...metadata,
      },
    });
  }

  private appendStepAudit(
    context: AiExecutionAuditContext,
    step: { step: number; command: string; description: string },
    phase: "started" | "completed" | "failed" | "aborted",
    level: "info" | "warn" | "error",
    metadata?: Record<string, unknown>
  ): void {
    this.deps.appendAuditLog({
      action: "ai.execution.step",
      level,
      connectionId: context.connectionId,
      message: `AI execution step ${phase}`,
      metadata: {
        runId: context.runId,
        conversationId: context.conversationId,
        planSummary: context.planSummary,
        step: step.step,
        command: step.command,
        description: step.description,
        ...metadata,
      },
    });
  }
}
