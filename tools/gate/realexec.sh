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
# step_streams: like step, but captures stdout and stderr into SEPARATE
# files ($SB/$name.out / .err) instead of step's merged 2>&1 -- the only way
# an empty-stderr assertion (ticket 34's verification strength, .scratch/
# parity-runner/issues/34-oracle-less-command-output-contract.md) can be made
# at all.
step_streams() { # step_streams NAME EXPECTED_RC CMD...
  name=$1; want=$2; shift 2
  steps=$((steps + 1))
  set +e
  "$@" > "$SB/$name.out" 2> "$SB/$name.err"
  rc=$?
  set -e
  if [ "$rc" -ne "$want" ]; then
    echo "FAIL  $name: rc=$rc want=$want"; sed 's/^/      /' "$SB/$name.out" "$SB/$name.err" | head -8
    failures=$((failures + 1))
  else
    echo "ok    $name (rc=$rc)"
  fi
}
# must_match_file: byte-for-byte comparison of a captured stream against a
# recorded expectation. Ticket 34 rejects a substring/must_grep containment
# check for the commands this file pins in place of a tools/parity corpus
# case -- the whole stream must match, not a piece of it.
must_match_file() { # must_match_file LABEL ACTUAL_FILE EXPECTED_FILE
  steps=$((steps + 1))
  if cmp -s "$2" "$3"; then
    echo "ok    $1 matches recorded transcript"
  else
    echo "FAIL  $1 differs from recorded transcript"; diff -u "$3" "$2" | head -20
    failures=$((failures + 1))
  fi
}

cd "$SB/work"

# --- happy path: plugin init -> pack -> pack --archive ------------------------
step plugin-init 0 "$BIN" plugin init demo --yes --target claude
must_exist demo/apm.yml
must_exist demo/plugin.json

# plugin validate: two happy-path scaffolds, zero findings (SC-001), proven
# read-only. This command has no Oracle counterpart, so its output contract
# is fixed HERE rather than by a tools/parity corpus case (ticket 34,
# .scratch/parity-runner/issues/34-oracle-less-command-output-contract.md),
# at the corpus's own strength: complete stdout, empty stderr, exact exit
# code, byte-identical tree before/after. The manifest argument is given as
# an ABSOLUTE path so displayManifestPath's relativization actually succeeds
# (relativizing an absolute manifest path against the absolute cwd works); a
# relative argument (e.g. "demo") hits filepath.Rel's absolute/relative
# operand mismatch and always falls back to the environment-specific
# absolute path instead -- a real gap against the contract's stated rule
# that the fallback is only for cross-drive impossibility, found while
# writing this step and reported rather than fixed (not in this WP's owned
# files).
cp -r demo "$SB/demo.before"
cat > "$SB/plugin-validate-claude.want.out" <<'EOF'
 > Validating plugin 'demo/plugin.json'...

 i Validation Results:
 + Structure: passed
 + Name: passed
 + Fields: passed
 + Paths: passed
 + Unrecognized: passed

 i Summary: 5 passed, 0 warnings, 0 errors
EOF
: > "$SB/plugin-validate-claude.want.err"
step_streams plugin-validate-claude 0 "$BIN" plugin validate "$SB/work/demo"
must_match_file plugin-validate-claude-stdout "$SB/plugin-validate-claude.out" "$SB/plugin-validate-claude.want.out"
must_match_file plugin-validate-claude-stderr "$SB/plugin-validate-claude.err" "$SB/plugin-validate-claude.want.err"
steps=$((steps + 1))
if diff -rq demo "$SB/demo.before" >/dev/null 2>&1; then echo "ok    plugin-validate-claude left demo/ byte-identical"; else echo "FAIL  plugin-validate-claude modified demo/"; failures=$((failures + 1)); fi

step plugin-init-agent 0 "$BIN" plugin init demo-agent --yes --target claude --format agent-plugin
must_exist demo-agent/plugin.json
cp -r demo-agent "$SB/demo-agent.before"
cat > "$SB/plugin-validate-agent.want.out" <<'EOF'
 > Validating plugin 'demo-agent/plugin.json'...

 i Validation Results:
 + Structure: passed
 + Name: passed
 + Fields: passed
 + Paths: passed
 + Unrecognized: passed

 i Summary: 5 passed, 0 warnings, 0 errors
EOF
: > "$SB/plugin-validate-agent.want.err"
step_streams plugin-validate-agent 0 "$BIN" plugin validate "$SB/work/demo-agent"
must_match_file plugin-validate-agent-stdout "$SB/plugin-validate-agent.out" "$SB/plugin-validate-agent.want.out"
must_match_file plugin-validate-agent-stderr "$SB/plugin-validate-agent.err" "$SB/plugin-validate-agent.want.err"
steps=$((steps + 1))
if diff -rq demo-agent "$SB/demo-agent.before" >/dev/null 2>&1; then echo "ok    plugin-validate-agent left demo-agent/ byte-identical"; else echo "FAIL  plugin-validate-agent modified demo-agent/"; failures=$((failures + 1)); fi

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

