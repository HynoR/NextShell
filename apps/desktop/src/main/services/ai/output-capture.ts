/** 单次提交给 LLM 分析的最大字符数（留余量给 system prompt 和历史消息） */
const LLM_OUTPUT_LIMIT = 6000;
/** 超长输出时，头部保留的行数 */
const HEAD_LINES = 30;
/** 超长输出时，尾部保留的行数 */
const TAIL_LINES = 50;

export interface TruncatedOutput {
  /** 处理后的文本，可直接嵌入 prompt */
  text: string;
  /** 原始总行数 */
  totalLines: number;
  /** 原始总字符数 */
  totalChars: number;
  /** 是否被截断过 */
  wasTruncated: boolean;
}

/**
 * 智能压缩命令输出：
 * 1. 短输出（≤ LLM_OUTPUT_LIMIT）直接返回
 * 2. 超长输出：保留头部 + 尾部（错误信息通常在末尾），中间用省略标记
 * 3. 如果头部+尾部仍然超限，按字符截断尾部
 */
export const truncateOutput = (raw: string, limit = LLM_OUTPUT_LIMIT): TruncatedOutput => {
  const totalChars = raw.length;
  const lines = raw.split("\n");
  const totalLines = lines.length;

  if (totalChars <= limit) {
    return { text: raw, totalLines, totalChars, wasTruncated: false };
  }

  const headPart = lines.slice(0, HEAD_LINES);
  const tailPart = lines.slice(-TAIL_LINES);
  const omitted = totalLines - HEAD_LINES - TAIL_LINES;

  const marker = `\n... [省略中间 ${omitted} 行，共 ${totalLines} 行 / ${totalChars} 字符] ...\n`;
  let combined = headPart.join("\n") + marker + tailPart.join("\n");

  if (combined.length > limit) {
    const halfLimit = Math.floor((limit - marker.length) / 2);
    const headText = headPart.join("\n").slice(0, halfLimit);
    const tailText = tailPart.join("\n").slice(-halfLimit);
    combined = headText + marker + tailText;
  }

  return { text: combined, totalLines, totalChars, wasTruncated: true };
};

const PARTIAL_MASK = "****";

const maskKeepEdges = (value: string, prefixLength = 4, suffixLength = 4): string => {
  if (value.length <= prefixLength + suffixLength) {
    return PARTIAL_MASK;
  }
  return `${value.slice(0, prefixLength)}${PARTIAL_MASK}${value.slice(-suffixLength)}`;
};

/**
 * 脱敏 AI 分析前的命令输出，避免把常见密钥或口令原样发给外部模型。
 */
export const sanitizeAiOutput = (raw: string): string => {
  return raw
    .replace(
      /(authorization\s*:\s*bearer\s+)([^\s]+)/gi,
      (_match, prefix: string, token: string) => `${prefix}${maskKeepEdges(token)}`
    )
    .replace(
      /((?:token|password|passwd|pwd|apikey|api_key)\s*[:=]\s*)([^\s"'`]+)/gi,
      (_match, prefix: string, secret: string) => `${prefix}${maskKeepEdges(secret)}`
    )
    .replace(
      /((?:OPENAI|ANTHROPIC|GEMINI)_[A-Z_]*KEY\s*[:=]\s*)([^\s"'`]+)/g,
      (_match, prefix: string, secret: string) => `${prefix}${maskKeepEdges(secret)}`
    )
    .replace(
      /(-----BEGIN [A-Z ]*PRIVATE KEY-----)([\s\S]*?)(-----END [A-Z ]*PRIVATE KEY-----)/g,
      (_match, begin: string, _body: string, end: string) => `${begin}\n${PARTIAL_MASK}\n${end}`
    );
};

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
