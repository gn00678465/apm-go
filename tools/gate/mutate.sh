#!/bin/sh
# Manual mutation layer: apply each mutant from tools/gate/mutants.txt to an
# isolated copy of the working tree (tracked + untracked-but-not-ignored
# files), run the scope test packages, and require a failure. The product
# tree is never edited.
#
# Proof of execution per mutant: the edit must apply (gatetool replace exits
# 0 only when the anchor occurs exactly once), the suite must exit non-zero,
# AND its log must contain a test failure line -- an rc alone could be a
# build error unrelated to the mutant, so a compile failure is classified as
# "broken", not as a kill. Before any mutant, the unmutated copy must pass
# the same command (baseline in the copy), otherwise the round is void.
set -eu

ART=${GATE_ART:?GATE_ART (artifact dir) is required}
SPEC=${GATE_MUTANTS:-tools/gate/mutants.txt}
PKGS=${GATE_SCOPE_PKGS:?GATE_SCOPE_PKGS is required}
GATETOOL=${GATETOOL:?GATETOOL (built gatetool binary) is required}
COPY="$ART/mutation-copy"
LOGS="$ART/mutants"

rm -rf "$COPY" "$LOGS"
mkdir -p "$COPY" "$LOGS"
git ls-files -z --cached --others --exclude-standard | tar --null -T - -cf - | tar -xf - -C "$COPY"

test_cmd() {
  # -count=1 defeats the test cache so every mutant is really executed.
  (cd "$COPY" && go test -count=1 ${GATE_TEST_ARGS:-} $PKGS)
}

echo "baseline in isolated copy: $PKGS"
if ! test_cmd > "$LOGS/_baseline.log" 2>&1; then
  echo "FAIL: unmutated copy does not pass; mutation round is void" >&2
  tail -20 "$LOGS/_baseline.log" >&2
  exit 1
fi

total=0; killed=0; survived=0; broken=0
TAB=$(printf '	')
while IFS="$TAB" read -r name file old new; do
  case "$name" in ''|'#'*) continue ;; esac
  total=$((total + 1))
  if ! (cd "$COPY" && "$GATETOOL" replace -file "$file" -old "$old" -new "$new") > "$LOGS/$name.apply.log" 2>&1; then
    echo "FAIL: mutant $name did not apply (anchor not unique or missing)" >&2
    cat "$LOGS/$name.apply.log" >&2
    exit 2
  fi
  if test_cmd > "$LOGS/$name.log" 2>&1; then
    echo "SURVIVED  $name ($file)"
    survived=$((survived + 1))
  elif grep -qE '^(--- FAIL|FAIL|panic:)' "$LOGS/$name.log"; then
    reason=$(grep -m1 -E '^--- FAIL' "$LOGS/$name.log" || true)
    echo "killed    $name ($file) ${reason:-}"
    killed=$((killed + 1))
  else
    echo "BROKEN    $name ($file): suite failed without a test failure (build error?)"
    broken=$((broken + 1))
  fi
  # restore the pristine file from the product tree
  cp "$file" "$COPY/$file"
done < "$SPEC"

if [ "$total" -eq 0 ]; then
  echo "FAIL: no mutants in $SPEC (fail closed)" >&2; exit 2
fi
echo "mutation: $killed/$total killed, $survived survived, $broken broken"
if [ "$survived" -ne 0 ] || [ "$broken" -ne 0 ]; then
  exit 1
fi
