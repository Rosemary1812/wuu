#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"
browser_dir="${repo_root}/browser"

default_browseros_repo="${repo_root}/.worktrees/browseros"
default_chromium_src="${repo_root}/.worktrees/chromium/src"

browseros_repo="${WUU_BROWSEROS_REPO:-${default_browseros_repo}}"
chromium_src="${WUU_CHROMIUM_SRC:-${default_chromium_src}}"

if [[ ! -d "${browseros_repo}" && -d "${HOME}/wuu-browseros" ]]; then
  browseros_repo="${HOME}/wuu-browseros"
fi

if [[ ! -d "${chromium_src}" && -d "${HOME}/browseros-chromium/src" ]]; then
  chromium_src="${HOME}/browseros-chromium/src"
fi

print_heading() {
  printf '\n== %s ==\n' "$1"
}

git_dirty_count() {
  local path="$1"
  git -C "${path}" status --porcelain 2>/dev/null | wc -l | tr -d ' '
}

git_head() {
  local path="$1"
  git -C "${path}" rev-parse --short=12 HEAD 2>/dev/null || true
}

print_heading "Wuu Browser product"
printf 'repo:             %s\n' "${repo_root}"
printf 'browser dir:      %s\n' "${browser_dir}"
printf 'chromium version: %s\n' "$(tr '\n' ' ' < "${browser_dir}/CHROMIUM_VERSION" | sed 's/[[:space:]]*$//')"
printf 'base commit:      %s\n' "$(cat "${browser_dir}/BASE_COMMIT")"

print_heading "Imported browser assets"
printf 'build scripts:    %s\n' "$([[ -d "${browser_dir}/build" ]] && echo yes || echo no)"
printf 'chromium patches: %s\n' "$([[ -d "${browser_dir}/chromium_patches" ]] && echo yes || echo no)"
printf 'series patches:   %s\n' "$([[ -f "${browser_dir}/series_patches/series" ]] && echo yes || echo no)"
printf 'patch tool:       %s\n' "$([[ -f "${browser_dir}/tools/patch/go.mod" ]] && echo yes || echo no)"
printf 'agent extension:  %s\n' "$([[ -d "${browser_dir}/agent/apps/agent/dist/chrome-mv3-dev" ]] && echo yes || echo no)"
printf 'server resources: %s\n' "$([[ -d "${browser_dir}/agent/dist/prod/server" ]] && echo yes || echo no)"

print_heading "BrowserOS reference checkout"
printf 'path:             %s\n' "${browseros_repo}"
if [[ -d "${browseros_repo}/.git" ]]; then
  printf 'head:             %s\n' "$(git_head "${browseros_repo}")"
  printf 'dirty entries:    %s\n' "$(git_dirty_count "${browseros_repo}")"
  if [[ -f "${browseros_repo}/packages/browseros/CHROMIUM_VERSION" ]]; then
    printf 'version match:    '
    if cmp -s "${browser_dir}/CHROMIUM_VERSION" "${browseros_repo}/packages/browseros/CHROMIUM_VERSION"; then
      printf 'yes\n'
    else
      printf 'no\n'
    fi
  fi
  if [[ -f "${browseros_repo}/packages/browseros/BASE_COMMIT" ]]; then
    printf 'base match:       '
    if cmp -s "${browser_dir}/BASE_COMMIT" "${browseros_repo}/packages/browseros/BASE_COMMIT"; then
      printf 'yes\n'
    else
      printf 'no\n'
    fi
  fi
else
  printf 'status:           missing\n'
fi

print_heading "Chromium checkout"
printf 'path:             %s\n' "${chromium_src}"
if [[ -d "${chromium_src}/.git" ]]; then
  chromium_head="$(git -C "${chromium_src}" rev-parse HEAD 2>/dev/null || true)"
  printf 'head:             %s\n' "$(git_head "${chromium_src}")"
  printf 'base head match:  '
  if [[ -n "${chromium_head}" && "${chromium_head}" == "$(cat "${browser_dir}/BASE_COMMIT")"* ]]; then
    printf 'yes\n'
  else
    printf 'no\n'
  fi
  printf 'dirty entries:    %s\n' "$(git_dirty_count "${chromium_src}")"
  if [[ -f "${chromium_src}/chrome/VERSION" ]]; then
    printf 'chrome/VERSION:   %s\n' "$(tr '\n' ' ' < "${chromium_src}/chrome/VERSION" | sed 's/[[:space:]]*$//')"
  fi
else
  printf 'status:           missing\n'
fi

print_heading "Next migration steps"
printf '1. Use browser-prepare-checkouts to create or refresh local BrowserOS/Chromium checkouts.\n'
printf '2. Keep browser-patch-check/apply green against the target Chromium checkout.\n'
printf '3. Use browser-build-dev to compile the prepared checkout from this repository.\n'
printf '4. Use browser-build-agent to build repo-owned Wuu workbench/server assets.\n'
printf '5. Move more Wuu Browser product changes from manual checkout state into repo-owned patches/resources.\n'
printf '6. Continue replacing imported BrowserOS product surface with Wuu Browser product behavior.\n'
printf '7. Add packaging paths for macOS .dmg and Windows installer outputs.\n'
