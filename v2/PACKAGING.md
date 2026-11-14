# 打包与发布 NextShell 2

```sh
cd v2 && go tool mygo build -skip-dmg    # -> dist/darwin-arm64/NextShell.app(本机自用,约 25 MB)
```

- CLI 是 `go.mod` 里的 Go tool(mygo v0.9.0),配置在 `mygo.json`:名称 `NextShell`,标识 `io.github.hynor.nextshell`,版本(见下「发布」),入口 `./cmd/nextshell`,macOS ad-hoc 签名,Linux 包只依赖 GTK 3(`linux.webview: false`,不依赖 WebKitGTK;托盘图标用 libayatana-appindicator3,运行时按需加载,没有它时设置里不出现托盘选项;托盘选项开着(启动时,或之后在设置里打开的那一刻)且没有 `--db` 时,应用取 mygo 的单实例锁(socket 在临时目录),再次启动只会把第一个实例的窗口显示出来,以免窗口隐藏后在没有托盘宿主的桌面上找不回)。
- `dist/` 已被 git 忽略。发布构建由 mygo 自动加 `-tags mygo_nodownload,mygo_noinspector`,不带 `nextshell_devtools`,所以没有 DevTools/`--agent-test`。
- 图标 `packaging/icon.png` 由 `go run packaging/icon.go`(在 `v2/` 下)生成;mygo 从它生成 `.icns`。
- 窗口、菜单栏(About / 设置 ⌘, / Hide / Quit)、退出确认与 `go run` 一致;默认 profile 仍是 `~/Library/Application Support/NextShell-v2/nextshell.db`。

## 安装

把 `NextShell.app` 拖进 `/Applications`。本机构建的应用是 ad-hoc 签名且没有隔离属性,直接双击即可。若是经 AirDrop/下载拷来的,首次需右键 → 打开(或 `xattr -dr com.apple.quarantine /Applications/NextShell.app`)。

Cmd+W:主窗口关当前标签,设置/远程编辑器窗口关自己(菜单里没有 Close Window,否则菜单会截走主窗口的 Cmd+W)。需要退出确认时应用会请求前置;若在后台(如 Dock → Quit),macOS 14+ 可能不允许应用自行激活,此时 Dock 图标持续跳动,点开即见确认框。

试用不碰真实数据:`open -n dist/darwin-arm64/NextShell.app --args --db /tmp/try/t.db`。

## DMG

去掉 `-skip-dmg` 即生成 `nextshell-<版本>-arm64.dmg`(`darwin/universal` 为 `nextshell-<版本>.dmg`)。mygo 在输出目录里挂载临时镜像,输出目录在外置卷上时 `hdiutil attach` 会报「权限被拒绝」(此环境复现),此时用 `-o <内置磁盘上的目录>`。

## 发布(GitHub Actions)

流程在 `.github/workflows/v2-release.yml`,产物都是**未签名**的预发布包:macOS ad-hoc、Windows 无签名、不公证、不配自动更新(没有 `updates`、没有密钥)。

1. 改 `v2/mygo.json` 的 `version`(如 `2.0.0-rc1`),在 `v2/CHANGELOG.md` 加 `## 2.0.0-rc1` 一节(整节原样成为 release 说明,写给用户看)。
   **同时更新 URL 里的 tag 和桥版本**:`nextshell-plugin/.mcp.json`、`nextshell-plugin/README.md`、`packages/mcp/README.md` 和 CHANGELOG 里都有 `…/releases/download/v<版本>/nextshell-mcp-<桥版本>.tgz`。桥版本是 `packages/mcp/package.json` 的 `version`,改了桥(含 `tools.generated.json`)要先升它。tag 运行会检查 `.mcp.json` 的 URL(不符立即失败),`packages/mcp` 的测试检查它与桥版本一致。
