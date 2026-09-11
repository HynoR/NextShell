# @nextshell/mcp

NextShell supports **stdio and Streamable HTTP**. Both use the same tools and the
same in-app authorization, command restrictions and activity reporting.

## stdio: offline discovery

Requires Node.js 24+. After `@nextshell/mcp@0.1.0` is published:

```json
{
  "mcpServers": {
    "nextshell": {
      "command": "npx",
      "args": ["-y", "@nextshell/mcp@0.1.0", "--port", "41777"]
    }
  }
}
```

The first download requires network access. The harness starts this small process;
initialization, tool discovery and ping do not contact or launch NextShell.
When a tool is called, open NextShell and enable 设置 → Agent 接入. If unavailable,
the tool returns `app_unavailable`; after opening the app the next call reconnects.
A lost response returns `connection_lost`: execution may already have happened.
Inspect the terminal before retrying; the bridge never replays the command.
An `incompatible_version` error means the app and package tool schemas differ:
update both and restart the MCP process. Client-side tool timeouts may need to be
increased for long commands; the bridge accommodates the app's one-hour limit.

`--port` defaults to 41777 and accepts integers 1–65535. The host is always
127.0.0.1; redirects and background reconnect loops are disabled. Use the port
configured in NextShell. No credential or application path belongs in the config.

## HTTP: direct connection

No Node.js required. Start NextShell and enable Agent access **before connecting**:

```json
{
  "mcpServers": {
    "nextshell": { "type": "http", "url": "http://127.0.0.1:41777/mcp" }
  }
}
```

HTTP is a fully supported alternative, not deprecated. Different clients can use
both methods concurrently. Usually choose one method per harness to avoid duplicate
tools. Stdio-only clients should use the stdio configuration above.

## Development and release

From the repository root:

```sh
pnpm --filter @nextshell/mcp run build
pnpm exec vitest run packages/mcp/src
pnpm --filter @nextshell/mcp run smoke:pack
```

`smoke:pack` packs and installs into an isolated temporary directory, then starts
the installed bin over stdio and verifies discovery with no application running.
It requires npm registry access and removes its temporary directory on completion.

Release order:

1. Confirm publishing rights for the `@nextshell` npm scope. No publishing credential
   is stored in this repository. The package is prepared locally, not automatically published.
2. Keep the package version, `MCP_PACKAGE_SPEC`, and plugin config aligned; run CI
   and the packed-install smoke check.
3. Publish from `packages/mcp` with `npm publish --access public` after building.
4. Verify `npm view @nextshell/mcp@0.1.0 version`, then release the desktop/plugin
   configurations that refer to that version. Do not ship an unpublished default.

Acceptance: unit/integration tests cover offline discovery, reconnection, schema
compatibility, concurrent HTTP/stdio access, policy result forwarding and no replay.
Real app approval/terminal flows and harness launch on macOS, Windows and Linux
require separate platform smoke testing; unit tests do not establish that acceptance.
