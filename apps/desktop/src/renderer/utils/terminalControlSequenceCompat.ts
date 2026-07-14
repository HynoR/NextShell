type ParserDisposable = {
  dispose: () => void;
};

type ParserLike = {
  registerCsiHandler: (
    id: { prefix?: string; intermediates?: string; final: string },
    callback: (params: (number | number[])[]) => boolean | Promise<boolean>
  ) => ParserDisposable;
  registerOscHandler: (
    ident: number,
    callback: (data: string) => boolean | Promise<boolean>
  ) => ParserDisposable;
  registerDcsHandler: (
    id: { prefix?: string; intermediates?: string; final: string },
    callback: (data: string, params: (number | number[])[]) => boolean | Promise<boolean>
  ) => ParserDisposable;
};

type TerminalLike = {
  parser: ParserLike;
};

export type TerminalQuerySuppressionKind =
  | "device-attributes"
  | "ansi-mode-request"
  | "private-mode-request"
  | "osc-color-query"
  | "status-string-request";

interface TerminalQueryCompatibilityOptions {
  isEnabled?: () => boolean;
  onSuppressed?: (kind: TerminalQuerySuppressionKind) => void;
}

interface TerminalQueryReplyFilterState {
  pending: string;
}

interface TerminalQueryReplyFilterResult {
  state: TerminalQueryReplyFilterState;
  text: string;
}

const ESC = "\u001b";
const BEL = "\u0007";
const ST = `${ESC}\\`;

