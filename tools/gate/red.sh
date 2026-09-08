#!/bin/sh
# RED reconstruction: replay every test file this change ADDED, one file at
# a time, against a worktree of the base ref, and record how each behaves
# there. A test that passes at base is either vacuous or covers behaviour
# that already existed; the mutation layer is what then proves it can fail.
#
#   tools/gate/red.sh [-base REF] [-out FILE] PATH...
#
# Outcomes per file: "failed (assertion)" when the package compiles and a
# test fails; "failed (collection)" when the file does not compile against
# the base (a weaker RED); "passed" when every test in it passes at base.
set -eu
cd "$(dirname "$0")/../.."
BASE=main
OUT=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -base) BASE=$2; shift 2 ;;
    -out) OUT=$2; shift 2 ;;
    *) break ;;
  esac
done
[ "$#" -gt 0 ] || { echo "usage: red.sh [-base REF] [-out FILE] PATH..." >&2; exit 2; }
WT=$(mktemp -d "${TMPDIR:-/tmp}/apm-red.XXXXXX")
trap 'git worktree remove --force "$WT" >/dev/null 2>&1 || true' EXIT
git worktree add --detach "$WT" "$BASE" >/dev/null 2>&1
files=$(git diff --name-status "$BASE...HEAD" -- "$@" | awk '$1=="A" && $2 ~ /_test\.go$/ {print $2}')
[ -n "$files" ] || { echo "FAIL: no added test file under the given paths (fail closed)" >&2; exit 2; }
report() { if [ -n "$OUT" ]; then printf '%s\n' "$1" >> "$OUT"; fi; printf '%s\n' "$1"; }
[ -z "$OUT" ] || : > "$OUT"
report "| Test file | Result at base | Note |"
report "|---|---|---|"
for f in $files; do
  dir=$(dirname "$f")
  if [ ! -d "$WT/$dir" ]; then
    report "| $f | failed (collection) | package $dir absent at base |"
    continue
  fi
  cp "$f" "$WT/$f"
  names=$(grep -oE '^func (Test[A-Za-z0-9_]+)' "$f" | sed 's/^func //' | paste -sd'|' -)
  if [ -z "$names" ]; then
    rm -f "$WT/$f"; report "| $f | n-a | no Test functions (helpers only) |"; continue
  fi
  log=$(cd "$WT" && go test -count=1 -run "^($names)\$" "./$dir" 2>&1 || true)
  rm -f "$WT/$f"
  if printf '%s' "$log" | grep -qE '^(# |.*\.go:[0-9]+:[0-9]+: )'; then
    reason=$(printf '%s' "$log" | grep -m1 -E '\.go:[0-9]+:[0-9]+: ' | sed 's/|/ /g')
    report "| $f | failed (collection) | $reason |"
  elif printf '%s' "$log" | grep -qE '^--- FAIL'; then
    n=$(printf '%s' "$log" | grep -cE '^--- FAIL')
    report "| $f | failed (assertion) | $n test(s) failed at base |"
  elif printf '%s' "$log" | grep -qE '^ok'; then
    report "| $f | passed | pre-existing behaviour; non-vacuity rests on the mutation layer |"
  else
    report "| $f | unknown | $(printf '%s' "$log" | head -1 | sed 's/|/ /g') |"
  fi
done
