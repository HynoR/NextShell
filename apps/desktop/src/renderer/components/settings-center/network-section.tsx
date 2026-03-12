import { App as AntdApp, Button, Input, InputNumber, Radio, Select, Typography } from "antd";
import { SettingsCard, SettingsRow, SettingsSwitchRow } from "./shared-components";
import type { SaveFn } from "./types";

export const NetworkSection = ({
  loading, nexttracePath, setNexttracePath, ssh, traceroute, save, message: msg,
}: {
  loading: boolean;
  nexttracePath: string;
  setNexttracePath: (v: string) => void;
  ssh: import("@nextshell/core").AppPreferences["ssh"];
  traceroute: import("@nextshell/core").AppPreferences["traceroute"];
  save: SaveFn;
  message: ReturnType<typeof AntdApp.useApp>["message"];
}) => (
  <>
    <SettingsCard title="路由追踪工具" description="配置 nexttrace 可执行文件路径">
      <SettingsRow label="nexttrace 可执行文件路径">
        <div className="flex gap-2">
          <Input
            style={{ flex: 1 }}
            value={nexttracePath}
            disabled={loading}
            onChange={(e) => setNexttracePath(e.target.value)}
            onBlur={() => save({ traceroute: { nexttracePath: nexttracePath.trim() } })}
            placeholder="留空则自动从 PATH 查找"
          />
          <Button
            onClick={() =>
              void (async () => {
                try {
                  const result = await window.nextshell.dialog.openFiles({ title: "选择 nexttrace 可执行文件", multi: false });
                  if (!result.canceled && result.filePaths[0]) {
                    setNexttracePath(result.filePaths[0]);
                    save({ traceroute: { nexttracePath: result.filePaths[0] } });
                  }
                } catch { msg.error("打开文件选择器失败"); }
              })()
            }
          >
            浏览
          </Button>
        </div>
      </SettingsRow>
      <div className="stg-note">
        尚未安装？前往{" "}
        <Typography.Link
          href="https://github.com/nxtrace/NTrace-core"
          target="_blank"
          style={{ fontSize: "inherit" }}
        >
          github.com/nxtrace/NTrace-core
        </Typography.Link>
        {" "}下载安装。
      </div>
    </SettingsCard>

    <SettingsCard title="SSH Keepalive" description="发送空包保持 SSH 连接稳定">
      <SettingsSwitchRow
        label="启用 Keepalive"
        hint="对所有连接生效（可在连接管理器中单独覆盖）"
        checked={ssh.keepAliveEnabled}
        disabled={loading}
        onChange={(v) => save({ ssh: { keepAliveEnabled: v } })}
      />
      <SettingsRow label="保活间隔（秒）" hint="范围 5–600">
        <InputNumber
          style={{ width: "100%" }}
          min={5} max={600} precision={0}
          value={ssh.keepAliveIntervalSec}
          disabled={loading || !ssh.keepAliveEnabled}
          onChange={(v) => {
            if (typeof v === "number" && Number.isInteger(v)) {
              save({ ssh: { keepAliveIntervalSec: v } });
            }
          }}
        />
      </SettingsRow>
      <div className="stg-note">修改后新连接生效，已连接会话需重连。</div>
    </SettingsCard>

    <SettingsCard title="探测参数" description="下次点击「开始追踪」时生效">
      <SettingsRow label="探测协议">
        <Radio.Group
          value={traceroute.protocol}
          disabled={loading}
          onChange={(e) => save({ traceroute: { protocol: e.target.value as string } })}
        >
          <Radio value="icmp">ICMP（默认）</Radio>
          <Radio value="tcp">TCP SYN</Radio>
          <Radio value="udp">UDP</Radio>
        </Radio.Group>
      </SettingsRow>

      {(traceroute.protocol === "tcp" || traceroute.protocol === "udp") && (
        <SettingsRow
          label="目标端口"
          hint={traceroute.protocol === "tcp" ? "默认 80" : "默认 33494"}
        >
          <InputNumber
            style={{ width: "100%" }}
            min={0} max={65535} precision={0}
            value={traceroute.port}
            disabled={loading}
            placeholder="0 = 使用协议默认值"
            onChange={(v) => save({ traceroute: { port: typeof v === "number" ? v : 0 } })}
          />
        </SettingsRow>
      )}

      <SettingsRow label="IP 版本">
        <Select
          style={{ width: "100%" }}
          value={traceroute.ipVersion}
          disabled={loading}
          onChange={(v) => save({ traceroute: { ipVersion: v } })}
          options={[
            { label: "自动", value: "auto" },
            { label: "仅 IPv4", value: "ipv4" },
            { label: "仅 IPv6", value: "ipv6" },
          ]}
        />
      </SettingsRow>

      <SettingsRow label="每跳探测次数" hint="默认 3，范围 1–10">
        <InputNumber
          style={{ width: "100%" }}
          min={1} max={10} precision={0}
          value={traceroute.queries}
          disabled={loading}
          onChange={(v) => {
            if (typeof v === "number" && v >= 1 && v <= 10) {
              save({ traceroute: { queries: v } });
            }
          }}
        />
      </SettingsRow>

      <SettingsRow label="最大跳数（TTL）" hint="默认 30">
        <InputNumber
          style={{ width: "100%" }}
          min={1} max={64} precision={0}
          value={traceroute.maxHops}
          disabled={loading}
          onChange={(v) => {
            if (typeof v === "number" && v >= 1 && v <= 64) {
              save({ traceroute: { maxHops: v } });
            }
          }}
        />
      </SettingsRow>
    </SettingsCard>

    <SettingsCard title="数据来源与显示" description="IP 归属地查询、反向解析等选项">
      <SettingsRow label="IP 地理数据来源">
        <Select
          style={{ width: "100%" }}
          value={traceroute.dataProvider}
          disabled={loading}
          onChange={(v) => save({ traceroute: { dataProvider: v } })}
          options={[
            { label: "LeoMoeAPI（默认）", value: "LeoMoeAPI" },
            { label: "IP-API.com", value: "ip-api.com" },
            { label: "IPInfo", value: "IPInfo" },
            { label: "IPInsight", value: "IPInsight" },
            { label: "IP.SB", value: "IP.SB" },
            { label: "禁用 GeoIP", value: "disable-geoip" },
          ]}
        />
      </SettingsRow>

      <SettingsRow label="PoW 服务商" hint="国内用户建议选 sakura">
        <Select
          style={{ width: "100%" }}
          value={traceroute.powProvider}
          disabled={loading}
          onChange={(v) => save({ traceroute: { powProvider: v } })}
          options={[
            { label: "api.nxtrace.org（默认）", value: "api.nxtrace.org" },
            { label: "sakura（国内推荐）", value: "sakura" },
          ]}
        />
      </SettingsRow>

      <SettingsRow label="界面语言">
        <Radio.Group
          value={traceroute.language}
          disabled={loading}
          onChange={(e) => save({ traceroute: { language: e.target.value as string } })}
        >
          <Radio value="cn">中文</Radio>
          <Radio value="en">English</Radio>
        </Radio.Group>
      </SettingsRow>

      <SettingsSwitchRow
        label="禁用反向 DNS 解析"
        hint="启用后不解析每跳的 PTR 记录，追踪速度更快"
        checked={traceroute.noRdns}
        disabled={loading}
        onChange={(v) => save({ traceroute: { noRdns: v } })}
      />
    </SettingsCard>
  </>
);
