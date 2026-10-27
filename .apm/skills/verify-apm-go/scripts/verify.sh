#!/bin/sh
# Harness for the verify-apm-go skill: build the binary, drive it in a
# throwaway sandbox, record evidence. Run from anywhere inside the repo.
#
#   verify.sh check [--offline]          run the repository checks; see SKILL.md "Check"
#   verify.sh build                      build .scratch/verify/bin/apm-go
#   verify.sh doctor                     is the build worth driving?
#   verify.sh new NAME                   create a run; prints its directory
#   verify.sh run RUN STEP [--ci] [--cd DIR] [--stdin FILE] -- ARGS...
#                                        run `apm-go ARGS` in RUN's sandbox
#   verify.sh tty RUN                    start a tmux shell in RUN's sandbox; prints the socket name
#   verify.sh keys RUN KEY...            send keys to that shell (tmux send-keys syntax)
#   verify.sh expect RUN PATTERN [SHOT]  wait up to 10s for PATTERN on screen; print it, save evidence/SHOT.screen
#   verify.sh clean RUN                  remove RUN's sandbox and tmux server, keep evidence
set -eu

ROOT=$(git -C "$(dirname "$0")" rev-parse --show-toplevel)
BASE="$ROOT/.scratch/verify"
BIN="$BASE/bin/apm-go"

die() { echo "verify: $*" >&2; exit 2; }

need_run() {
  [ -n "${1:-}" ] || die "missing RUN directory"
  case "$1" in "$BASE"/runs/*) ;; *) die "RUN must be a directory under $BASE/runs" ;; esac
  [ -d "$1/evidence" ] || die "not a run directory: $1"
}

# One tmux server per run, on its own socket, so clean kills only what this
# run started.
sock_of() { echo "apmgo-verify-$(basename "$1")"; }

# Sorted listing of the sandbox: "sha256  path" per file, "-> target  path"
# per symlink, "dir  path" per empty directory.
tree_of() {
  ( cd "$1/sandbox"
    find . -type f -exec sha256sum {} +
    find . -type l -exec sh -c 'printf "%s  %s\n" "-> $(readlink "$1")" "$1"' _ {} \;
    find . -type d -empty -exec printf 'dir  %s\n' {} \;
  ) | sort -k2
}

# Pinned so a rerun uses the same checker. Both run through
# `go run <module>@<version>`.
STATICCHECK_VERSION=2026.2.1
GOVULNCHECK_VERSION=v1.7.0

cmd=${1:-}; [ $# -gt 0 ] && shift
case "$cmd" in
check)
  offline=
  case "${1:-}" in "") ;; --offline) offline=1 ;; *) die "usage: verify.sh check [--offline]" ;; esac
  cd "$ROOT"
  # go.mod's `go` directive selects the toolchain only when the local Go is
  # older; a newer local Go runs as itself, and the pinned staticcheck cannot
  # read its export data. Every step runs on the directive's version, the
  # one CI installs (go-version-file: go.mod).
  GOTOOLCHAIN="go$(sed -n 's/^go //p' go.mod)"; export GOTOOLCHAIN
  # No recursive delete names a path built from a variable: each one enters
  # the parent directory and removes a literal name there, so an empty or
  # wrong variable cannot aim it at anything else.
  out="$BASE/check"; mkdir -p "$BASE"; (cd "$BASE" && rm -rf check); mkdir -p "$out"
  # The Go build work directory and every t.TempDir() go here, not to /tmp:
  # a small or quota-limited /tmp fails the suite with "disk quota exceeded"
  # at link time, which says nothing about the code. It is outside the
  # repository because tests that build a git repository in t.TempDir() fail
  # inside another work tree.
  tmpparent="${XDG_CACHE_HOME:-$HOME/.cache}/apm-go-verify"
  TMPDIR="$tmpparent/tmp"; export TMPDIR
  mkdir -p "$tmpparent"; (cd "$tmpparent" && rm -rf tmp); mkdir -p "$TMPDIR"
  fail=0
  # step NAME CMD...: every step runs, also after an earlier one failed, so
  # one run reports everything that is wrong. The log is the evidence.
  step() {
    name=$1; shift
    if "$@" > "$out/$name.log" 2>&1; then echo "ok    $name"
    else echo "FAIL  $name (rc=$?); log: $out/$name.log"; tail -15 "$out/$name.log" | sed 's/^/      /'; fail=1; fi
  }
  # A checker that prints findings and still exits 0 must not pass.
  silent() { "$@" > "$out/findings.tmp" 2>&1; rc=$?; cat "$out/findings.tmp"; [ "$rc" -eq 0 ] && [ ! -s "$out/findings.tmp" ]; }
  fmt() { files=$(git ls-files '*.go'); [ -n "$files" ] || { echo "no .go file listed"; return 2; }; silent gofmt -l $files; }
  lint() {
    # The list is passed explicitly: staticcheck's own discovery of
    # staticcheck.conf depends on where the tree is checked out.
    checks=$(sed -n 's/^checks *= *\[\(.*\)\].*/\1/p' staticcheck.conf | tr -d ' "')
    [ -n "$checks" ] || { echo "staticcheck.conf has no checks list"; return 2; }
    silent go run "honnef.co/go/tools/cmd/staticcheck@$STATICCHECK_VERSION" -checks "$checks" ./...
  }
  realexec() {
    mkdir -p "$out/realexec"
    go build -o "$out/realexec/apm-go" ./cmd/apm-go || return 1
    REALEXEC_ART="$out/realexec" REALEXEC_BIN="$out/realexec/apm-go" sh tools/realexec.sh > "$out/realexec.out" 2>&1; rc=$?
    cat "$out/realexec.out"
    [ "$rc" -eq 0 ] && grep -q '^real-execution:' "$out/realexec.out"
  }
  seed=$(date +%s)
  echo "toolchain $GOTOOLCHAIN, shuffle seed $seed, logs in $out"
  step build go build ./...
  step test go test -count=1 -shuffle="$seed" ./...
  step vet silent go vet ./...
  step gofmt fmt
  step staticcheck lint
  step realexec realexec
  # govulncheck reports and never fails the run: a finding in the Go standard
  # library appears when its database is updated, with no change in this
  # repository, and the fix is a new Go version in go.mod.
  if [ -n "$offline" ]; then echo "skip  govulncheck (--offline: it downloads the vulnerability database)"
  elif go run "golang.org/x/vuln/cmd/govulncheck@$GOVULNCHECK_VERSION" ./... > "$out/govulncheck.log" 2>&1; then echo "ok    govulncheck"
  else echo "note  govulncheck: $(grep -c '^Vulnerability #' "$out/govulncheck.log") finding(s) that the code calls, or the scan did not run; log: $out/govulncheck.log"; fi
  rm -f "$out/findings.tmp"; (cd "$tmpparent" && rm -rf tmp) 2>/dev/null || true
  [ "$fail" -eq 0 ] && echo "check: all steps passed" || { echo "check: at least one step failed"; exit 1; }
  ;;

