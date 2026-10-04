#!/usr/bin/env bash
# Keep each complete Component group inside the original per-package deadline.
# Discover current tests so future Test, Example and Fuzz seed cases are included.
set -euo pipefail
mode=race
declare -a race_flags=(-race)
if (($# == 1)) && [[ "$1" == --normal ]]; then
  mode=normal
  race_flags=()
elif (($# != 0)); then
  echo 'Expected no argument or --normal' >&2
  exit 1
fi

if inventory=$(go test -p=1 -count=1 -tags=integration -timeout=120s ./conformance/component -list .); then
  :
else
  status=$?
  echo 'Component integration-race discovery failed' >&2
  exit "$status"
fi

declare -a closure=() content=() durable=() other=()
declare -A seen=()
count=0
while IFS= read -r name; do
  if [[ "$name" =~ ^(Test|Example|Fuzz)[^[:space:]]*$ ]]; then
    if [[ -n "${seen[$name]+present}" ]]; then
      echo "Duplicate Component runnable: $name" >&2
      exit 1
    fi
    seen[$name]=1
    count=$((count + 1))
    if [[ "$name" == TestContentFullClosureIncludesIntermediateVersionsAndExact64Bound ]]; then
      closure+=("$name")
    elif [[ "$name" == TestContent* ]]; then
      content+=("$name")
    elif [[ "$name" == TestDurable* ]]; then
      durable+=("$name")
    else
      other+=("$name")
    fi
  elif [[ "$name" == Benchmark* && "$name" != *[[:space:]]* ]]; then
    # Default go test does not execute benchmarks.
    :
  elif [[ "$name" =~ ^ok[[:space:]]+github\.com/ruipengliu/lerna/conformance/component[[:space:]] ]]; then
    :
  else
    echo "Unexpected Component discovery output: $name" >&2
    exit 1
  fi
done <<< "$inventory"

if ((count == 0 || count != ${#closure[@]} + ${#content[@]} + ${#durable[@]} + ${#other[@]})); then
  echo 'Component integration-race inventory is empty or incomplete' >&2
  exit 1
fi
echo "Component integration $mode: $count runnables (${#closure[@]} closure, ${#content[@]} content, ${#durable[@]} durable, ${#other[@]} other)"

# Use exact positive unions (Go RE2 has no negative lookahead). Escape literals
# rather than assuming future Go-valid Unicode identifiers fit an ASCII filter.
run_group() {
  local group=$1 name char escaped selector='' i
  shift
  if (($# == 0)); then
    echo "Component integration $mode: empty $group group"
    return
  fi
  for name in "$@"; do
    escaped=''
    for ((i = 0; i < ${#name}; i++)); do
      char=${name:i:1}
      case "$char" in
        '.'|'['|']'|'('|')'|'{'|'}'|'*'|'+'|'?'|'^'|'$'|'|'|'\') escaped+='\' ;;
      esac
      escaped+="$char"
    done
    selector+="${selector:+|}$escaped"
  done
  echo "Component integration $mode: running $group ($# runnables)"
  go test -p=1 -count=1 "${race_flags[@]}" -tags=integration -timeout=120s ./conformance/component -run "^($selector)$"
}

run_group closure "${closure[@]}"
run_group content "${content[@]}"
run_group durable "${durable[@]}"
run_group other "${other[@]}"
