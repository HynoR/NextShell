import React from "react";
import { Input, InputNumber, Space } from "antd";
import { AuditRetentionDaysInput } from "./AuditRetentionDaysInput";

const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

(() => {
  const element = AuditRetentionDaysInput({
    value: 30,
    disabled: false,
    onChange: () => undefined
  });

  assert(element.type === Space.Compact, "audit retention input should use Space.Compact");

  const children = React.Children.toArray(element.props.children) as Array<
    React.ReactElement<Record<string, unknown>>
  >;
  assert(children.length === 2, "audit retention input should render number input and unit suffix");

  const [numberInput, unitInput] = children;
  assert(numberInput?.type === InputNumber, "first child should remain InputNumber");
  assert(
    !Object.prototype.hasOwnProperty.call(numberInput?.props ?? {}, "addonAfter"),
    "InputNumber should not use deprecated addonAfter"
  );

  assert(unitInput?.type === Input, "second child should be an Input suffix");
  assert(unitInput?.props.value === "天", "unit suffix should display days");
  assert(unitInput?.props.readOnly === true, "unit suffix should be read-only");
})();
