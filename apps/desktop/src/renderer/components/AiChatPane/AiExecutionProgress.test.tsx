import React from "react";
import { describe, expect, test } from "bun:test";
import { Button } from "antd";
import type { ReactElement, ReactNode } from "react";
import { AiExecutionProgressCard } from "./AiExecutionProgress";

const collectText = (node: ReactNode): string => {
  if (typeof node === "string" || typeof node === "number") {
    return String(node);
  }
  if (!node || typeof node === "boolean") {
    return "";
  }
  if (Array.isArray(node)) {
    return node.map((item) => collectText(item)).join("");
  }
  if (React.isValidElement(node)) {
    return collectText(node.props.children);
  }
  return "";
};

const findElements = (
  node: ReactNode,
  predicate: (element: ReactElement<Record<string, unknown>>) => boolean
): Array<ReactElement<Record<string, unknown>>> => {
  const matches: Array<ReactElement<Record<string, unknown>>> = [];

  const visit = (current: ReactNode): void => {
    if (!React.isValidElement(current)) {
      if (Array.isArray(current)) {
        current.forEach(visit);
      }
      return;
    }

    if (predicate(current as ReactElement<Record<string, unknown>>)) {
      matches.push(current as ReactElement<Record<string, unknown>>);
    }

    React.Children.forEach(current.props.children, visit);
  };

  visit(node);
  return matches;
};

describe("AiExecutionProgressCard", () => {
  test("shows failed header and exposes resume actions", () => {
    let retried = false;
    let edited = false;

    const element = AiExecutionProgressCard({
      progress: {
        planSummary: "排查磁盘",
        steps: [
          { step: 1, status: "success", output: "ok" },
          { step: 2, status: "failed", error: "权限不足" },
          { step: 3, status: "pending" },
        ],
        currentStep: 2,
        completed: true,
      },
      retrySourceStep: 2,
      canResume: true,
      onRetry: () => {
        retried = true;
      },
      onEditRetryPlan: () => {
        edited = true;
      },
    });

    expect(collectText(element)).toContain("执行失败");
    expect(collectText(element)).toContain("从步骤 2 继续");
    expect(collectText(element)).toContain("编辑后继续");

    const buttons = findElements(
      element,
      (current) => current.type === Button
    );

    expect(buttons).toHaveLength(2);

    buttons[0]?.props.onClick?.();
    buttons[1]?.props.onClick?.();

    expect(retried).toBe(true);
    expect(edited).toBe(true);
  });

  test("does not render resume actions for successful completion", () => {
    const element = AiExecutionProgressCard({
      progress: {
        planSummary: "排查磁盘",
        steps: [{ step: 1, status: "success", output: "ok" }],
        currentStep: 1,
        completed: true,
      },
      canResume: false,
    });

    expect(collectText(element)).toContain("执行完成");
    expect(collectText(element)).not.toContain("编辑后继续");
    expect(
      findElements(element, (current) => current.type === Button)
    ).toHaveLength(0);
  });
});
