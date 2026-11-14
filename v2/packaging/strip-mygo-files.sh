#!/usr/bin/env bash
# strip-mygo-files.sh <package.deb>...
#
# mygo writes its updater's file list, .mygo-files, into every Linux app directory it packages.
# NextShell does not use mygo's updater, so the file is only clutter in /opt/nextshell: this
# repacks each .deb without it (and without its md5sums line), in place, keeping the control
# fields, owners (root) and gzip compression. The tar.gz is already repacked without it by the
# release workflow. RPMs are left as they are: rebuilding one needs rpmbuild and a spec.
set -euo pipefail
for deb in "$@"; do
  work=$(mktemp -d)
  trap 'rm -rf "$work"' EXIT
  dpkg-deb -R "$deb" "$work/root"
  found=$(cd "$work/root" && find . -path ./DEBIAN -prune -o -name .mygo-files -print)
  if [ -z "$found" ]; then
    echo "$deb: no .mygo-files"
    rm -rf "$work"; trap - EXIT
    continue
  fi
  while IFS= read -r f; do
    rm -f "$work/root/$f"
    if [ -f "$work/root/DEBIAN/md5sums" ]; then
      grep -v "  ${f#./}\$" "$work/root/DEBIAN/md5sums" >"$work/md5sums" || true
      cat "$work/md5sums" >"$work/root/DEBIAN/md5sums"
    fi
  done <<<"$found"
  dpkg-deb --root-owner-group -Zgzip --build "$work/root" "$work/out.deb" >/dev/null
  mv "$work/out.deb" "$deb"
  echo "$deb: removed $(echo "$found" | tr '\n' ' ')"
  rm -rf "$work"; trap - EXIT
done
