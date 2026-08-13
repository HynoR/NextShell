import { useCallback, useEffect, useRef, useState } from "react";
import { App as AntdApp, Input } from "antd";
import type { ConnectionProfile } from "@nextshell/core";
import { formatErrorMessage } from "../../../utils/errorMessage";

interface UseConnectionPasswordRevealOptions {
  activeAuthType?: string;
  modal: ReturnType<typeof AntdApp.useApp>["modal"];
  message: ReturnType<typeof AntdApp.useApp>["message"];
  primarySelectedId?: string;
  selectedConnection?: ConnectionProfile;
}

export const useConnectionPasswordReveal = ({
  activeAuthType,
  modal,
  message,
  primarySelectedId,
  selectedConnection
}: UseConnectionPasswordRevealOptions) => {
  const [revealedLoginPassword, setRevealedLoginPassword] = useState<string>();
  const [revealingLoginPassword, setRevealingLoginPassword] = useState(false);
  const revealPasswordTimeoutRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  const clearRevealConnectionPassword = useCallback(() => {
    setRevealedLoginPassword(undefined);
    if (revealPasswordTimeoutRef.current) {
      clearTimeout(revealPasswordTimeoutRef.current);
      revealPasswordTimeoutRef.current = undefined;
    }
  }, []);

  useEffect(() => {
    clearRevealConnectionPassword();
  }, [activeAuthType, clearRevealConnectionPassword, primarySelectedId]);

  useEffect(() => {
    return () => {
      if (revealPasswordTimeoutRef.current) {
        clearTimeout(revealPasswordTimeoutRef.current);
      }
    };
  }, []);

  const getMasterPasswordAvailability = useCallback(async (): Promise<boolean> => {
    try {
      const result = await window.nextshell.masterPassword.getCached();
      return result.available;
    } catch {
      return false;
    }
  }, []);

  const promptMasterPasswordForReveal = useCallback(
    (unlocked: boolean): Promise<string | null> => {
      return new Promise((resolve) => {
        let password = "";
        let settled = false;
        const settle = (value: string | null): void => {
          if (settled) return;
          settled = true;
          resolve(value);
        };

        modal.confirm({
          title: "查看登录密码",
          okText: "查看",
          cancelText: "取消",
          content: (
            <div style={{ display: "grid", gap: 8 }}>
              {unlocked ? (
                <div style={{ fontSize: 12, color: "var(--t3)" }}>
                  主密码已解锁，直接确认即可查看登录密码。
                </div>
              ) : (
                <>
                  <div style={{ fontSize: 12, color: "var(--t3)" }}>
                    主密码未解锁，请输入主密码后查看。
                  </div>
                  <Input.Password
                    placeholder="请输入主密码"
                    onChange={(event) => {
                      password = event.target.value;
                    }}
                  />
                </>
              )}
            </div>
          ),
          onOk: async () => {
            if (unlocked) {
              settle("");
              return;
            }
            const trimmed = password.trim();
            if (!trimmed) {
              message.warning("请输入主密码。");
              throw new Error("empty-master-password");
            }
            settle(trimmed);
          },
          onCancel: () => settle(null)
        });
      });
    },
    [message, modal]
  );

  const handleRevealConnectionPassword = useCallback(async () => {
    if (!selectedConnection || !primarySelectedId) {
      return;
    }
    if (
      selectedConnection.authType !== "password" &&
      selectedConnection.authType !== "interactive"
    ) {
      message.warning("仅密码或交互式认证连接支持查看登录密码。");
      return;
    }

    const unlocked = await getMasterPasswordAvailability();
    const inputPassword = await promptMasterPasswordForReveal(unlocked);
    if (inputPassword === null) {
      return;
    }

    try {
      setRevealingLoginPassword(true);
      const result = await window.nextshell.connection.revealPassword(
        inputPassword
          ? { connectionId: primarySelectedId, masterPassword: inputPassword }
          : { connectionId: primarySelectedId }
      );
      setRevealedLoginPassword(result.password);
      if (revealPasswordTimeoutRef.current) {
        clearTimeout(revealPasswordTimeoutRef.current);
      }
      revealPasswordTimeoutRef.current = setTimeout(() => {
        setRevealedLoginPassword(undefined);
        revealPasswordTimeoutRef.current = undefined;
      }, 30_000);
      message.success("已显示登录密码，30 秒后自动隐藏。");
    } catch (error) {
      message.error(`查看登录密码失败：${formatErrorMessage(error, "请检查主密码")}`);
    } finally {
      setRevealingLoginPassword(false);
    }
  }, [
    getMasterPasswordAvailability,
    message,
    primarySelectedId,
    promptMasterPasswordForReveal,
    selectedConnection
  ]);

  return {
    clearRevealConnectionPassword,
    handleRevealConnectionPassword,
    revealedLoginPassword,
    revealingLoginPassword
  };
};
