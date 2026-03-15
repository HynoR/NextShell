import { Form, Select, Switch } from "antd";

export const AdvancedTab = () => {
  return (
    <>
      <div className="flex gap-3 items-start">
        <Form.Item
          label="监控会话"
          name="monitorSession"
          valuePropName="checked"
          className="shrink-0 !mb-0"
        >
          <Switch size="small" />
        </Form.Item>
        <div className="mgr-monitor-hint">
          启用后支持进程管理器和网络监控
        </div>
      </div>

      <Form.Item
        label="字符编码"
        name="terminalEncoding"
      >
        <Select
          options={[
            { label: "UTF-8", value: "utf-8" },
            { label: "GB18030", value: "gb18030" },
            { label: "GBK", value: "gbk" },
            { label: "Big5", value: "big5" }
          ]}
        />
      </Form.Item>

      <div className="mgr-section-label">按键序列</div>

      <div className="flex gap-3 items-start">
        <Form.Item
          label="Backspace 退格键"
          name="backspaceMode"
          className="flex-1"
        >
          <Select
            options={[
              { label: "ASCII - Backspace", value: "ascii-backspace" },
              { label: "ASCII - Delete", value: "ascii-delete" }
            ]}
          />
        </Form.Item>
        <Form.Item
          label="Delete 删除键"
          name="deleteMode"
          className="flex-1"
        >
          <Select
            options={[
              { label: "VT220 - Delete", value: "vt220-delete" },
              { label: "ASCII - Delete", value: "ascii-delete" },
              { label: "ASCII - Backspace", value: "ascii-backspace" }
            ]}
          />
        </Form.Item>
      </div>

      <div className="mgr-form-subtitle">终端高级配置保存后需重连会话生效。</div>
    </>
  );
};
