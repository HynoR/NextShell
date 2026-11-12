/** Publish this exact version before releasing desktop/plugin configurations. */
export const MCP_PACKAGE_SPEC = "@nextshell/mcp@0.2.0";
/** Without a port the bridge reads it, and the token, from the mcp.json the app writes. */
export const buildStdioConfig = (port?: number) => ({
  command: "npx",
  args: ["-y", MCP_PACKAGE_SPEC, ...(port === undefined ? [] : ["--port", String(port)])]
});
