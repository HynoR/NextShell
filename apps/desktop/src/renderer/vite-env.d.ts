/// <reference types="vite/client" />

import type { NextShellApi } from "@nextshell/shared";

declare global {
  const __APP_VERSION__: string;
  const __GITHUB_REPO__: string;

  interface Window {
    nextshell: NextShellApi;
  }
}

export {};
