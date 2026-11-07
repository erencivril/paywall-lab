#!/usr/bin/env bash
# Mutation check for the entitlement state machine.
#
# Applies each patch in mutation/ to internal/entitlement/entitlement.go, runs
# the unit tests and expects them to fail. A mutant that still passes the tests
# (SURVIVED), or a patch that no longer applies (code moved), fails the script.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

TARGET="internal/entitlement/entitlement.go"
PKG="./internal/entitlement/"
MUTANT_DIR="${MUTATION_DIR:-mutation}"

restore() { git checkout --quiet -- "$TARGET"; }
trap restore EXIT

if ! git diff --quiet -- "$TARGET"; then
  echo "error: $TARGET has uncommitted changes; refusing to run" >&2
  exit 2
fi

shopt -s nullglob
patches=("$MUTANT_DIR"/*.patch)
if [ ${#patches[@]} -eq 0 ]; then
  echo "error: no mutants found in $MUTANT_DIR/" >&2
  exit 2
fi

# Sanity check: the tests must pass on the unmodified code, otherwise every
# mutant would look "killed" for the wrong reason.
if ! go test -count=1 "$PKG" >/dev/null 2>&1; then
  echo "error: tests already fail on unmodified code" >&2
  exit 2
fi

summary="${GITHUB_STEP_SUMMARY:-}"
if [ -n "$summary" ]; then
  {
    echo "### Mutation check"
    echo
    echo "| Mutant | Description | Result | Failing tests |"
    echo "| --- | --- | --- | --- |"
  } >>"$summary"
fi

total=${#patches[@]}
killed=0
bad=0

for patch in "${patches[@]}"; do
  name="$(basename "$patch" .patch)"
  desc="$(sed -n 's/^Description: //p' "$patch" | head -n 1)"
  desc="${desc:-$name}"
  result=""
  tests=""

  if ! git apply "$patch" 2>/dev/null; then
    result="NOT APPLIED"
    echo "$name: NOT APPLIED (patch no longer matches $TARGET)"
    bad=$((bad + 1))
  elif git diff --quiet -- "$TARGET"; then
    result="NOT APPLIED"
    echo "$name: NOT APPLIED (patch left $TARGET unchanged)"
    bad=$((bad + 1))
  else
    out="$(mktemp)"
    if go test -count=1 -v "$PKG" >"$out" 2>&1; then
      result="SURVIVED"
      echo "$name: SURVIVED"
      bad=$((bad + 1))
    else
      # Top-level failing tests only (subtests are indented).
      tests="$(sed -n 's/^--- FAIL: \([^ ]*\).*/\1/p' "$out" | paste -sd, - | sed 's/,/, /g')"
      if [ -z "$tests" ]; then
        # Failed without a failing test: most likely a compile error, which
        # does not prove the tests catch the bug.
        result="ERROR"
        echo "$name: ERROR (go test failed without a failing test, see output below)"
        cat "$out"
        bad=$((bad + 1))
      else
        result="KILLED"
        echo "$name: KILLED by $tests"
        killed=$((killed + 1))
      fi
    fi
    rm -f "$out"
  fi

  restore

  if [ -n "$summary" ]; then
    echo "| \`$name\` | $desc | $result | ${tests:--} |" >>"$summary"
  fi
done

echo "$killed/$total mutants killed"
if [ -n "$summary" ]; then
  echo >>"$summary"
  echo "**$killed/$total mutants killed**" >>"$summary"
fi

[ "$bad" -eq 0 ]