2. 推到分支 `v2`。改动了工作流、`v2/mygo.json` 或 `v2/packaging/**` 的推送会跑**只构建**模式:`go vet` + `go test`,再在三个原生 runner 上打包,包留在这次运行的 artifacts 里(保留 7 天),不创建 release。打 tag 之前先在这里看一遍包。
3. 打 tag 并推送:`git tag v2.0.0-rc1 && git push origin v2.0.0-rc1`。tag 必须是 `v` + `mygo.json` 的版本,不符立即失败(`packaging/check-version.sh`,本机可跑:`packaging/check-version.sh v2.0.0-rc1`)。

tag 运行的顺序:版本检查(含插件 URL)→ `go vet` + `go test ./...`(Linux)→ 三平台和 MCP 桥并行打包 → 全部成功后才发布(`gh release create --prerelease --latest=false`,说明取自 CHANGELOG 对应小节,附 `SHA256SUMS.txt`)。上传期间 release 是草稿,上传完才公开。

| 平台 | runner | 产物 |
|---|---|---|
| macOS | `macos-latest` | `NextShell-<版本>-macos-universal.dmg`、`.zip`(`ditto -c -k --keepParent`) |
| Windows | `windows-latest`(mygo 自己下载 NSIS) | `NextShell-<版本>-windows-{amd64,arm64}-setup.exe`、`NextShell-<版本>-windows-{amd64,arm64}.zip`(`NextShell.exe` + `ghostty-vt.dll`) |
| Linux | `ubuntu-latest` | `NextShell-<版本>-linux-amd64.{tar.gz,deb,rpm}`(不出 PKGBUILD,不带 `install.sh`);deb/rpm 装 `/usr/bin/nextshell`(`linux.command`) |
| MCP 桥 | `ubuntu-latest` | `nextshell-mcp-<桥版本>.tgz`(`packages/mcp` 构建后 `npm pack`,发布前用 `npx -y ./file.tgz --help` 冒烟) |

`@nextshell/mcp` 不在 npm 上,桥作为 release 附件发布,插件用 URL 运行它:`npx --allow-remote=root -y <URL>`。`--allow-remote=root` 不能省:npm 12 默认(`allow-remote=none`)拒绝 URL 包(`EALLOWREMOTE`),root 只放行命令行上的这一个;npm 10/11 接受这个选项。注意 npm 把 `./file.tgz` 按仓库根的 `package.json` 解析,本机试 tgz 要在仓库外的目录里。

文件名由工作流统一成纯 ASCII(mygo 给 deb/rpm 的版本是 `2.0.0~rc1`,GitHub 可能改写 `~`)。某一步缺文件会让该任务失败,不会默默少发。

mygo 会把它更新器用的文件清单 `.mygo-files` 写进 Linux 应用目录;NextShell 不用 mygo 的更新器,所以 tar.gz 解包重打时排除它,deb 由 `packaging/strip-mygo-files.sh` 重打去掉(连同 md5sums 里那一行)。rpm 仍带着这个 59 字节的清单(`/opt/nextshell/.mygo-files`):重打 rpm 需要 rpmbuild 和 spec,暂未处理。

和 1.x 的隔离:

- `release-electron.yml` 对每个 tag 触发,所以它的 `tags-ignore` 排除了 `v2.*`;1.x 的 `v0.*` tag 不受影响。GitHub 用 tag 所在提交里的工作流文件,所以这个改动必须在被打 tag 的提交里。
- 1.x 的更新检查读仓库的 latest release。这里一律 `--prerelease --latest=false`,latest 仍是 1.x。**发正式版 2.0.0 之前**要先决定怎么处理这一点(工作流现在写死预发布)。
- `v2.yml`(测试)只有 `paths` 过滤、没有分支限制,GitHub 不对 tag 推送评估 `paths`,所以 v2 tag 上它大概率也会跑一遍,只是多一份测试。

失败重跑:构建任务失败用「Re-run failed jobs」。发布任务若中途失败,先到 Releases 删掉它留下的草稿(tag 保留),再重跑。

## 暂不做

Developer ID 签名与公证、Windows 代码签名、自动更新(`mygo keygen` + `updates`;rc1 刻意不开,装了更新配置的应用会去读仓库的 latest release)、PKGBUILD / `install.sh`、Linux arm64、i18n 目录(菜单项目前是英文)。
