#!/usr/bin/env bash
# Maps a release tag to an Android versionName and versionCode.
#   apk-version.sh v0.1.0-rc1   prints versionName= and versionCode= lines
#   apk-version.sh --self-test  checks the mapping and its ordering
# versionCode = MAJOR*1000000 + MINOR*10000 + PATCH*100 + (N for -rcN, else 99)
set -euo pipefail

version_of() {
  local tag=$1
  local re='^v(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]?)\.(0|[1-9][0-9]?)(-rc([1-9][0-9]?))?$'
  if [[ ! $tag =~ $re ]]; then
    echo "tag '$tag' is not vMAJOR.MINOR.PATCH or vMAJOR.MINOR.PATCH-rcN (N 1-98)" >&2
    return 1
  fi
  local rc=${BASH_REMATCH[5]:-99}
  if [[ -n ${BASH_REMATCH[4]} ]] && ((rc > 98)); then
    echo "tag '$tag': rc number must be 1-98" >&2
    return 1
  fi
  echo "versionName=${tag#v}"
  echo "versionCode=$((BASH_REMATCH[1] * 1000000 + BASH_REMATCH[2] * 10000 + BASH_REMATCH[3] * 100 + rc))"
}

self_test() {
  local fail=0 prev=0 tag out code
  # In ascending order.
  for tag in v0.0.1-rc1 v0.1.0-rc1 v0.1.0-rc2 v0.1.0-rc98 v0.1.0 v0.1.1-rc1 v0.1.1 v0.2.0 v1.0.0-rc1 v1.0.0 v10.20.30; do
    out=$(version_of "$tag") || { echo "$tag rejected, want accepted" >&2; fail=1; continue; }
    code=${out##*versionCode=}
    if ((code <= prev)); then
      echo "$tag: versionCode = $code, want more than $prev" >&2
      fail=1
    fi
    prev=$code
  done

  out=$(version_of v0.1.0-rc1)
  [[ $out == $'versionName=0.1.0-rc1\nversionCode=10001' ]] || { echo "v0.1.0-rc1 = $out, want 0.1.0-rc1 / 10001" >&2; fail=1; }
  out=$(version_of v0.1.0)
  [[ $out == $'versionName=0.1.0\nversionCode=10099' ]] || { echo "v0.1.0 = $out, want 0.1.0 / 10099" >&2; fail=1; }
  out=$(version_of v1.2.3)
  [[ $out == $'versionName=1.2.3\nversionCode=1020399' ]] || { echo "v1.2.3 = $out, want 1.2.3 / 1020399" >&2; fail=1; }

  for tag in "" v 0.1.0 v1 v1.2 v1.2.3.4 v1.2.3-rc v1.2.3-rc0 v1.2.3-rc99 v1.2.3-rc100 v1.2.3-beta1 v1.2.3-rc1-x v01.2.3 v1.02.3 v1.100.0 v1.0.100 v1000.0.0 V1.2.3 "v1.2.3 "; do
    if version_of "$tag" >/dev/null 2>&1; then
      echo "'$tag' accepted, want rejected" >&2
      fail=1
    fi
  done

  ((fail == 0)) && echo "apk-version self-test passed"
  return $fail
}

case ${1:-} in
  --self-test) self_test ;;
  "") echo "usage: apk-version.sh <tag> | --self-test" >&2; exit 2 ;;
  *) version_of "$1" ;;
esac
