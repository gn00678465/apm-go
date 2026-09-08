#!/bin/sh
# Verification-gate entry point: one command that runs every layer in order
# and fails on the first broken one. Layers that derive their number from
# suite behaviour (mutation, changed-line coverage) run after suite health.
# Coverage is last on purpose: it is the layer most likely to miss its 100%
# threshold, and every other layer's evidence is still wanted when it does.
#
#   tools/gate.sh [-base REF] [-scope NAME] [-selftest-only]
#
# Artifacts go under .gate/<scope>/ (gitignored). Tool versions are pinned in
# tools/gate/versions.env. The source state is checked before and after the
# run and the two must be identical.
set -eu
cd "$(dirname "$0")/.."
. tools/gate/lib.sh
. tools/gate/versions.env

BASE=main
SCOPE=$(git rev-parse --abbrev-ref HEAD | tr '/' '-')
SELFTEST_ONLY=0
while [ "$#" -gt 0 ]; do
  case "$1" in
    -base) BASE=$2; shift 2 ;;
    -scope) SCOPE=$2; shift 2 ;;
    -selftest-only) SELFTEST_ONLY=1; shift ;;
    *) echo "usage: tools/gate.sh [-base REF] [-scope NAME] [-selftest-only]" >&2; exit 2 ;;
  esac
done

export GATE_BASE="$BASE"
export GATE_ART=".gate/$SCOPE"
export GATE_UNTRACKED_OK=".gate/"
MODULE=$(go list -m)
EXE=$(go env GOEXE)
export GATETOOL="$PWD/.gate/$SCOPE/gatetool$EXE"
# The subject: the plugin / marketplace command surface and the packages it
# owns. Coverage and mutation target these; the suite layers run everything.
SCOPE_PATHS="cmd/apm-go/plugin.go cmd/apm-go/pluginwarn.go cmd/apm-go/pack.go cmd/apm-go/pack_json.go cmd/apm-go/init.go cmd/apm-go/marketplace.go cmd/apm-go/marketplace_authoring.go cmd/apm-go/marketplace_authoring_audit.go cmd/apm-go/marketplace_authoring_migrate.go cmd/apm-go/marketplace_package.go internal/marketplace internal/pack internal/pluginjson internal/rootfs"
export GATE_SCOPE_PKGS="./cmd/apm-go/... ./internal/marketplace/... ./internal/pack/... ./internal/pluginjson/... ./internal/rootfs/..."
COVERPKG=$(printf '%s' "$GATE_SCOPE_PKGS" | tr ' ' ',')

GATE_EXPECTED_LAYERS="selftest source-state-before build tests vet lint-format staticcheck suite-health property supply-chain real-execution mutation changed-units changed-line-coverage source-state-after"

# Fresh by mechanism: nothing from a previous run survives.
rm -rf "$GATE_ART"
mkdir -p "$GATE_ART"
# gatetool is built once: `go run` folds every non-zero exit into 1, which
# would hide the rc-2 "checker broke" code the self-test relies on.
go build -o "$GATETOOL" ./tools/gate/gatetool

