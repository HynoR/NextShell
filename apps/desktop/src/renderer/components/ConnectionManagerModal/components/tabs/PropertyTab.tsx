import { Form, Input, Select, Switch } from "antd";
import { ZONE_DISPLAY_NAMES, ZONE_ORDER } from "@nextshell/shared";

export const PropertyTab = () => {
  return (
    <>
      <Form.Item label="分组路径" required>
        <div className="flex gap-2 items-start">
          <Form.Item name="groupZone" noStyle>
            <Select
              style={{ width: 120, flexShrink: 0 }}
              options={ZONE_ORDER.map((zone) => ({
                label: ZONE_DISPLAY_NAMES[zone],
                value: zone
              }))}
            />
          </Form.Item>
          <Form.Item name="groupSubPath" noStyle>
            <Input
              placeholder="/production"
              prefix={<i className="ri-folder-3-line" style={{ color: "var(--t3)", fontSize: 13 }} />}
              style={{ fontFamily: "var(--mono)" }}
            />
          </Form.Item>
        </div>
      </Form.Item>

      <div className="flex gap-3 items-start">
        <Form.Item label="标签" name="tags" className="flex-1">
          <Select
            mode="tags"
            tokenSeparators={[","]}
            placeholder="web, linux, prod"
          />
        </Form.Item>
        <Form.Item
          label="收藏"
          name="favorite"
          valuePropName="checked"
          className="shrink-0 !mb-0"
        >
          <Switch size="small" />
        </Form.Item>
      </div>

      <Form.Item label="备注" name="notes" className="!mb-0">
        <Input.TextArea rows={2} placeholder="可选备注信息..." className="mgr-textarea" />
      </Form.Item>
    </>
  );
};
