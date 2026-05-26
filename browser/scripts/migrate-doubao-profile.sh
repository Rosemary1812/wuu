#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: browser/scripts/migrate-doubao-profile.sh [--dry-run] [--apply] [--source-profile DIR] [--target-profile DIR]

Recover Doubao browser login state into the local Wuu Browser product profile.

This is a local recovery tool for the current macOS migration path. It copies
Doubao's encrypted cookie/password databases into the Wuu Browser profile and
copies the Doubao Safe Storage keychain secret to Wuu Browser Safe Storage using
macOS Security APIs. The secret is never printed or passed as a command-line
argument.

Environment overrides:
  WUU_DOUBAO_PROFILE_DIR   Source profile root. Defaults to
                           ~/Library/Application Support/Doubao.
  WUU_BROWSER_PROFILE_DIR  Target profile root. Defaults to
                           ~/Library/Application Support/Wuu Browser.

Options:
  --dry-run                Show what would change without modifying files or
                           keychain entries. This is the default.
  --apply                  Stop Wuu Browser, back up the target profile, and
                           apply the migration.
USAGE
}

mode=dry-run
source_profile="${WUU_DOUBAO_PROFILE_DIR:-${HOME}/Library/Application Support/Doubao}"
target_profile="${WUU_BROWSER_PROFILE_DIR:-${HOME}/Library/Application Support/Wuu Browser}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run)
      mode=dry-run
      shift
      ;;
    --apply)
      mode=apply
      shift
      ;;
    --source-profile)
      source_profile="${2:-}"
      if [[ -z "${source_profile}" ]]; then
        echo "--source-profile requires a directory" >&2
        exit 2
      fi
      shift 2
      ;;
    --target-profile)
      target_profile="${2:-}"
      if [[ -z "${target_profile}" ]]; then
        echo "--target-profile requires a directory" >&2
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

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "Doubao profile migration currently supports macOS only." >&2
  exit 2
fi

if [[ ! -d "${source_profile}" ]]; then
  echo "Doubao source profile not found: ${source_profile}" >&2
  exit 1
fi

if [[ ! -d "${source_profile}/Default" ]]; then
  echo "Doubao Default profile not found: ${source_profile}/Default" >&2
  exit 1
fi

if ! command -v swift >/dev/null 2>&1; then
  echo "swift is required to copy keychain entries without exposing secrets." >&2
  exit 2
fi

sqlite_bin=""
if [[ -x /usr/bin/sqlite3 ]]; then
  sqlite_bin=/usr/bin/sqlite3
elif command -v sqlite3 >/dev/null 2>&1; then
  sqlite_bin="$(command -v sqlite3)"
fi

source_service="Doubao Safe Storage"
source_account="Doubao"
target_service="Wuu Browser Safe Storage"
target_account="Wuu Browser"

credential_files=(
  "Default/Cookies"
  "Default/Cookies-journal"
  "Default/Login Data"
  "Default/Login Data-journal"
  "Default/Login Data For Account"
  "Default/Login Data For Account-journal"
  "Default/Network/Cookies"
  "Default/Network/Cookies-journal"
)

stop_wuu_browser() {
  local pids
  pids="$(ps -axo pid=,args= | awk -v target="${target_profile}" 'index($0, target) && $0 !~ /awk -v target/ {print $1}')"
  if [[ -z "${pids}" ]]; then
    return
  fi

  kill -TERM ${pids} 2>/dev/null || true
  sleep 1

  local remaining
  remaining="$(ps -p ${pids} -o pid= 2>/dev/null | tr '\n' ' ' || true)"
  if [[ -n "${remaining}" ]]; then
    kill -KILL ${remaining} 2>/dev/null || true
  fi
}

keychain_entry_exists() {
  local service="$1"
  local account="$2"
  security find-generic-password -s "${service}" -a "${account}" >/dev/null 2>&1
}

sqlite_count() {
  local db="$1"
  local query="$2"
  if [[ -z "${sqlite_bin}" || ! -f "${db}" ]]; then
    printf 'n/a'
    return
  fi
  local result
  if result="$("${sqlite_bin}" "${db}" "${query}" 2>/dev/null)"; then
    printf '%s' "${result}"
  else
    printf 'locked'
  fi
}

print_profile_summary() {
  local label="$1"
  local profile="$2"
  echo "${label}: ${profile}"
  if [[ -f "${profile}/Default/Cookies" ]]; then
    printf '  cookies:    '
    sqlite_count "${profile}/Default/Cookies" "select count(*) from cookies;"
    printf '\n'
    printf '  twitter/x:  '
    sqlite_count "${profile}/Default/Cookies" "select count(*) from cookies where host_key like '%twitter%' or host_key like '%x.com%';"
    printf '\n'
    printf '  bilibili:   '
    sqlite_count "${profile}/Default/Cookies" "select count(*) from cookies where host_key like '%bilibili%' or host_key like '%biliapi%';"
    printf '\n'
  fi
  if [[ -f "${profile}/Default/Login Data" ]]; then
    printf '  passwords:  '
    sqlite_count "${profile}/Default/Login Data" "select count(*) from logins;"
    printf '\n'
  fi
}

