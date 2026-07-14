import { Drawer, Tag, Typography } from "antd";
import type { BatchCommandExecutionResult, ConnectionProfile } from "@nextshell/core";

interface BatchResultDrawerProps {
  result?: BatchCommandExecutionResult;
  connections: ConnectionProfile[];
  open: boolean;
  onClose: () => void;
}

export const BatchResultDrawer = ({
  result,
  connections,
  open,
  onClose
}: BatchResultDrawerProps) => (
  <Drawer
    title={result ? `批量执行结果：${result.command}` : "批量执行结果"}
    open={open}
    onClose={onClose}
    size="large"
  >
    {result ? (
      <div className="cc-batch-result cc-batch-drawer">
        <div className="cc-batch-summary">
          <span>
            总计 <strong>{result.total}</strong>
          </span>
          <span className="cc-batch-ok">成功 {result.successCount}</span>
          <span className="cc-batch-fail">失败 {result.failedCount}</span>
          <span>{result.durationMs}ms</span>
        </div>
        <div className="cc-batch-items">
          {result.results.map((item) => {
            const target = connections.find((c) => c.id === item.connectionId);
            return (
              <div key={`${item.connectionId}-${item.executedAt}`} className="cc-result-item">
                <div className="cc-result-item-head">
                  <span>{target?.name ?? item.connectionId}</span>
                  <Tag
                    color={item.success ? "green" : "red"}
                    style={{ margin: 0, lineHeight: "18px", fontSize: 10 }}
                  >
                    {item.success ? "成功" : "失败"} / {item.attempts}次
                  </Tag>
                </div>
                <pre className="cc-output">{item.stdout || "(empty)"}</pre>
                {item.stderr || item.error ? (
                  <pre className="cc-output error">{item.stderr || item.error}</pre>
                ) : null}
              </div>
            );
          })}
        </div>
      </div>
    ) : (
      <Typography.Text type="secondary">暂无批量执行结果。</Typography.Text>
    )}
  </Drawer>
);
