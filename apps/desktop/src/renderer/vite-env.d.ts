/// <reference types="vite/client" />

import type { NextShellApi } from "@nextshell/shared";

declare global {
  interface Window {
    nextshell: NextShellApi;
  }
}

export {};
