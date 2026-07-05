import React from "react";
import { AppErrorBoundary } from "./AppErrorBoundary";

const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

const findButtons = (node: React.ReactNode): Array<React.ReactElement<Record<string, unknown>>> => {
  const buttons: Array<React.ReactElement<Record<string, unknown>>> = [];
  const visit = (current: React.ReactNode): void => {
    for (const child of React.Children.toArray(current)) {
      if (!React.isValidElement(child)) {
        continue;
      }
      const element = child as React.ReactElement<Record<string, unknown>>;
      if (element.type === "button") {
        buttons.push(element);
      }
      visit(element.props.children as React.ReactNode);
    }
  };
  visit(node);
  return buttons;
};

// getDerivedStateFromError captures the error message
(() => {
  const state = AppErrorBoundary.getDerivedStateFromError(new Error("boom"));
  assert(state.hasError, "boundary should flag hasError on thrown Error");
  assert(state.errorMessage === "boom", "boundary should capture the error message");

  const nonErrorState = AppErrorBoundary.getDerivedStateFromError("plain failure");
  assert(nonErrorState.hasError, "boundary should flag hasError on non-Error throw");
  assert(nonErrorState.errorMessage === "plain failure", "boundary should stringify non-Error values");
})();

// Normal path: renders children untouched
(() => {
  const child = React.createElement("span", null, "ok");
  const boundary = new AppErrorBoundary({ children: child });
  assert(boundary.render() === child, "boundary should pass children through when no error");
})();

// Failure path: renders fallback with recover and reload buttons
(() => {
  const boundary = new AppErrorBoundary({ children: React.createElement("span") });
  boundary.state = { hasError: true, errorMessage: "boom" };

  const fallback = boundary.render();
  assert(React.isValidElement(fallback), "fallback should be a React element");

  const buttons = findButtons(fallback);
  assert(buttons.length === 2, "fallback should render exactly two buttons");
  assert(buttons[0]?.props.children === "恢复界面", "first button should recover the UI");
  assert(buttons[1]?.props.children === "重新加载", "second button should reload the window");
  assert(buttons[0]?.props.onClick === boundary.handleReset, "recover button should reset boundary state");
  assert(buttons[1]?.props.onClick === boundary.handleReload, "reload button should trigger window reload");
})();
