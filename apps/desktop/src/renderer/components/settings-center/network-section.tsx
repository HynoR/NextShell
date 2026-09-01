import { App as AntdApp, Button, Input, InputNumber, Select, Typography } from "antd";
import { SettingsCard, SettingsRow, SettingsSwitchRow } from "./shared-components";
import type { SaveFn } from "./types";

export const NetworkSection = ({
  loading,
  nexttracePath,
  setNexttracePath,
  ssh,
  traceroute,
  save,
  message: msg
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
                  const result = await window.nextshell.dialog.openFiles({
                    title: "选择 nexttrace 可执行文件",
                    multi: false
                  });
                  if (!result.canceled && result.filePaths[0]) {
                    setNexttracePath(result.filePaths[0]);
                    save({ traceroute: { nexttracePath: result.filePaths[0] } });
                  }
                } catch {
                  msg.error("打开文件选择器失败");
                }
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
        </Typography.Link>{" "}
        下载安装。
      </div>
      <SettingsRow label="PoW 服务商" hint="国内用户建议选 sakura">
        <Select
          style={{ width: "100%" }}
          value={traceroute.powProvider}
          disabled={loading}
          onChange={(v) => save({ traceroute: { powProvider: v } })}
          options={[
            { label: "api.nxtrace.org（默认）", value: "api.nxtrace.org" },
            { label: "sakura（国内推荐）", value: "sakura" }
          ]}
        />
      </SettingsRow>
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
          min={5}
          max={600}
          precision={0}
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
  </>
);
