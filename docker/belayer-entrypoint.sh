#!/bin/bash
set -e

# ─── Error trapping ────────────────────────────────────────────
# Log agent exit code to the session event store for debugging.
log_exit() {
    local exit_code=$?
    if [ -S "${BELAYER_SOCKET:-/belayer/daemon.sock}" ] && [ -n "$BELAYER_SESSION_ID" ]; then
        belayer note "agent exited with code $exit_code" --socket "${BELAYER_SOCKET:-/belayer/daemon.sock}" 2>/dev/null || true
    fi
}
trap log_exit EXIT

# ─── UID/GID synchronization ───────────────────────────────────────
# Match the container's belayer user UID/GID to the host user's,
# so volume-mounted files have correct ownership.
if [ -n "$BELAYER_HOST_UID" ] && [ "$BELAYER_HOST_UID" != "1000" ]; then
    usermod -u "$BELAYER_HOST_UID" belayer 2>/dev/null || true
fi
if [ -n "$BELAYER_HOST_GID" ] && [ "$BELAYER_HOST_GID" != "1000" ]; then
    groupmod -g "$BELAYER_HOST_GID" belayer 2>/dev/null || true
fi

# Fix ownership of home directory after UID/GID change
chown -R belayer:belayer /home/belayer 2>/dev/null || true

# ─── Dev-mode binary override ──────────────────────────────────────
# If BELAYER_DEV_BINARIES is set, symlink the host-built binary in.
# This avoids rebuilding the Docker image during development.
if [ -n "$BELAYER_DEV_BINARIES" ] && [ -f "$BELAYER_DEV_BINARIES/belayer" ]; then
    ln -sf "$BELAYER_DEV_BINARIES/belayer" /usr/local/bin/belayer
fi

# ─── tmux session setup ────────────────────────────────────────────
# Two windows: 'agent' runs the vendor CLI, 'shell' is for debugging.
# If BELAYER_AGENT_CMD is empty, both windows are interactive shells.
# tmux attach requires a TTY; fall back to sleep when none is allocated
# (e.g. docker compose up without tty: true).
AGENT_CMD="${BELAYER_AGENT_CMD:-bash}"

if [ -t 0 ] && [ -t 1 ]; then
    exec sudo -u belayer -E env "PATH=$PATH" "HOME=/home/belayer" \
        tmux new-session -d -s belayer -n agent "$AGENT_CMD; EXIT_CODE=\$?; echo ''; echo \"Agent exited with code \$EXIT_CODE. Shell available for debugging.\"; exec bash" \; \
        new-window -t belayer -n shell \; \
        select-window -t belayer:agent \; \
        attach-session -t belayer
else
    echo "No TTY allocated — running agent without tmux (attach will not work)" >&2
    exec sudo -u belayer -E env "PATH=$PATH" "HOME=/home/belayer" \
        bash -c "$AGENT_CMD"
fi
