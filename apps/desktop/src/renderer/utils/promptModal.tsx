import { App as AntdApp, Input } from "antd";

/**
 * Drop-in replacement for window.prompt() using Ant Design Modal.
 * Returns the trimmed input value, or null if the user cancels / leaves blank.
 */
type ModalInstance = ReturnType<typeof AntdApp.useApp>["modal"];

export function promptModal(
  modal: ModalInstance,
  title: string,
  placeholder?: string,
  defaultValue?: string
): Promise<string | null> {
  return new Promise((resolve) => {
    let value = defaultValue ?? "";
    let close: (() => void) | undefined;
    let settled = false;
    const settle = (result: string | null): void => {
      if (settled) return;
      settled = true;
      resolve(result);
    };
    const instance = modal.confirm({
      title,
      content: (
        <Input
          defaultValue={defaultValue}
          placeholder={placeholder}
          onChange={(e) => { value = e.target.value; }}
          onPressEnter={() => {
            const trimmed = value.trim();
            close?.();
            settle(trimmed || null);
          }}
          autoFocus
        />
      ),
      onOk: () => {
        const trimmed = value.trim();
        settle(trimmed || null);
      },
      onCancel: () => settle(null),
    });
    close = instance.destroy;
  });
}
