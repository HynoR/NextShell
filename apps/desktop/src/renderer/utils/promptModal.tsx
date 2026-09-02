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
  defaultValue?: string,
  selectStem = false
): Promise<string | null> {
  return new Promise((resolve) => {
    let value = defaultValue ?? "";
    let close: (() => void) | undefined;
    let settled = false;
    let stemSelected = false;
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
          onChange={(e) => {
            value = e.target.value;
          }}
          onPressEnter={() => {
            const trimmed = value.trim();
            close?.();
            settle(trimmed || null);
          }}
          ref={(input) => {
            // 重命名场景：预填文件名时只选中不含扩展名的部分，方便直接覆盖输入。
            if (!input || stemSelected || !selectStem || !defaultValue) return;
            stemSelected = true;
            const dot = defaultValue.lastIndexOf(".");
            input.setSelectionRange(0, dot > 0 ? dot : defaultValue.length);
          }}
          autoFocus
        />
      ),
      onOk: () => {
        const trimmed = value.trim();
        settle(trimmed || null);
      },
      onCancel: () => settle(null)
    });
    close = instance.destroy;
  });
}
