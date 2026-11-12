#!/usr/bin/env bash
# release-notes.sh <version>: print the "## <version>" section of v2/CHANGELOG.md, without its
# heading; fail when the section is missing or empty.
set -euo pipefail
version=${1:?usage: release-notes.sh <version>}
notes=$(awk -v h="## $version" '$0 == h {on = 1; next} /^## / {on = 0} on' "$(dirname "$0")/../CHANGELOG.md")
[[ -n ${notes//[[:space:]]/} ]] || { echo "no '## $version' section in v2/CHANGELOG.md" >&2; exit 1; }
printf '%s\n' "$notes"
