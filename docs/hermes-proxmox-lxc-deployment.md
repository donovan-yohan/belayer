# Hermes Agent on Proxmox LXC

Deploy Hermes Agent to a privileged Proxmox LXC container with persistent data via bind mounts, Tailscale SSH for keyless access, and network containment to prevent lateral movement.

## Prerequisites

- Proxmox host accessible via SSH
- Tailscale account with admin access
- Debian 12 LXC template cached in Proxmox (`local:vztmpl/debian-12-standard_*`)

## Architecture

The LXC is a **citadel** — it receives connections, it does not initiate them to other machines.

- Discord gateway: outbound HTTPS only (safe)
- Tailscale SSH: inbound from your devices only
- No OpenSSH daemon
- No outbound SSH keys
- Persistent data on host storage via bind mount

## Step 1: SSH Key Setup (one-time)

Generate an ed25519 key pair for Proxmox host access:

```bash
ssh-keygen -t ed25519 -C "hermes-proxmox-$(date +%Y%m%d)" -f ~/.ssh/hermes_proxmox_ed25519 -N ""
cat ~/.ssh/hermes_proxmox_ed25519.pub
```

Add the public key to Proxmox root's authorized_keys:
```bash
ssh root@proxmox-host "mkdir -p /root/.ssh && echo 'YOUR_PUBKEY' >> /root/.ssh/authorized_keys"
```

Configure SSH client:
```bash
cat >> ~/.ssh/config << 'EOF'
Host beanworld-pve
    HostName beanworld
    User root
    IdentityFile ~/.ssh/hermes_proxmox_ed25519
    StrictHostKeyChecking no
    UserKnownHostsFile /dev/null
EOF
```

## Step 2: Check Resources

```bash
ssh beanworld-pve "pveversion; pvesm status; ls /etc/pve/lxc/; ls /etc/pve/qemu-server/"
```

Note existing IDs to avoid collision. Check storage pools.

## Step 3: Create Persistent Data Directory

```bash
ssh beanworld-pve "mkdir -p /storage/hermes-data && chmod 700 /storage/hermes-data"
```

## Step 4: Create the LXC

```bash
ssh beanworld-pve "pct create 102 local:vztmpl/debian-12-standard_12.12-1_amd64.tar.zst \
  --hostname hermes-devbox \
  --rootfs local-lvm:32 \
  --cores 8 \
  --memory 6144 \
  --swap 2048 \
  --features nesting=1,keyctl=1 \
  --net0 name=eth0,bridge=vmbr0,ip=dhcp,type=veth \
  --mp0 /storage/hermes-data,mp=/root/.hermes,backup=1 \
  --start 1 --unprivileged 0"
```

**Why privileged + nesting?** Hermes spawns Docker containers. Unprivileged LXC + Docker is a cgroup nightmare.

## Step 5: Install Base Packages and Docker

```bash
ssh beanworld-pve "pct exec 102 -- bash -c '
export DEBIAN_FRONTEND=noninteractive
export LANG=C.UTF-8
apt-get update -qq
apt-get install -y -qq curl git ca-certificates build-essential libssl-dev pkg-config htop nano
curl -fsSL https://get.docker.com | sh
systemctl enable --now docker
'"
```

## Step 6: Install Hermes

```bash
ssh beanworld-pve "pct exec 102 -- bash -c '
cd /opt
git clone --recursive https://github.com/donovan-yohan/hermes-agent.git
cd hermes-agent
git checkout dy-main
bash setup-hermes.sh <<< \"n\"
'"
```

**Note:** The install script is `setup-hermes.sh`, not `install.sh`. It uses `uv` for Python environment management.

## Step 7: Add SSH Access Key

```bash
ssh beanworld-pve "pct exec 102 -- bash -c '
mkdir -p /root/.ssh && chmod 700 /root/.ssh
echo \"YOUR_PUBKEY\" > /root/.ssh/authorized_keys
chmod 600 /root/.ssh/authorized_keys
'"
```

## Step 8: Tailscale Setup (CRITICAL — requires TUN device)

**The gotcha:** Tailscale needs `/dev/net/tun` which LXC does not provide by default. Add these to the container config:

```bash
ssh beanworld-pve "cat >> /etc/pve/lxc/102.conf << 'EOF'
lxc.mount.entry: /dev/net/tun dev/net/tun none bind,create=file 0 0
lxc.cgroup2.devices.allow: c 10:200 rwm
EOF"
```

Restart the LXC to pick up the TUN mount:
```bash
ssh beanworld-pve "pct stop 102 && sleep 2 && pct start 102"
```

Install and auth Tailscale using an **auth key** (headless, survives reboots):
```bash
ssh beanworld-pve "pct exec 102 -- curl -fsSL https://tailscale.com/install.sh | sh"
ssh beanworld-pve "pct exec 102 -- tailscale up --ssh --accept-routes=false --advertise-routes= --authkey=tskey-auth-..."
```

Generate the auth key at https://login.tailscale.com/admin/settings/keys with:
- Reusable: Yes
- Ephemeral: No
- Tags: `tag:hermes-lxc`

## Step 9: Disable OpenSSH (security hardening)

Once Tailscale SSH is confirmed working, remove OpenSSH:
```bash
ssh beanworld-pve "pct exec 102 -- systemctl disable --now ssh ssh.socket"
```

Verify no port 22 listeners:
```bash
ssh beanworld-pve "pct exec 102 -- ss -tlnp | grep ':22' || echo 'Port 22 closed'"
```

## Step 10: Remove Outbound SSH Keys