copy_keychain_secret() {
  local tmp_dir
  local swift_file
  tmp_dir="$(mktemp -d -t wuu-copy-keychain.XXXXXX)"
  swift_file="${tmp_dir}/copy-keychain.swift"
  trap 'rm -rf "${tmp_dir}"' RETURN

  cat > "${swift_file}" <<'SWIFT'
import Foundation
import Security

func die(_ message: String, _ code: Int32 = 1) -> Never {
  fputs(message + "\n", stderr)
  exit(code)
}

guard CommandLine.arguments.count == 5 else {
  die("usage: copy-keychain <source-service> <source-account> <target-service> <target-account>", 2)
}

let sourceService = CommandLine.arguments[1]
let sourceAccount = CommandLine.arguments[2]
let targetService = CommandLine.arguments[3]
let targetAccount = CommandLine.arguments[4]

let sourceQuery: [String: Any] = [
  kSecClass as String: kSecClassGenericPassword,
  kSecAttrService as String: sourceService,
  kSecAttrAccount as String: sourceAccount,
  kSecReturnData as String: true,
  kSecMatchLimit as String: kSecMatchLimitOne,
]

var sourceItem: CFTypeRef?
let readStatus = SecItemCopyMatching(sourceQuery as CFDictionary, &sourceItem)
guard readStatus == errSecSuccess, let sourceData = sourceItem as? Data else {
  die("failed to read source keychain item: \(readStatus)")
}

let targetQuery: [String: Any] = [
  kSecClass as String: kSecClassGenericPassword,
  kSecAttrService as String: targetService,
  kSecAttrAccount as String: targetAccount,
]

let updateStatus = SecItemUpdate(targetQuery as CFDictionary, [
  kSecValueData as String: sourceData,
] as CFDictionary)

if updateStatus == errSecSuccess {
  print("updated target keychain item")
  exit(0)
}

if updateStatus == errSecItemNotFound {
  var targetItem = targetQuery
  targetItem[kSecValueData as String] = sourceData
  targetItem[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlock
  let addStatus = SecItemAdd(targetItem as CFDictionary, nil)
  guard addStatus == errSecSuccess else {
    die("failed to add target keychain item: \(addStatus)")
  }
  print("added target keychain item")
  exit(0)
}

die("failed to update target keychain item: \(updateStatus)")
SWIFT

  swift "${swift_file}" "${source_service}" "${source_account}" "${target_service}" "${target_account}" >/dev/null
}

echo "Wuu Browser Doubao profile migration"
echo "  mode:   ${mode}"
print_profile_summary "  source" "${source_profile}"
print_profile_summary "  target" "${target_profile}"

if keychain_entry_exists "${source_service}" "${source_account}"; then
  echo "  source keychain: found"
else
  echo "  source keychain: missing" >&2
  exit 1
fi

if keychain_entry_exists "${target_service}" "${target_account}"; then
  echo "  target keychain: found"
else
  echo "  target keychain: will be created"
fi

echo "  files:"
for file in "${credential_files[@]}"; do
  if [[ -e "${source_profile}/${file}" ]]; then
    echo "    ${file}"
  fi
done

if [[ "${mode}" == "dry-run" ]]; then
  echo "Dry run only. Re-run with --apply to migrate local credential state."
  exit 0
fi

timestamp="$(date +%Y%m%d-%H%M%S)"
backup_profile="${target_profile}.backup-${timestamp}"

echo "Stopping Wuu Browser processes that use the target profile..."
stop_wuu_browser

mkdir -p "$(dirname "${target_profile}")"
if [[ -d "${target_profile}" ]]; then
  echo "Backing up target profile to ${backup_profile}"
  cp -pR "${target_profile}" "${backup_profile}"
else
  mkdir -p "${target_profile}"
fi

echo "Copying Doubao Safe Storage key to Wuu Browser Safe Storage..."
copy_keychain_secret

echo "Copying credential databases..."
for file in "${credential_files[@]}"; do
  source_file="${source_profile}/${file}"
  target_file="${target_profile}/${file}"
  if [[ ! -e "${source_file}" ]]; then
    continue
  fi
  mkdir -p "$(dirname "${target_file}")"
  cp -p "${source_file}" "${target_file}"
done

echo "Migration applied."
print_profile_summary "  target" "${target_profile}"
