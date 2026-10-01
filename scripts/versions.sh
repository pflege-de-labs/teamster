#!/usr/bin/env bash
# Writes versions.json and the root redirect from the pages-vX.Y branches on origin.
set -euo pipefail

out=${1:?usage: versions.sh OUTPUT_DIR}
root=${DOCS_ROOT:-/teamster/}
mkdir -p "$out"

# A while-read loop rather than mapfile, which the macOS bash 3.2 lacks.
versions=()
while read -r v; do versions+=("$v"); done < <(
  git ls-remote --heads origin 'pages-v*' |
    sed -n 's#.*refs/heads/pages-\(v[0-9][0-9]*\.[0-9][0-9]*\)$#\1#p' |
    sort -rV
)
latest=${versions[0]:-dev}

{
  printf '[\n  {"version": "dev", "path": "dev/", "latest": %s}' "$([[ $latest == dev ]] && echo true || echo false)"
  for v in ${versions[@]+"${versions[@]}"}; do
    printf ',\n  {"version": "%s", "path": "%s/", "latest": %s}' "$v" "$v" "$([[ $v == "$latest" ]] && echo true || echo false)"
  done
  printf '\n]\n'
} >"$out/versions.json"

cat >"$out/index.html" <<HTML
<!doctype html>
<meta charset="utf-8">
<title>Teamster documentation</title>
<meta http-equiv="refresh" content="0; url=${root}${latest}/">
<link rel="canonical" href="${root}${latest}/">
<a href="${root}${latest}/">Teamster documentation</a>
HTML
