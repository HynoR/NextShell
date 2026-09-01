import { useCallback, useEffect, useRef, useState } from "react";
import { App as AntdApp } from "antd";
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

  const handleRevealConnectionPassword = useCallback(() => {
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

    modal.confirm({
      title: "确认显示明文密码？",
      content: "密码将在当前窗口中显示，30 秒后自动隐藏。",
      okText: "显示密码",
      cancelText: "取消",
      onOk: async () => {
        try {
          setRevealingLoginPassword(true);
          const result = await window.nextshell.connection.revealPassword({
            connectionId: primarySelectedId
          });
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
          message.error(`查看登录密码失败：${formatErrorMessage(error, "请稍后重试")}`);
        } finally {
          setRevealingLoginPassword(false);
        }
      }
    });
  }, [
    message,
    modal,
    primarySelectedId,
    selectedConnection
  ]);

  return {
    clearRevealConnectionPassword,
    handleRevealConnectionPassword,
    revealedLoginPassword,
    revealingLoginPassword
  };
};
