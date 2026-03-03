cask "nextshell" do
  version "0.2.6"
  sha256 :no_check

  url "https://github.com/HynoR/NextShell/releases/download/v#{version}/NextShell-#{version}-mac-arm64.dmg"
  name "NextShell"
  desc "Terminal workspace for development workflows"
  homepage "https://github.com/HynoR/NextShell"

  livecheck do
    url "https://github.com/HynoR/NextShell/releases/latest"
    strategy :github_latest
  end

  depends_on arch: :arm64

  app "NextShell.app"

  zap trash: [
    "~/Library/Application Support/NextShell",
    "~/Library/Preferences/com.nextshell.desktop.plist",
    "~/Library/Saved Application State/com.nextshell.desktop.savedState",
  ]
end
