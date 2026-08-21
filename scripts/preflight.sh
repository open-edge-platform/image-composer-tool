#!/usr/bin/env bash
# SPDX-FileCopyrightText: (C) 2026 Intel Corporation
# SPDX-License-Identifier: Apache-2.0
#
# preflight.sh — fail-fast tool validator for the ICT builder container.
#
# Checks every command path in internal/utils/shell/shell.go commandMap.
# Exits 0 if all commands resolve; exits 1 and prints the full missing list.

set -euo pipefail

command_map_file="internal/utils/shell/shell.go"
check_only=false

usage() {
    cat <<'EOF'
Usage: preflight.sh [--check-only] [--command-map-file <path>] [--] [command [args...]]

Options:
  --check-only                Validate required commands and exit.
  --command-map-file <path>   Path to shell.go containing commandMap.
  -h, --help                  Show this help.
EOF
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --check-only)
            check_only=true
            shift
            ;;
        --command-map-file)
            if [[ $# -lt 2 ]]; then
                echo "ERROR: --command-map-file requires a path" >&2
                exit 2
            fi
            command_map_file="$2"
            shift 2
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        --)
            shift
            break
            ;;
        -*)
            echo "ERROR: unknown argument: $1" >&2
            usage
            exit 2
            ;;
        *)
            break
            ;;
    esac
done

if [[ ! -f "$command_map_file" ]]; then
    echo "ERROR: command map file not found: $command_map_file" >&2
    exit 1
fi

mapfile -t command_entries < <(
    awk '
        /var[[:space:]]+commandMap[[:space:]]*=[[:space:]]*map\[string\]\[\]string{/ {in_map=1; next}
        in_map && /^[[:space:]]*}/ {in_map=0}
        in_map {
            if (match($0, /^[[:space:]]*\"([^\"]+)\":[[:space:]]*\{([^}]*)\}/, m)) {
                cmd = m[1]
                paths = m[2]
                gsub(/\"/, "", paths)
                gsub(/[[:space:]]+/, "", paths)
                print cmd "|" paths
            }
        }
    ' "$command_map_file"
)

expected_count=$(awk '
    /var[[:space:]]+commandMap[[:space:]]*=[[:space:]]*map\[string\]\[\]string{/ {in_map=1; next}
    in_map && /^[[:space:]]*}/ {in_map=0}
    in_map && match($0, /^[[:space:]]*\"[^\"]+\":[[:space:]]*/, m) {count++}
    END {print count+0}
' "$command_map_file")

if [[ ${#command_entries[@]} -eq 0 ]]; then
    echo "ERROR: no commands parsed from $command_map_file" >&2
    exit 1
fi

if [[ ${#command_entries[@]} -ne "$expected_count" ]]; then
    echo "ERROR: parsed ${#command_entries[@]} commandMap entries, expected $expected_count; preflight parser may be out of date" >&2
    exit 1
fi

missing=()
for entry in "${command_entries[@]}"; do
    cmd="${entry%%|*}"
    csv_paths="${entry#*|}"

    IFS=',' read -r -a paths <<< "$csv_paths"

    found=false
    for path in "${paths[@]}"; do
        # commandMap stores shell builtins (e.g. cd, command) as themselves.
        if [[ "$path" == "$cmd" ]]; then
            found=true
            break
        fi
        if [[ -x "$path" ]]; then
            found=true
            break
        fi
    done

    if [[ "$found" == "false" ]]; then
        missing+=("$cmd (expected one of: ${csv_paths//,/ })")
    fi
done

if [[ ${#missing[@]} -gt 0 ]]; then
    echo "PREFLIGHT FAILED: required tools missing from this image:" >&2
    for item in "${missing[@]}"; do
        echo "  - $item" >&2
    done
    exit 1
fi

echo "PREFLIGHT OK: ${#command_entries[@]} commandMap entries resolved."

if [[ "$check_only" == "true" ]]; then
    exit 0
fi

if [[ $# -gt 0 ]]; then
    exec "$@"
fi
