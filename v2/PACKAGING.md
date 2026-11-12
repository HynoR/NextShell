# 打包 NextShell 2(本机自用)

```sh
cd v2 && go tool mygo build -skip-dmg    # -> dist/darwin-arm64/NextShell.app(约 25 MB)
```

- CLI 是 `go.mod` 里的 Go tool(mygo v0.9.0),配置在 `mygo.json`:名称 `NextShell`,标识 `io.github.hynor.nextshell`,版本 `2.0.0-dev`,入口 `./cmd/nextshell`。
- `dist/` 已被 git 忽略。发布构建由 mygo 自动加 `-tags mygo_nodownload,mygo_noinspector`,不带 `nextshell_devtools`,所以没有 DevTools/`--agent-test`。
- 图标 `packaging/icon.png` 由 `go run packaging/icon.go`(在 `v2/` 下)生成;mygo 从它生成 `.icns`。
- 窗口、菜单栏(About / 设置 ⌘, / Hide / Quit)、退出确认与 `go run` 一致;默认 profile 仍是 `~/Library/Application Support/NextShell-v2/nextshell.db`。

## 安装

把 `NextShell.app` 拖进 `/Applications`。本机构建的应用是 ad-hoc 签名且没有隔离属性,直接双击即可。若是经 AirDrop/下载拷来的,首次需右键 → 打开(或 `xattr -dr com.apple.quarantine /Applications/NextShell.app`)。

试用不碰真实数据:`open -n dist/darwin-arm64/NextShell.app --args --db /tmp/try/t.db`。

## DMG

去掉 `-skip-dmg` 即生成 `nextshell-2.0.0-dev-arm64.dmg`。mygo 在输出目录里挂载临时镜像,输出目录在外置卷上时 `hdiutil attach` 会报「权限被拒绝」(此环境复现),此时用 `-o <内置磁盘上的目录>`。

## 暂不做(M5)

Developer ID 签名与公证、自动更新(`mygo keygen` + `updates`)、Windows 安装包、Linux 包、CI 发布流程、i18n 目录(菜单项目前是英文)。
