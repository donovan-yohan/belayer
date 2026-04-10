#!/bin/bash
set -euo pipefail

# Generate tinyproxy filter from ALLOWED_HOSTS env var.
# Entries are escaped for POSIX extended regex and anchored.
# Supports exact hosts (github.com) and wildcard prefixes (*.github.com).
# Emits two patterns per host: one for HTTP URLs, one for HTTPS CONNECT.
FILTER_FILE="/etc/tinyproxy/filter"
FILTER_DIR="$(dirname "$FILTER_FILE")"
mkdir -p "$FILTER_DIR"
echo "# Auto-generated allowed hosts" > "$FILTER_FILE"

if [ -z "${ALLOWED_HOSTS:-}" ]; then
    echo "warning: ALLOWED_HOSTS is empty, all traffic will be denied" >&2
    exec tinyproxy -d
fi

IFS=',' read -ra HOSTS <<< "${ALLOWED_HOSTS}"
for host in "${HOSTS[@]}"; do
    host=$(echo "$host" | xargs)  # trim whitespace
    [ -z "$host" ] && continue

    # Handle wildcard prefix: *.example.com → match any subdomain
    if [[ "$host" == \*.* ]]; then
        base="${host#\*.}"
        if [[ ! "$base" =~ ^([A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?\.)*[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?$ ]]; then
            echo "Skipping invalid ALLOWED_HOSTS entry: $host" >&2
            continue
        fi
        escaped_base=$(printf '%s' "$base" | sed 's/[][\\.^$*+?(){}|]/\\&/g')
        # HTTP full-URL match
        echo "^https?://([^.]+\\.)*${escaped_base}(:[0-9]+)?/" >> "$FILTER_FILE"
        # HTTPS CONNECT match (host:port without scheme)
        echo "^([^.]+\\.)*${escaped_base}(:[0-9]+)?$" >> "$FILTER_FILE"
        continue
    fi

    # Validate exact host
    if [[ ! "$host" =~ ^([A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?\.)*[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?$ ]]; then
        echo "Skipping invalid ALLOWED_HOSTS entry: $host" >&2
        continue
    fi

    # Escape regex metacharacters
    escaped=$(printf '%s' "$host" | sed 's/[][\\.^$*+?(){}|]/\\&/g')
    # HTTP full-URL match
    echo "^https?://${escaped}(:[0-9]+)?/" >> "$FILTER_FILE"
    # HTTPS CONNECT match (host:port without scheme)
    echo "^${escaped}(:[0-9]+)?$" >> "$FILTER_FILE"
done

exec tinyproxy -d
