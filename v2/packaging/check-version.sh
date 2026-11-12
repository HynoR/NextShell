#!/usr/bin/env bash
# check-version.sh [tag]: print the version of v2/mygo.json. With a tag (v2-release.yml passes
# $GITHUB_REF_NAME), fail unless the tag is "v" + that version, e.g. v2.0.0-rc1 for 2.0.0-rc1,
# and the plugin's .mcp.json pins this release's MCP bridge tarball (the release attaches it).
set -euo pipefail
root=$(dirname "$0")/../..
version=$(jq -er .version "$root/v2/mygo.json")
if [[ -n ${1:-} ]]; then
  [[ $1 == "v$version" ]] || { echo "tag $1 does not match v2/mygo.json version $version (expected v$version)" >&2; exit 1; }
  [[ $version =~ ^2\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || { echo "version $version is not a 2.x semver" >&2; exit 1; }
  url=https://github.com/HynoR/NextShell/releases/download/$1/nextshell-mcp-$(jq -er .version "$root/packages/mcp/package.json").tgz
  jq -e --arg u "$url" '.mcpServers.nextshell.args | index($u) != null' "$root/nextshell-plugin/.mcp.json" >/dev/null ||
    { echo "nextshell-plugin/.mcp.json must run the bridge from $url" >&2; exit 1; }
fi
echo "$version"
