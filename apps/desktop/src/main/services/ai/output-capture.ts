/**
 * 去除 ANSI 控制序列，保留纯文本内容。
 */
export const stripAnsi = (text: string): string => {
  // ESC[ ... m  (SGR), ESC[ ... 其它, OSC sequences, 以及其它常见序列
  return text
    .replace(/\x1B\[[0-9;]*[a-zA-Z]/g, "")
    .replace(/\x1B\][^\x07]*\x07/g, "")
    .replace(/\x1B\(B/g, "")
    .replace(/\r/g, "");
};

/**
 * 从终端输出中检测 shell prompt，判断命令是否执行完毕。
 * 常见 prompt 模式：`$`, `#`, `>`, `%` 在行尾。
 */
export const detectPromptEnd = (text: string): boolean => {
  const lines = text.split("\n").filter((l) => l.trim().length > 0);
  if (lines.length === 0) return false;

  const lastLine = lines[lines.length - 1]?.trim() ?? "";
  return /[$#%>]\s*$/.test(lastLine);
};

/**
 * 终端输出聚合器：收集一段时间内的输出，
 * 直到检测到 prompt 或超时。
 */
export class OutputCollector {
  private chunks: string[] = [];
  private resolve?: (output: string) => void;
  private timeoutId?: ReturnType<typeof setTimeout>;
  private promptCheckId?: ReturnType<typeof setInterval>;

  collect(timeoutMs: number): Promise<string> {
    return new Promise<string>((resolve) => {
      this.resolve = resolve;

      this.timeoutId = setTimeout(() => {
        this.finish();
      }, timeoutMs);

      this.promptCheckId = setInterval(() => {
        const combined = this.chunks.join("");
        const cleaned = stripAnsi(combined);
        if (detectPromptEnd(cleaned) && this.chunks.length > 1) {
          this.finish();
        }
      }, 200);
    });
  }

  push(data: string): void {
    this.chunks.push(data);
  }

  private finish(): void {
    if (this.timeoutId) clearTimeout(this.timeoutId);
    if (this.promptCheckId) clearInterval(this.promptCheckId);

    const combined = this.chunks.join("");
    const cleaned = stripAnsi(combined);
    this.resolve?.(cleaned);
    this.resolve = undefined;
  }

  dispose(): void {
    if (this.timeoutId) clearTimeout(this.timeoutId);
    if (this.promptCheckId) clearInterval(this.promptCheckId);
    this.resolve?.("");
  }
}