# plugin validate: the four sharpest failure modes (spec.md US1/US2,
# contracts/cli-plugin-validate.md), each proven read-only at ticket 34's
# strength. Absolute arguments again sidestep displayManifestPath's
# relative-argument fallback (see the happy-path comment above) so the
# recorded transcript stays a literal, portable string; "empty" stays
# relative on purpose -- the missing-manifest message never relativizes,
# it prints the directory argument exactly as given.
mkdir x y z empty
printf '{' > x/plugin.json
# "./../outside/x.md" -- '..' after a leading './' -- so the check actually
# reaches "must not contain '..'"; a bare "../outside/x.md" (no leading
# './') fails the earlier "must start with './'" check instead, which is
# what the WP04 task prompt's own illustrative fixture would have hit.
printf '{"name":"x","commands":"./../outside/x.md"}' > y/plugin.json
printf '{"name":"x","descripton":"d"}' > z/plugin.json

cp -r x "$SB/x.before"
cat > "$SB/plugin-validate-invalid-json.want.out" <<'EOF'
 > Validating plugin 'x/plugin.json'...

 i Validation Results:
 x Structure: invalid JSON: unexpected end of JSON input

 i Summary: 0 passed, 0 warnings, 1 errors
EOF
: > "$SB/plugin-validate-invalid-json.want.err"
step_streams plugin-validate-invalid-json 1 "$BIN" plugin validate "$SB/work/x"
must_match_file plugin-validate-invalid-json-stdout "$SB/plugin-validate-invalid-json.out" "$SB/plugin-validate-invalid-json.want.out"
must_match_file plugin-validate-invalid-json-stderr "$SB/plugin-validate-invalid-json.err" "$SB/plugin-validate-invalid-json.want.err"
steps=$((steps + 1))
if diff -rq x "$SB/x.before" >/dev/null 2>&1; then echo "ok    plugin-validate-invalid-json left x/ byte-identical"; else echo "FAIL  plugin-validate-invalid-json modified x/"; failures=$((failures + 1)); fi

cp -r y "$SB/y.before"
cat > "$SB/plugin-validate-traversal.want.out" <<'EOF'
 > Validating plugin 'y/plugin.json'...

 i Validation Results:
 + Structure: passed
 + Name: passed
 + Fields: passed
 x Paths: 'commands' must not contain '..'
 + Unrecognized: passed

 i Summary: 4 passed, 0 warnings, 1 errors
EOF
: > "$SB/plugin-validate-traversal.want.err"
step_streams plugin-validate-traversal 1 "$BIN" plugin validate "$SB/work/y"
must_match_file plugin-validate-traversal-stdout "$SB/plugin-validate-traversal.out" "$SB/plugin-validate-traversal.want.out"
must_match_file plugin-validate-traversal-stderr "$SB/plugin-validate-traversal.err" "$SB/plugin-validate-traversal.want.err"
steps=$((steps + 1))
if diff -rq y "$SB/y.before" >/dev/null 2>&1; then echo "ok    plugin-validate-traversal left y/ byte-identical"; else echo "FAIL  plugin-validate-traversal modified y/"; failures=$((failures + 1)); fi

cp -r z "$SB/z.before"
cat > "$SB/plugin-validate-strict-typo.want.out" <<'EOF'
 > Validating plugin 'z/plugin.json'...

 i Validation Results:
 + Structure: passed
 + Name: passed
 + Fields: passed
 + Paths: passed
 ! Unrecognized: unrecognized field 'descripton' (did you mean 'description'?)

 i Summary: 4 passed, 1 warnings, 0 errors
EOF
: > "$SB/plugin-validate-strict-typo.want.err"
step_streams plugin-validate-strict-typo 1 "$BIN" plugin validate "$SB/work/z" --strict
must_match_file plugin-validate-strict-typo-stdout "$SB/plugin-validate-strict-typo.out" "$SB/plugin-validate-strict-typo.want.out"
must_match_file plugin-validate-strict-typo-stderr "$SB/plugin-validate-strict-typo.err" "$SB/plugin-validate-strict-typo.want.err"
steps=$((steps + 1))
if diff -rq z "$SB/z.before" >/dev/null 2>&1; then echo "ok    plugin-validate-strict-typo left z/ byte-identical"; else echo "FAIL  plugin-validate-strict-typo modified z/"; failures=$((failures + 1)); fi

cp -r empty "$SB/empty.before"
cat > "$SB/plugin-validate-missing.want.out" <<'EOF'
 x no plugin.json found in empty (looked in plugin.json, .github/plugin/plugin.json, .claude-plugin/plugin.json, .cursor-plugin/plugin.json)
EOF
: > "$SB/plugin-validate-missing.want.err"
step_streams plugin-validate-missing 1 "$BIN" plugin validate empty
must_match_file plugin-validate-missing-stdout "$SB/plugin-validate-missing.out" "$SB/plugin-validate-missing.want.out"
must_match_file plugin-validate-missing-stderr "$SB/plugin-validate-missing.err" "$SB/plugin-validate-missing.want.err"
steps=$((steps + 1))
if diff -rq empty "$SB/empty.before" >/dev/null 2>&1; then echo "ok    plugin-validate-missing left empty/ byte-identical"; else echo "FAIL  plugin-validate-missing modified empty/"; failures=$((failures + 1)); fi

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
