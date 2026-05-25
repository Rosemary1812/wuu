#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: browser/scripts/prepare-checkouts.sh [options]

Prepare the local BrowserOS reference checkout and Chromium source checkout
used by Wuu Browser development. The checkouts live outside Git history under
.worktrees/ by default.

Options:
  --browseros-repo DIR  BrowserOS reference checkout directory.
  --chromium-src DIR    Chromium src checkout directory.
  --skip-browseros      Do not prepare the BrowserOS reference checkout.
  --skip-chromium       Do not prepare the Chromium checkout.
  --skip-fetch          Require an existing Chromium checkout; do not run fetch.
  --skip-sync           Do not run gclient sync after checking out Chromium.
  --force-sync          Force gclient to refresh dependencies, useful after
                        interrupted or incomplete syncs.
  --gclient-jobs N      Limit parallel gclient SCM jobs.
  --apply-patches       Apply Wuu Browser patches after Chromium setup.
  --allow-dirty         Allow operating on dirty existing checkouts.
  --dry-run             Print commands without executing them.

Environment:
  WUU_BROWSEROS_REPO       BrowserOS reference checkout override.
  WUU_CHROMIUM_SRC         Chromium src checkout override.
  WUU_BROWSEROS_GIT_URL    BrowserOS repository URL.
  WUU_GCLIENT_JOBS         Parallel gclient SCM jobs.
USAGE
}

dry_run=false
skip_browseros=false
skip_chromium=false
skip_fetch=false
skip_sync=false
force_sync=false
apply_patches=false
allow_dirty=false
gclient_jobs="${WUU_GCLIENT_JOBS:-}"

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"
browser_dir="${repo_root}/browser"

browseros_repo="${WUU_BROWSEROS_REPO:-${repo_root}/.worktrees/browseros}"
chromium_src="${WUU_CHROMIUM_SRC:-${repo_root}/.worktrees/chromium/src}"
browseros_git_url="${WUU_BROWSEROS_GIT_URL:-https://github.com/browseros-ai/BrowserOS.git}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --browseros-repo)
      browseros_repo="${2:-}"
      if [[ -z "${browseros_repo}" ]]; then
        echo "--browseros-repo requires a directory" >&2
        exit 2
      fi
      shift 2
      ;;
    --chromium-src)
      chromium_src="${2:-}"
      if [[ -z "${chromium_src}" ]]; then
        echo "--chromium-src requires a directory" >&2
        exit 2
      fi
      shift 2
      ;;
    --skip-browseros)
      skip_browseros=true
      shift
      ;;
    --skip-chromium)
      skip_chromium=true
      shift
      ;;
    --skip-fetch)
      skip_fetch=true
      shift
      ;;
    --skip-sync)
      skip_sync=true
      shift
      ;;
    --force-sync)
      force_sync=true
      shift
      ;;
    --gclient-jobs)
      gclient_jobs="${2:-}"
      if [[ -z "${gclient_jobs}" ]]; then
        echo "--gclient-jobs requires a positive integer" >&2
        exit 2
      fi
      shift 2
      ;;
    --apply-patches)
      apply_patches=true
      shift
      ;;
    --allow-dirty)
      allow_dirty=true
      shift
      ;;
    --dry-run)
      dry_run=true
      shift
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

read_first_metadata_value() {
  local label="$1"
  local file="$2"
  sed -n "s/^${label}: *//p" "${file}" | head -n 1 | tr -d '\r'
}

read_chromium_version() {
  local file="$1"
  local major=""
  local minor=""
  local build=""
  local patch=""

  while IFS='=' read -r key value; do
    case "${key}" in
      MAJOR) major="${value}" ;;
      MINOR) minor="${value}" ;;
      BUILD) build="${value}" ;;
      PATCH) patch="${value}" ;;
    esac
  done < "${file}"

  if [[ -n "${major}" && -n "${minor}" && -n "${build}" && -n "${patch}" ]]; then
    printf '%s.%s.%s.%s\n' "${major}" "${minor}" "${build}" "${patch}"
    return
  fi

  tr '\n' ' ' < "${file}" | sed 's/[[:space:]]*$//'
}

