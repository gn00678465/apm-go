#!/bin/sh
# Supply-chain layer: known vulnerabilities, dependency-set delta, secrets in
# the diff, and the capability diff (which std/x packages the change started
# importing). Reads declarations only -- import lines and go.mod -- never
# function bodies.
set -eu
. tools/gate/lib.sh
. tools/gate/versions.env

BASE=${GATE_BASE:-main}
ART=${GATE_ART:?GATE_ART is required}

echo "-- govulncheck $GOVULNCHECK_VERSION"
go run "golang.org/x/vuln/cmd/govulncheck@$GOVULNCHECK_VERSION" ./... | tee "$ART/govulncheck.txt"

echo "-- dependency delta (go.mod, $BASE...HEAD)"
git diff "$BASE...HEAD" -- go.mod | grep -E '^[+-][^+-]' | tee "$ART/gomod-delta.txt" || true
newdeps=$(grep -E '^\+' "$ART/gomod-delta.txt" | grep -v '// indirect' | grep -vE '^\+(go |toolchain )' || true)
if [ -n "$newdeps" ]; then
  echo "new direct requirements (each must trace to intent):"
  printf '%s\n' "$newdeps"
fi

echo "-- secrets scan of added lines"
# tools/gate* is the verifier, not the subject: it carries the scan pattern
# itself and a deliberately bad fixture for the negative control.
# An empty diff (wrong base, or run on the base itself) is refused. A diff
# whose added lines are all in the excluded paths has nothing to scan and
# says so, so a verifier-only branch does not fail by construction.
if [ -z "$(git diff --name-only "$BASE...HEAD")" ]; then
  echo "FAIL: the diff $BASE...HEAD is empty (fail closed)"; exit 2
fi
git diff "$BASE...HEAD" -- . ':(exclude)*.golden.json' ':(exclude)tools/parity/cases/**' ':(exclude)tools/gate.sh' ':(exclude)tools/gate/**' | grep -E '^\+[^+]' > "$ART/added-lines.txt" || true
if [ -s "$ART/added-lines.txt" ]; then
  must_not_match 'AKIA[0-9A-Z]{16}|-----BEGIN (RSA|EC|OPENSSH|DSA) PRIVATE KEY-----|ghp_[A-Za-z0-9]{36}|github_pat_[A-Za-z0-9_]{22,}|xox[baprs]-[0-9A-Za-z-]{10,}|sk-[A-Za-z0-9]{32,}' "$ART/added-lines.txt"
else
  echo "secrets scan: no added line outside the excluded paths, nothing to scan"
fi

echo "-- capability diff (imports added on the new side, $BASE...HEAD)"
git diff "$BASE...HEAD" -- '*.go' ':(exclude)*_test.go' | grep -E '^\+\s+"[a-z0-9/._-]+"$' | sed -E 's/^\+\s+"([^"]+)"$/\1/' | sort -u > "$ART/imports-added.txt" || true
caps=$(grep -E '^(os/exec|net|net/http|syscall|unsafe|plugin|os/signal|crypto/tls|golang\.org/x/sys)' "$ART/imports-added.txt" || true)
if [ -n "$caps" ]; then
  echo "capability-bearing imports newly declared (review against intent):"
  printf '  %s\n' $caps
else
  echo "no new capability-bearing import (os/exec, net*, syscall, unsafe, plugin, os/signal, crypto/tls, x/sys)"
fi
