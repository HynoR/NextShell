import { useEffect, useState } from "react";
import { Button, Modal, Space, Typography } from "antd";
import type { SessionDescriptor } from "@nextshell/core";
import type { AgentOpenRequestEvent } from "@nextshell/shared";
import { useAgentActivityStore } from "../store/useAgentActivityStore";

interface Props {
  /** The same path a click in the connection manager takes, so the tab is a normal tab. */
  startSession: (connectionId: string) => Promise<SessionDescriptor | undefined>;
}

const formatRemaining = (expiresAt: string, now: number): string => {
  const seconds = Math.max(0, Math.floor((new Date(expiresAt).getTime() - now) / 1000));
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`;
};

/**
 * The one dialog in the agent surface: an agent asked to open a host, or (with
 * `execApproval` on permission) to run a command. Every pending request is listed;
 * nothing happens until the user clicks 授权, and the main process denies
 * anything unanswered after five minutes.
 */
export const AgentOpenRequestModal = ({ startSession }: Props) => {
  const requests = useAgentActivityStore((s) => s.openRequests);
  const remove = useAgentActivityStore((s) => s.removeOpenRequest);
  const [busy, setBusy] = useState<string | null>(null);
  const [now, setNow] = useState(() => Date.now());

  // Countdown, and drop entries the main process has already timed out.
  useEffect(() => {
    if (requests.length === 0) return;
    const timer = setInterval(() => {
      const current = Date.now();
      setNow(current);
      for (const request of requests) {
        if (new Date(request.expiresAt).getTime() <= current) remove(request.id);
      }
    }, 1000);
    return () => clearInterval(timer);
  }, [requests, remove]);

  const deny = (request: AgentOpenRequestEvent): void => {
    remove(request.id);
    void window.nextshell.agent.respondOpen({ id: request.id, approved: false });
  };

  const approve = async (request: AgentOpenRequestEvent): Promise<void> => {
    if (request.kind === "exec") {
      remove(request.id);
      void window.nextshell.agent.respondOpen({ id: request.id, approved: true });
      return;
    }
    setBusy(request.id);
    try {
      const session = await startSession(request.connectionId);
      await window.nextshell.agent.respondOpen({
        id: request.id,
        approved: true,
        ...(session ? { sessionId: session.id } : {})
      });
    } finally {
      setBusy(null);
      remove(request.id);
    }
  };

  return (
    <Modal
      open={requests.length > 0}
      title="Agent 请求授权"
      footer={null}
      closable={false}
      maskClosable={false}
      width={520}
    >
      <Typography.Paragraph type="secondary" style={{ fontSize: 12 }}>
        只有你点击「授权」才会执行；不处理的请求会在倒计时结束后自动拒绝。
      </Typography.Paragraph>
      <Space direction="vertical" style={{ width: "100%" }} size={12}>
        {requests.map((request) => (
          <div key={request.id} className="agent-open-request">
            <div className="flex items-center justify-between gap-3">
              <div className="min-w-0">
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  {request.kind === "exec"
                    ? `在「${request.connectionName}」执行（${request.mode === "background" ? "后台" : "前台"}）`
                    : "打开服务器"}
                </Typography.Text>
                {request.kind === "exec" ? (
                  <pre className="agent-open-request-command">{request.command}</pre>
                ) : (
                  <div>
                    <Typography.Text strong>{request.connectionName}</Typography.Text>
                    <Typography.Text type="secondary" style={{ marginLeft: 8, fontSize: 12 }}>
                      {request.host}
                    </Typography.Text>
                  </div>
                )}
                <div style={{ fontSize: 12 }}>
                  <Typography.Text type="secondary">
                    {request.clientName ?? "未知客户端"} · 剩余{" "}
                    {formatRemaining(request.expiresAt, now)}
                  </Typography.Text>
                </div>
                {request.reason && (
                  <Typography.Paragraph
                    style={{ marginBottom: 0, marginTop: 4, whiteSpace: "pre-wrap" }}
                    ellipsis={{ rows: 3, expandable: true }}
                  >
                    {request.reason}
                  </Typography.Paragraph>
                )}
              </div>
              <Space>
                <Button size="small" disabled={busy !== null} onClick={() => deny(request)}>
                  拒绝
                </Button>
                <Button
                  size="small"
                  type="primary"
                  loading={busy === request.id}
                  disabled={busy !== null && busy !== request.id}
                  onClick={() => void approve(request)}
                >
                  授权
                </Button>
              </Space>
            </div>
          </div>
        ))}
      </Space>
    </Modal>
  );
};
