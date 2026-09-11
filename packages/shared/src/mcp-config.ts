/** Publish this exact version before releasing desktop/plugin configurations. */
export const MCP_PACKAGE_SPEC = "@nextshell/mcp@0.1.0";
export const buildStdioConfig = (port: number) => ({
  command: "npx",
  args: ["-y", MCP_PACKAGE_SPEC, "--port", String(port)]
});