build)
  mkdir -p "$BASE/bin"
  ( cd "$ROOT" && go build -o "$BIN" ./cmd/apm-go )
  git -C "$ROOT" rev-parse HEAD > "$BASE/bin/built-from"
  echo "built $BIN"
  ;;

doctor)
  fail=0
  ok()  { echo "ok    $*"; }
  bad() { echo "FAIL  $*"; fail=1; }
  command -v git >/dev/null && ok "git: $(git --version)" || bad "git not on PATH (install, marketplace and doctor need it)"
  if [ ! -x "$BIN" ]; then
    bad "no binary; run: verify.sh build"
  else
    ok "binary exists: $BIN"
    v=$("$BIN" --version 2>&1) || true
    [ "$v" = "apm-go version dev" ] && ok "version: $v" || bad "unexpected --version output: $v"
    stale=$(cd "$ROOT" && find cmd internal go.mod go.sum -newer "$BIN" \( -name '*.go' -o -name 'go.mod' -o -name 'go.sum' \) -not -name '*_test.go' | head -3 | tr '\n' ' ')
    [ -z "$stale" ] && ok "binary is newer than every non-test source file" || bad "source changed after the build ($stale); run: verify.sh build"
    head=$(git -C "$ROOT" rev-parse HEAD)
    [ "$(cat "$BASE/bin/built-from" 2>/dev/null)" = "$head" ] && ok "built from HEAD $head" || bad "built from another commit; run: verify.sh build"
  fi
  if [ -d "$BASE/runs" ]; then
    left=$(find "$BASE/runs" -mindepth 2 -maxdepth 2 -name sandbox | wc -l)
    [ "$left" -eq 0 ] && ok "no sandbox left from an earlier run" || echo "note  $left sandbox(es) left under $BASE/runs; clean the ones you started"
  fi
  [ "$fail" -eq 0 ] || exit 1
  ;;

new)
  [ -n "${1:-}" ] || die "usage: verify.sh new NAME"
  run="$BASE/runs/$(date +%Y%m%d-%H%M%S)-$1"
  mkdir -p "$run/sandbox/home" "$run/sandbox/work" "$run/evidence"
  echo "$run"
  ;;

