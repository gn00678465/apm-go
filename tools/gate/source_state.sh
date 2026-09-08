#!/bin/sh
# Source-state provenance for the gate. Prints one line
#   commit=<sha> tree=<tree-sha>
# and refuses (rc 1) to emit anything when the state is not reproducible:
# truncated history, staged/unstaged/deleted changes, or untracked files
# outside the excusal whitelist.
#
# Whitelist: GATE_UNTRACKED_OK is a space-separated list of path prefixes.
# A prefix is admitted only when `git ls-files -- <prefix>` is empty (it
# holds no tracked file at all, so it cannot be product code) AND it does not
# appear in the change set's diff. Both are checked here, per prefix, on
# every run -- the list is never trusted on its own.
set -eu

BASE=${GATE_BASE:-main}
OK=${GATE_UNTRACKED_OK:-.gate/}

if [ "$(git rev-parse --is-shallow-repository)" != "false" ]; then
  echo "FAIL: shallow repository; history is truncated" >&2; exit 1
fi
if [ -f "$(git rev-parse --git-dir)/info/grafts" ]; then
  echo "FAIL: grafted history" >&2; exit 1
fi
if ! git cat-file -e "$BASE^{commit}" 2>/dev/null; then
  echo "FAIL: base '$BASE' does not resolve to a local commit" >&2; exit 1
fi

for p in $OK; do
  if [ -n "$(git ls-files -- "$p")" ]; then
    echo "FAIL: whitelist prefix '$p' holds tracked files; not excusable" >&2; exit 1
  fi
  if git diff --name-only "$BASE...HEAD" | grep -q "^$p"; then
    echo "FAIL: whitelist prefix '$p' appears in the change set; not excusable" >&2; exit 1
  fi
done

status=$(git status --porcelain --untracked-files=all)
bad=0
if [ -n "$status" ]; then
  printf '%s\n' "$status" | while IFS= read -r line; do
    code=$(printf '%s' "$line" | cut -c1-2)
    path=$(printf '%s' "$line" | cut -c4-)
    case "$code" in
      '??')
        excused=0
        for p in $OK; do
          case "$path" in "$p"*) excused=1 ;; esac
        done
        if [ "$excused" -eq 0 ]; then
          echo "FAIL: untracked product path: $path" >&2; exit 3
        fi ;;
      *) echo "FAIL: tracked change present ($code): $path" >&2; exit 3 ;;
    esac
  done || bad=$?
fi
if [ "$bad" -ne 0 ]; then
  echo "FAIL: working tree is not the committed state" >&2; exit 1
fi

echo "commit=$(git rev-parse HEAD) tree=$(git rev-parse 'HEAD^{tree}')"