The LXC should never have private keys to other machines:
```bash
ssh beanworld-pve "pct exec 102 -- rm -f /root/.ssh/id_* /root/.ssh/hermes_*"
ssh beanworld-pve "pct exec 102 -- find /root/.ssh -type f -name 'id_*' | wc -l"  # should be 0
```

## Step 11: Tailscale ACL Lockdown

Paste this into your Tailscale ACL (https://login.tailscale.com/admin/acls):

```json
{
  "grants": [
    {"src": ["autogroup:owner", "group:household"], "dst": ["*"], "ip": ["*"]},
    {"src": ["tag:hermes-lxc"], "dst": ["tag:hermes-lxc"], "ip": ["*"]}
  ],
  "ssh": [
    {"action": "check", "src": ["autogroup:member"], "dst": ["autogroup:self"], "users": ["autogroup:nonroot", "root"]},
    {"action": "check", "src": ["autogroup:owner", "group:household"], "dst": ["tag:hermes-lxc"], "users": ["root"]}
  ],
  "tagOwners": {
    "tag:hermes-lxc": ["autogroup:admin"]
  },
  "nodeAttrs": [
    {"target": ["tag:hermes-lxc"], "attr": ["funnel"]}
  ]
}
```

**What this does:**
- LXC can only reach itself on the tailnet (required for Tailscale internals)
- LXC **cannot** SSH to or reach any other tailnet node
- Only household members + admins can SSH **to** the LXC

## Step 12: Disaster Recovery Rebuild Script

Save this to `/storage/hermes-rebuild.sh` on the Proxmox host:

```bash
#!/bin/bash
set -e
LXC_ID=102
LXC_HOSTNAME=hermes-devbox
TEMPLATE="local:vztmpl/debian-12-standard_12.12-1_amd64.tar.zst"

echo "=== Hermes Devbox Rebuild ==="
read -p "Continue? [y/N] " -n 1 -r
echo
[[ ! $REPLY =~ ^[Yy]$ ]] && exit 1

if [ ! -d "/storage/hermes-data" ]; then
    echo "ERROR: /storage/hermes-data not found"
    exit 1
fi

if pct status $LXC_ID &>/dev/null; then
    pct stop $LXC_ID 2>/dev/null || true
    pct destroy $LXC_ID
fi

pct create $LXC_ID $TEMPLATE \
    --hostname $LXC_HOSTNAME --rootfs local-lvm:32 --cores 8 --memory 6144 --swap 2048 \
    --features nesting=1,keyctl=1 \
    --net0 name=eth0,bridge=vmbr0,ip=dhcp,type=veth \
    --mp0 /storage/hermes-data,mp=/root/.hermes,backup=1 \
    --unprivileged 0 --start 1

sleep 5

cat >> /etc/pve/lxc/${LXC_ID}.conf << 'EOF'
lxc.mount.entry: /dev/net/tun dev/net/tun none bind,create=file 0 0
lxc.cgroup2.devices.allow: c 10:200 rwm
EOF

pct stop $LXC_ID && sleep 2 && pct start $LXC_ID && sleep 5

pct exec $LXC_ID -- bash -c '
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -qq
    apt-get install -y -qq curl git ca-certificates build-essential libssl-dev pkg-config
    curl -fsSL https://get.docker.com | sh
    systemctl enable --now docker
    curl -fsSL https://tailscale.com/install.sh | sh
    systemctl enable tailscaled
'

pct exec $LXC_ID -- bash -c '
    cd /opt && git clone --recursive https://github.com/donovan-yohan/hermes-agent.git
    cd /opt/hermes-agent && git checkout dy-main
    bash setup-hermes.sh <<< "n"
'

pct exec $LXC_ID -- bash -c '
    mkdir -p /root/.ssh && chmod 700 /root/.ssh
    echo "YOUR_PUBKEY_HERE" > /root/.ssh/authorized_keys
    chmod 600 /root/.ssh/authorized_keys
'

echo "=== Rebuild Complete ==="
echo "IP: $(pct exec $LXC_ID -- hostname -I | awk '{print $1}')"
echo "Auth Tailscale, then run: hermes setup && hermes gateway install"
```

## Key Design Decisions

| Decision | Rationale |
|----------|-----------|
| **Privileged LXC** | Docker requires it. Unprivileged + Docker is a cgroup nightmare. |
| **Bind mount `/storage/hermes-data` → `/root/.hermes`** | Persistent state survives container destruction. |
| **Tailscale SSH instead of OpenSSH** | Keyless, ACL-enforced, audit-logged, no port forwarding. |
| **Auth key (not interactive)** | Headless server needs unattended auth. |
| **No outbound SSH keys on LXC** | Prevents lateral movement if the agent is compromised. |
| **Disable OpenSSH daemon** | Reduces attack surface once Tailscale SSH is confirmed working. |

## Troubleshooting

### TUN device not available
**Symptom:** `tailscaled` fails with `tstun.New("tailscale0"): operation not permitted`
**Fix:** Ensure both `lxc.mount.entry` and `lxc.cgroup2.devices.allow` are in the LXC config, then restart.

### Docker repo fails during install
**Symptom:** `E: The repository '...' does not have a Release file`
**Fix:** A broken `/etc/apt/sources.list.d/docker.list` was created by an earlier failed command. Remove it and use `curl -fsSL https://get.docker.com | sh` instead.

### Hermes install script 404
**Symptom:** `curl: (22) The requested URL returned error: 404`
**Fix:** The upstream `install.sh` doesn't exist in the fork. Use `setup-hermes.sh` instead.

### Tailscale auth times out
**Symptom:** `tailscale up` hangs waiting for browser auth
**Fix:** Use `--authkey` with a pre-generated key. Interactive auth requires a TTY/browser which headless LXCs don't have.
