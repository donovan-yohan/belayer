#!/bin/bash
# Generate tinyproxy filter from ALLOWED_HOSTS env var.
# Entries are escaped for POSIX extended regex and anchored.
FILTER_FILE="/etc/tinyproxy/filter"
echo "# Auto-generated allowed hosts" > "$FILTER_FILE"

if [ -z "$ALLOWED_HOSTS" ]; then
    echo "warning: ALLOWED_HOSTS is empty, all traffic will be denied" >&2
    exec tinyproxy -d
fi

IFS=',' read -ra HOSTS <<< "$ALLOWED_HOSTS"
for host in "${HOSTS[@]}"; do
    host=$(echo "$host" | xargs)  # trim whitespace
    [ -z "$host" ] && continue
    # Escape regex metacharacters: . → \. and other specials
    escaped=$(echo "$host" | sed 's/[][\\.^$*+?(){}|]/\\&/g')
    # Anchor the pattern so it only matches the intended host
    echo "^https?://${escaped}(:[0-9]+)?/" >> "$FILTER_FILE"
done

exec tinyproxy -d
