#!/usr/bin/env bash
# Fails when a content page exists in only one of English (name.md) and German (name.de.md).
set -euo pipefail

status=0
while IFS= read -r -d '' f; do
  case "$f" in
    *.de.md) other=${f%.de.md}.md ;;
    *) other=${f%.md}.de.md ;;
  esac
  if [[ ! -f $other ]]; then
    echo "missing translation: $other (for $f)"
    status=1
  fi
done < <(find content -name '*.md' -print0)
exit $status
