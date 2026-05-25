#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: browser/scripts/patch-checkout.sh [status|check|apply] [options]

Inspect or apply Wuu Browser Chromium patches against a local Chromium checkout.

Commands:
  status   Summarize chromium_patches sync state. This is non-mutating.
  check    Run status plus series_patches apply/reverse checks. This is non-mutating.
  apply    Apply series_patches, then chromium_patches. Refuses dirty checkouts by default.

Options:
  --src DIR       Chromium source checkout. Defaults to WUU_CHROMIUM_SRC,
                  then .worktrees/chromium/src, then ~/browseros-chromium/src.
  --json          Emit raw JSON for chromium_patches status.
  --reset         Pass --reset to browseros-patch apply.
  --allow-dirty   Allow apply against a dirty Chromium checkout.
  --changed REF   Apply only chromium_patches changed in REF.
  --range-end REF End revision when using --changed as a range start.

Environment:
  WUU_CHROMIUM_SRC           Chromium source checkout override.
  WUU_PATCH_STATUS_LIMIT     Number of paths to show per status bucket. Defaults to 25.
USAGE
}

command_name="${1:-status}"
if [[ $# -gt 0 ]]; then
  case "$1" in
    status|check|apply)
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      command_name="status"
      ;;
  esac
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"
browser_dir="${repo_root}/browser"

chromium_src="${WUU_CHROMIUM_SRC:-${repo_root}/.worktrees/chromium/src}"
json_output=false
reset=false
allow_dirty=false
changed_ref=""
range_end=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --src)
      chromium_src="${2:-}"
      if [[ -z "${chromium_src}" ]]; then
        echo "--src requires a Chromium source directory" >&2
        exit 2
      fi
      shift 2
      ;;
    --json)
      json_output=true
      shift
      ;;
    --reset)
      reset=true
      shift
      ;;
    --allow-dirty)
      allow_dirty=true
      shift
      ;;
    --changed)
      changed_ref="${2:-}"
      if [[ -z "${changed_ref}" ]]; then
        echo "--changed requires a git revision" >&2
        exit 2
      fi
      shift 2
      ;;
    --range-end)
      range_end="${2:-}"
      if [[ -z "${range_end}" ]]; then
        echo "--range-end requires a git revision" >&2
        exit 2
      fi
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ ! -d "${chromium_src}" && -d "${HOME}/browseros-chromium/src" ]]; then
  chromium_src="${HOME}/browseros-chromium/src"
fi

if [[ ! -d "${chromium_src}/.git" ]]; then
  echo "Chromium checkout not found or not a git repository: ${chromium_src}" >&2
  exit 1
fi

patch_tool_dir="${browser_dir}/tools/patch"
if [[ ! -f "${patch_tool_dir}/go.mod" ]]; then
  echo "BrowserOS patch tool not found: ${patch_tool_dir}" >&2
  exit 1
fi

run_patch_tool() {
  (cd "${patch_tool_dir}" && go run . "$@")
}

patch_status_json() {
  run_patch_tool status --src "${chromium_src}" --json
}

summarize_patch_status() {
  local status_json="$1"
  local limit="${WUU_PATCH_STATUS_LIMIT:-25}"
  local status_file
  status_file="$(mktemp -t wuu-browser-patch-status.XXXXXX)"
  printf '%s' "${status_json}" > "${status_file}"
  if ! python3 - "${limit}" "${status_file}" <<'PY'
import json
import sys

limit = int(sys.argv[1])
with open(sys.argv[2], encoding="utf-8") as f:
    data = json.load(f)

print("chromium_patches:")
print(f"  checkout:     {data['workspace']['path']}")
print(f"  repo head:    {data['repo_head'][:12]}")
print(f"  base commit:  {data['base_commit']}")
print(f"  sync state:   {data['sync_state']}")

for key, label in [
    ("needs_apply", "needs apply"),
    ("needs_update", "needs update"),
    ("orphaned", "orphaned"),
    ("up_to_date", "up to date"),
]:
    values = data.get(key) or []
    print(f"  {label}: {len(values)}")
    if key == "up_to_date":
        continue
    for path in values[:limit]:
        print(f"    - {path}")
    if len(values) > limit:
        print(f"    ... {len(values) - limit} more")
PY
  then
    rm -f "${status_file}"
    return 1
  fi
  rm -f "${status_file}"
}

series_files_for_platform() {
  local series_dir="${browser_dir}/series_patches"
  local platform
  case "$(uname -s)" in
    Darwin) platform="macos" ;;
    Linux) platform="linux" ;;
    MINGW*|MSYS*|CYGWIN*) platform="windows" ;;
    *) platform="" ;;
  esac

  [[ -f "${series_dir}/series" ]] && printf '%s\n' "${series_dir}/series"
  if [[ -n "${platform}" && -f "${series_dir}/series.${platform}" ]]; then
    printf '%s\n' "${series_dir}/series.${platform}"
  fi
}

