import { App as AntdApp } from "antd";
import { useEffect, useRef } from "react";
import type { DeviceKeyNotice } from "../../../../../packages/shared/src/index";

export const DEVICE_KEY_NOTICE_TITLE = "设备密钥已重置";
export const DEVICE_KEY_NOTICE_CONTENT =
  "系统钥匙串中的设备密钥无法读取，已生成新密钥。此前保存的密码/私钥无法解密，请在连接配置中重新录入。";

/**
 * One-time notice: the legacy keychain device key was unreadable and every
 * stored credential is lost. Checked once on mount (persisted across restarts
 * until acknowledged) and again when main resolves the key lazily mid-session.
 */
export const useDeviceKeyNotice = (): void => {
  const { modal } = AntdApp.useApp();
  const shownRef = useRef(false);

  useEffect(() => {
    const show = (notice: DeviceKeyNotice) => {
      if (!notice.credentialsUnrecoverable || shownRef.current) return;
      shownRef.current = true;
      modal.warning({
        title: DEVICE_KEY_NOTICE_TITLE,
        content: DEVICE_KEY_NOTICE_CONTENT,
        okText: "我知道了",
        onOk: () => window.nextshell.security.acknowledgeDeviceKeyNotice().then(() => undefined)
      });
    };
    void window.nextshell.security.getDeviceKeyNotice().then(show, () => undefined);
    return window.nextshell.security.onDeviceKeyNotice(show);
  }, [modal]);
};
