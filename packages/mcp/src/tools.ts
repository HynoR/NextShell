import generated from "./tools.generated.json";

/**
 * The tool list of NextShell 2, served while the app is closed so stdio clients can discover tools.
 *
 * The app is the single source of truth: tools.generated.json is written from the app's own tools/list
 * (v2/internal/agent/tools.go via mcpendpoint.Tools()) and a Go test fails when it is stale. Regenerate
 * with `NEXTSHELL_UPDATE_TOOLS=1 go test ./internal/mcpendpoint -run Snapshot` from v2/. The bridge
 * does not validate arguments: the app does, and answers `{ ok, data?, error? }`, which the bridge
 * passes through untouched.
 */
export interface ToolDefinition {
  name: string;
  title: string;
  description: string;
  inputSchema: {
    type: "object";
    properties: Record<string, unknown>;
    required?: string[];
  } & Record<string, unknown>;
  annotations: {
    readOnlyHint: boolean;
    destructiveHint: boolean;
    idempotentHint: boolean;
    openWorldHint: boolean;
  };
}

export const toolDefinitions: ToolDefinition[] = generated.tools as ToolDefinition[];
export const INSTRUCTIONS: string = generated.instructions;
