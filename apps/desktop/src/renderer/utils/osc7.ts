interface Osc7ParserState {
  pending: string;
}

interface Osc7ChunkResult {
  state: Osc7ParserState;
  visibleText: string;
  cwdPath?: string;
}

const OSC7_PREFIX = "\u001b]7;";
const BEL = "\u0007";
const ST = "\u001b\\";

const normalizePosixPath = (rawPath: string): string | undefined => {
  const trimmed = rawPath.trim();
  if (!trimmed.startsWith("/")) {
    return undefined;
  }

  const normalized = trimmed.replace(/\/+/g, "/").replace(/\/$/, "");
  return normalized || "/";
};

const parseOsc7Path = (payload: string): string | undefined => {
  let url: URL;
  try {
    url = new URL(payload);
  } catch {
    return undefined;
  }

  if (url.protocol !== "file:") {
    return undefined;
  }

  try {
    return normalizePosixPath(decodeURIComponent(url.pathname));
  } catch {
    return undefined;
  }
};

const findTerminator = (
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

const getTrailingPrefixStart = (input: string): number => {
  const maxPrefixLength = OSC7_PREFIX.length - 1;
  const start = Math.max(0, input.length - maxPrefixLength);

  for (let index = start; index < input.length; index += 1) {
    if (OSC7_PREFIX.startsWith(input.slice(index))) {
      return index;
    }
  }

  return -1;
};

export const createOsc7ParserState = (): Osc7ParserState => ({
  pending: ""
});

export const consumeOsc7Chunk = (state: Osc7ParserState, chunk: string): Osc7ChunkResult => {
  if (!chunk) {
    return {
      state,
      visibleText: ""
    };
  }

  const input = `${state.pending}${chunk}`;
  let cursor = 0;
  let visibleText = "";
  let cwdPath: string | undefined;

  while (cursor < input.length) {
    const start = input.indexOf(OSC7_PREFIX, cursor);
    if (start < 0) {
      const tailStart = getTrailingPrefixStart(input.slice(cursor));
      if (tailStart >= 0) {
        const relativeVisible = input.slice(cursor);
        visibleText += relativeVisible.slice(0, tailStart);
        return {
          state: {
            pending: relativeVisible.slice(tailStart)
          },
          visibleText,
          cwdPath
        };
      }

      visibleText += input.slice(cursor);
      return {
        state: createOsc7ParserState(),
        visibleText,
        cwdPath
      };
    }

    visibleText += input.slice(cursor, start);
    const terminator = findTerminator(input, start + OSC7_PREFIX.length);
    if (!terminator) {
      return {
        state: {
          pending: input.slice(start)
        },
        visibleText,
        cwdPath
      };
    }

    const payload = input.slice(start + OSC7_PREFIX.length, terminator.end);
    const parsedPath = parseOsc7Path(payload);
    if (parsedPath) {
      cwdPath = parsedPath;
    }
    cursor = terminator.end + terminator.length;
  }

  return {
    state: createOsc7ParserState(),
    visibleText,
    cwdPath
  };
};
