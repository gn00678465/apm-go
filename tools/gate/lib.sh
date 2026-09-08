#!/bin/sh
# Shared fail-closed helpers for tools/gate.sh and its layer scripts.
# Sourced, never executed. Every helper returns 2 when the check itself is
# broken (unknown layer, empty work list, unreadable input) so a broken
# checker can never share an exit code with a passing one.

GATE_COMPLETED_LAYERS=""

run_layer() {
  if [ "$#" -lt 2 ]; then
    echo "FAIL: run_layer requires a layer name and command" >&2
    return 2
  fi
  layer=$1
  shift
  case " $GATE_EXPECTED_LAYERS " in
    *" $layer "*) ;;
    *) echo "FAIL: unknown layer '$layer'" >&2; return 2 ;;
  esac
  case " $GATE_COMPLETED_LAYERS " in
    *" $layer "*) echo "FAIL: duplicate layer '$layer'" >&2; return 2 ;;
  esac
  printf '\n=== %s ===\n' "$layer"
  if "$@"; then
    GATE_COMPLETED_LAYERS="$GATE_COMPLETED_LAYERS $layer"
    return 0
  else
    rc=$?
    printf "FAIL: layer '%s' failed (rc=%s)\n" "$layer" "$rc" >&2
    return "$rc"
  fi
}

finish_gate() {
  missing=0
  for layer in $GATE_EXPECTED_LAYERS; do
    case " $GATE_COMPLETED_LAYERS " in
      *" $layer "*) ;;
      *) echo "FAIL: missing layer '$layer'" >&2; missing=1 ;;
    esac
  done
  if [ "$missing" -ne 0 ]; then
    return 1
  fi
  echo "=== gate: all layers green ==="
}

# must_not_match PATTERN PATH...: grep rc 1 (no match) is the only pass.
# rc 0 = forbidden pattern present (return 1); rc >= 2 = grep itself broke
# (return 2); an empty path list is refused with 2 because a scan that
# inspected nothing must not look like a scan that found nothing.
must_not_match() {
  pattern=$1; shift
  if [ "$#" -eq 0 ]; then
    echo "FAIL: no paths given to scan (fail closed): $pattern"; return 2
  fi
  if grep -rniE "$pattern" "$@"; then
    echo "FAIL: forbidden pattern present: $pattern"; return 1
  elif [ $? -ne 1 ]; then
    echo "FAIL: scan itself broke (fail closed): $pattern"; return 2
  fi
}

# require_file PATH: a layer that reads a report must refuse a missing one.
require_file() {
  if [ ! -s "$1" ]; then
    echo "FAIL: expected non-empty file missing: $1" >&2; return 2
  fi
}