browseros_commit="$(read_first_metadata_value "commit" "${browser_dir}/BROWSEROS_SOURCE.md")"
browseros_branch="$(read_first_metadata_value "branch" "${browser_dir}/BROWSEROS_SOURCE.md")"
base_commit="$(tr -d '[:space:]' < "${browser_dir}/BASE_COMMIT")"
chromium_version="$(read_chromium_version "${browser_dir}/CHROMIUM_VERSION")"

if [[ -z "${base_commit}" || -z "${chromium_version}" ]]; then
  echo "Missing Chromium pin files under ${browser_dir}" >&2
  exit 1
fi

if [[ -n "${gclient_jobs}" && ! "${gclient_jobs}" =~ ^[1-9][0-9]*$ ]]; then
  echo "gclient jobs must be a positive integer: ${gclient_jobs}" >&2
  exit 2
fi

print_cmd() {
  printf '+'
  printf ' %q' "$@"
  printf '\n'
}

run() {
  print_cmd "$@"
  if [[ "${dry_run}" != "true" ]]; then
    "$@"
  fi
}

run_in() {
  local cwd="$1"
  shift
  printf '+ cd %q &&' "${cwd}"
  printf ' %q' "$@"
  printf '\n'
  if [[ "${dry_run}" != "true" ]]; then
    (cd "${cwd}" && "$@")
  fi
}

try_run_in() {
  local cwd="$1"
  shift
  printf '+ cd %q &&' "${cwd}"
  printf ' %q' "$@"
  printf '\n'
  if [[ "${dry_run}" != "true" ]]; then
    (cd "${cwd}" && "$@")
  fi
}

is_dirty() {
  local dir="$1"
  [[ -n "$(git -C "${dir}" status --porcelain 2>/dev/null)" ]]
}

require_clean_or_allowed() {
  local dir="$1"
  local label="$2"
  if [[ ! -d "${dir}/.git" && "${dry_run}" == "true" ]]; then
    return
  fi
  if [[ "${allow_dirty}" == "true" ]]; then
    return
  fi
  if is_dirty "${dir}"; then
    echo "${label} checkout is dirty: ${dir}" >&2
    echo "Commit, clean, or rerun with --allow-dirty." >&2
    exit 1
  fi
}

ensure_chromium_base_available() {
  if git -C "${chromium_src}" rev-parse --verify "${base_commit}^{commit}" >/dev/null 2>&1; then
    return
  fi

  if [[ -n "${chromium_version}" ]]; then
    try_run_in "${chromium_src}" git fetch origin "refs/tags/${chromium_version}:refs/tags/${chromium_version}" || true
  fi

  if git -C "${chromium_src}" rev-parse --verify "${base_commit}^{commit}" >/dev/null 2>&1; then
    return
  fi

  run_in "${chromium_src}" git fetch origin "${base_commit}"
}

gclient_sync_args() {
  local args=(sync -D --no-history --shallow)
  if [[ "${force_sync}" == "true" ]]; then
    args+=(--force)
  fi
  if [[ -n "${gclient_jobs}" ]]; then
    args+=(-j "${gclient_jobs}")
  fi
  printf '%s\0' "${args[@]}"
}

