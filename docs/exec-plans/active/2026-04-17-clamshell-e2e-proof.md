# Clamshell End-to-End Proof of Concept

> **Status**: Active | **Created**: 2026-04-17 | **Last Updated**: 2026-04-17
> **Design Doc**: `docs/design-docs/2026-04-16-sandbox-runtime-and-crag-proof.md`
> **Consulted Learnings**: None
> **For Claude:** Use /harness:orchestrate to execute this plan.

## Goal

Prove that Belayer can run a full nightshift in a secure sandboxed environment: agents run inside Clamshell containers on a Colima VM, arielcharts pnpm dev server is the live runtime, and a real bugfix spec ("persist user's pan/zoom/view of chart on re-render") is completed end-to-end with full telemetry and network egress control.

## Architecture

Colima VM hosts: belayer daemon, pnpm dev (runtime), and the clamshell gateway. Bridge subprocesses (`python3 -m hermes_bridge`) run INSIDE clamshell Docker containers. The daemon communicates with sandboxed bridges over a TCP listener (not Unix socket — containers can't reach host Unix sockets). The pnpm dev server runs on the VM host; containers reach it at `172.17.0.1:3000`. QA agent uses Playwright (baked into the Docker image) to drive the running web app.

## Tech Stack

Go (belayer daemon, clamshell driver, daemon TCP listener), Python (hermes_bridge), Docker/clamshell (sandboxing), pnpm/Next.js (arielcharts runtime), Playwright (QA agent).

---

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-04-17 | Design | Use Colima over Lima | Lima had seccomp issues with Docker; Colima has better Docker compatibility |
| 2026-04-17 | Design | Bake hermes-agent + hermes_bridge into Docker image | `read_only_paths` in clamshell policy are Landlock rules, NOT Docker bind mounts — host paths aren't visible in container |
| 2026-04-17 | Design | Daemon TCP listener for bridge→daemon comms in clamshell mode | Unix socket is on host, containers can't reach it; TCP on 0.0.0.0 is reachable via Docker host gateway IP (172.17.0.1) |
| 2026-04-17 | Design | Use opencode-go provider (kimi-k2.5) for all agents | OPENCODE_GO_API_KEY is available; avoids Anthropic dependency for initial run |
| 2026-04-17 | Design | Per-session sandbox, shared /workspace | All agents in session share one clamshell sandbox; simplest model for PoC |

## Progress

- [ ] Task 1: Verify Colima VM environment (.belayer.env, dependencies)
- [ ] Task 2: Install clamshell CLI in Colima VM
- [ ] Task 3: Install hermes in Colima VM
- [ ] Task 4: Build and install belayer binary (linux/arm64, -tags clamshell) in Colima VM
- [ ] Task 5: Clone arielcharts and configure .belayer/ for clamshell + opencode-go
- [ ] Task 6: Fix seccomp profile (add statx syscall)
- [ ] Task 7: Build clamshell sandbox Docker image (with hermes-agent + playwright)
- [ ] Task 8: Add TCP listener to belayer daemon for clamshell bridge comms
- [ ] Task 9: Update http_client.py to support HTTP URLs (for TCP daemon socket)
- [ ] Task 10: Update daemon bridge env — pass TCP socket path in clamshell mode
- [ ] Task 11: Configure clamshell policy for arielcharts (opencode.ai, pnpm dev, daemon socket)
- [ ] Task 12: Verify noop-mode run (sanity check before clamshell)
- [ ] Task 13: First clamshell run — iterate on network policy until agents complete
- [ ] Task 14: Verify telemetry and add network deny event logging
- [ ] Task 15: Write summary document

## Surprises & Discoveries

_Updated as we work._

## Plan Drift

_None yet._

---

## Task 1: Verify Colima VM Environment

**Files:**
- Colima VM: `~/.belayer.env`, `~/.bash_profile`

**What we expect to be in place (from prior session):**
- Docker 29.2.1 running ✓
- `~/extend-clamshell/` with modified Dockerfile ✓
- `~/.belayer.env` with OPENCODE_GO_API_KEY, HERMES_MAX_ITERATIONS=100, GITHUB_TOKEN, OPENCODE_GO_BASE_URL ✓
- `python3-pip`, `python3-venv`, `git` installed ✓

- [ ] **Step 1: SSH into Colima and verify .belayer.env**

```bash
colima ssh -- bash -c "cat ~/.belayer.env | sed 's/=.*/=***/' && echo '---' && ls ~/extend-clamshell/ && docker --version"
```

Expected: four env var names (values masked), extend-clamshell directory listing, Docker 29.2.x

- [ ] **Step 2: Ensure .bash_profile sources .belayer.env**

```bash
colima ssh -- bash -c "grep -n belayer.env ~/.bash_profile 2>/dev/null || echo 'NOT FOUND — need to add'"
```

If not present:
```bash
colima ssh -- bash -c "cat >> ~/.bash_profile << 'PROFILE_EOF'
set -a; [ -f ~/.belayer.env ] && source ~/.belayer.env; set +a
PROFILE_EOF"
```

- [ ] **Step 3: Verify env vars load in a fresh shell**

```bash
colima ssh -- bash -lc "echo OPENCODE_GO_BASE_URL=\$OPENCODE_GO_BASE_URL && echo GITHUB_TOKEN_SET=\$([ -n \"\$GITHUB_TOKEN\" ] && echo yes || echo no)"
```

Expected: `OPENCODE_GO_BASE_URL=https://opencode.ai/zen/go/v1`, `GITHUB_TOKEN_SET=yes`

- [ ] **Step 4: Commit (no code change, just verify)**

---

## Task 2: Install clamshell CLI in Colima VM

**Files:**
- Colima VM: `~/extend-clamshell/` (already exists)

- [ ] **Step 1: Install clamshell as editable package**

```bash
colima ssh -- bash -lc "cd ~/extend-clamshell && pip3 install -e . 2>&1 | tail -10"
```

Expected: `Successfully installed clamshell-...`

- [ ] **Step 2: Verify clamshell CLI is working**

```bash
colima ssh -- bash -lc "clamshell --version || python3 -m clamshell --version"
```

Expected: version string

- [ ] **Step 3: Start clamshell gateway**

```bash
colima ssh -- bash -lc "clamshell gateway start 2>&1"
```

Expected: gateway starts or is already running

- [ ] **Step 4: Verify gateway is running**

```bash
colima ssh -- bash -lc "clamshell gateway status 2>&1"
```

Expected: running/listening status

---

## Task 3: Install hermes in Colima VM

**Files:**
- Colima VM: `~/.hermes/hermes-agent/`

The bridge's `defaultPythonCmd()` in `internal/bridge/bridge.go:22` looks for `~/.hermes/hermes-agent/venv/bin/python3` first. We need hermes installed there.

- [ ] **Step 1: Create hermes directories and venv**

```bash
colima ssh -- bash -lc "mkdir -p ~/.hermes/hermes-agent ~/.hermes/profiles && python3 -m venv ~/.hermes/hermes-agent/venv"
```

Expected: no errors

- [ ] **Step 2: Install hermes-agent from GitHub**

hermes-agent is not on PyPI, must install from GitHub:
```bash
colima ssh -- bash -lc "~/.hermes/hermes-agent/venv/bin/pip install --upgrade pip && ~/.hermes/hermes-agent/venv/bin/pip install git+https://github.com/NousResearch/hermes-agent.git 2>&1 | tail -5"
```

Expected: `Successfully installed hermes-agent-...`

- [ ] **Step 3: Verify hermes modules are importable**

```bash
colima ssh -- bash -lc "~/.hermes/hermes-agent/venv/bin/python3 -c \"from run_agent import AIAgent; print('hermes OK')\""
```

Expected: `hermes OK`

- [ ] **Step 4: Verify hermes_bridge is also importable (via site-packages in Docker image — skip for now, verify in Task 7)**

This is for the container; on the VM host, hermes_bridge is accessible via PYTHONPATH (set by belayer to BelayerRoot). That's handled when belayer runs noop mode. For clamshell mode, it's baked into the image.

---

## Task 4: Build and Install belayer Binary in Colima VM

**Files:**
- Local Mac: `cmd/belayer/`, `internal/sandbox/clamshell.go`
- Colima VM: `~/bin/belayer` or `~/.local/bin/belayer`

The clamshell driver only compiles with `-tags clamshell`. We cross-compile from Mac for linux/arm64 (Colima uses ARM64 on Apple Silicon).

- [ ] **Step 1: Cross-compile belayer for linux/arm64 with clamshell tag**

Run on Mac (local terminal):
```bash
cd /Users/donovanyohan/Documents/Programs/personal/belayer
GOOS=linux GOARCH=arm64 go build -tags clamshell -o /tmp/belayer-linux-arm64 ./cmd/belayer && echo "Build OK: $(file /tmp/belayer-linux-arm64)"
```

Expected: `ELF 64-bit LSB executable, ARM aarch64`

- [ ] **Step 2: Copy binary into Colima VM**

```bash
colima ssh -- bash -c "mkdir -p ~/.local/bin"
colima cp /tmp/belayer-linux-arm64 colima:~/.local/bin/belayer
colima ssh -- chmod +x ~/.local/bin/belayer
```

- [ ] **Step 3: Add ~/.local/bin to PATH in .bash_profile**

```bash
colima ssh -- bash -c "grep -q '.local/bin' ~/.bash_profile || echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.bash_profile"
```

- [ ] **Step 4: Verify belayer is installed and built with clamshell**

```bash
colima ssh -- bash -lc "belayer --version && belayer daemon --help | grep -i sandbox"
```

Expected: version string; help text mentioning sandbox/socket options

- [ ] **Step 5: Commit**

```bash
git add -p  # if any belayer source changes were needed
git commit -m "build: cross-compile belayer with clamshell tag for linux/arm64"
```

---

## Task 5: Clone arielcharts and Configure .belayer/

**Files:**
- Colima VM: `~/arielcharts/.belayer/config.yaml`, `~/arielcharts/.belayer/agents/*/agent.yaml`, `~/arielcharts/.belayer/policies/standard.yaml`

- [ ] **Step 1: Clone arielcharts in Colima VM**

```bash
colima ssh -- bash -lc "[ -d ~/arielcharts ] && echo 'already exists' || git clone https://github.com/donovan-yohan/arielcharts.git ~/arielcharts"
```

- [ ] **Step 2: Run belayer init to scaffold .belayer/**

```bash
colima ssh -- bash -lc "cd ~/arielcharts && belayer init && ls .belayer/"
```

Expected: `.belayer/agents/` with 6 agent directories, `config.yaml`, `policies/`

- [ ] **Step 3: Update .belayer/config.yaml for clamshell mode and runtime**

```bash
colima ssh -- python3 -c "
import os
cfg = '''sandbox:
  mode: clamshell
  policy: .belayer/policies/standard.yaml

runtime:
  up: \"pnpm install --frozen-lockfile && pnpm dev &\"
  health: \"curl -sf http://localhost:3000 || curl -sf http://localhost:4000/health\"
  down: \"pkill -f next || pkill -f tsx || true\"
  endpoints:
    - name: web
      host: localhost
      port: 3000
    - name: server
      host: localhost
      port: 4000
'''
os.makedirs(os.path.expanduser('~/arielcharts/.belayer'), exist_ok=True)
open(os.path.expanduser('~/arielcharts/.belayer/config.yaml'), 'w').write(cfg)
print('config.yaml written')
"
```

- [ ] **Step 4: Update all agent.yaml files to use opencode-go + kimi-k2.5**

```bash
colima ssh -- bash -lc "for f in ~/arielcharts/.belayer/agents/*/agent.yaml; do
  python3 -c \"
import sys, re
path = sys.argv[1]
content = open(path).read()
content = re.sub(r'vendor:.*', 'vendor: opencode-go', content)
content = re.sub(r'model:.*', 'model: kimi-k2.5', content)
open(path, 'w').write(content)
print('Updated:', path)
\" \"\$f\"
done"
```

- [ ] **Step 5: Create .belayer/policies/standard.yaml with correct endpoints**

```bash
colima ssh -- python3 -c "
policy = '''version: 5

sandbox:
  user: sandbox
  group: sandbox
  work_dir: /workspace
  runtime_dir: /run/agent
  artifact_outbox: /run/agent/outbox
  read_only_paths:
    - /usr
    - /lib
    - /lib64
    - /bin
    - /sbin
    - /etc
    - /var/log
    - /app
  read_write_paths:
    - /tmp
    - /workspace
    - /run/agent
    - /run/agent/outbox
  masked_paths:
    - /proc/kcore
    - /proc/latency_stats
    - /proc/timer_list
    - /sys/firmware
  tmpfs_paths:
    - /tmp

process:
  no_new_privileges: true
  drop_capabilities: [\"ALL\"]
  memory_limit: 4096m
  pids_limit: 512
  cpu_quota: 200000
  read_only_rootfs: false

network:
  mode: proxy
  proxy_listen: 3128
  allow_http: false
  providers:
    github: true
    npm: true
    pypi: true
  custom_endpoints:
    - host: opencode.ai
      ports: [443]
      protocol: rest
      note: OpenCode Go inference (kimi-k2.5)
    - host: api.moonshot.cn
      ports: [443]
      protocol: rest
      note: Moonshot/Kimi inference
    - host: portal.nousresearch.com
      ports: [443]
      protocol: rest
      note: Hermes Nous OAuth
    - host: inference-api.nousresearch.com
      ports: [443]
      protocol: rest
      note: Hermes Nous inference
  localhost:
    - host: 172.17.0.1
      ports: [3000, 4000, 7523]
      note: arielcharts pnpm dev server + belayer daemon TCP socket
  tcp_endpoints: []
'''
import os
os.makedirs(os.path.expanduser('~/arielcharts/.belayer/policies'), exist_ok=True)
open(os.path.expanduser('~/arielcharts/.belayer/policies/standard.yaml'), 'w').write(policy)
print('policy written')
"
```

Note: Port 7523 is the belayer daemon TCP listener (added in Task 8). `172.17.0.1` is the Docker host gateway IP reachable from inside Colima Docker containers.

- [ ] **Step 6: Install Node.js + pnpm in Colima VM (for runtime)**

```bash
colima ssh -- bash -lc "which node || (curl -fsSL https://deb.nodesource.com/setup_20.x | sudo -E bash - && sudo apt-get install -y nodejs && npm install -g pnpm)"
colima ssh -- bash -lc "node --version && pnpm --version"
```

Expected: node 20.x, pnpm version

---

## Task 6: Fix Seccomp Profile (statx Syscall)

**Files:**
- Colima VM: `~/extend-clamshell/etc/seccomp-default.json`

The default clamshell seccomp profile uses `SCMP_ACT_ERRNO` (deny all) as the default action but is missing `statx` which Docker/runc uses during container setup. This causes exit 127/125 on container startup.

- [ ] **Step 1: Verify statx is missing from the profile**

```bash
colima ssh -- bash -lc "grep -c 'statx' ~/extend-clamshell/etc/seccomp-default.json && echo 'statx present' || echo 'statx MISSING'"
```

- [ ] **Step 2: Check current syscall list structure**

```bash
colima ssh -- bash -lc "python3 -c \"import json; d=json.load(open('/root/extend-clamshell/etc/seccomp-default.json')); print('defaultAction:', d.get('defaultAction')); print('syscall count:', len(d.get('syscalls', [])))\""
```

Expected: `defaultAction: SCMP_ACT_ERRNO`, syscall count >100

- [ ] **Step 3: Add statx and other commonly missing syscalls**

```bash
colima ssh -- python3 << 'PYEOF'
import json, copy

path = '/root/extend-clamshell/etc/seccomp-default.json'
with open(path) as f:
    profile = json.load(f)

# Check if statx exists
existing_names = set()
for entry in profile.get('syscalls', []):
    for name in entry.get('names', []):
        existing_names.add(name)

missing = [s for s in ['statx', 'clone3', 'faccessat2', 'epoll_pwait2', 'landlock_create_ruleset', 'landlock_add_rule', 'landlock_restrict_self', 'io_uring_setup', 'io_uring_enter', 'io_uring_register', 'memfd_secret'] if s not in existing_names]
print('Missing syscalls to add:', missing)

if missing:
    profile['syscalls'].append({
        'names': missing,
        'action': 'SCMP_ACT_ALLOW'
    })
    with open(path, 'w') as f:
        json.dump(profile, f, indent=2)
    print('Updated seccomp profile with', len(missing), 'missing syscalls')
else:
    print('All target syscalls already present')
PYEOF
```

- [ ] **Step 4: Verify statx is now in the profile**

```bash
colima ssh -- bash -lc "python3 -c \"import json; d=json.load(open('/root/extend-clamshell/etc/seccomp-default.json')); names=[n for e in d['syscalls'] for n in e.get('names',[])]; print('statx:', 'statx' in names)\""
```

Expected: `statx: True`

---

## Task 7: Build Clamshell Sandbox Docker Image

**Files:**
- Colima VM: `~/extend-clamshell/images/sandbox-rootfs/Dockerfile`

The Dockerfile already has hermes-agent and hermes_bridge modifications from the prior session. We need to verify, add Playwright, and build.

- [ ] **Step 1: Review the current Dockerfile**

```bash
colima ssh -- bash -lc "cat ~/extend-clamshell/images/sandbox-rootfs/Dockerfile"
```

Expected: should include `pip install git+https://github.com/NousResearch/hermes-agent.git` and `hermes_bridge` copy

- [ ] **Step 2: Ensure Dockerfile has Playwright**

Add playwright AFTER hermes installation. Append to Dockerfile if missing:
```bash
colima ssh -- bash -lc "grep -q playwright ~/extend-clamshell/images/sandbox-rootfs/Dockerfile && echo 'playwright present' || echo 'playwright MISSING'"
```

If missing, add it:
```bash
colima ssh -- python3 -c "
path = '/root/extend-clamshell/images/sandbox-rootfs/Dockerfile'
content = open(path).read()

if 'playwright' not in content:
    # Find a good insertion point — after the pip installs
    playwright_lines = '''
# Install Playwright for QA agent
RUN pip install --no-cache-dir playwright \\\\ 
    && playwright install chromium \\\\ 
    && playwright install-deps chromium
'''
    # Insert before the last CMD or ENTRYPOINT
    if 'CMD' in content:
        content = content.replace('CMD', playwright_lines + '\nCMD', 1)
    else:
        content += playwright_lines
    open(path, 'w').write(content)
    print('Added playwright to Dockerfile')
else:
    print('playwright already in Dockerfile')
"
```

- [ ] **Step 3: Build the Docker image**

```bash
colima ssh -- bash -lc "cd ~/extend-clamshell/images/sandbox-rootfs && docker build -t belayer-sandbox:latest . 2>&1 | tail -20"
```

Expected: `Successfully built ...`, `Successfully tagged belayer-sandbox:latest`

This may take a few minutes (first build, downloads hermes-agent from GitHub).

- [ ] **Step 4: Verify hermes imports work inside the image**

```bash
colima ssh -- docker run --rm belayer-sandbox:latest python3 -c "from run_agent import AIAgent; from hermes_state import SessionDB; import hermes_bridge; print('ALL OK')"
```

Expected: `ALL OK`

- [ ] **Step 5: Verify playwright is importable**

```bash
colima ssh -- docker run --rm belayer-sandbox:latest python3 -c "from playwright.sync_api import sync_playwright; print('playwright OK')"
```

Expected: `playwright OK`

- [ ] **Step 6: Check that clamshell knows about the new image**

```bash
colima ssh -- bash -lc "clamshell image list 2>&1 || clamshell images list 2>&1 || echo 'check clamshell docs for image command'"
```

Update extend-clamshell config to point to `belayer-sandbox:latest` if needed (consult clamshell docs for the config key).

---

## Task 8: Add TCP Listener to Belayer Daemon

**Files:**
- `internal/daemon/daemon.go`
- `internal/cli/daemon.go`

When `sandbox.mode: clamshell`, agents run inside Docker containers that cannot reach the host Unix socket `~/.belayer/daemon.sock`. The daemon needs to also bind a TCP listener so containers can reach it via Docker's host gateway (`172.17.0.1`).

- [ ] **Step 1: Add TCPAddr field to daemon.Config**

In `internal/daemon/daemon.go`, find the `Config` struct (line 28) and add:

```go
type Config struct {
	SocketPath  string
	DBPath      string
	BelayerRoot string
	// TCPAddr, if non-empty, causes the daemon to listen on this TCP address
	// in addition to the Unix socket. Used when sandbox.mode=clamshell so
	// bridge subprocesses inside Docker containers can reach the daemon via
	// the Docker host gateway (172.17.0.1).
	TCPAddr string
	// ...existing fields...
	SandboxDrivers *sandbox.Registry
	Runtime        runtime.Provider
}
```

- [ ] **Step 2: Write a failing test for TCP listener**

In `internal/daemon/daemon_test.go` (or create it), add:

```go
func TestDaemonTCPListener(t *testing.T) {
    cfg := daemon.Config{
        SocketPath: filepath.Join(t.TempDir(), "daemon.sock"),
        DBPath:     filepath.Join(t.TempDir(), "test.db"),
        TCPAddr:    "127.0.0.1:0", // port 0 = OS picks a free port
    }
    d, err := daemon.New(cfg)
    if err != nil {
        t.Fatalf("New: %v", err)
    }
    ctx, cancel := context.WithCancel(context.Background())
    errCh := make(chan error, 1)
    go func() { errCh <- d.Start(ctx) }()
    time.Sleep(50 * time.Millisecond) // wait for listeners

    // Daemon should expose a TCPPort() method returning the actual bound port.
    port := d.TCPPort()
    if port == 0 {
        t.Fatal("TCPPort() returned 0 after Start")
    }

    // Hit /health over TCP.
    resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
    if err != nil {
        t.Fatalf("GET /health over TCP: %v", err)
    }
    resp.Body.Close()
    if resp.StatusCode != 200 {
        t.Fatalf("expected 200, got %d", resp.StatusCode)
    }

    cancel()
    <-errCh
}
```

Run: `go test ./internal/daemon/... -run TestDaemonTCPListener -v`
Expected: FAIL (TCPAddr not wired yet)

- [ ] **Step 3: Implement TCP listener in daemon.Start()**

In `internal/daemon/daemon.go`, modify the `Start` function after the Unix socket listener setup. Find where `net.Listen("unix", ...)` is called (around line 163) and add TCP listener:

```go
func (d *Daemon) Start(ctx context.Context) error {
    // ... existing Unix socket setup ...
    ln, err := net.Listen("unix", d.config.SocketPath)
    if err != nil {
        return fmt.Errorf("daemon: listen unix: %w", err)
    }
    os.Chmod(d.config.SocketPath, 0o600)
    d.listener = ln

    // TCP listener for clamshell containers (optional).
    var tcpListener net.Listener
    if d.config.TCPAddr != "" {
        tcpLn, err := net.Listen("tcp", d.config.TCPAddr)
        if err != nil {
            ln.Close()
            return fmt.Errorf("daemon: listen tcp %s: %w", d.config.TCPAddr, err)
        }
        d.tcpListener = tcpLn
        d.tcpPort = tcpLn.Addr().(*net.TCPAddr).Port
        log.Printf("daemon: TCP listener on %s (port %d)", d.config.TCPAddr, d.tcpPort)
    }
    // ... rest of Start ...
}
```

Add fields to the `Daemon` struct:
```go
type Daemon struct {
    // ...existing fields...
    tcpListener net.Listener
    tcpPort     int
}
```

Add a `TCPPort()` method:
```go
func (d *Daemon) TCPPort() int { return d.tcpPort }
```

Ensure the TCP listener also serves the same HTTP mux. After `d.server = &http.Server{Handler: mux}`, also serve on tcpListener:
```go
if d.tcpListener != nil {
    tcpServer := &http.Server{Handler: mux}
    go tcpServer.Serve(d.tcpListener)
    // Add tcpServer to Shutdown call in the shutdown path.
}
```

Also close tcpListener in `Shutdown()`.

- [ ] **Step 4: Run the test to verify it passes**

```bash
go test ./internal/daemon/... -run TestDaemonTCPListener -v
```

Expected: PASS

- [ ] **Step 5: Add --tcp-addr flag to daemon CLI**

In `internal/cli/daemon.go`, add flag and wiring:

```go
var tcpAddr string
// in RunE:
if tcpAddr != "" {
    cfg.TCPAddr = tcpAddr
}
// in cmd.Flags():
cmd.Flags().StringVar(&tcpAddr, "tcp-addr", "", "Also bind a TCP listener (e.g. 0.0.0.0:7523) for clamshell container access")
```

- [ ] **Step 6: Run all daemon tests**

```bash
go test ./internal/daemon/... -v 2>&1 | tail -30
```

Expected: all PASS

- [ ] **Step 7: Commit**

```bash
git add internal/daemon/daemon.go internal/cli/daemon.go
git commit -m "feat(daemon): add optional TCP listener for clamshell container bridge access"
```

---

## Task 9: Update hermes_bridge http_client to Support HTTP URLs

**Files:**
- `hermes_bridge/http_client.py`

When `BELAYER_SOCKET` is an HTTP URL (e.g., `http://172.17.0.1:7523`), use regular HTTP instead of Unix socket.

- [ ] **Step 1: Update unix_post and unix_get to handle both Unix and HTTP**

Replace the contents of `hermes_bridge/http_client.py`:

```python
"""HTTP client for daemon communication.

Supports both Unix socket paths (e.g. /path/to/daemon.sock) and
HTTP URL addresses (e.g. http://172.17.0.1:7523) for the BELAYER_SOCKET
env var. Unix sockets are used when belayer runs with noop sandbox mode;
HTTP URLs are used when agents run inside clamshell Docker containers and
need to reach the daemon on the host via Docker's bridge gateway.
"""

import json
import logging
import http.client
import socket as sock
from urllib.parse import urlparse

log = logging.getLogger("http_client")


def _is_http_url(socket_path: str) -> bool:
    return socket_path.startswith("http://") or socket_path.startswith("https://")


def unix_post(socket_path: str, path: str, body: dict) -> tuple[int, str]:
    """POST JSON body to the daemon over its Unix socket or TCP address."""
    try:
        payload = json.dumps(body).encode()
        headers = {"Content-Type": "application/json"}

        if _is_http_url(socket_path):
            parsed = urlparse(socket_path)
            conn = http.client.HTTPConnection(parsed.hostname, parsed.port or 80)
        else:
            conn = http.client.HTTPConnection("localhost")
            s = sock.socket(sock.AF_UNIX, sock.SOCK_STREAM)
            s.connect(socket_path)
            conn.sock = s

        conn.request("POST", path, body=payload, headers=headers)
        resp = conn.getresponse()
        resp_body = resp.read().decode()
        conn.close()
        return resp.status, resp_body
    except Exception as e:
        log.debug("unix_post %s failed: %s", path, e)
        return 0, str(e)


def unix_get(socket_path: str, path: str) -> tuple[int, str]:
    """GET from the daemon over its Unix socket or TCP address."""
    try:
        if _is_http_url(socket_path):
            parsed = urlparse(socket_path)
            conn = http.client.HTTPConnection(parsed.hostname, parsed.port or 80)
        else:
            conn = http.client.HTTPConnection("localhost")
            s = sock.socket(sock.AF_UNIX, sock.SOCK_STREAM)
            s.connect(socket_path)
            conn.sock = s

        conn.request("GET", path)
        resp = conn.getresponse()
        resp_body = resp.read().decode()
        conn.close()
        return resp.status, resp_body
    except Exception as e:
        log.debug("unix_get %s failed: %s", path, e)
        return 0, str(e)
```

- [ ] **Step 2: Run existing tests (if any)**

```bash
cd /Users/donovanyohan/Documents/Programs/personal/belayer
python3 -m pytest hermes_bridge/ -v 2>&1 | tail -20 || echo "no tests found (ok)"
```

- [ ] **Step 3: Quick manual smoke test**

```bash
python3 -c "
from hermes_bridge.http_client import _is_http_url
assert _is_http_url('http://172.17.0.1:7523') == True
assert _is_http_url('/tmp/daemon.sock') == False
assert _is_http_url('http://localhost:7523') == True
print('smoke test passed')
"
```

Expected: `smoke test passed`

- [ ] **Step 4: Commit**

```bash
git add hermes_bridge/http_client.py
git commit -m "feat(bridge): support HTTP URL in BELAYER_SOCKET for clamshell container daemon access"
```

---

## Task 10: Update Daemon to Pass TCP Socket Path in Clamshell Mode

**Files:**
- `internal/daemon/agents.go` (line ~264, `bridgeLaunchAgent`)
- `internal/daemon/daemon.go`

When the sandbox driver for a session is clamshell, bridge agents run inside Docker containers. They need `BELAYER_SOCKET` to point to the TCP address rather than the Unix socket path.

The Docker host gateway from inside a Docker container is typically `172.17.0.1` on Linux. This is configurable via a new daemon config field.

- [ ] **Step 1: Add DockerHostGateway field to daemon.Config**

In `internal/daemon/daemon.go`, add to Config:

```go
// DockerHostGateway is the IP address of the Docker host as seen from inside
// Docker containers. Used to construct BELAYER_SOCKET for clamshell-mode bridge
// subprocesses. Defaults to "172.17.0.1" (standard Docker bridge network).
DockerHostGateway string
```

And in `DefaultConfig()`:
```go
DockerHostGateway: "172.17.0.1",
```

- [ ] **Step 2: Write a test for clamshell socket path selection**

In `internal/daemon/agents_test.go` (or create), add a test that when the session driver is clamshell, the bridge config SocketPath is the TCP URL:

```go
func TestBridgeCfgSocketPathForClamshell(t *testing.T) {
    // This is a unit test for the socket path selection logic.
    // bridgeLaunchAgent is unexported; test via the exported helper that
    // constructs the bridge Config. If no such helper exists, add one.
    //
    // For now, this verifies the logic by inspecting logs or using a thin
    // exported function bridgeSocketPath(mode, unixPath, gateway, port).
    
    got := bridgeSocketPath("clamshell", "/tmp/daemon.sock", "172.17.0.1", 7523)
    want := "http://172.17.0.1:7523"
    if got != want {
        t.Fatalf("want %q, got %q", want, got)
    }

    got = bridgeSocketPath("noop", "/tmp/daemon.sock", "172.17.0.1", 7523)
    if got != "/tmp/daemon.sock" {
        t.Fatalf("noop mode should use unix socket, got %q", got)
    }
}
```

Run: `go test ./internal/daemon/... -run TestBridgeCfgSocketPathForClamshell -v`
Expected: FAIL (function doesn't exist yet)

- [ ] **Step 3: Add bridgeSocketPath helper and wire it into bridgeLaunchAgent**

In `internal/daemon/agents.go`, add helper (unexported is fine, make exported for testability):

```go
// bridgeSocketPath returns the socket path/URL to inject into BELAYER_SOCKET
// for a bridge subprocess. For clamshell sandboxes the bridge runs inside a
// Docker container and must reach the daemon via the Docker host gateway over
// TCP. For all other drivers the Unix socket path is used directly.
func bridgeSocketPath(driverName, unixPath, dockerGateway string, tcpPort int) string {
    if driverName == "clamshell" && tcpPort > 0 {
        return fmt.Sprintf("http://%s:%d", dockerGateway, tcpPort)
    }
    return unixPath
}
```

Then in `bridgeLaunchAgent()`, find where `cfg.SocketPath` is set (it's currently `d.config.SocketPath`). Replace with:

```go
socketPath := bridgeSocketPath(
    driverName,              // from resolved sandbox driver
    d.config.SocketPath,
    d.config.DockerHostGateway,
    d.tcpPort,
)
cfg := bridge.Config{
    // ...
    SocketPath: socketPath,
    // ...
}
```

You'll need to pass `driverName` into `bridgeLaunchAgent`. Check the existing function signature in `agents.go:264` and adjust.

- [ ] **Step 4: Add --docker-gateway flag to daemon CLI**

In `internal/cli/daemon.go`:
```go
var dockerGateway string
// in RunE:
if dockerGateway != "" {
    cfg.DockerHostGateway = dockerGateway
}
// in cmd.Flags():
cmd.Flags().StringVar(&dockerGateway, "docker-gateway", "", "Docker host gateway IP for clamshell bridge access (default 172.17.0.1)")
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/daemon/... -v 2>&1 | tail -30
```

Expected: all PASS including new test

- [ ] **Step 6: Commit**

```bash
git add internal/daemon/daemon.go internal/daemon/agents.go internal/cli/daemon.go
git commit -m "feat(daemon): route bridge BELAYER_SOCKET to TCP in clamshell mode"
```

---

## Task 11: Rebuild and Redeploy belayer to Colima

After Tasks 8-10, we have Go code changes. Rebuild and redeploy.

- [ ] **Step 1: Build tests pass locally**

```bash
go test ./... 2>&1 | tail -20
```

Expected: all PASS

- [ ] **Step 2: Cross-compile new binary**

```bash
GOOS=linux GOARCH=arm64 go build -tags clamshell -o /tmp/belayer-linux-arm64 ./cmd/belayer && echo "Build OK"
```

- [ ] **Step 3: Copy to Colima and verify**

```bash
colima cp /tmp/belayer-linux-arm64 colima:~/.local/bin/belayer
colima ssh -- chmod +x ~/.local/bin/belayer
colima ssh -- bash -lc "belayer daemon --help | grep -E 'tcp-addr|docker-gateway'"
```

Expected: both flags present in help output

---

## Task 12: Verify Noop Mode Run (Sanity Check)

Before attempting clamshell, verify belayer works in the Colima VM in noop mode. This catches environment issues before clamshell complexity.

- [ ] **Step 1: Temporarily set sandbox mode to noop**

```bash
colima ssh -- bash -lc "sed -i 's/mode: clamshell/mode: noop/' ~/arielcharts/.belayer/config.yaml && grep mode ~/arielcharts/.belayer/config.yaml"
```

- [ ] **Step 2: Start belayer daemon in Colima (background)**

```bash
colima ssh -- bash -lc "cd ~/arielcharts && belayer daemon --workdir ~/arielcharts > /tmp/daemon.log 2>&1 &
sleep 2 && tail -5 /tmp/daemon.log"
```

Expected: `belayer daemon listening on ~/.belayer/daemon.sock`, `belayer runtime: command (loaded from ...)`

- [ ] **Step 3: Start a belayer run with a simple task**

```bash
colima ssh -- bash -lc "cd ~/arielcharts && belayer run start --task 'Write a one-line comment at the top of README.md' 2>&1"
```

Watch the output. The supervisor should spawn and the agents should communicate.

- [ ] **Step 4: Watch logs**

```bash
colima ssh -- bash -lc "belayer logs 2>&1 | head -50"
```

Expected: session created, supervisor spawned, agents communicating

- [ ] **Step 5: Stop daemon**

```bash
colima ssh -- bash -lc "pkill -f 'belayer daemon' || true"
```

- [ ] **Step 6: Restore clamshell mode**

```bash
colima ssh -- bash -lc "sed -i 's/mode: noop/mode: clamshell/' ~/arielcharts/.belayer/config.yaml"
```

---

## Task 13: First Clamshell Run — Iterate Until Complete

This is the core iteration loop. Run with clamshell, watch errors, fix policy/config, repeat.

**Files:**
- Colima VM: `~/arielcharts/.belayer/policies/standard.yaml`
- Colima VM: logs at `~/.extend-clamshell/runtime/*/host/events/`

- [ ] **Step 1: Start clamshell gateway**

```bash
colima ssh -- bash -lc "clamshell gateway start 2>&1 && sleep 1 && clamshell gateway status"
```

- [ ] **Step 2: Start belayer daemon with TCP listener**

```bash
colima ssh -- bash -lc "cd ~/arielcharts && belayer daemon --workdir ~/arielcharts --tcp-addr 0.0.0.0:7523 > /tmp/daemon.log 2>&1 &
sleep 2 && tail -10 /tmp/daemon.log"
```

Expected: `daemon listening on ~/.belayer/daemon.sock`, `TCP listener on 0.0.0.0:7523`

- [ ] **Step 3: Start the arielcharts run with the bugfix spec**

```bash
colima ssh -- bash -lc "cd ~/arielcharts && belayer run start --task 'Bugfix: persist user pan/zoom/view of the chart when the chart is re-rendered. The chart should remember the viewport state (pan position, zoom level) across re-renders so the user does not lose their view.' 2>&1 &"
```

- [ ] **Step 4: Tail deny events (in a separate terminal)**

```bash
colima ssh -- bash -lc "find ~/.extend-clamshell/runtime -name 'deny-events.json' -exec tail -f {} + 2>/dev/null || echo 'no deny events yet'"
```

Watch for blocked network requests. Each blocked host needs to be added to the policy.

- [ ] **Step 5: Iteration — fix policy for each deny event**

For each blocked host in deny events, add to `custom_endpoints` in `~/arielcharts/.belayer/policies/standard.yaml`:
```yaml
- host: <blocked-host>
  ports: [443]
  protocol: rest
  note: <why it's needed>
```

After updating policy: stop daemon, stop existing session, restart gateway, restart daemon, retry run.

Common expected blocks:
- `opencode.ai` — already in policy
- `api.moonshot.cn` — already in policy  
- `registry.npmjs.org` — enabled via `npm: true` provider
- `pypi.org` — enabled via `pypi: true` provider
- `github.com` / `api.github.com` — enabled via `github: true` provider

If new hosts appear (e.g., a CDN, a dependency), add them and document in the Surprises section.

- [ ] **Step 6: Verify bridge→daemon communication works**

Check daemon logs for bridge events:
```bash
colima ssh -- bash -lc "tail -f /tmp/daemon.log"
```

Expected: `bridge:started`, `bridge:turn_usage` events flowing in

If bridge can't reach daemon, check:
1. Is TCP listener bound? `ss -tlnp | grep 7523`
2. Is 172.17.0.1:7523 reachable from container? `docker run --rm belayer-sandbox:latest curl -s http://172.17.0.1:7523/health`
3. Is clamshell policy allowing port 7523 on 172.17.0.1?

- [ ] **Step 7: Verify pnpm dev is running and accessible from container**

```bash
colima ssh -- bash -lc "curl -s http://localhost:3000 | head -5"
```

If pnpm dev isn't started yet (the runtime provider should start it), check daemon logs for `runtime up` errors.

Test QA agent can reach it from inside a container:
```bash
colima ssh -- docker run --rm belayer-sandbox:latest curl -s http://172.17.0.1:3000 | head -5
```

Expected: HTML response from Next.js

- [ ] **Step 8: Continue until a full session completes**

Watch `belayer status` for the session to reach `completed` or `failed` state.

```bash
colima ssh -- bash -lc "watch -n 5 'belayer status 2>&1 | tail -20'"
```

Expected eventually: session status `completed`

---

## Task 14: Verify Telemetry and Add Network Deny Event Logging

**Goal:** Ensure rich agent telemetry is captured even through clamshell, and that deny events are persisted as session artifacts.

- [ ] **Step 1: Verify agent events flow through**

```bash
colima ssh -- bash -lc "belayer logs 2>&1 | grep -E 'bridge:|agent_status:' | head -30"
```

Expected: `bridge:started`, `bridge:turn_usage`, `bridge:idle`/`bridge:finished` events

- [ ] **Step 2: Check if deny events are readable from belayer**

Deny events are at `~/.extend-clamshell/runtime/{sandbox-name}/host/events/deny-events.json`:
```bash
colima ssh -- bash -lc "find ~/.extend-clamshell -name 'deny-events.json' -exec wc -l {} +"
```

- [ ] **Step 3: Add deny event logging to belayer daemon**

In `internal/daemon/daemon.go` (or a new `internal/daemon/sandbox_monitor.go`), add a goroutine that watches for clamshell sandbox deny events and logs them as session events.

The deny events file path pattern is: `~/.extend-clamshell/runtime/{sandbox-name}/host/events/deny-events.json`

Where `sandbox-name` is the session ID (same as `cfg.Name` in `sandbox.Config`).

Add this function:

```go
// watchDenyEvents tails the clamshell deny events file for a session and
// logs each block as a "sandbox:deny" event in the session event stream.
// Returns when ctx is done.
func (d *Daemon) watchDenyEvents(ctx context.Context, sessionID, sandboxName string) {
    home, _ := os.UserHomeDir()
    denyPath := filepath.Join(home, ".extend-clamshell", "runtime", sandboxName, "host", "events", "deny-events.json")
    
    // Poll every 2s — deny events are low-volume
    ticker := time.NewTicker(2 * time.Second)
    defer ticker.Stop()
    
    var offset int64
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            f, err := os.Open(denyPath)
            if err != nil {
                continue // file doesn't exist yet
            }
            f.Seek(offset, io.SeekStart)
            scanner := bufio.NewScanner(f)
            for scanner.Scan() {
                line := scanner.Text()
                if line == "" { continue }
                var event map[string]any
                if err := json.Unmarshal([]byte(line), &event); err == nil {
                    d.store.AppendEvent(sessionID, "sandbox:deny", event)
                    log.Printf("session %s: sandbox deny: %v", sessionID, event)
                }
            }
            offset, _ = f.Seek(0, io.SeekCurrent)
            f.Close()
        }
    }
}
```

Call `go d.watchDenyEvents(ctx, sessionID, sandboxName)` after sandbox Create() in the session start flow.

- [ ] **Step 4: Verify deny events appear in belayer logs**

```bash
colima ssh -- bash -lc "belayer logs 2>&1 | grep 'sandbox:deny' | head -10"
```

Expected: if there are any deny events, they should appear here

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/
git commit -m "feat(daemon): log clamshell network deny events as session events"
```

---

## Task 15: Write Summary Document

**Files:**
- `docs/CLAMSHELL-POC-SUMMARY.md`

- [ ] **Step 1: Write the summary document**

Create `docs/CLAMSHELL-POC-SUMMARY.md` covering:

1. **Steps to get it working** — exact setup commands for a fresh Colima VM
2. **Code changes made** — list every file changed in belayer, arielcharts, extend-clamshell
3. **Network policy** — final list of required endpoints (from deny event log)
4. **Risks and pitfalls** — what was hard, what can break
5. **Next steps / improvements** — things to do before production use
6. **Architecture insights** — anything surprising about the runtime↔sandbox connection

Template:
```markdown
# Clamshell PoC: Steps to Production

## Summary

What was proven: [one paragraph]

## Setup Steps (Fresh Colima VM)

[Step by step commands from scratch]

## Code Changes Required

### belayer repo
- [file]: [what changed and why]

### extend-clamshell (Docker image)
- [file]: [what changed and why]

### arielcharts repo
- [file]: [what changed and why]

## Required Network Endpoints

| Host | Port | Purpose |
|------|------|---------|
| ... | 443 | ... |

## Risks and Pitfalls

- **Seccomp syscall gaps**: The default seccomp profile may be missing syscalls...
- **Docker bridge IP hardcoded**: 172.17.0.1 is Docker default but may differ...
- **hermes-agent from GitHub**: No pinned version...

## Next Steps for Production

1. ...
2. ...

## Architecture Notes

[anything surprising]
```

- [ ] **Step 2: Commit summary**

```bash
git add docs/CLAMSHELL-POC-SUMMARY.md
git commit -m "docs: clamshell PoC summary and production roadmap"
```

---

## Outcomes & Retrospective

_Filled when work is done._

**What worked:**
-

**What didn't:**
-

**Learnings to codify:**
-
