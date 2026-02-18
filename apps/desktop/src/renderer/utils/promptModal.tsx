import { Input, Modal } from "antd";

/**
 * Drop-in replacement for window.prompt() using Ant Design Modal.
 * Returns the trimmed input value, or null if the user cancels / leaves blank.
 */
export function promptModal(title: string, placeholder?: string, defaultValue?: string): Promise<string | null> {
  return new Promise((resolve) => {
    let value = defaultValue ?? "";
    Modal.confirm({
      title,
      content: (
        <Input
          defaultValue={defaultValue}
          placeholder={placeholder}
          onChange={(e) => { value = e.target.value; }}
          onPressEnter={() => {
            const trimmed = value.trim();
            Modal.destroyAll();
            resolve(trimmed || null);
          }}
          autoFocus
        />
      ),
      onOk: () => {
        const trimmed = value.trim();
        resolve(trimmed || null);
      },
      onCancel: () => resolve(null),
    });
  });
}