const CSI_SECONDARY_DEVICE_ATTRIBUTES_REPLY = /^\u001b\[\>\d+(?:;\d+)*c/;
const CSI_MODE_REPLY = /^\u001b\[\??\d+(?:;\d+)*\$y/;
const OSC_COLOR_REPLY_PREFIX = /^\u001b\](10|11|12);/;
const OSC_COLOR_REPLY_PAYLOAD =
  /^(rgb:[0-9a-fA-F]{1,4}\/[0-9a-fA-F]{1,4}\/[0-9a-fA-F]{1,4}|#[0-9a-fA-F]{3,12})$/;
const DCS_STATUS_REPLY_PREFIX = /^\u001bP([01])\$r/;

const isOscColorQuery = (data: string): boolean =>
  data.split(";").some((segment) => segment.trim() === "?");

const shouldHandle = (options?: TerminalQueryCompatibilityOptions): boolean =>
  options?.isEnabled ? options.isEnabled() : true;

const markSuppressed = (
  kind: TerminalQuerySuppressionKind,
  options?: TerminalQueryCompatibilityOptions
): true => {
  options?.onSuppressed?.(kind);
  return true;
};

const findSequenceTerminator = (
  input: string,
  from: number
): { end: number; length: number } | undefined => {
  const belIndex = input.indexOf(BEL, from);
  const stIndex = input.indexOf(ST, from);

  if (belIndex < 0 && stIndex < 0) {
    return undefined;
  }

  if (belIndex >= 0 && (stIndex < 0 || belIndex < stIndex)) {
    return { end: belIndex, length: BEL.length };
  }

  return { end: stIndex, length: ST.length };
};

const consumeKnownReply = (
  input: string,
  cursor: number
): { nextCursor: number; recognized: boolean; incomplete: boolean } => {
  const remaining = input.slice(cursor);

  const daMatch = remaining.match(CSI_SECONDARY_DEVICE_ATTRIBUTES_REPLY);
  if (daMatch) {
    return {
      nextCursor: cursor + daMatch[0].length,
      recognized: true,
      incomplete: false
    };
  }

  if (/^\u001b\[\>$/.test(remaining) || /^\u001b\[\>\d[\d;]*$/.test(remaining)) {
    return {
      nextCursor: cursor,
      recognized: false,
      incomplete: true
    };
  }

  const modeMatch = remaining.match(CSI_MODE_REPLY);
  if (modeMatch) {
    return {
      nextCursor: cursor + modeMatch[0].length,
      recognized: true,
      incomplete: false
    };
  }

  if (/^\u001b\[\??[\d;]*\$?$/.test(remaining)) {
    return {
      nextCursor: cursor,
      recognized: false,
      incomplete: true
    };
  }

  if (OSC_COLOR_REPLY_PREFIX.test(remaining)) {
    const terminator = findSequenceTerminator(input, cursor + 5);
    if (!terminator) {
      return {
        nextCursor: cursor,
        recognized: false,
        incomplete: true
      };
    }

    const payload = input.slice(cursor + 5, terminator.end);
    if (OSC_COLOR_REPLY_PAYLOAD.test(payload)) {
      return {
        nextCursor: terminator.end + terminator.length,
        recognized: true,
        incomplete: false
      };
    }
  }

  if (DCS_STATUS_REPLY_PREFIX.test(remaining)) {
    const terminator = findSequenceTerminator(input, cursor + 4);
    if (!terminator) {
      return {
        nextCursor: cursor,
        recognized: false,
        incomplete: true
      };
    }

    return {
      nextCursor: terminator.end + terminator.length,
      recognized: true,
      incomplete: false
    };
  }

  if (/^\u001bP(?:[01])?\$r.*$/.test(remaining)) {
    return {
      nextCursor: cursor,
      recognized: false,
      incomplete: true
    };
  }

  return {
    nextCursor: cursor + 1,
    recognized: false,
    incomplete: false
  };
};

export const createTerminalQueryReplyFilterState = (): TerminalQueryReplyFilterState => ({
  pending: ""
});

export const consumeTerminalQueryReplyChunk = (
  state: TerminalQueryReplyFilterState,
  chunk: string
): TerminalQueryReplyFilterResult => {
  if (!chunk) {
    return {
      state,
      text: ""
    };
  }

  const input = `${state.pending}${chunk}`;
  let cursor = 0;
  let text = "";

  while (cursor < input.length) {
    if (input[cursor] !== ESC) {
      text += input[cursor];
      cursor += 1;
      continue;
    }

    const consumed = consumeKnownReply(input, cursor);
    if (consumed.incomplete) {
      return {
        state: {
          pending: input.slice(cursor)
        },
        text
      };
    }

    if (consumed.recognized) {
      cursor = consumed.nextCursor;
      continue;
    }

    text += input[cursor];
    cursor += 1;
  }

  return {
    state: createTerminalQueryReplyFilterState(),
    text
  };
};

export const installTerminalQueryCompatibilityGuards = (
  terminal: TerminalLike,
  options?: TerminalQueryCompatibilityOptions
): ParserDisposable => {
  const disposables: ParserDisposable[] = [
    terminal.parser.registerCsiHandler({ prefix: ">", final: "c" }, () => {
      if (!shouldHandle(options)) {
        return false;
      }
      return markSuppressed("device-attributes", options);
    }),
    terminal.parser.registerCsiHandler({ intermediates: "$", final: "p" }, () => {
      if (!shouldHandle(options)) {
        return false;
      }
      return markSuppressed("ansi-mode-request", options);
    }),
    terminal.parser.registerCsiHandler({ prefix: "?", intermediates: "$", final: "p" }, () => {
      if (!shouldHandle(options)) {
        return false;
      }
      return markSuppressed("private-mode-request", options);
    }),
    terminal.parser.registerOscHandler(10, (data) => {
      if (!shouldHandle(options) || !isOscColorQuery(data)) {
        return false;
      }
      return markSuppressed("osc-color-query", options);
    }),
    terminal.parser.registerOscHandler(11, (data) => {
      if (!shouldHandle(options) || !isOscColorQuery(data)) {
        return false;
      }
      return markSuppressed("osc-color-query", options);
    }),
    terminal.parser.registerOscHandler(12, (data) => {
      if (!shouldHandle(options) || !isOscColorQuery(data)) {
        return false;
      }
      return markSuppressed("osc-color-query", options);
    }),
    terminal.parser.registerDcsHandler({ intermediates: "$", final: "q" }, () => {
      if (!shouldHandle(options)) {
        return false;
      }
      return markSuppressed("status-string-request", options);
    })
  ];

  return {
    dispose() {
      for (const disposable of disposables) {
        disposable.dispose();
      }
    }
  };
};