run_gclient_sync() {
  if [[ "${force_sync}" != "true" && -z "${gclient_jobs}" ]]; then
    if [[ "${dry_run}" == "true" ]]; then
      run_in "${chromium_src}" gclient sync -D --no-history --shallow
    elif command -v gclient >/dev/null 2>&1; then
      run_in "${chromium_src}" gclient sync -D --no-history --shallow
    elif command -v gclient.bat >/dev/null 2>&1; then
      run_in "${chromium_src}" gclient.bat sync -D --no-history --shallow
    else
      echo "gclient is required to sync Chromium dependencies." >&2
      echo "Install depot_tools and put it on PATH, or rerun with --skip-sync." >&2
      exit 2
    fi
    return
  fi

  local sync_args=()
  while IFS= read -r -d '' arg; do
    sync_args+=("${arg}")
  done < <(gclient_sync_args)

  if [[ "${dry_run}" == "true" ]]; then
    run_in "${chromium_src}" gclient "${sync_args[@]}"
  elif command -v gclient >/dev/null 2>&1; then
    run_in "${chromium_src}" gclient "${sync_args[@]}"
  elif command -v gclient.bat >/dev/null 2>&1; then
    run_in "${chromium_src}" gclient.bat "${sync_args[@]}"
  else
    echo "gclient is required to sync Chromium dependencies." >&2
    echo "Install depot_tools and put it on PATH, or rerun with --skip-sync." >&2
    exit 2
  fi
}

prepare_browseros() {
  if [[ "${skip_browseros}" == "true" ]]; then
    return
  fi

  echo "Preparing BrowserOS reference checkout"
  echo "  path:   ${browseros_repo}"
  echo "  url:    ${browseros_git_url}"
  echo "  branch: ${browseros_branch:-unknown}"
  echo "  commit: ${browseros_commit:-unknown}"

  if [[ ! -d "${browseros_repo}/.git" ]]; then
    run mkdir -p "$(dirname "${browseros_repo}")"
    run git clone "${browseros_git_url}" "${browseros_repo}"
  fi

  require_clean_or_allowed "${browseros_repo}" "BrowserOS"
  run_in "${browseros_repo}" git fetch origin

  if [[ -n "${browseros_commit}" ]]; then
    run_in "${browseros_repo}" git checkout "${browseros_commit}"
  elif [[ -n "${browseros_branch}" ]]; then
    run_in "${browseros_repo}" git checkout "${browseros_branch}"
  fi
}

prepare_chromium() {
  if [[ "${skip_chromium}" == "true" ]]; then
    return
  fi

  echo "Preparing Chromium checkout"
  echo "  src:              ${chromium_src}"
  echo "  chromium version: ${chromium_version}"
  echo "  base commit:      ${base_commit}"

  if [[ ! -d "${chromium_src}/.git" ]]; then
    if [[ "${skip_fetch}" == "true" ]]; then
      echo "Chromium checkout is missing and --skip-fetch was passed: ${chromium_src}" >&2
      exit 1
    fi
    if ! command -v fetch >/dev/null 2>&1 && [[ "${dry_run}" != "true" ]]; then
      echo "depot_tools fetch is required to create a Chromium checkout." >&2
      echo "Install depot_tools and put it on PATH, or set WUU_CHROMIUM_SRC to an existing checkout." >&2
      exit 2
    fi
    chromium_parent="$(dirname "${chromium_src}")"
    run mkdir -p "${chromium_parent}"
    run_in "${chromium_parent}" fetch --nohooks chromium
  fi

  if [[ ! -d "${chromium_src}/.git" && "${dry_run}" != "true" ]]; then
    echo "Chromium checkout not found after fetch: ${chromium_src}" >&2
    exit 1
  fi

  require_clean_or_allowed "${chromium_src}" "Chromium"
  ensure_chromium_base_available
  run_in "${chromium_src}" git checkout "${base_commit}"

  if [[ "${skip_sync}" != "true" ]]; then
    run_gclient_sync
  fi

  if [[ "${apply_patches}" == "true" ]]; then
    patch_args=(apply --src "${chromium_src}" --reset)
    [[ "${allow_dirty}" == "true" ]] && patch_args+=(--allow-dirty)
    run bash "${browser_dir}/scripts/patch-checkout.sh" "${patch_args[@]}"
  fi
}

echo "Wuu Browser checkout preparation"
echo "  repo: ${repo_root}"
echo "  mode: $([[ "${dry_run}" == "true" ]] && echo dry-run || echo execute)"

prepare_browseros
prepare_chromium

echo "Checkout preparation finished."
echo "Next:"
echo "  make browser-status"
echo "  make browser-patch-check ARGS=\"--src ${chromium_src}\""