# ---- negative controls for the harness itself -------------------------------
layer_selftest() {
  tmp="$GATE_ART/selftest"; mkdir -p "$tmp"
  # orchestration: a missing layer must not print green; unknown/duplicate = rc 2
  ( GATE_EXPECTED_LAYERS="a b"; GATE_COMPLETED_LAYERS=""; run_layer a true >/dev/null; ! finish_gate 2>/dev/null ) || { echo "selftest: missing layer went green"; return 1; }
  ( GATE_EXPECTED_LAYERS="a"; GATE_COMPLETED_LAYERS=""; run_layer zz true >/dev/null 2>&1; [ $? -eq 2 ] ) || { echo "selftest: unknown layer not rc 2"; return 1; }
  ( GATE_EXPECTED_LAYERS="a"; GATE_COMPLETED_LAYERS=""; run_layer a true >/dev/null; run_layer a true >/dev/null 2>&1; [ $? -eq 2 ] ) || { echo "selftest: duplicate layer not rc 2"; return 1; }
  ( GATE_EXPECTED_LAYERS="a"; GATE_COMPLETED_LAYERS=""; run_layer a sh -c 'exit 7' >/dev/null 2>&1; [ $? -eq 7 ] ) || { echo "selftest: failing command lost its rc"; return 1; }
  ( GATE_EXPECTED_LAYERS="a b"; GATE_COMPLETED_LAYERS=""; run_layer a true >/dev/null && run_layer b true >/dev/null && finish_gate >/dev/null ) || { echo "selftest: complete manifest did not reach green"; return 1; }
  # checker: forbidden present = 1, clean = 0, unreadable = 2, empty list = 2
  printf 'x AKIAABCDEFGHIJKLMNOP y\n' > "$tmp/bad.txt"; printf 'clean\n' > "$tmp/good.txt"
  ( must_not_match 'AKIA[0-9A-Z]{16}' "$tmp/bad.txt" >/dev/null; [ $? -eq 1 ] ) || { echo "selftest: forbidden pattern not caught"; return 1; }
  must_not_match 'AKIA[0-9A-Z]{16}' "$tmp/good.txt" >/dev/null || { echo "selftest: clean file failed"; return 1; }
  ( must_not_match 'x' "$tmp/does-not-exist" >/dev/null 2>&1; [ $? -eq 2 ] ) || { echo "selftest: unreadable path not rc 2"; return 1; }
  ( must_not_match 'x' >/dev/null; [ $? -eq 2 ] ) || { echo "selftest: empty path list not rc 2"; return 1; }
  # gatetool replace: zero or many anchors must be refused (rc 2)
  printf 'a\na\n' > "$tmp/two.txt"
  ( "$GATETOOL" replace -file "$tmp/two.txt" -old a -new b 2>/dev/null; [ $? -eq 2 ] ) || { echo "selftest: replace accepted a non-unique anchor"; return 1; }
  ( "$GATETOOL" replace -file "$tmp/two.txt" -old zzz -new b 2>/dev/null; [ $? -eq 2 ] ) || { echo "selftest: replace accepted a missing anchor"; return 1; }
  # gatetool coverage: an empty subject must be refused (rc 2)
  printf 'mode: set\n' > "$tmp/empty.out"
  ( "$GATETOOL" coverage -base "$BASE" -module "$MODULE" -profile "$tmp/empty.out" tools/gate/versions.env >/dev/null 2>&1; [ $? -eq 2 ] ) || { echo "selftest: coverage accepted an empty subject"; return 1; }
  # source-state: an untracked product file must be refused; a whitelisted
  # untracked path must still emit a state; a listed prefix that holds
  # tracked files must be refused even though it is listed.
  touch internal/.gate-selftest-untracked
  ( GATE_UNTRACKED_OK=".gate/" sh tools/gate/source_state.sh >/dev/null 2>&1; [ $? -ne 0 ] ) || { rm -f internal/.gate-selftest-untracked; echo "selftest: untracked product file not refused"; return 1; }
  rm -f internal/.gate-selftest-untracked
  ( GATE_UNTRACKED_OK=".gate/ internal/" sh tools/gate/source_state.sh >/dev/null 2>&1; [ $? -ne 0 ] ) || { echo "selftest: prefix with tracked files was admitted"; return 1; }
  GATE_UNTRACKED_OK=".gate/" sh tools/gate/source_state.sh >/dev/null || { echo "selftest: clean tree with only .gate/ untracked did not emit a state"; return 1; }
  echo "selftest: all negative controls behaved"
}

layer_source_state() {
  sh tools/gate/source_state.sh | tee "$GATE_ART/source-state-$1.txt"
  grep -q '^commit=' "$GATE_ART/source-state-$1.txt"
}

layer_build() { go build ./... && go build -o "$GATE_ART/apm-go$EXE" ./cmd/apm-go; }

layer_tests() {
  go test -count=1 ./... 2>&1 | tee "$GATE_ART/tests.log"
  if grep -qE '^(FAIL|--- FAIL|panic:)' "$GATE_ART/tests.log"; then
    echo "test failures: $(grep -c '^--- FAIL' "$GATE_ART/tests.log") (see tests.log)"; return 1
  fi
  echo "packages ok: $(grep -c '^ok' "$GATE_ART/tests.log")"
}

layer_vet() {
  go vet ./... 2>&1 | tee "$GATE_ART/vet.log"
  if grep -q . "$GATE_ART/vet.log"; then return 1; fi
  echo "vet: 0 findings"
}

layer_lint_format() {
  # Check the committed blobs, not the checkout: with core.autocrlf=true the
  # working tree is CRLF and gofmt -l would flag every file on Windows while
  # the same tree is clean on CI. source-state already guarantees the
  # checkout equals HEAD, so HEAD's bytes are the subject.
  : > "$GATE_ART/gofmt.txt"
  n=0
  for f in $(git ls-files -- '*.go'); do
    n=$((n + 1))
    if [ -n "$(git show "HEAD:$f" | gofmt -l 2>&1)" ]; then echo "$f" >> "$GATE_ART/gofmt.txt"; fi
  done
  [ "$n" -gt 0 ] || { echo "no .go files listed (fail closed)"; return 2; }
  if [ -s "$GATE_ART/gofmt.txt" ]; then echo "gofmt drift:"; cat "$GATE_ART/gofmt.txt"; return 1; fi
  echo "gofmt: 0 of $n committed .go files need formatting"
}

