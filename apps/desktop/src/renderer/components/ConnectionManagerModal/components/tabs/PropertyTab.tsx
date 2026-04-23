import { Form, Input, Select, Switch, Typography } from "antd";
import type { CloudSyncWorkspaceProfile } from "@nextshell/core";
import { CONNECTION_ZONES, ZONE_DISPLAY_NAMES, ZONE_ORDER } from "@nextshell/shared";

interface PropertyTabProps {
  workspaces: CloudSyncWorkspaceProfile[];
  scopeLocked: boolean;
}

export const PropertyTab = ({ workspaces, scopeLocked }: PropertyTabProps) => {
  const form = Form.useFormInstance();
  const groupZone = Form.useWatch("groupZone", form);

  return (
    <>
      <Form.Item label="分组路径" required>
        <div className="flex gap-2 items-start">
          <Form.Item name="groupZone" noStyle>
            <Select
              style={{ width: 120, flexShrink: 0 }}
              disabled={scopeLocked}
              options={ZONE_ORDER.map((zone) => ({
                label: ZONE_DISPLAY_NAMES[zone],
                value: zone,
                disabled: zone === CONNECTION_ZONES.WORKSPACE && workspaces.length === 0
              }))}
            />
          </Form.Item>
          <Form.Item name="groupSubPath" noStyle>
            <Input
              placeholder={groupZone === CONNECTION_ZONES.WORKSPACE ? "/production" : "/production"}
              prefix={<i className="ri-folder-3-line" style={{ color: "var(--t3)", fontSize: 13 }} />}
              style={{ fontFamily: "var(--mono)" }}
            />
          </Form.Item>
        </div>
      </Form.Item>

      {groupZone === CONNECTION_ZONES.WORKSPACE ? (
        <Form.Item
          label="所属 Workspace"
          name="workspaceId"
          extra={scopeLocked ? "共享连接的 workspace 根节点已锁定，可调整子路径但不能改到其他作用域。" : undefined}
        >
          <Select
            placeholder="选择 workspace"
            disabled={scopeLocked}
            options={workspaces.map((workspace) => ({
              label: workspace.displayName || workspace.workspaceName,
              value: workspace.id
            }))}
            notFoundContent={
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                暂无可用 workspace，请先在「Workspace Repos」中添加。
              </Typography.Text>
            }
          />
        </Form.Item>
      ) : null}

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
