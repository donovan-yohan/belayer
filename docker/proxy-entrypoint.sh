#!/bin/bash
# Generate tinyproxy filter from ALLOWED_HOSTS env var
FILTER_FILE="/etc/tinyproxy/filter"
echo "# Auto-generated allowed hosts" > "$FILTER_FILE"

IFS=',' read -ra HOSTS <<< "$ALLOWED_HOSTS"
for host in "${HOSTS[@]}"; do
    host=$(echo "$host" | xargs)  # trim whitespace
    echo "$host" >> "$FILTER_FILE"
done

exec tinyproxy -d
