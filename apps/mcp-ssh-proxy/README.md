# NextShell MCP SSH Proxy

独立发布的本地 `stdio` MCP 程序，用来复用 NextShell 已保存的 SSH 凭据。

## 能力

- `nextshell/list`
- `nextshell/search`
- `nextshell/connect`
- `nextshell/exec`
- `nextshell/disconnect`

## 约束

- 不提供交互式 shell。
- 每次 `exec` 都是单次远程命令执行，不保留 shell 上下文。
- 不返回密码、私钥、passphrase 或任何 secret store 原始内容。

## 本地运行

```bash
bun install
bun run --cwd apps/mcp-ssh-proxy build
node /absolute/path/to/apps/mcp-ssh-proxy/dist/index.js
```

也可以在 MCP 客户端里直接配置该构建产物的绝对路径。

## MCP 客户端配置示例

```json
{
  "mcpServers": {
    "nextshell-ssh": {
      "command": "node",
      "args": [
        "/absolute/path/to/apps/mcp-ssh-proxy/dist/index.js"
      ]
    }
  }
}
```

## 数据来源

程序默认读取 NextShell 桌面端的本地数据目录：

- macOS: `~/Library/Application Support/NextShell/storage/nextshell.db`
- Windows: `%APPDATA%\\NextShell\\storage\\nextshell.db`
- Linux: `${XDG_CONFIG_HOME:-~/.config}/NextShell/storage/nextshell.db`

测试或自定义环境下也可以通过以下环境变量覆盖：

- `NEXTSHELL_DB_PATH`
- `NEXTSHELL_DATA_DIR`
