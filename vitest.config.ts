import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    environment: "node",
    include: [
      "apps/desktop/src/**/*.{test,spec}.{ts,tsx}",
      "packages/{mcp,core,security,shared,ssh,storage,terminal,ui-kit}/src/**/*.{test,spec}.{ts,tsx}"
    ],
    // Several regression files execute assertions during module collection.
    // Collection errors still fail the run even though those files register no test cases.
    passWithNoTests: true
  }
});
