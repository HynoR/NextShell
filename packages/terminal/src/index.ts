export interface TerminalPasteOptions {
  bracketedPaste: boolean;
  normalizeCrLf: boolean;
}

const defaultOptions: TerminalPasteOptions = {
  bracketedPaste: true,
  normalizeCrLf: true
};

export const normalizeTerminalInput = (
  raw: string,
  options: Partial<TerminalPasteOptions> = {}
): string => {
  const merged = { ...defaultOptions, ...options };
  const normalized = merged.normalizeCrLf ? raw.replace(/\r?\n/g, "\r") : raw;

  if (!merged.bracketedPaste) {
    return normalized;
  }

  return `\u001b[200~${normalized}\u001b[201~`;
};
