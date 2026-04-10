#!/bin/bash
set -e

# ─── UID/GID synchronization ───────────────────────────────────────
# Match the container's belayer user UID/GID to the host user's,
# so volume-mounted files have correct ownership.
ownership_sync_needed=false
if [ -n "${BELAYER_HOST_UID:-}" ] && [ "${BELAYER_HOST_UID}" != "1000" ]; then
    if usermod -u "$BELAYER_HOST_UID" belayer 2>/dev/null; then
        ownership_sync_needed=true
    fi
fi
if [ -n "${BELAYER_HOST_GID:-}" ] && [ "${BELAYER_HOST_GID}" != "1000" ]; then
    if groupmod -g "$BELAYER_HOST_GID" belayer 2>/dev/null; then
        ownership_sync_needed=true
    fi
fi

# Fix ownership of home directory only when UID/GID actually changed
if [ "$ownership_sync_needed" = true ]; then
    chown -R belayer:belayer /home/belayer 2>/dev/null || true
fi

# ─── Dev-mode binary override ──────────────────────────────────────
# If BELAYER_DEV_BINARIES is set, symlink the host-built binary in.
# This avoids rebuilding the Docker image during development.
if [ -n "${BELAYER_DEV_BINARIES:-}" ] && [ -f "$BELAYER_DEV_BINARIES/belayer" ]; then
    ln -sf "$BELAYER_DEV_BINARIES/belayer" /usr/local/bin/belayer
fi

# ─── Exit logging helper ─────────────────────────────────────────
# Since exec replaces this shell (EXIT trap would never fire), we
# inject exit logging directly into the tmux agent command wrapper.
BELAYER_SOCKET="${BELAYER_SOCKET:-/belayer/daemon.sock}"
BELAYER_SESSION_ID="${BELAYER_SESSION_ID:-}"
EXIT_LOG_CMD=""
if [ -n "$BELAYER_SESSION_ID" ]; then
    EXIT_LOG_CMD="belayer note \"agent exited with code \$EXIT_CODE\" --socket \"$BELAYER_SOCKET\" 2>/dev/null || true;"
fi

# ─── tmux session setup ────────────────────────────────────────────
# Two windows: 'agent' runs the vendor CLI, 'shell' is for debugging.
# If BELAYER_AGENT_CMD is empty, both windows are interactive shells.
# tmux attach requires a TTY; fall back when none is allocated
# (e.g. docker compose up without tty: true).
AGENT_CMD="${BELAYER_AGENT_CMD:-bash}"
AGENT_WRAPPER="$AGENT_CMD; EXIT_CODE=\$?; $EXIT_LOG_CMD echo ''; echo \"Agent exited with code \$EXIT_CODE. Shell available for debugging.\"; exec bash"

if [ -t 0 ] && [ -t 1 ]; then
    exec sudo -u belayer -E env "PATH=$PATH" "HOME=/home/belayer" \
        tmux new-session -d -s belayer -n agent "$AGENT_WRAPPER" \; \
        new-window -t belayer -n shell \; \
        select-window -t belayer:agent \; \
        attach-session -t belayer
else
    echo "No TTY allocated — running agent without tmux (attach will not work)" >&2
    exec sudo -u belayer -E env "PATH=$PATH" "HOME=/home/belayer" \
        bash -c "$AGENT_CMD; EXIT_CODE=\$?; $EXIT_LOG_CMD exit \$EXIT_CODE"
fi
