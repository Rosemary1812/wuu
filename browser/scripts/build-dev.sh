#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: browser/scripts/build-dev.sh [options]

Build a local Wuu Browser development app from a prepared Chromium checkout.

The default flow assumes the checkout has already been prepared with
browser-prepare-checkouts and Wuu Browser patches have already been applied.
Pass --prepare to run that setup first.

Options:
  --chromium-src DIR   Chromium source checkout directory.
  --arch ARCH          Build architecture: arm64 or x64. Defaults to host arch.
  --build-type TYPE    Build type passed to the BrowserOS build system.
                       Defaults to debug.
  --modules LIST       Comma-separated BrowserOS build modules.
                       Defaults to sparkle_setup,resources,bundled_extensions,
                       chromium_replace,configure,compile on macOS and omits
                       sparkle_setup on other platforms.
  --prepare            Prepare checkout and apply patches before building.
  --allow-dirty        Allow prepare/apply against a dirty checkout.
  --package-macos      Stage browser/out/Wuu Browser Dev.app after building.
  --dry-run            Print commands without executing them.

Environment:
  WUU_CHROMIUM_SRC          Chromium source checkout override.
  WUU_BROWSER_BUILD_ARCH    Build architecture override.
  WUU_BROWSER_BUILD_TYPE    Build type override.
  WUU_BROWSER_BUILD_MODULES Build module list override.
USAGE
}

dry_run=false
prepare=false
allow_dirty=false
package_macos=false

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"
browser_dir="${repo_root}/browser"

chromium_src="${WUU_CHROMIUM_SRC:-${repo_root}/.worktrees/chromium/src}"
build_arch="${WUU_BROWSER_BUILD_ARCH:-}"
build_type="${WUU_BROWSER_BUILD_TYPE:-debug}"
build_modules="${WUU_BROWSER_BUILD_MODULES:-}"

host_arch() {
  case "$(uname -m)" in
    arm64|aarch64) printf 'arm64' ;;
    x86_64|amd64) printf 'x64' ;;
    *)
      echo "Unsupported host architecture: $(uname -m)" >&2
      exit 2
      ;;
  esac
}

default_build_modules() {
  local modules="resources,bundled_extensions,chromium_replace,configure,compile"
  case "$(uname -s)" in
    Darwin) modules="sparkle_setup,${modules}" ;;
  esac
  printf '%s' "${modules}"
}

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

while [[ $# -gt 0 ]]; do
  case "$1" in
    --chromium-src)
      chromium_src="${2:-}"
      if [[ -z "${chromium_src}" ]]; then
        echo "--chromium-src requires a directory" >&2
        exit 2
      fi
      shift 2
      ;;
    --arch)
      build_arch="${2:-}"
      if [[ -z "${build_arch}" ]]; then
        echo "--arch requires arm64 or x64" >&2
        exit 2
      fi
      shift 2
      ;;
    --build-type)
      build_type="${2:-}"
      if [[ -z "${build_type}" ]]; then
        echo "--build-type requires a value" >&2
        exit 2
      fi
      shift 2
      ;;
    --modules)
      build_modules="${2:-}"
      if [[ -z "${build_modules}" ]]; then
        echo "--modules requires a comma-separated module list" >&2
        exit 2
      fi
      shift 2
      ;;
    --prepare)
      prepare=true
      shift
      ;;
    --allow-dirty)
      allow_dirty=true
      shift
      ;;
    --package-macos)
      package_macos=true
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

if [[ -z "${build_arch}" ]]; then
  build_arch="$(host_arch)"
fi

if [[ -z "${build_modules}" ]]; then
  build_modules="$(default_build_modules)"
fi

case "${build_arch}" in
  arm64|x64) ;;
  *)
    echo "Unsupported --arch value: ${build_arch}" >&2
    exit 2
    ;;
esac

if [[ ! -d "${chromium_src}" && -d "${HOME}/browseros-chromium/src" ]]; then
  chromium_src="${HOME}/browseros-chromium/src"
fi

if [[ ! -d "${chromium_src}/.git" && "${dry_run}" != "true" && "${prepare}" != "true" ]]; then
  echo "Chromium checkout not found or not a git repository: ${chromium_src}" >&2
  echo "Run make browser-prepare-checkouts or pass --prepare." >&2
  exit 1
fi

python_runner=()
if command -v uv >/dev/null 2>&1; then
  python_runner=(uv run python)
elif command -v python3 >/dev/null 2>&1; then
  python_runner=(python3)
else
  echo "Python 3 is required to run the BrowserOS build system." >&2
  exit 2
fi

prepare_args=(--chromium-src "${chromium_src}" --apply-patches)
if [[ "${allow_dirty}" == "true" ]]; then
  prepare_args+=(--allow-dirty)
fi
if [[ "${dry_run}" == "true" ]]; then
  prepare_args+=(--dry-run)
fi

build_args=(
  -m build.browseros
  build
  --modules "${build_modules}"
  --chromium-src "${chromium_src}"
  --arch "${build_arch}"
  --build-type "${build_type}"
)

echo "Wuu Browser Dev build"
echo "  chromium src: ${chromium_src}"
echo "  arch:         ${build_arch}"
echo "  build type:   ${build_type}"
echo "  modules:      ${build_modules}"
echo "  prepare:      ${prepare}"
echo "  package mac:  ${package_macos}"
echo "  mode:         $([[ "${dry_run}" == "true" ]] && echo dry-run || echo execute)"

if [[ "${prepare}" == "true" ]]; then
  run bash "${browser_dir}/scripts/prepare-checkouts.sh" "${prepare_args[@]}"
fi

run_in "${browser_dir}" "${python_runner[@]}" "${build_args[@]}"

if [[ "${package_macos}" == "true" ]]; then
  package_args=(--source-app "${chromium_src}/out/Default_${build_arch}/BrowserOS Dev.app")
  if [[ "${dry_run}" == "true" ]]; then
    package_args+=(--dry-run)
  fi
  run bash "${browser_dir}/scripts/package-dev-macos.sh" "${package_args[@]}"
fi

echo "Wuu Browser Dev build flow finished."