layer_staticcheck() {
  go run "honnef.co/go/tools/cmd/staticcheck@$STATICCHECK_VERSION" ./... 2>&1 | tee "$GATE_ART/staticcheck.log"
  if grep -q . "$GATE_ART/staticcheck.log"; then return 1; fi
  echo "staticcheck: 0 findings"
}

layer_suite_health() {
  # Randomized test order; the seed is printed per package so a failure can
  # be replayed with -shuffle=<seed>.
  # go test prints the shuffle seed only under -v, so the gate picks the
  # seed itself and records it; rerun with -shuffle=<seed> to replay.
  seed=$(date +%s)
  echo "shuffle seed: $seed"
  go test -count=1 -shuffle="$seed" ./... 2>&1 | tee "$GATE_ART/shuffle.log"
  if grep -qE '^(FAIL|--- FAIL|panic:)' "$GATE_ART/shuffle.log"; then return 1; fi
  echo "shuffled packages ok: $(grep -c '^ok' "$GATE_ART/shuffle.log") (seed $seed)"
}

layer_property() {
  go test -count=1 -run 'Property' -v ./internal/marketplace/... ./internal/rootfs/... 2>&1 | tee "$GATE_ART/property.log"
  if grep -qE '^(FAIL|--- FAIL|panic:)' "$GATE_ART/property.log"; then return 1; fi
  n=$(grep -cE '^--- PASS: Test\w*Property' "$GATE_ART/property.log" || true)
  [ "$n" -gt 0 ] || { echo "no property test ran (fail closed)"; return 2; }
  echo "properties passed: $n"
}

layer_supply_chain() { sh tools/gate/supplychain.sh; }

layer_real_execution() {
  GATE_BIN="$GATE_ART/apm-go$EXE" sh tools/gate/realexec.sh 2>&1 | tee "$GATE_ART/realexec.log"
  grep -q '^real-execution:' "$GATE_ART/realexec.log" || return 2
  if grep -q '^FAIL' "$GATE_ART/realexec.log"; then return 1; fi
}

layer_mutation() {
  sh tools/gate/mutate.sh 2>&1 | tee "$GATE_ART/mutation.log"
  grep -q '^mutation: ' "$GATE_ART/mutation.log" || return 2
  if grep -qE '^(SURVIVED|BROKEN|FAIL)' "$GATE_ART/mutation.log"; then return 1; fi
}

produce_profile() {
  if [ ! -s "$GATE_ART/cover.out" ]; then
    go test -count=1 -coverprofile="$GATE_ART/cover.out" -coverpkg="$COVERPKG" $GATE_SCOPE_PKGS > "$GATE_ART/cover.log" 2>&1 || { cat "$GATE_ART/cover.log"; return 1; }
  fi
}

layer_changed_units() {
  produce_profile
  "$GATETOOL" units -base "$BASE" -module "$MODULE" -profile "$GATE_ART/cover.out" $SCOPE_PATHS > "$GATE_ART/units.tsv"
  tail -1 "$GATE_ART/units.tsv"
}

layer_changed_line_coverage() {
  produce_profile
  "$GATETOOL" coverage -base "$BASE" -module "$MODULE" -profile "$GATE_ART/cover.out" $SCOPE_PATHS > "$GATE_ART/coverage.txt" || { cat "$GATE_ART/coverage.txt"; return 1; }
  cat "$GATE_ART/coverage.txt"
}

run_layer selftest layer_selftest
if [ "$SELFTEST_ONLY" -eq 1 ]; then exit 0; fi
run_layer source-state-before layer_source_state before
run_layer build layer_build
run_layer tests layer_tests
run_layer vet layer_vet
run_layer lint-format layer_lint_format
run_layer staticcheck layer_staticcheck
run_layer suite-health layer_suite_health
run_layer property layer_property
run_layer supply-chain layer_supply_chain
run_layer real-execution layer_real_execution
run_layer mutation layer_mutation
run_layer changed-units layer_changed_units
run_layer changed-line-coverage layer_changed_line_coverage
run_layer source-state-after layer_source_state after
if ! cmp -s "$GATE_ART/source-state-before.txt" "$GATE_ART/source-state-after.txt"; then
  echo "FAIL: source state changed during the run" >&2; exit 1
fi
finish_gate
