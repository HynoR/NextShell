import { Form, InputNumber, Select } from "antd";
import type { ProxyProfile } from "@nextshell/core";

interface NetworkTabProps {
  keepAliveSetting?: boolean;
  proxies: ProxyProfile[];
}

export const NetworkTab = ({ keepAliveSetting, proxies }: NetworkTabProps) => {
  return (
    <>
      <Form.Item
        label="代理"
        name="proxyId"
      >
        <Select
          placeholder="直连（不使用代理）"
          allowClear
          options={proxies.map((proxy) => ({
            label: `${proxy.name} (${proxy.proxyType.toUpperCase()} ${proxy.host}:${proxy.port})`,
            value: proxy.id
          }))}
          notFoundContent={
            <div style={{ textAlign: "center", padding: "8px 0", color: "var(--text-muted)" }}>
              暂无代理，请先在「代理管理」中添加
            </div>
          }
        />
      </Form.Item>

      <div className="mgr-section-label mgr-section-gap">连接保活</div>

      <Form.Item label="Keepalive（发送空包）" name="keepAliveEnabled">
        <Select
          placeholder="跟随全局设置"
          allowClear
          options={[
            { label: "启用", value: true },
            { label: "禁用", value: false }
          ]}
        />
      </Form.Item>

      <Form.Item label="保活间隔（秒）" name="keepAliveIntervalSec">
        <InputNumber
          min={5}
          max={600}
          precision={0}
          style={{ width: "100%" }}
          placeholder="留空跟随全局"
          disabled={keepAliveSetting === false}
        />
      </Form.Item>

      <div className="mgr-form-subtitle">
        留空表示跟随全局设置，修改后需重连会话生效。
      </div>
    </>
  );
};
