#!/bin/sh
# Real-execution layer: run the built binary end to end on realistic inputs
# in a throwaway sandbox (own HOME / APM_CONFIG_DIR), then an adversarial
# pass with hostile inputs. Every step asserts an exit code and an
# observable effect (a file that must exist, or a path that must NOT exist
# outside the sandbox). Expected values were observed on the Oracle-parity
# surface, not derived from the implementation.
set -eu

ART=${GATE_ART:?GATE_ART is required}
# Absolute: the steps below cd into the sandbox and still address $SB.
case "$ART" in /*|?:*) ;; *) ART="$PWD/$ART" ;; esac
BIN=${GATE_BIN:?GATE_BIN (built apm-go binary) is required}
SB="$ART/sandbox"
rm -rf "$SB"
mkdir -p "$SB/home" "$SB/work"
case "$BIN" in /*|?:*) ;; *) BIN="$PWD/$BIN" ;; esac

export HOME="$SB/home" APM_CONFIG_DIR="$SB/home/.apm" NO_COLOR=1 CI=1
if command -v cygpath >/dev/null 2>&1; then
  export USERPROFILE="$(cygpath -w "$SB/home")"
fi

steps=0; failures=0
step() { # step NAME EXPECTED_RC CMD...
  name=$1; want=$2; shift 2
  steps=$((steps + 1))
  set +e
  "$@" > "$SB/$name.out" 2>&1
  rc=$?
  set -e
  if [ "$rc" -ne "$want" ]; then
    echo "FAIL  $name: rc=$rc want=$want"; sed 's/^/      /' "$SB/$name.out" | head -8
    failures=$((failures + 1))
  else
    echo "ok    $name (rc=$rc)"
  fi
}
must_exist() { steps=$((steps + 1)); if [ -e "$1" ]; then echo "ok    exists $1"; else echo "FAIL  missing $1"; failures=$((failures + 1)); fi; }
must_not_exist() { steps=$((steps + 1)); if [ ! -e "$1" ]; then echo "ok    absent $1"; else echo "FAIL  ESCAPED: $1 exists"; failures=$((failures + 1)); fi; }
must_grep() { steps=$((steps + 1)); if grep -q -- "$2" "$SB/$1.out"; then echo "ok    $1 says '$2'"; else echo "FAIL  $1 lacks '$2'"; failures=$((failures + 1)); fi; }

cd "$SB/work"

# --- happy path: plugin init -> pack -> pack --archive ------------------------
step plugin-init 0 "$BIN" plugin init demo --yes --target claude
must_exist demo/apm.yml
must_exist demo/plugin.json
cd demo
step pack 0 "$BIN" pack
must_exist build/demo-0.1.0/plugin.json
step pack-archive-zip 0 "$BIN" pack --archive -o dist
must_exist dist/demo-0.1.0.zip
step pack-archive-tgz 0 "$BIN" pack --archive --archive-format tar.gz -o dist2
must_exist dist2/demo-0.1.0.tar.gz
step pack-dry-run 0 "$BIN" pack --dry-run -o dry
must_not_exist dry
cd ..

# --- happy path: marketplace authoring -> registry -----------------------------
mkdir mk && cd mk
step mk-init 0 "$BIN" marketplace init --name mk --owner me
must_exist apm.yml
step mk-pkg-remove-example 0 "$BIN" marketplace package remove example-package --yes
mkdir -p packages && cp -r ../demo packages/demo && rm -rf packages/demo/build packages/demo/dist packages/demo/dist2
step mk-pkg-add 0 "$BIN" marketplace package add ./packages/demo --name demo --category tools --no-verify
must_grep mk-pkg-add "Added package 'demo'"
step mk-pkg-set 0 "$BIN" marketplace package set demo --tags a,b
step mk-pack 0 "$BIN" pack
must_exist .claude-plugin/marketplace.json
step mk-check-clean 0 "$BIN" pack --check-clean --dry-run
cd ..
step mk-add 0 "$BIN" marketplace add ./mk --name mk
must_exist "$HOME/.apm/marketplaces.json"
step mk-list 0 "$BIN" marketplace list
must_grep mk-list "mk"
step mk-browse 0 "$BIN" marketplace browse mk
must_grep mk-browse "demo"
step mk-validate 0 "$BIN" marketplace validate mk
must_grep mk-validate "0 errors"
step mk-audit 0 "$BIN" marketplace audit mk --strict
must_grep mk-audit "1 clean"
step mk-update 0 "$BIN" marketplace update mk
step mk-remove 0 "$BIN" marketplace remove mk --yes
step mk-list-empty 0 "$BIN" marketplace list
must_grep mk-list-empty "No marketplaces registered"

# --- adversarial: hostile inputs must be refused and must not escape ----------
mkdir adv && cd adv
step adv-add-traversal 1 "$BIN" marketplace add me/.. --name bad
must_grep adv-add-traversal "traversal"
step adv-add-double-encoded 1 "$BIN" marketplace add 'me/%252e%252e' --name bad
must_grep adv-add-double-encoded "traversal"
step adv-add-control-char 1 "$BIN" marketplace add "$(printf 'a\tb/c')" --name bad
must_grep adv-add-control-char "control characters"
step adv-add-missing-local 1 "$BIN" marketplace add ../../nowhere --name bad
must_not_exist "$SB/nowhere"
step adv-plugin-init-escape 1 "$BIN" plugin init ../escape --yes --target claude
must_not_exist "$SB/work/escape"
must_not_exist "$SB/escape"
step adv-plugin-init-format-noarg 2 "$BIN" plugin init z --format
must_not_exist z
step adv-plugin-init-format-conflict 2 "$BIN" plugin init z --format claude --claude-plugin
must_not_exist z
cd ../demo
step adv-pack-output-escape 1 "$BIN" pack -o ../../outside
must_grep adv-pack-output-escape "escapes the project root"
must_not_exist "$SB/outside"
step adv-pack-archive-escape 1 "$BIN" pack --archive -o ../../outside2
must_not_exist "$SB/outside2"
step adv-pack-format-unknown 2 "$BIN" pack --format nope
step adv-pack-format-conflict 2 "$BIN" pack --format agent-plugin --claude-plugin
cd ../mk
cp apm.yml "$SB/mk-apm.yml.before"
step adv-pkg-add-traversal 2 "$BIN" marketplace package add ../demo --name t --no-verify
must_grep adv-pkg-add-traversal '".." path segment'
step adv-pkg-tagpattern-double 2 "$BIN" marketplace package add ./packages/demo --name d2 --tag-pattern '{version}{version}' --no-verify
must_grep adv-pkg-tagpattern-double "exactly one {version}"
step adv-pkg-tagpattern-unsupported 2 "$BIN" marketplace package add ./packages/demo --name d3 --tag-pattern '{oops}v{version}' --no-verify
must_grep adv-pkg-tagpattern-unsupported "unsupported placeholder"
steps=$((steps + 1)); if cmp -s apm.yml "$SB/mk-apm.yml.before"; then echo "ok    rejected packages left apm.yml byte-identical"; else echo "FAIL  a rejected package add changed apm.yml"; failures=$((failures + 1)); fi

echo "real-execution: $((steps - failures))/$steps checks passed"
[ "$failures" -eq 0 ]
