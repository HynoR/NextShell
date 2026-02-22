const AUTH_REQUIRED_PREFIX = "AUTH_REQUIRED::";

export type TerminalAuthStage = "username" | "password" | "submitting";

export interface TerminalAuthState {
  stage: TerminalAuthStage;
  username?: string;
  usernameBuffer: string;
  passwordBuffer: string;
  attempt: number;
  nonce: number;
}

export interface TerminalAuthSubmitPayload {
  username: string;
  password: string;
  nonce: number;
}

export interface ConsumeTerminalAuthInputResult {
  nextState: TerminalAuthState;
  output: string;
  submit?: TerminalAuthSubmitPayload;
  canceled?: boolean;
}

export const isAuthFailureReason = (reason?: string): reason is string =>
  typeof reason === "string" && reason.includes(AUTH_REQUIRED_PREFIX);

export const stripAuthFailurePrefix = (reason?: string): string => {
  if (!reason) {
    return "";
  }
  if (!isAuthFailureReason(reason)) {
    return reason;
  }
  const index = reason.indexOf(AUTH_REQUIRED_PREFIX);
  return reason.slice(index + AUTH_REQUIRED_PREFIX.length);
};

export const createTerminalAuthState = (): TerminalAuthState => ({
  stage: "username",
  usernameBuffer: "",
  passwordBuffer: "",
  attempt: 1,
  nonce: 0
});

export const resetTerminalAuthForRetry = (state: TerminalAuthState): TerminalAuthState => ({
  stage: "username",
  username: undefined,
  usernameBuffer: "",
  passwordBuffer: "",
  attempt: state.attempt + 1,
  nonce: state.nonce
});

export const buildTerminalAuthIntro = (reason?: string): string => {
  const detail = stripAuthFailurePrefix(reason).trim();
  if (!detail) {
    return "\r\nAuthentication required\r\nlogin as: ";
  }
  return `\r\nAuthentication required: ${detail}\r\nlogin as: `;
};

export const buildTerminalAuthRetryNotice = (reason?: string): string => {
  const detail = stripAuthFailurePrefix(reason).trim();
  if (!detail) {
    return "\r\nAuthentication failed\r\nlogin as: ";
  }
  return `\r\n${detail}\r\nlogin as: `;
};

const isBackspace = (value: string): boolean =>
  value === "\b" || value === "\x7f";

const isEnter = (value: string): boolean =>
  value === "\r" || value === "\n";

const isInterrupt = (value: string): boolean =>
  value === "\x03";

const isPrintable = (value: string): boolean => {
  if (!value) return false;
  const code = value.charCodeAt(0);
  return code >= 0x20 && code !== 0x7f;
};

export const consumeTerminalAuthInput = (
  state: TerminalAuthState,
  data: string
): ConsumeTerminalAuthInputResult => {
  let nextState = { ...state };
  let output = "";
  let submit: TerminalAuthSubmitPayload | undefined;
  let canceled = false;

  for (const char of data) {
    if (nextState.stage === "submitting") {
      break;
    }

    if (isInterrupt(char)) {
      nextState = {
        ...nextState,
        stage: "username",
        username: undefined,
        usernameBuffer: "",
        passwordBuffer: ""
      };
      output += "^C\r\nlogin as: ";
      canceled = true;
      continue;
    }

    if (nextState.stage === "username") {
      if (isEnter(char)) {
        const username = nextState.usernameBuffer.trim();
        if (!username) {
          output += "\r\nlogin as: ";
          continue;
        }
        nextState = {
          ...nextState,
          stage: "password",
          username,
          usernameBuffer: "",
          passwordBuffer: ""
        };
        output += "\r\npassword: ";
        continue;
      }

      if (isBackspace(char)) {
        if (nextState.usernameBuffer.length > 0) {
          nextState = {
            ...nextState,
            usernameBuffer: nextState.usernameBuffer.slice(0, -1)
          };
          output += "\b \b";
        }
        continue;
      }

      if (!isPrintable(char)) {
        continue;
      }

      nextState = {
        ...nextState,
        usernameBuffer: nextState.usernameBuffer + char
      };
      output += char;
      continue;
    }

    if (isEnter(char)) {
      if (!nextState.username) {
        nextState = {
          ...nextState,
          stage: "username",
          usernameBuffer: "",
          passwordBuffer: ""
        };
        output += "\r\nlogin as: ";
        continue;
      }

      if (!nextState.passwordBuffer) {
        output += "\r\npassword: ";
        continue;
      }

      const nonce = nextState.nonce + 1;
      submit = {
        username: nextState.username,
        password: nextState.passwordBuffer,
        nonce
      };
      nextState = {
        ...nextState,
        stage: "submitting",
        nonce,
        passwordBuffer: ""
      };
      output += "\r\nAuthenticating...\r\n";
      break;
    }

    if (isBackspace(char)) {
      if (nextState.passwordBuffer.length > 0) {
        nextState = {
          ...nextState,
          passwordBuffer: nextState.passwordBuffer.slice(0, -1)
        };
      }
      continue;
    }

    if (!isPrintable(char)) {
      continue;
    }

    nextState = {
      ...nextState,
      passwordBuffer: nextState.passwordBuffer + char
    };
  }

  return {
    nextState,
    output,
    submit,
    canceled
  };
};
