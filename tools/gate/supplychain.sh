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
git diff "$BASE...HEAD" -- . ':(exclude)*.golden.json' ':(exclude)tools/parity/cases/**' | grep -E '^\+[^+]' > "$ART/added-lines.txt" || true
require_file "$ART/added-lines.txt"
must_not_match 'AKIA[0-9A-Z]{16}|-----BEGIN (RSA|EC|OPENSSH|DSA) PRIVATE KEY-----|ghp_[A-Za-z0-9]{36}|github_pat_[A-Za-z0-9_]{22,}|xox[baprs]-[0-9A-Za-z-]{10,}|sk-[A-Za-z0-9]{32,}' "$ART/added-lines.txt"

echo "-- capability diff (imports added on the new side, $BASE...HEAD)"
git diff "$BASE...HEAD" -- '*.go' ':(exclude)*_test.go' | grep -E '^\+\s+"[a-z0-9/._-]+"$' | sed -E 's/^\+\s+"([^"]+)"$/\1/' | sort -u > "$ART/imports-added.txt" || true
caps=$(grep -E '^(os/exec|net|net/http|syscall|unsafe|plugin|os/signal|crypto/tls|golang\.org/x/sys)' "$ART/imports-added.txt" || true)
if [ -n "$caps" ]; then
  echo "capability-bearing imports newly declared (review against intent):"
  printf '  %s\n' $caps
else
  echo "no new capability-bearing import (os/exec, net*, syscall, unsafe, plugin, os/signal, crypto/tls, x/sys)"
fi