run)
  run=${1:-}; need_run "$run"; step=${2:-}; [ -n "$step" ] || die "missing STEP name"; shift 2
  [ -d "$run/sandbox" ] || die "sandbox already cleaned: $run"
  ci=; sub=; stdin=/dev/null
  while [ $# -gt 0 ]; do
    case "$1" in
      --ci) ci=1; shift ;;
      --cd) sub=$2; shift 2 ;;
      --stdin) stdin=$2; shift 2 ;;
      --) shift; break ;;
      *) die "unknown option before --: $1" ;;
    esac
  done
  [ -x "$BIN" ] || die "no binary; run: verify.sh build"
  ev="$run/evidence/$step"
  [ ! -e "$ev.rc" ] || die "step '$step' already recorded in this run; pick another name"
  wd="$run/sandbox/work${sub:+/$sub}"
  [ -d "$wd" ] || die "no such directory in the sandbox: work/$sub"
  { printf 'cwd: work%s\nci: %s\nargv: apm-go' "${sub:+/$sub}" "${ci:-0}"; for a in "$@"; do printf ' %s' "$a"; done; printf '\n'; } > "$ev.cmd"
  # Nothing is inherited but PATH: no token, proxy or CI variable of the
  # invoking shell reaches the binary, and HOME / APM_CONFIG_DIR point into
  # the sandbox so the user's ~/.apm is never read or written.
  set +e
  ( cd "$wd" && exec env -i PATH="$PATH" HOME="$run/sandbox/home" APM_CONFIG_DIR="$run/sandbox/home/.apm" \
      GIT_CEILING_DIRECTORIES="$run/sandbox" NO_COLOR=1 TERM=dumb ${ci:+CI=1} "$BIN" "$@" ) < "$stdin" > "$ev.out" 2> "$ev.err"
  rc=$?
  set -e
  echo "$rc" > "$ev.rc"
  tree_of "$run" > "$ev.tree"
  echo "rc=$rc  evidence: $ev.{cmd,out,err,rc,tree}"
  echo "--- stdout"; cat "$ev.out"
  if [ -s "$ev.err" ]; then echo "--- stderr"; cat "$ev.err"; fi
  ;;

tty)
  run=${1:-}; need_run "$run"
  [ -d "$run/sandbox" ] || die "sandbox already cleaned: $run"
  command -v tmux >/dev/null || die "tmux not on PATH; the interactive path cannot be driven"
  sock=$(sock_of "$run")
  # tmux itself needs a real TERM; the shell inside gets the sandbox env.
  tmux -L "$sock" -f /dev/null new-session -d -s v -x 100 -y 40 -c "$run/sandbox/work" \
    "env -i PATH='$PATH' HOME='$run/sandbox/home' APM_CONFIG_DIR='$run/sandbox/home/.apm' GIT_CEILING_DIRECTORIES='$run/sandbox' TERM=tmux-256color APMGO='$BIN' PS1='verify\$ ' sh"
  echo "$sock"
  ;;

keys)
  run=${1:-}; need_run "$run"; shift
  tmux -L "$(sock_of "$run")" send-keys -t v "$@"
  ;;

expect)
  run=${1:-}; need_run "$run"; pat=${2:-}; [ -n "$pat" ] || die "usage: verify.sh expect RUN PATTERN [SHOT]"
  shot=${3:-}
  i=0
  until tmux -L "$(sock_of "$run")" capture-pane -p -t v | grep -q -- "$pat"; do
    i=$((i + 1))
    if [ "$i" -gt 50 ]; then
      echo "verify: screen never showed '$pat' in 10s; current screen:" >&2
      tmux -L "$(sock_of "$run")" capture-pane -p -t v >&2
      exit 1
    fi
    sleep 0.2
  done
  if [ -n "$shot" ]; then
    tmux -L "$(sock_of "$run")" capture-pane -p -t v > "$run/evidence/$shot.screen"
    echo "screen saved: $run/evidence/$shot.screen"
  fi
  tmux -L "$(sock_of "$run")" capture-pane -p -t v | sed -e :a -e '/^\n*$/{$d;N;ba' -e '}'
  ;;

clean)
  run=${1:-}; need_run "$run"
  tmux -L "$(sock_of "$run")" kill-server 2>/dev/null || true
  # A killed server leaves its socket file behind.
  rm -f "${TMUX_TMPDIR:-/tmp}/tmux-$(id -u)/$(sock_of "$run")"
  (cd "$run" && rm -rf sandbox)
  n=$(find "$run/evidence" -type f | wc -l)
  echo "cleaned sandbox; $n evidence file(s) kept in $run/evidence"
  ;;

*)
  sed -n '2,14p' "$0" | sed 's/^# \{0,1\}//'
  [ -z "$cmd" ] || exit 2
  ;;
esac