read_series_entries() {
  local series_file="$1"
  while IFS= read -r line || [[ -n "${line}" ]]; do
    line="${line#"${line%%[![:space:]]*}"}"
    line="${line%"${line##*[![:space:]]}"}"
    [[ -z "${line}" || "${line}" == \#* ]] && continue
    line="${line%% #*}"
    line="${line%"${line##*[![:space:]]}"}"
    [[ -n "${line}" ]] && printf '%s\n' "${line}"
  done < "${series_file}"
}

series_patch_state() {
  local patch_path="$1"
  if git -C "${chromium_src}" apply --check --ignore-whitespace -p1 "${patch_path}" >/dev/null 2>&1; then
    printf 'pending'
    return
  fi
  if git -C "${chromium_src}" apply --reverse --check --ignore-whitespace -p1 "${patch_path}" >/dev/null 2>&1; then
    printf 'applied'
    return
  fi
  printf 'failed'
}

check_series_patches() {
  local failed=0
  local pending=0
  local applied=0
  local total=0
  local series_file

  echo "series_patches:"
  while IFS= read -r series_file; do
    [[ -z "${series_file}" ]] && continue
    local rel
    while IFS= read -r rel; do
      [[ -z "${rel}" ]] && continue
      total=$((total + 1))
      local patch_path="${browser_dir}/series_patches/${rel}"
      if [[ ! -f "${patch_path}" ]]; then
        failed=$((failed + 1))
        echo "  missing: ${rel}"
        continue
      fi

      local state
      state="$(series_patch_state "${patch_path}")"
      case "${state}" in
        pending)
          pending=$((pending + 1))
          echo "  pending: ${rel}"
          ;;
        applied)
          applied=$((applied + 1))
          echo "  applied: ${rel}"
          ;;
        failed)
          failed=$((failed + 1))
          echo "  failed:  ${rel}"
          ;;
      esac
    done < <(read_series_entries "${series_file}")
  done < <(series_files_for_platform)

  echo "  total:   ${total}"
  echo "  pending: ${pending}"
  echo "  applied: ${applied}"
  echo "  failed:  ${failed}"
  [[ "${failed}" -eq 0 ]]
}

apply_series_patches() {
  local series_file
  while IFS= read -r series_file; do
    [[ -z "${series_file}" ]] && continue
    local rel
    while IFS= read -r rel; do
      [[ -z "${rel}" ]] && continue
      local patch_path="${browser_dir}/series_patches/${rel}"
      if [[ ! -f "${patch_path}" ]]; then
        echo "Series patch not found: ${rel}" >&2
        exit 1
      fi

      local state
      state="$(series_patch_state "${patch_path}")"
      case "${state}" in
        pending)
          echo "Applying series patch: ${rel}"
          git -C "${chromium_src}" apply --ignore-whitespace --whitespace=nowarn -p1 "${patch_path}"
          ;;
        applied)
          echo "Series patch already applied: ${rel}"
          ;;
        failed)
          echo "Series patch cannot be applied cleanly: ${rel}" >&2
          exit 1
          ;;
      esac
    done < <(read_series_entries "${series_file}")
  done < <(series_files_for_platform)
}

ensure_clean_checkout_for_apply() {
  if [[ "${allow_dirty}" == "true" ]]; then
    return
  fi
  if [[ -n "$(git -C "${chromium_src}" status --porcelain)" ]]; then
    echo "Chromium checkout is dirty: ${chromium_src}" >&2
    echo "Refusing to apply patches without --allow-dirty." >&2
    exit 1
  fi
}

case "${command_name}" in
  status)
    status_json="$(patch_status_json)"
    if [[ "${json_output}" == "true" ]]; then
      printf '%s\n' "${status_json}"
    else
      summarize_patch_status "${status_json}"
    fi
    ;;
  check)
    status_json="$(patch_status_json)"
    if [[ "${json_output}" == "true" ]]; then
      printf '%s\n' "${status_json}"
    else
      summarize_patch_status "${status_json}"
      check_series_patches
    fi
    ;;
  apply)
    ensure_clean_checkout_for_apply
    apply_series_patches
    apply_args=(apply --src "${chromium_src}")
    [[ "${reset}" == "true" ]] && apply_args+=(--reset)
    [[ -n "${changed_ref}" ]] && apply_args+=(--changed "${changed_ref}")
    [[ -n "${range_end}" ]] && apply_args+=(--range-end "${range_end}")
    run_patch_tool "${apply_args[@]}"
    ;;
  *)
    echo "Unknown command: ${command_name}" >&2
    usage >&2
    exit 2
    ;;
esac
